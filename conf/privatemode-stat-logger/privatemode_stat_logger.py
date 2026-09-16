"""vLLM stat-logger plugin forwards events to the co-located inference-proxy.

The plugin's only job is generating events from the information vLLM passes to
`record()` and then POST-ing them in batches via a background worker. The background
worker sends any available data immediately, optimizing for latency.

There is one instance of the class running per vLLM Frontend process. All interpretation
of the events must happen in the inference-proxy where the information from all frontends
can be aggregated. The plugin cannot handle DP>1 where there are multiple engines.
Installing this plugin force-enables vLLM's stats collection, overriding
`--disable-log-stats`.

The worker is a coroutine on the frontend's event loop. `record()` effectively schedules
a callback to that worker on the loop by pushing to the event queue.

The feed is best-effort: events may be dropped and, because a failed POST is retried
once, may also arrive twice.
"""

from __future__ import annotations

import asyncio
import logging
import os
import random
import time
import uuid
from dataclasses import dataclass
from typing import TYPE_CHECKING, Any

import aiohttp
import msgspec
from prometheus_client import REGISTRY, Counter, Gauge, Histogram

from vllm.v1.metrics.loggers import AggregateStatLoggerBase

if TYPE_CHECKING:
    # These are only used in type annotations
    from vllm.config import VllmConfig
    from vllm.v1.metrics.stats import (
        FinishedRequestStats,
        IterationStats,
        MultiModalCacheStats,
        SchedulerStats,
    )

# Prefix "vllm." so records reach vLLM's configured handler.
logger = logging.getLogger("vllm." + __name__)

# All attributes read from vLLM types, mostly documented as unstable in vLLM.
#
#  * Unit tests build their doubles from the lists, verifying that the code does not rely
#    on further fields.
#  * The "contract" test in vLLM ensures a failure when any of these types change on
#    version upgrade.
#
# **This value must be copied to the contract test on any change.**
REQUIRED_VLLM_FIELDS: tuple[tuple[str, tuple[str, ...]], ...] = (
    ("VllmConfig", ("parallel_config",)),
    ("ParallelConfig", ("_api_process_rank",)),
    (
        "SchedulerStats",
        (
            "num_waiting_reqs",
            "num_skipped_waiting_reqs",
            "num_running_reqs",
            "kv_cache_usage",
        ),
    ),
    ("IterationStats", ("finished_requests",)),
    (
        "FinishedRequestStats",
        (
            "request_id",
            "finish_reason",
            "num_prompt_tokens",
            "num_generation_tokens",
            "num_cached_tokens",
            "max_tokens_param",
            "queued_time",
            "prefill_time",
            "decode_time",
            "e2e_latency",
        ),
    ),
)

ENV_URL = "PRIVATEMODE_STAT_LOGGER_URL"
ENV_TIMEOUT_SECONDS = "PRIVATEMODE_STAT_LOGGER_TIMEOUT_SECONDS"
ENV_BATCH_SIZE = "PRIVATEMODE_STAT_LOGGER_BATCH_SIZE"
ENV_BUFFER_SIZE = "PRIVATEMODE_STAT_LOGGER_BUFFER_SIZE"

# 1000 small events serialise to a few hundred KB, below the receiver's 1 MiB body cap.
_DEFAULT_BATCH_SIZE = 1000
_DEFAULT_BUFFER_SIZE = 8192
_DEFAULT_TIMEOUT_SECONDS = 1.0

_BACKOFF_SECONDS = 1.0

_HEADERS = {"Content-Type": "application/json"}

_ENCODER = msgspec.json.Encoder()


@dataclass(frozen=True)
class _Config:
    url: str
    timeout_seconds: float
    batch_size: int
    buffer_size: int


def _config_from_env() -> _Config:
    # ENV_URL is the only required env var
    url = os.environ.get(ENV_URL)
    if not url:
        raise ValueError(f"{ENV_URL} must be set")

    config = _Config(
        url=url,
        timeout_seconds=float(
            os.environ.get(ENV_TIMEOUT_SECONDS, _DEFAULT_TIMEOUT_SECONDS)
        ),
        batch_size=int(os.environ.get(ENV_BATCH_SIZE, _DEFAULT_BATCH_SIZE)),
        buffer_size=int(os.environ.get(ENV_BUFFER_SIZE, _DEFAULT_BUFFER_SIZE)),
    )
    if min(config.batch_size, config.buffer_size) <= 0:
        raise ValueError(f"sizes must be positive: {config}")
    if not config.timeout_seconds > 0:
        raise ValueError(f"timeout must be > 0: {config}")
    return config


def _create_session(config: _Config) -> aiohttp.ClientSession:
    """Returns a session of one keep-alive connection. Requires a running loop."""
    return aiohttp.ClientSession(
        connector=aiohttp.TCPConnector(limit=1, force_close=False),
        timeout=aiohttp.ClientTimeout(total=config.timeout_seconds),  # connect and read
    )


def _child(
    metric_cls: Any,
    name: str,
    documentation: str,
    labels: dict[str, str],
    **kwargs: Any,
) -> Any:
    """Creates child of metric `name` with `labels`, registering on first use."""
    family = REGISTRY._names_to_collectors.get(name)
    if family is None:
        family = metric_cls(name, documentation, labelnames=list(labels), **kwargs)
    return family.labels(**labels)


@dataclass(frozen=True)
class _Metrics:
    """This frontend's metric children, label values already bound."""

    healthy: Gauge
    dropped_events: Counter
    discarded_events: Counter
    posts_succeeded: Counter
    posts_failed: Counter
    record_calls: Counter
    record_seconds: Counter
    batch_events: Histogram


# Buckets for the privatemode_stat_logger_batch_events (events per batch histogram).
_BATCH_EVENT_BUCKETS = (1, 10, 100, 500, 1000)


def _create_metrics(frontend_index: int) -> _Metrics:
    labels = {"frontend_index": str(frontend_index)}
    # Reused for one metric family with `outcome` as a label: `success` or `failure`
    posts_name = "privatemode_stat_logger_posts"
    posts_doc = "POST requests to the collector, retries included, by outcome."
    return _Metrics(
        # healthy starts as 0 implicitly.
        healthy=_child(
            Gauge,
            "privatemode_stat_logger_healthy",
            "Whether the stat-logger plugin is operating (1) or has given up (0).",
            labels,
            multiprocess_mode="livemin",
        ),
        dropped_events=_child(
            Counter,
            "privatemode_stat_logger_dropped_events",
            "Events evicted from a full buffer.",
            labels,
        ),
        discarded_events=_child(
            Counter,
            "privatemode_stat_logger_discarded_events",
            "Events lost with a batch discarded after a POST and its retry failed.",
            labels,
        ),
        posts_succeeded=_child(
            Counter, posts_name, posts_doc, {**labels, "outcome": "success"}
        ),
        posts_failed=_child(
            Counter, posts_name, posts_doc, {**labels, "outcome": "failure"}
        ),
        record_calls=_child(
            Counter,
            "privatemode_stat_logger_record_calls",
            "Calls into record() that completed, i.e. the sample count belonging to "
            "privatemode_stat_logger_record_seconds.",
            labels,
        ),
        record_seconds=_child(
            Counter,
            "privatemode_stat_logger_record_seconds",
            "Wall-clock seconds spent inside record(), on the frontend event loop.",
            labels,
        ),
        batch_events=_child(
            Histogram,
            "privatemode_stat_logger_batch_events",
            "Events per POSTed batch, capped by the configured batch size.",
            labels,
            buckets=_BATCH_EVENT_BUCKETS,
        ),
    )


def _queue_state_event(stats: SchedulerStats) -> dict[str, Any]:
    return {
        "type": "queue_state",
        "num_waiting_capacity": stats.num_waiting_reqs,
        "num_waiting_deferred": stats.num_skipped_waiting_reqs,
        "num_running": stats.num_running_reqs,
        "kv_cache_usage": stats.kv_cache_usage,
    }


def _request_finished_event(stats: FinishedRequestStats) -> dict[str, Any]:
    return {
        "type": "request_finished",
        # vLLM emits n requests when n > 1 (parallel sampling), so this field
        # distinguishes them.
        # TODO: Switch to uuid.uuid7() once the vLLM image ships >= Python 3.14
        "event_id": str(uuid.uuid4()),
        # vLLM internal request ID: incoming X-Request-Id prefixed e.g. with "chatcmpl-"
        "request_id": stats.request_id,
        "finish_reason": str(stats.finish_reason),
        "num_prompt_tokens": stats.num_prompt_tokens,
        "num_generation_tokens": stats.num_generation_tokens,
        "num_cached_tokens": stats.num_cached_tokens,
        "max_tokens": stats.max_tokens_param,
        "queued_time_engine_seconds": stats.queued_time,
        "prefill_time_engine_seconds": stats.prefill_time,
        "decode_time_engine_seconds": stats.decode_time,
        "e2e_latency_frontend_seconds": stats.e2e_latency,
    }


class PrivatemodeStatLogger(AggregateStatLoggerBase):
    """Forwards queue-state and request-finished events to the inference-proxy."""

    def __init__(self, vllm_config: VllmConfig, engine_indexes: list[int]) -> None:
        del engine_indexes
        # Initialize values which cannot fail
        self._pid = os.getpid()
        self._frontend_index = -1  # replaced below; only reached if that fails
        self._enabled = False
        self._queue: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self._worker: asyncio.Task[None] | None = None
        self._session: aiohttp.ClientSession  # Assigned by the worker

        # Initialize proper values, where exceptions may occur but should not abort vLLM
        try:
            self._frontend_index = vllm_config.parallel_config._api_process_rank
            self._metrics = _create_metrics(self._frontend_index)
            self._config = _config_from_env()
            # Exception will be raised if not in an event loop, disabling the plugin.
            loop = asyncio.get_running_loop()
            self._worker = loop.create_task(self._run(), name="pm-stat-logger")
        except Exception:
            self._degrade("Privatemode stat logger startup failed, plugin disabled")
            return

        self._enabled = True
        self._metrics.healthy.set(1)

    def record(
        self,
        scheduler_stats: SchedulerStats | None,
        iteration_stats: IterationStats | None,
        mm_cache_stats: MultiModalCacheStats | None = None,
        engine_idx: int = 0,
        **kwargs: Any,
    ) -> None:
        del mm_cache_stats, engine_idx, kwargs
        if not self._enabled:
            return
        try:
            started = time.perf_counter()
            if scheduler_stats is not None:
                self._append(_queue_state_event(scheduler_stats))
            if iteration_stats is not None:
                for finished in iteration_stats.finished_requests:
                    self._append(_request_finished_event(finished))
            self._metrics.record_calls.inc()
            self._metrics.record_seconds.inc(time.perf_counter() - started)
        except Exception:
            self._degrade("Privatemode stat logger record failed, plugin disabled")

    def log_engine_initialized(self) -> None:
        pass

    def _append(self, event: dict[str, Any]) -> None:
        # The queue is unbounded, the size limit is enforced here
        if self._queue.qsize() >= self._config.buffer_size:
            self._queue.get_nowait()
            self._metrics.dropped_events.inc()
        self._queue.put_nowait(event)

    def _degrade(self, message: str) -> None:
        """Turns the plugin off and reports it. Cannot raise.

        Call from an `except` block: the report includes the active exception.
        """
        self._enabled = False
        if self._worker is not None:
            self._worker.cancel()
        try:
            logger.exception(message)
            # Proper metrics may not have been created yet. Frontend index may still be
            # the initial -1.
            self._metrics = _create_metrics(self._frontend_index)
            self._metrics.healthy.set(0)
        except Exception:
            pass

    async def _run(self) -> None:
        """Drains the queue and POSTs events, on the frontend's event loop.

        Runs in an asyncio task, which can be cancelled to stop the worker.
        """
        try:
            self._session = _create_session(self._config)
        except Exception:
            self._degrade("Privatemode stat logger worker failed, plugin disabled")
            return

        try:
            while True:
                try:
                    await self._drain()
                except Exception:
                    logger.exception("Privatemode stat logger worker iteration failed")
                    await self._backoff()
        finally:
            try:
                await self._session.close()
            except Exception:
                logger.debug(
                    "Privatemode stat logger session close failed", exc_info=True
                )

    async def _drain(self) -> None:
        """Waits for events, then POSTs everything queued, at most `batch_size`."""
        batch = [await self._queue.get()]
        while len(batch) < self._config.batch_size and not self._queue.empty():
            batch.append(self._queue.get_nowait())
        self._metrics.batch_events.observe(len(batch))
        payload = _ENCODER.encode(
            {
                "frontend_index": self._frontend_index,
                "pid": self._pid,
                "events": batch,
            }
        )
        if not await self._post(payload):
            self._metrics.discarded_events.inc(len(batch))
            await self._backoff()

    async def _post(self, payload: bytes) -> bool:
        """POSTs one encoded batch, retrying once. Returns False if both failed.

        The retry may cover: keep-alive connection broke, network blip, ...
        """
        for _ in range(2):
            reason: Any
            try:
                async with self._session.post(
                    self._config.url,
                    data=payload,
                    headers=_HEADERS,
                    allow_redirects=False,
                ) as response:
                    # Drain the body so the connection returns to the pool.
                    await response.read()
                    if 200 <= response.status < 300:
                        self._metrics.posts_succeeded.inc()
                        return True
                    reason = f"{response.status} response from server"
            except Exception as exc:
                reason = exc
            self._metrics.posts_failed.inc()
            logger.debug("Privatemode stat logger POST failed: %s", reason)
        return False

    async def _backoff(self) -> None:
        # Jitter drawn from [0.5, 1.5] * _BACKOFF_SECONDS
        await asyncio.sleep(_BACKOFF_SECONDS * (0.5 + random.random()))
