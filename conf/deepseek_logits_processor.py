# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the vLLM project
"""No-repeat n-gram logits processor for the DeepSeek-OCR model.

Applies the ban rule of vLLM's ``NoRepeatNGramLogitsProcessor``
(``vllm/model_executor/models/deepseek_ocr.py``) per request, on device over
the whole batch (``vllm.v1.worker.gpu.sample.logits_processor`` interface).

This module is mounted standalone into the vLLM workspace and loaded via
``deepseek_logits_processor:NGramPerReqLogitsProcessor``, so it must only
import from installed packages.
"""

from typing import TYPE_CHECKING

import numpy as np
import torch

from vllm.utils.torch_utils import async_tensor_h2d
from vllm.v1.worker.gpu.sample.logits_processor import (
    LogitsContext,
    LogitsProcessor,
    LogitsProcRequestState,
)

if TYPE_CHECKING:
    from vllm.config import VllmConfig

# Default no-repeat n-gram parameters. These are applied to every request so the
# processor runs unconditionally. Not configurable per request: GPU memory grows
# with window_size x ngram_size.
_NGRAM_SIZE = 30
_WINDOW_SIZE = 90
_WHITELIST_TOKEN_IDS = [128821, 128822]


class NGramPerReqLogitsProcessor(LogitsProcessor):
    """Per-request no-repeat n-gram ban."""

    def __init__(
        self, vllm_config: "VllmConfig", req_states: LogitsProcRequestState
    ) -> None:
        if vllm_config.speculative_config is not None:
            raise ValueError(
                f"{type(self).__name__} does not support speculative decoding."
            )
        self.req_states = req_states
        self.device = req_states.device
        self.ngram_size = _NGRAM_SIZE
        self.window_size = _WINDOW_SIZE
        self._whitelist = torch.tensor(
            _WHITELIST_TOKEN_IDS, dtype=torch.long, device=self.device
        )

    def apply(self, logits: torch.Tensor, ctx: LogitsContext) -> torch.Tensor:
        slots = ctx.idx_mapping_np
        if logits.shape[0] != slots.shape[0]:
            raise RuntimeError(f"{type(self).__name__} received draft-expanded logits")

        # Padded to the CUDA graph's request count under FULL graphs.
        seq_lens = ctx.seq_lens_upper_bound_np[: slots.shape[0]].astype(np.int64)
        output_lens = seq_lens - self.req_states.prompt_len.np[slots]
        # Fewer output tokens than ngram_size cannot ban anything. This also
        # skips prefill chunks.
        rows = np.flatnonzero(output_lens >= self.ngram_size)
        if rows.size:
            self._ban_repeats(logits, rows, slots, seq_lens, output_lens)
        return logits

    def _ban_repeats(
        self,
        logits: torch.Tensor,
        rows: np.ndarray,
        slots: np.ndarray,
        seq_lens: np.ndarray,
        output_lens: np.ndarray,
    ) -> None:
        """Ban, for the given rows, every token that would complete an n-gram
        already present in the request's last window_size output tokens.

        Same rule as NoRepeatNGramLogitsProcessor.__call__: with L output
        tokens, the n-grams starting at [max(0, L - window_size), L - ngram_size]
        whose first ngram_size - 1 tokens equal the last ngram_size - 1 tokens
        ban their final token.
        """
        ngram_size, window_size = self.ngram_size, self.window_size
        # Last window_size committed tokens per row, right-aligned: column j
        # holds output position L - window_size + j. Columns before the output
        # start are clamped and masked out below.
        positions = async_tensor_h2d(seq_lens[rows], self.device)[:, None] + (
            torch.arange(-window_size, 0, device=self.device)
        )
        tokens = self.req_states.all_token_ids.gpu[
            async_tensor_h2d(slots[rows], self.device)[:, None],
            positions.clamp(min=0),
        ].long()

        # ngrams[:, k] starts at column k; the last one ends at the newest token.
        ngrams = tokens.unfold(1, ngram_size, 1)
        prefix = tokens[:, window_size - ngram_size + 1 :]
        banned = (ngrams[:, :, :-1] == prefix[:, None, :]).all(dim=-1)
        # Keep only n-grams that lie fully in the output.
        first_start = window_size - np.minimum(output_lens[rows], window_size)
        starts = torch.arange(ngrams.shape[1], device=self.device)
        banned &= starts[None, :] >= async_tensor_h2d(first_start, self.device)[:, None]

        banned_tokens = ngrams[:, :, -1]
        if self._whitelist.numel():
            banned &= ~torch.isin(banned_tokens, self._whitelist)

        # Accumulate tolerates a token banned by several n-grams and avoids the
        # host sync of boolean indexing.
        penalty = torch.zeros(banned.shape, dtype=logits.dtype, device=self.device)
        penalty.masked_fill_(banned, -float("inf"))
        row_index = async_tensor_h2d(rows, self.device)[:, None].expand_as(banned)
        logits.index_put_(
            (row_index.reshape(-1), banned_tokens.reshape(-1)),
            penalty.reshape(-1),
            accumulate=True,
        )
