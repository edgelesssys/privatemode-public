# sitecustomize.py - auto-loaded by CPython on startup if on sys.path.
#
# 1. Drops a benign, noisy print() from the remote-code tokenizer path.
# 2. If VLLM_MEM_POLL_SECONDS is set, emits periodic "MEMSTATS {json}" lines
#    with torch allocator stats (reserved vs. allocated fragmentation) that
#    nvidia-smi cannot see from outside the process, plus NVML utilization
#    and power draw.
import builtins
import json
import os
import sys
import threading
import time


def _nvml_stat(fn, dev):
    # None if NVML is unavailable or the query unsupported.
    try:
        return fn(dev)
    except Exception:
        return None


def _mem_poll_loop(interval, with_snapshot):
    last_error = None
    while True:
        time.sleep(interval)
        try:
            # Never import torch here; sitecustomize loads into every python
            # subprocess. Only report processes that already use CUDA.
            torch = sys.modules.get("torch")
            if torch is None or not hasattr(torch, "cuda"):
                continue
            if not torch.cuda.is_initialized():
                continue
            snapshot = None  # global snapshot, taken lazily once per poll
            snapshot_ms = 0
            for dev in range(torch.cuda.device_count()):
                # memory_stats() is a free stat read; mem_get_info() runs under
                # a device guard and would create a ~0.5GiB context on unused
                # devices, so gate it on reserved>0 (proves a context exists).
                s = torch.cuda.memory_stats(dev)
                reserved = s.get("reserved_bytes.all.current", 0)
                if not reserved:
                    continue
                free, total = torch.cuda.mem_get_info(dev)
                allocated = s.get("allocated_bytes.all.current", 0)
                rec = {
                    "ts": round(time.time(), 1),
                    "pid": os.getpid(),
                    "dev": dev,
                    "device_free_mib": free >> 20,
                    "device_total_mib": total >> 20,
                    "reserved_mib": reserved >> 20,
                    "allocated_mib": allocated >> 20,
                    "resv_unalloc_mib": (reserved - allocated) >> 20,
                    "inactive_split_mib": s.get("inactive_split_bytes.all.current", 0)
                    >> 20,
                    "alloc_retries": s.get("num_alloc_retries", 0),
                    "ooms": s.get("num_ooms", 0),
                    "gpu_util_pct": _nvml_stat(torch.cuda.utilization, dev),
                    "power_w": _nvml_stat(
                        lambda d: round(torch.cuda.power_draw(d) / 1000, 1), dev
                    ),
                }
                if with_snapshot:
                    # largest_free_block: an alloc bigger than both it and
                    # device_free fails -> the fragmentation limit.
                    if snapshot is None:
                        # holds the allocator lock; disable via
                        # VLLM_MEM_POLL_SNAPSHOT=0 if snapshot_ms grows large.
                        t0 = time.monotonic()
                        snapshot = torch.cuda.memory_snapshot()
                        snapshot_ms = int((time.monotonic() - t0) * 1000)
                    largest = 0
                    free_cached = 0
                    for seg in snapshot:
                        if seg.get("device", dev) != dev:
                            continue
                        for blk in seg.get("blocks", []):
                            if blk.get("state") == "inactive":
                                size = blk.get("size", 0)
                                free_cached += size
                                largest = max(largest, size)
                    rec["largest_free_block_mib"] = largest >> 20
                    rec["free_cached_mib"] = free_cached >> 20
                    rec["snapshot_ms"] = snapshot_ms
                sys.stderr.write("MEMSTATS " + json.dumps(rec) + "\n")
                sys.stderr.flush()
            last_error = None
        except Exception as exc:  # never take down the host process
            msg = repr(exc)  # log each distinct persistent error only once
            if msg != last_error:
                last_error = msg
                try:
                    sys.stderr.write("MEMSTATS error: %s\n" % msg)
                    sys.stderr.flush()
                except Exception:
                    pass


_mem_poll_seconds = os.environ.get("VLLM_MEM_POLL_SECONDS")
if _mem_poll_seconds:
    try:
        _interval = float(_mem_poll_seconds)
        if _interval <= 0:  # non-positive would hot-spin the allocator lock
            raise ValueError("VLLM_MEM_POLL_SECONDS must be > 0")
        threading.Thread(
            target=_mem_poll_loop,
            args=(_interval, os.environ.get("VLLM_MEM_POLL_SNAPSHOT", "1") != "0"),
            daemon=True,
        ).start()
    except Exception:
        pass

_real_print = builtins.print


def _filtered_print(*args, **kwargs):
    if (
        args
        and isinstance(args[0], str)
        and args[0].startswith("Failed to convert tools to TypeScript style")
    ):
        sys.stderr.write(
            "remote tokenizer: tool-to-typescript conversion failed (suppressed)\n"
        )
        sys.stderr.flush()
        return
    return _real_print(*args, **kwargs)


builtins.print = _filtered_print
