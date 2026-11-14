# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the vLLM project

import math
import os
from typing import TYPE_CHECKING, Any

import regex as re
import torch

from vllm.logger import init_logger
from vllm.v1.worker.gpu.sample.logits_processor import (
    LogitsContext,
    LogitsProcessor,
    LogitsProcRequestState,
)

if TYPE_CHECKING:
    from vllm.config import VllmConfig

# Prefix "vllm." so the logger inherits vLLM's INFO handler: this module loads
# off PYTHONPATH, so __name__ alone sits outside the vllm.* namespace and its
# INFO records would be dropped.
logger = init_logger("vllm." + __name__)

# How much a model drifts differs per model, so the bias is a deployment setting
# with no default: every model enabling this processor states its own value
# (see extraEnv in values.yaml).
LOGIT_BIAS_ENV = "LANGUAGE_PENALTY_LOGIT_BIAS"
_HAN_RE = re.compile(r"\p{Han}")
_KANA_RE = re.compile(r"[\p{Hiragana}\p{Katakana}]")


def configured_logit_bias() -> float:
    """Bias added to Han-only token logits, read from the environment.

    Required: a missing, malformed or not-negative-finite value raises instead
    of falling back to some assumed bias, so a deployment mistake fails at
    engine start rather than silently serving the model unpenalized. Called
    once per processor, not per request.
    """
    raw = os.environ.get(LOGIT_BIAS_ENV)
    if raw is None or not raw.strip():
        raise ValueError(f"{LOGIT_BIAS_ENV} is required but unset")
    try:
        bias = float(raw)
    except ValueError:
        raise ValueError(f"{LOGIT_BIAS_ENV}={raw!r} is not a number") from None
    # float() accepts "nan" and "inf": nan would pass a plain `>= 0.0` check and
    # poison every penalized logit, so require a finite value explicitly.
    if not math.isfinite(bias) or bias >= 0.0:
        raise ValueError(f"{LOGIT_BIAS_ENV}={raw!r} must be a finite negative number")
    return bias


def _should_penalize_token(decoded: str) -> bool:
    if not _HAN_RE.search(decoded):
        return False
    # Spare tokens containing kana (explicitly Japanese); penalize Han-only,
    # including ambiguous kanji shared across Chinese and Japanese.
    return not _KANA_RE.search(decoded)


def compute_bias_from_tokenizer(
    tokenizer: Any, vocab_size: int, bias: float
) -> torch.Tensor:
    logit_bias = torch.zeros(vocab_size, dtype=torch.float32)
    max_token_id = getattr(tokenizer, "max_token_id", None)
    upper_bound = vocab_size
    if isinstance(max_token_id, int):
        upper_bound = min(upper_bound, max_token_id + 1)
    for token_id in range(upper_bound):
        try:
            decoded = tokenizer.decode(
                [token_id],
                skip_special_tokens=False,
                clean_up_tokenization_spaces=False,
            )
        except TypeError:
            decoded = tokenizer.decode([token_id])
        except Exception:
            continue
        if _should_penalize_token(decoded):
            logit_bias[token_id] = bias
    return logit_bias


class LanguagePenaltyLogitsProcessor(LogitsProcessor):
    """Bias Han-only (Chinese) tokens down to suppress language drift."""

    def __init__(
        self, vllm_config: "VllmConfig", req_states: LogitsProcRequestState
    ) -> None:
        # Resolved at engine start so a bad override crashes the workload early.
        bias = configured_logit_bias()
        model_config = vllm_config.model_config
        if model_config is None:
            raise ValueError("Language penalty requires a model_config.")
        from vllm.tokenizers import cached_tokenizer_from_config

        tokenizer = cached_tokenizer_from_config(model_config)
        if tokenizer is None:
            raise ValueError("Language penalty requires tokenizer initialization.")
        logit_bias = compute_bias_from_tokenizer(tokenizer, req_states.vocab_size, bias)
        # One-time confirmation that the penalty is active, with the effective
        # bias so the configured value is visible in the log.
        logger.info(
            "Language penalty active: %d tokens biased by %s for %s (vocab_size=%d).",
            int(torch.count_nonzero(logit_bias).item()),
            bias,
            model_config.tokenizer or model_config.model,
            req_states.vocab_size,
        )
        self.logit_bias = logit_bias.to(req_states.device)

    def apply(self, logits: torch.Tensor, ctx: LogitsContext) -> torch.Tensor:
        # Same bias for every row, so no row-to-request mapping is needed.
        del ctx
        logits.add_(self.logit_bias)
        return logits
