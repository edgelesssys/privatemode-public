# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the vLLM project
"""No-repeat n-gram logits processor for the DeepSeek-OCR model.

Adapted from the vLLM project's ``NoRepeatNGramLogitsProcessor`` and
``NGramPerReqLogitsProcessor``, which live in the DeepSeek-OCR model module:
https://github.com/vllm-project/vllm/blob/main/vllm/model_executor/models/deepseek_ocr.py

This module is mounted standalone into the vLLM workspace and loaded via
``deepseek_logits_processor:NGramPerReqLogitsProcessor``, so it must only import
from installed packages (no relative imports / model code).

Modifications from upstream:
  * Standalone extraction. Only the two logits-processor classes are kept; all
    DeepSeek-OCR model / encoder code and its relative imports were dropped so
    this file can be mounted and imported on its own.
  * Always-on instead of opt-in. Upstream returns ``None`` (skips the processor)
    unless a request passes ``ngram_size`` in ``extra_args``. Here the processor
    is applied to *every* request using the module defaults below
    (``_DEFAULT_NGRAM_SIZE`` / ``_DEFAULT_WINDOW_SIZE`` /
    ``_DEFAULT_WHITELIST_TOKEN_IDS``); a request may still override any of them
    via ``vllm_xargs``.
  * Tightened ``validate_params``. ``ngram_size`` must be >= 2 (upstream accepted
    1, which the repeat-detection slicing does not support), and
    ``whitelist_token_ids`` must be a non-string iterable of integers (upstream
    only checked ``Iterable``, so a string or a list with non-int elements would
    pass validation and later crash during logits indexing).

The ``NoRepeatNGramLogitsProcessor.__call__`` ban logic is unchanged from
upstream.
"""

from collections.abc import Iterable

import torch

from vllm.sampling_params import SamplingParams
from vllm.v1.sample.logits_processor import (
    AdapterLogitsProcessor,
    RequestLogitsProcessor,
)

# Default no-repeat n-gram parameters. These are applied to every request so the
# processor runs unconditionally, even when the request does not pass the
# `ngram_size` / `window_size` / `whitelist_token_ids` extra args. A request may
# still override any of them via `vllm_xargs`.
_DEFAULT_NGRAM_SIZE = 30
_DEFAULT_WINDOW_SIZE = 90
_DEFAULT_WHITELIST_TOKEN_IDS = [128821, 128822]


class NoRepeatNGramLogitsProcessor:
    def __init__(
        self,
        ngram_size: int,
        window_size: int,
        whitelist_token_ids: set[int] | None = None,
    ):
        self.ngram_size = ngram_size
        self.window_size = window_size
        self.whitelist_token_ids = whitelist_token_ids or set()

    def __call__(
        self,
        output_ids: list[int],
        logits: torch.Tensor,
    ) -> torch.Tensor:
        if len(output_ids) < self.ngram_size:
            return logits

        current_prefix = tuple(output_ids[-(self.ngram_size - 1) :])

        search_start = max(0, len(output_ids) - self.window_size)
        search_end = len(output_ids) - self.ngram_size + 1

        banned_tokens = set()
        for i in range(search_start, search_end):
            ngram = tuple(output_ids[i : i + self.ngram_size])
            if ngram[:-1] == current_prefix:
                banned_tokens.add(ngram[-1])

        banned_tokens = banned_tokens - self.whitelist_token_ids

        if banned_tokens:
            logits[list(banned_tokens)] = -float("inf")

        return logits


class NGramPerReqLogitsProcessor(AdapterLogitsProcessor):
    """Example of overriding the wrapper class `__init__()` in order to utilize
    info about the device type"""

    @classmethod
    def validate_params(cls, params: SamplingParams):
        extra_args = params.extra_args or {}
        ngram_size = extra_args.get("ngram_size", _DEFAULT_NGRAM_SIZE)
        window_size = extra_args.get("window_size", _DEFAULT_WINDOW_SIZE)
        whitelist_token_ids = extra_args.get(
            "whitelist_token_ids", _DEFAULT_WHITELIST_TOKEN_IDS
        )

        # `ngram_size` must be >= 2: the repeat-detection logic slices the
        # prefix as `output_ids[-(ngram_size - 1):]`, which degenerates to
        # `output_ids[-0:]` (the whole sequence) when `ngram_size == 1`, so
        # single-token n-grams are not supported. `bool` is excluded because it
        # is an `int` subclass but not a meaningful size.
        if (
            not isinstance(ngram_size, int)
            or isinstance(ngram_size, bool)
            or ngram_size < 2
        ):
            raise ValueError(
                f"`ngram_size` has to be an integer >= 2, got {ngram_size!r}."
            )
        if (
            not isinstance(window_size, int)
            or isinstance(window_size, bool)
            or window_size <= 0
        ):
            raise ValueError(
                "`window_size` has to be a strictly positive integer, "
                f"got {window_size!r}."
            )
        # `whitelist_token_ids` must be a non-string iterable of ints. A bare
        # `Iterable` check passes strings (iterable of chars) and lists with
        # non-int elements, both of which crash later during logits indexing.
        if whitelist_token_ids is not None:
            if isinstance(whitelist_token_ids, (str, bytes)) or not isinstance(
                whitelist_token_ids, Iterable
            ):
                raise ValueError(
                    "`whitelist_token_ids` has to be an iterable of integers, "
                    f"got {whitelist_token_ids!r}."
                )
            if any(
                not isinstance(t, int) or isinstance(t, bool)
                for t in whitelist_token_ids
            ):
                raise ValueError(
                    "`whitelist_token_ids` has to contain only integers, "
                    f"got {whitelist_token_ids!r}."
                )

    def is_argmax_invariant(self) -> bool:
        return False

    def new_req_logits_processor(
        self,
        params: SamplingParams,
    ) -> RequestLogitsProcessor | None:
        extra_args = params.extra_args or {}
        ngram_size = extra_args.get("ngram_size", _DEFAULT_NGRAM_SIZE)
        window_size = extra_args.get("window_size", _DEFAULT_WINDOW_SIZE)
        whitelist_token_ids = extra_args.get(
            "whitelist_token_ids", _DEFAULT_WHITELIST_TOKEN_IDS
        )

        whitelist_token_ids = set(whitelist_token_ids) if whitelist_token_ids else None
        return NoRepeatNGramLogitsProcessor(
            ngram_size=ngram_size,
            window_size=window_size,
            whitelist_token_ids=whitelist_token_ids,
        )
