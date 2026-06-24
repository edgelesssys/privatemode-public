# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the vLLM project

from dataclasses import dataclass
from typing import TYPE_CHECKING

import torch

from vllm.v1.sample.logits_processor.interface import (
    BatchUpdate,
    LogitsProcessor,
    MoveDirectionality,
)

if TYPE_CHECKING:
    from vllm.config import VllmConfig

THINK_START = "<think>"
THINK_END = "</think>"
IM_END = "<|im_end|>"
TOOL_SECTION_BEGIN = "<|tool_calls_section_begin|>"


@dataclass
class _RequestState:
    output_tok_ids: list[int]  # live reference, kept current by the engine
    consumed: int = 0
    in_think: bool = False


class KimiLogitsProcessor(LogitsProcessor):
    """Think-exit guard for kimi-k2.6."""

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
            raise ValueError("kimi logits processor requires a model_config")
        # Marker token ids, resolved lazily on first request. Resolution aborts
        # (raises) if a marker can't be resolved — this processor is only loaded
        # for kimi, where it must work; we want a loud failure, not a silent no-op.
        self._marker_ids: dict[str, int] | None = None
        self._states: dict[int, _RequestState] = {}
        # Guarded batch indices (sorted) and the same as a device tensor;
        # rebuilt only when the set changes.
        self._guarded: list[int] = []
        self._guarded_idx: torch.Tensor | None = None

    def _resolve_marker_ids(self) -> dict[str, int]:
        if self._marker_ids is not None:
            return self._marker_ids
        from vllm.tokenizers import cached_tokenizer_from_config

        tokenizer = cached_tokenizer_from_config(self.model_config)
        vocab = tokenizer.get_vocab()
        ids: dict[str, int] = {}
        for marker in (THINK_START, THINK_END, IM_END, TOOL_SECTION_BEGIN):
            # Vocab lookup (as the kimi_k2 parser does); single-token encode() as
            # fallback for tokenizers that split special-token strings.
            token_id = vocab.get(marker)
            if token_id is None:
                encoded = tokenizer.encode(marker, add_special_tokens=False)
                if len(encoded) == 1:
                    token_id = encoded[0]
            if token_id is None:
                raise ValueError(
                    f"kimi logits processor: marker {marker!r} did not resolve to "
                    "a single token id (wrong tokenizer / not a kimi model?)"
                )
            ids[marker] = token_id
        if len(set(ids.values())) != len(ids):
            # Distinct ids are an invariant: apply() promotes `</think>` then
            # bans `<|im_end|>`, so a collision (e.g. think_end == im_end) would
            # ban the very token it just promoted.
            raise ValueError(
                f"kimi logits processor: marker ids are not distinct: {ids}"
            )
        self._marker_ids = ids
        return ids

    def _initial_in_think(self, prompt_tok_ids: list[int] | None) -> bool:
        # Guard iff the nearest think marker at the end of the prompt is an open
        # `<think>` (the chat template's generation prefill).
        if prompt_tok_ids is None:
            return False
        ids = self._marker_ids
        assert ids is not None
        think_start = ids[THINK_START]
        think_end = ids[THINK_END]
        tool_begin = ids[TOOL_SECTION_BEGIN]
        for tok in reversed(prompt_tok_ids):
            if tok == think_start:
                return True
            if tok in (think_end, tool_begin):
                return False
        return False

    def update_state(self, batch_update: BatchUpdate | None) -> None:
        if batch_update is not None:
            for index in batch_update.removed:
                self._states.pop(index, None)

            if batch_update.added:
                self._resolve_marker_ids()  # resolve markers now, or abort

            for index, _params, prompt_tok_ids, output_tok_ids in batch_update.added:
                self._states[index] = _RequestState(
                    output_tok_ids=output_tok_ids,
                    consumed=0,
                    in_think=self._initial_in_think(prompt_tok_ids),
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
        self._refresh_guarded()

    def _scan_states(self) -> None:
        # Consume newly visible output tokens; release the guard on `</think>` /
        # tool-section. Stop at the first `-1` async-scheduling placeholder
        # (consuming it would skip the real token); apply() rescans once the
        # engine repairs it, so release happens in the same step.
        if not self._states or self._marker_ids is None:
            return
        think_end = self._marker_ids[THINK_END]
        tool_begin = self._marker_ids[TOOL_SECTION_BEGIN]
        for state in self._states.values():
            toks = state.output_tok_ids
            i = state.consumed
            n = len(toks)
            while i < n:
                tok = toks[i]
                if tok < 0:
                    break
                if state.in_think and tok in (think_end, tool_begin):
                    state.in_think = False
                i += 1
            state.consumed = i

    def _refresh_guarded(self) -> None:
        guarded = sorted(i for i, s in self._states.items() if s.in_think)
        if guarded != self._guarded:
            self._guarded = guarded
            self._guarded_idx = (
                torch.tensor(guarded, dtype=torch.long, device=self.device)
                if guarded
                else None
            )

    def apply(self, logits: torch.Tensor) -> torch.Tensor:
        # Rescan first: the previous step's async placeholder is repaired before
        # the sampler runs, so a `</think>` sampled last step releases the guard now.
        self._scan_states()
        self._refresh_guarded()
        idx = self._guarded_idx
        if self._marker_ids is None or idx is None:
            return logits

        im_end = self._marker_ids[IM_END]
        think_end = self._marker_ids[THINK_END]

        # Redirect the model's turn-end *choice* to closing reasoning: promote
        # `</think>` to at least `<|im_end|>`'s logit (so it wins wherever the
        # model would have ended the turn), then ban `<|im_end|>`. A pure
        # relabel that adds no stop pressure beyond the model's own; `max` (not
        # `logaddexp`) avoids amplification by top-k/top-p and T<1 — see
        # dev-docs/models/kimi_logits_processor.md.
        logits[idx, think_end] = torch.maximum(
            logits[idx, think_end], logits[idx, im_end]
        )
        logits[idx, im_end] = float("-inf")
        return logits

    def is_argmax_invariant(self) -> bool:
        return False
