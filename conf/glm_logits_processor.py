from dataclasses import dataclass
from enum import Enum
from typing import TYPE_CHECKING

import numpy as np
import torch

from vllm.logger import init_logger
from vllm.utils.torch_utils import async_tensor_h2d
from vllm.v1.worker.gpu.sample.logits_processor import (
    LogitsContext,
    LogitsProcessor,
    LogitsProcRequestState,
)

if TYPE_CHECKING:
    from vllm.config import VllmConfig

# Prefix "vllm." so the logger inherits vLLM's INFO handler (see language_penalty.py).
logger = init_logger("vllm." + __name__)

THINK_START = "<think>"
THINK_END = "</think>"
TOOL_CALL_START = "<tool_call>"
# Tokens that end the assistant turn (generation_config eos_token_id).
END_OF_TURN = ("<|endoftext|>", "<|user|>", "<|observation|>")
# Tokens that start a turn in a rendered prompt; the prompt scan stops at them.
TURN_BOUNDARIES = ("<|system|>", "<|user|>", "<|assistant|>", "<|observation|>")


class _Phase(Enum):
    THINK = 1  # inside the think block
    ANSWER_START = 2  # after </think>, no answer token yet
    FREE = 3  # answer or tool call started, or no think block in this turn


@dataclass(frozen=True)
class _Markers:
    think_start: int
    think_end: int
    tool_call_start: int
    end_of_turn: tuple[int, ...]
    turn_boundaries: tuple[int, ...]


def _resolve_markers(tokenizer) -> _Markers:
    vocab = tokenizer.get_vocab()

    def token_id(marker: str) -> int:
        tid = vocab.get(marker)
        if tid is None:
            raise ValueError(f"glm logits processor: {marker!r} is not a single token")
        return tid

    markers = _Markers(
        think_start=token_id(THINK_START),
        think_end=token_id(THINK_END),
        tool_call_start=token_id(TOOL_CALL_START),
        end_of_turn=tuple(token_id(m) for m in END_OF_TURN),
        turn_boundaries=tuple(token_id(m) for m in TURN_BOUNDARIES),
    )
    if markers.think_end in markers.end_of_turn:
        raise ValueError("glm logits processor: </think> is an end-of-turn token")
    return markers


_THINK = _Phase.THINK.value
_ANSWER_START = _Phase.ANSWER_START.value
_FREE = _Phase.FREE.value


class GlmLogitsProcessor(LogitsProcessor):
    """Prevents empty GLM answers: no turn end before the answer."""

    def __init__(
        self, vllm_config: "VllmConfig", req_states: LogitsProcRequestState
    ) -> None:
        if vllm_config.speculative_config is not None:
            raise ValueError(
                f"{type(self).__name__} does not support speculative decoding."
            )
        model_config = vllm_config.model_config
        if model_config is None:
            raise ValueError("glm logits processor requires a model_config")
        from vllm.tokenizers import cached_tokenizer_from_config

        tokenizer = cached_tokenizer_from_config(model_config)
        # Raises at engine start if a marker is missing: only GLM models load this processor.
        self._markers = m = _resolve_markers(tokenizer)
        logger.info("GLM logits processor active, end of turn %s.", list(m.end_of_turn))
        self.req_states = req_states
        self.device = device = req_states.device
        self._eot_idx = torch.tensor(m.end_of_turn, dtype=torch.long, device=device)
        self._boundary_idx = torch.tensor(
            m.turn_boundaries, dtype=torch.long, device=device
        )
        # Ids past the tokenizer's vocabulary (vocab padding) are not whitespace.
        num_tokens = min(max(tokenizer.get_vocab().values()) + 1, req_states.vocab_size)
        is_whitespace = torch.zeros(req_states.vocab_size, dtype=torch.bool)
        is_whitespace[:num_tokens] = torch.tensor(
            [_decodes_to_whitespace(tokenizer, t) for t in range(num_tokens)],
            dtype=torch.bool,
        )
        self._is_whitespace = is_whitespace.to(device)
        self._phase = torch.full(
            (req_states.max_num_reqs,), _FREE, dtype=torch.int32, device=device
        )
        # Per slot: tokens already folded into _phase.
        self._scanned = np.zeros(req_states.max_num_reqs, dtype=np.int64)
        self._pending: dict[int, None] = {}

    def add_request(self, req_idx: int, sampling_params) -> bool:
        del sampling_params
        # The slot's tokens are on the device only by apply_staged_writes().
        self._pending[req_idx] = None
        return True

    def apply_staged_writes(self) -> None:
        if not self._pending:
            return
        slots = np.fromiter(self._pending, dtype=np.int64)
        self._pending.clear()
        prompt_lens = self.req_states.prompt_len.np[slots].astype(np.int64)
        prefill_lens = self.req_states.prefill_len.np[slots].astype(np.int64)
        slots_t = self._h2d(slots)
        # One slot at a time: padding all slots to the longest prefill (up to the
        # context window) would take GBs with many slots added in one step.
        for i, (prompt_len, prefill_len) in enumerate(
            zip(prompt_lens.tolist(), prefill_lens.tolist())
        ):
            slot_t = slots_t[i : i + 1]
            if prefill_len == 0:
                self._phase[slot_t] = _FREE
                continue
            pos = torch.arange(prefill_len, device=self.device)[None, :]
            tokens = self._tokens(slot_t, pos)
            prompt = pos < prompt_len
            phase = self._prompt_phase(tokens, pos, prompt)
            self._phase[slot_t] = self._advance(phase, tokens, ~prompt)
        self._scanned[slots] = prefill_lens

    def apply(self, logits: torch.Tensor, ctx: LogitsContext) -> torch.Tensor:
        slots = ctx.idx_mapping_np
        if logits.shape[0] != slots.shape[0]:
            raise RuntimeError(f"{type(self).__name__} received draft-expanded logits")
        # Padded to the CUDA graph's request count under FULL graphs.
        seq_lens = ctx.seq_lens_upper_bound_np[: slots.shape[0]].astype(np.int64)
        # Rows of a non-final prefill chunk sample a token that is discarded.
        rows = np.flatnonzero(seq_lens >= self.req_states.prefill_len.np[slots])
        if rows.size == 0:
            return logits
        row_slots = slots[rows].astype(np.int64)
        starts = self._scanned[row_slots]
        ends = seq_lens[rows]
        rows_t = self._h2d(rows)
        slots_t = self._h2d(row_slots)

        phase = self._phase[slots_t]
        width = int(np.maximum(ends - starts, 0).max())
        if width:
            pos = self._h2d(starts)[:, None] + torch.arange(width, device=self.device)
            new = pos < self._h2d(ends)[:, None]
            tokens = self._tokens(slots_t, pos)
            phase = self._advance(phase, tokens, new)
            self._phase[slots_t] = phase
            self._scanned[row_slots] = np.maximum(starts, ends)

        # Neither rule overrides an earlier mask (grammar, client ban): a token the grammar
        # rejects ends the request.
        m = self._markers
        eot = self._eot_idx
        eot_cols = (rows_t[:, None], eot[None, :])
        eot_logits = logits[eot_cols]
        think_end = logits[rows_t, m.think_end]
        think = (phase == _THINK) & torch.isfinite(think_end)
        # Answer start: ban the turn end only if another token stays allowed.
        logits[eot_cols] = torch.where(
            (phase == _ANSWER_START)[:, None], float("-inf"), eot_logits
        )
        answer = (phase == _ANSWER_START) & torch.isfinite(logits.amax(dim=1)[rows_t])
        logits[eot_cols] = torch.where(
            (think | answer)[:, None], float("-inf"), eot_logits
        )
        # Where the model would end its turn, close the think block instead. max, not
        # logaddexp: no stop pressure beyond the model's own.
        logits[rows_t, m.think_end] = torch.where(
            think, torch.maximum(think_end, eot_logits.max(dim=1).values), think_end
        )
        return logits

    def _h2d(self, data: np.ndarray) -> torch.Tensor:
        return async_tensor_h2d(data, self.device, torch.long)

    def _tokens(self, slots_t: torch.Tensor, pos: torch.Tensor) -> torch.Tensor:
        """Committed tokens at pos per slot; out-of-range positions hold junk
        the callers mask out."""
        all_token_ids = self.req_states.all_token_ids.gpu
        pos = pos.clamp(0, all_token_ids.shape[1] - 1)
        tokens = all_token_ids[slots_t[:, None], pos].long()
        return tokens.clamp(0, self._is_whitespace.shape[0] - 1)

    def _prompt_phase(
        self, tokens: torch.Tensor, pos: torch.Tensor, prompt: torch.Tensor
    ) -> torch.Tensor:
        """Phase at the end of the prompt: the last think marker after the
        last turn boundary decides."""
        m = self._markers
        boundary = torch.isin(tokens, self._boundary_idx) & prompt
        last_boundary = torch.where(boundary, pos, -1).amax(dim=1, keepdim=True)
        marker = (
            ((tokens == m.think_start) | (tokens == m.think_end))
            & prompt
            & (pos > last_boundary)
        )
        last_marker = torch.where(marker, pos, -1).amax(dim=1, keepdim=True)
        marker_tok = tokens.gather(1, last_marker.clamp(min=0)).squeeze(1)
        answered = (~self._is_whitespace[tokens] & prompt & (pos > last_marker)).any(
            dim=1
        )
        free = torch.full_like(marker_tok, _FREE, dtype=torch.int32)
        after_end = torch.where(answered, free, _ANSWER_START)
        phase = torch.where(marker_tok == m.think_start, _THINK, after_end)
        return torch.where(last_marker.squeeze(1) >= 0, phase, free)

    def _advance(
        self,
        phase: torch.Tensor,
        tokens: torch.Tensor,
        valid: torch.Tensor,
    ) -> torch.Tensor:
        """Phase after the valid tokens of each row, in column order."""
        m = self._markers
        cols = torch.arange(tokens.shape[1], device=self.device)[None, :]
        tool_call = (tokens == m.tool_call_start) & valid
        stop = ((tokens == m.think_end) & valid) | tool_call
        # THINK ends at the first </think> or <tool_call>.
        first = stop.int().argmax(dim=1, keepdim=True)
        non_ws = ~self._is_whitespace[tokens] & valid
        free = torch.full_like(phase, _FREE)
        # After </think>, any non-whitespace token frees the row.
        answered_after = (non_ws & (cols > first)).any(dim=1)
        from_think = torch.where(
            tool_call.gather(1, first).squeeze(1) | answered_after,
            free,
            _ANSWER_START,
        )
        from_think = torch.where(stop.any(dim=1), from_think, _THINK)
        from_answer = torch.where(non_ws.any(dim=1), free, _ANSWER_START)
        phase_next = torch.where(phase == _THINK, from_think, phase)
        return torch.where(phase == _ANSWER_START, from_answer, phase_next).to(
            phase.dtype
        )


def _decodes_to_whitespace(tokenizer, tok: int) -> bool:
    text = tokenizer.decode([tok], skip_special_tokens=False)
    return text != "" and not text.strip()
