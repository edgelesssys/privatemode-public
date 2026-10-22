import time
from dataclasses import dataclass
from enum import Enum
from typing import TYPE_CHECKING

import torch

from vllm.logger import init_logger
from vllm.utils.torch_utils import async_tensor_h2d
from vllm.v1.sample.logits_processor.interface import (
    BatchUpdate,
    LogitsProcessor,
    MoveDirectionality,
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
# Seconds between log lines with the probability mass each rule removed.
STATS_INTERVAL_S = 60.0
# The mass is measured on every STATS_EVERY-th step and scaled up: it costs a full softmax.
STATS_EVERY = 16
STATS = ("turn_end_in_think", "turn_end_before_answer")


class _Phase(Enum):
    THINK = 1  # inside the think block
    ANSWER_START = 2  # after </think>, no answer token yet
    FREE = 3  # answer or tool call started, or no think block in this turn


@dataclass
class _RequestState:
    output_tok_ids: list[int]  # live reference, kept current by the engine
    phase: _Phase
    consumed: int = 0


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


class GlmLogitsProcessor(LogitsProcessor):
    """Prevents empty GLM answers: no turn end before the answer."""

    def __init__(
        self,
        vllm_config: "VllmConfig",
        device: torch.device,
        is_pin_memory: bool,
    ) -> None:
        del is_pin_memory
        self.device = device
        self.model_config = vllm_config.model_config
        if self.model_config is None:
            raise ValueError("glm logits processor requires a model_config")
        from vllm.tokenizers import cached_tokenizer_from_config

        self._tokenizer = cached_tokenizer_from_config(self.model_config)
        # Raises at engine start if a marker is missing: only GLM models load this processor.
        self._markers = _resolve_markers(self._tokenizer)
        logger.info(
            "GLM logits processor active, end of turn %s.",
            list(self._markers.end_of_turn),
        )
        self._eot_idx = torch.tensor(
            self._markers.end_of_turn, dtype=torch.long, device=device
        )
        self._whitespace: dict[int, bool] = {}
        self._row_cache: dict[str, tuple[list[int], torch.Tensor]] = {}
        self._states: dict[int, _RequestState] = {}
        self._removed_mass = torch.zeros(len(STATS), dtype=torch.float32, device=device)
        self._stats_since = time.monotonic()
        self._step = 0

    def _is_whitespace(self, tok: int) -> bool:
        cached = self._whitespace.get(tok)
        if cached is None:
            text = self._tokenizer.decode([tok], skip_special_tokens=False)
            cached = self._whitespace[tok] = text != "" and not text.strip()
        return cached

    def _initial_phase(self, prompt_tok_ids: list[int] | None) -> _Phase:
        """Phase at the end of the prompt."""
        if not prompt_tok_ids:
            return _Phase.FREE
        m = self._markers
        for i in range(len(prompt_tok_ids) - 1, -1, -1):
            tok = prompt_tok_ids[i]
            if tok == m.think_start:
                return _Phase.THINK
            if tok == m.think_end:
                tail = prompt_tok_ids[i + 1 :]
                if all(self._is_whitespace(t) for t in tail):
                    return _Phase.ANSWER_START
                return _Phase.FREE
            if tok in m.turn_boundaries:
                break
        return _Phase.FREE

    def update_state(self, batch_update: BatchUpdate | None) -> None:
        if batch_update is not None:
            for index in batch_update.removed:
                self._states.pop(index, None)

            for index, _params, prompt_tok_ids, output_tok_ids in batch_update.added:
                self._states[index] = _RequestState(
                    output_tok_ids=output_tok_ids,
                    phase=self._initial_phase(prompt_tok_ids),
                )

            for i1, i2, direction in batch_update.moved:
                if direction == MoveDirectionality.SWAP:
                    s1 = self._states.pop(i1, None)
                    s2 = self._states.pop(i2, None)
                    if s2 is not None:
                        self._states[i1] = s2
                    if s1 is not None:
                        self._states[i2] = s1
                else:
                    s1 = self._states.pop(i1, None)
                    if s1 is not None:
                        self._states[i2] = s1
                    else:
                        self._states.pop(i2, None)

        self._scan_states()

    def _scan_states(self) -> None:
        # Stops at a -1 async-scheduling placeholder; apply() rescans once the
        # engine has filled it in, so a phase change takes effect in the same step.
        m = self._markers
        for state in self._states.values():
            toks = state.output_tok_ids
            i = state.consumed
            while i < len(toks) and state.phase != _Phase.FREE:
                tok = toks[i]
                if tok < 0:
                    break
                if state.phase == _Phase.THINK:
                    if tok == m.think_end:
                        state.phase = _Phase.ANSWER_START
                    elif tok == m.tool_call_start:
                        state.phase = _Phase.FREE
                elif not self._is_whitespace(tok):
                    state.phase = _Phase.FREE
                i += 1
            state.consumed = i

    def apply(self, logits: torch.Tensor) -> torch.Tensor:
        self._scan_states()
        m = self._markers
        if not self._states:
            return logits

        think_rows: list[int] = []
        answer_rows: list[int] = []
        for index, state in sorted(self._states.items()):
            if state.phase == _Phase.THINK:
                think_rows.append(index)
            elif state.phase == _Phase.ANSWER_START:
                answer_rows.append(index)

        if not (think_rows or answer_rows):
            self._log_stats()
            return logits
        eot = self._eot_idx
        count = self._step % STATS_EVERY == 0
        self._step += 1
        if think_rows:
            rows = self._rows("think", think_rows)
            if count:
                self._count(0, logits, rows, eot)
            # Where the model would end its turn, close the think block instead. max, not
            # logaddexp: no stop pressure beyond the model's own.
            eot_max = logits[rows.unsqueeze(1), eot].max(dim=1).values
            logits[rows, m.think_end] = torch.maximum(
                logits[rows, m.think_end], eot_max
            )
            logits[rows.unsqueeze(1), eot] = float("-inf")
        if answer_rows:
            rows = self._rows("answer", answer_rows)
            if count:
                self._count(1, logits, rows, eot)
            logits[rows.unsqueeze(1), eot] = float("-inf")
        self._log_stats()
        return logits

    def _count(
        self,
        stat: int,
        logits: torch.Tensor,
        rows: torch.Tensor,
        toks: torch.Tensor,
    ) -> None:
        """Add the probability (before temperature and top-p) of the tokens a rule removes, scaled.

        Called before the rule changes its rows; the rule groups are disjoint.
        """
        removed = torch.logsumexp(logits[rows.unsqueeze(1), toks], dim=1)
        mass = torch.exp(removed - torch.logsumexp(logits[rows], dim=1))
        self._removed_mass[stat] += torch.nan_to_num(mass).sum() * STATS_EVERY

    def _log_stats(self) -> None:
        now = time.monotonic()
        if now - self._stats_since < STATS_INTERVAL_S:
            return
        self._stats_since = now
        mass = self._removed_mass.tolist()
        self._removed_mass.zero_()
        if any(mass):
            logger.info(
                "GLM logits processor, expected interventions in the last %.0f s: %s",
                STATS_INTERVAL_S,
                ", ".join(f"{k}={v:.4f}" for k, v in zip(STATS, mass)),
            )

    def _rows(self, key: str, rows: list[int]) -> torch.Tensor:
        """Row indices as a device tensor, rebuilt only when the set changes."""
        cached = self._row_cache.get(key)
        if cached is None or cached[0] != rows:
            cached = (
                rows,
                async_tensor_h2d(rows, device=self.device, dtype=torch.long),
            )
            self._row_cache[key] = cached
        return cached[1]

    def is_argmax_invariant(self) -> bool:
        return False
