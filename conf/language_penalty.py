# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the vLLM project

from typing import TYPE_CHECKING, Any

import regex as re
import torch

from vllm.logger import init_logger
from vllm.v1.sample.logits_processor.interface import BatchUpdate, LogitsProcessor

if TYPE_CHECKING:
    from vllm.config import VllmConfig
    from vllm.config.model import ModelConfig

# Prefix "vllm." so the logger inherits vLLM's INFO handler: this module loads
# off PYTHONPATH, so __name__ alone sits outside the vllm.* namespace and its
# INFO records would be dropped.
logger = init_logger("vllm." + __name__)

# -3 is insufficient for Kimi
LANGUAGE_LOGIT_BIAS = -5.0
_HAN_RE = re.compile(r"\p{Han}")
_KANA_RE = re.compile(r"[\p{Hiragana}\p{Katakana}]")
_LOGIT_BIAS_CACHE: dict[tuple[str, str, str | None, bool, int], torch.Tensor] = {}


def _should_penalize_token(decoded: str) -> bool:
    if not _HAN_RE.search(decoded):
        return False
    # Spare tokens containing kana (explicitly Japanese); penalize Han-only,
    # including ambiguous kanji shared across Chinese and Japanese.
    return not _KANA_RE.search(decoded)


def compute_bias_from_tokenizer(tokenizer: Any, vocab_size: int) -> torch.Tensor:
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
            logit_bias[token_id] = LANGUAGE_LOGIT_BIAS
    return logit_bias


def _get_logit_bias_cpu(model_config: "ModelConfig", vocab_size: int) -> torch.Tensor:
    from vllm.tokenizers import cached_tokenizer_from_config

    tokenizer_name = model_config.tokenizer or model_config.model
    cache_key = (
        tokenizer_name,
        model_config.tokenizer_mode,
        model_config.tokenizer_revision,
        model_config.trust_remote_code,
        vocab_size,
    )
    if cache_key in _LOGIT_BIAS_CACHE:
        return _LOGIT_BIAS_CACHE[cache_key]
    tokenizer = cached_tokenizer_from_config(model_config)
    if tokenizer is None:
        raise ValueError("Language penalty requires tokenizer initialization.")
    logit_bias = compute_bias_from_tokenizer(tokenizer, vocab_size)
    # One-time (per model/vocab) confirmation that the penalty is active.
    logger.info(
        "Language penalty active: %d tokens biased by %s for %s (vocab_size=%d).",
        int(torch.count_nonzero(logit_bias).item()),
        LANGUAGE_LOGIT_BIAS,
        tokenizer_name,
        vocab_size,
    )
    _LOGIT_BIAS_CACHE[cache_key] = logit_bias
    return logit_bias


def get_logit_bias(
    model_config: "ModelConfig", vocab_size: int, device: torch.device
) -> torch.Tensor:
    logit_bias = _get_logit_bias_cpu(model_config=model_config, vocab_size=vocab_size)
    return logit_bias.to(device=device, non_blocking=True)


class LanguagePenaltyLogitsProcessor(LogitsProcessor):
    """Bias Han-only (Chinese) tokens down to suppress language drift."""

    def __init__(
        self,
        vllm_config: "VllmConfig",
        device: torch.device,
        is_pin_memory: bool,
    ) -> None:
        del device
        del is_pin_memory
        self.model_config = vllm_config.model_config
        self.logit_bias: torch.Tensor | None = None

    def apply(self, logits: torch.Tensor) -> torch.Tensor:
        logit_bias = self._get_logit_bias(logits)
        if logit_bias is not None:
            logits.add_(logit_bias)
        return logits

    def is_argmax_invariant(self) -> bool:
        return False

    def update_state(self, batch_update: BatchUpdate | None) -> None:
        del batch_update

    def _get_logit_bias(self, logits: torch.Tensor) -> torch.Tensor | None:
        if self.model_config is None:
            return None
        if (
            self.logit_bias is None
            or self.logit_bias.device != logits.device
            or self.logit_bias.shape[0] != logits.shape[-1]
        ):
            self.logit_bias = get_logit_bias(
                model_config=self.model_config,
                vocab_size=logits.shape[-1],
                device=logits.device,
            )
        return self.logit_bias
