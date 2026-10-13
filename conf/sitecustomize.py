# sitecustomize.py - auto-loaded by CPython on startup if on sys.path.
#
# 1. Drops a benign, noisy print() from the remote-code tokenizer path.
# 2. If VLLM_MEM_POLL_SECONDS is set, emits periodic "MEMSTATS {json}" lines
#    with torch allocator stats (reserved vs. allocated fragmentation) that
#    nvidia-smi cannot see from outside the process, plus NVML utilization,
#    power draw and the worker's engine step count.
# 3. Dumps the Python stacks of all workers before the EngineCore shuts them
#    down, plus the threads' kernel-side state, CUDA probes and NCCL RAS status.
#    The dump and the step count need the mp executor (TP 1 runs uni).
# 4. With VLLM_MEM_POLL_SECONDS, the `vllm serve` process also emits "PROCMEM
#    {json}" lines: host memory of every process in the container and tmpfs usage.
import builtins
import faulthandler
import json
import logging
import os
import sys
import threading
import time

# Engine steps of this worker and the CUDA device and streams for the hang probes.
_worker_state = {
    "patched": False,
    "count": 0,
    "recorded": False,
    "device": None,
    "stream": None,
    "spare": None,
}

_CUDA_SUCCESS = 0
_CUDA_ERROR_NOT_READY = 600
_CU_STREAM_NON_BLOCKING = 1


def _poll_forever(interval, tag, poll_once):
    # Never takes down the host process; logs each distinct persistent error once.
    last_error = None
    while True:
        time.sleep(interval)
        try:
            poll_once()
            last_error = None
        except Exception as exc:
            msg = repr(exc)
            if msg != last_error:
                last_error = msg
                try:
                    sys.stderr.write("%s error: %s\n" % (tag, msg))
                    sys.stderr.flush()
                except Exception:
                    pass


def _nvml_stat(fn, dev):
    # None if NVML is unavailable or the query unsupported.
    try:
        return fn(dev)
    except Exception:
        return None


def _mem_poll_once(with_snapshot):
    # Never import torch here; sitecustomize loads into every python
    # subprocess. Only report processes that already use CUDA.
    torch = sys.modules.get("torch")
    if torch is None or not hasattr(torch, "cuda"):
        return
    if not torch.cuda.is_initialized():
        return
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
            "inactive_split_mib": s.get("inactive_split_bytes.all.current", 0) >> 20,
            "alloc_retries": s.get("num_alloc_retries", 0),
            "ooms": s.get("num_ooms", 0),
            "gpu_util_pct": _nvml_stat(torch.cuda.utilization, dev),
            "power_w": _nvml_stat(
                lambda d: round(torch.cuda.power_draw(d) / 1000, 1), dev
            ),
        }
        if _worker_state["patched"]:
            rec["steps"] = _worker_state["count"]
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


def _kib_fields(path):
    # "Key:   123 kB" lines of a /proc status-style file, in KiB
    out = {}
    with open(path) as f:
        for line in f:
            key, _, value = line.partition(":")
            parts = value.split()
            if parts and parts[0].isdigit():
                out[key] = int(parts[0])
    return out


def _tmpfs_used_mib():
    # Memory-backed mounts count as Shmem in /proc/meminfo without belonging to
    # any process; one entry per filesystem, as bind mounts report the same usage.
    used, seen = {}, set()
    with open("/proc/mounts") as f:
        for line in f:
            parts = line.split()
            if len(parts) > 2 and parts[2] == "tmpfs":
                try:
                    st = os.statvfs(parts[1])
                    dev = os.stat(parts[1]).st_dev
                except OSError:
                    continue
                mib = (st.f_blocks - st.f_bfree) * st.f_frsize >> 20
                if mib and dev not in seen:
                    seen.add(dev)
                    used[parts[1]] = mib
    return used


def _proc_mem_once():
    # Host memory per process; the guest's OOM killer kills API servers that grow.
    procs, pids, unreadable = [], 0, {}
    for pid in os.listdir("/proc"):
        if not pid.isdigit():
            continue
        pids += 1
        try:
            status = _kib_fields("/proc/%s/status" % pid)
            with open("/proc/%s/comm" % pid) as f:
                name = f.read().strip()
        except OSError as e:  # exited meanwhile, or hidden from this process
            kind = type(e).__name__
            unreadable[kind] = unreadable.get(kind, 0) + 1
            continue
        if status.get("VmRSS"):  # kernel threads have none
            procs.append(
                {
                    "pid": int(pid),
                    "name": name,
                    "rss_mib": status["VmRSS"] >> 10,
                    "anon_mib": status.get("RssAnon", 0) >> 10,
                    "shmem_mib": status.get("RssShmem", 0) >> 10,
                }
            )
    meminfo = _kib_fields("/proc/meminfo")
    rec = {
        "ts": round(time.time(), 1),
        "mem_total_mib": meminfo.get("MemTotal", 0) >> 10,
        "mem_available_mib": meminfo.get("MemAvailable", 0) >> 10,
        "shmem_mib": meminfo.get("Shmem", 0) >> 10,
        "pids": pids,
        "unreadable": unreadable,
        "tmpfs_used_mib": _tmpfs_used_mib(),
        "procs": sorted(procs, key=lambda p: -p["rss_mib"]),
    }
    sys.stderr.write("PROCMEM " + json.dumps(rec) + "\n")
    sys.stderr.flush()


_mem_poll_seconds = os.environ.get("VLLM_MEM_POLL_SECONDS")
if _mem_poll_seconds:
    try:
        _interval = float(_mem_poll_seconds)
        if _interval <= 0:  # non-positive would hot-spin the allocator lock
            raise ValueError("VLLM_MEM_POLL_SECONDS must be > 0")
        _snapshot = os.environ.get("VLLM_MEM_POLL_SNAPSHOT", "1") != "0"
        threading.Thread(
            target=_poll_forever,
            args=(_interval, "MEMSTATS", lambda: _mem_poll_once(_snapshot)),
            daemon=True,
        ).start()
        if sys.argv[1:2] == ["serve"]:  # the top-level `vllm serve` process only
            threading.Thread(
                target=_poll_forever,
                args=(_interval, "PROCMEM", _proc_mem_once),
                daemon=True,
            ).start()
    except Exception:
        pass


# How long the EngineCore waits for the workers' dumps and probes.
_DUMP_WAIT_SECONDS = 20


def _stack_dump_request(engine_pid):
    tmp = os.environ.get("TMPDIR", "/tmp")
    return os.path.join(tmp, "vllm-stack-dump-%d" % engine_pid)


def _is_vllm_worker():
    mp = sys.modules.get("multiprocessing")
    return bool(mp and mp.current_process().name.startswith("VllmWorker"))


def _spare_stream(torch):
    # A driver stream of its own: torch.cuda.Stream() comes from a pool that
    # NCCL and vLLM also draw from.
    ctypes = sys.modules["ctypes"]
    handle = ctypes.c_void_p()
    rc = ctypes.CDLL("libcuda.so.1").cuStreamCreate(
        ctypes.byref(handle), _CU_STREAM_NON_BLOCKING
    )
    if rc != _CUDA_SUCCESS:
        raise RuntimeError("cuStreamCreate: CUresult %d" % rc)
    return torch.cuda.ExternalStream(handle.value)


def _count_steps():
    # Wraps vLLM's Worker.execute_model (called once per engine step on every
    # rank) once vLLM has imported it; the worker looks it up on every call.
    gpu_worker = sys.modules.get("vllm.v1.worker.gpu_worker")
    worker = getattr(gpu_worker, "Worker", None)
    if worker is None or not hasattr(worker, "execute_model"):
        return
    real = worker.execute_model

    def execute_model(self, *args, **kwargs):
        _worker_state["count"] += 1
        if not _worker_state["recorded"]:
            _worker_state["recorded"] = True
            try:
                torch = sys.modules["torch"]
                _worker_state["device"] = torch.cuda.current_device()
                _worker_state["stream"] = torch.cuda.current_stream()
                _worker_state["spare"] = _spare_stream(torch)
            except Exception as e:  # reported by the probe
                _worker_state["spare"] = e
        return real(self, *args, **kwargs)

    worker.execute_model = execute_model
    _worker_state["patched"] = True


# x86_64 syscall numbers whose first argument is a file descriptor
_FD_SYSCALLS = {0: "read", 1: "write", 16: "ioctl", 17: "pread64", 18: "pwrite64"}
_SYSCALL_NAMES = {
    **_FD_SYSCALLS,
    7: "poll",
    202: "futex",
    232: "epoll_wait",
    271: "ppoll",
}


def _read(path):
    try:
        with open(path) as f:
            return f.read().strip()
    except OSError as e:
        return "<%s>" % e.strerror


def _thread_states():
    # Kernel-side state of every thread, including native ones (libcuda,
    # NCCL) that faulthandler cannot see.
    states = {}
    for tid in sorted(os.listdir("/proc/self/task"), key=int):
        base = "/proc/self/task/%s/" % tid
        stat = _read(base + "stat")
        fields = stat.rsplit(")", 1)[-1].split()
        syscall = _read(base + "syscall")
        args = syscall.split()
        name = ""
        if args and args[0].isdigit():
            nr = int(args[0])
            name = _SYSCALL_NAMES.get(nr, "")
            if nr in _FD_SYSCALLS and len(args) > 1:
                try:
                    name += " fd %d -> %s" % (
                        int(args[1], 16),
                        os.readlink("/proc/self/fd/%d" % int(args[1], 16)),
                    )
                except (OSError, ValueError):
                    pass
        states[tid] = {
            "comm": _read(base + "comm"),
            "state": fields[0] if fields else "?",
            "utime": int(fields[11]) if len(fields) > 12 else -1,
            "stime": int(fields[12]) if len(fields) > 12 else -1,
            "wchan": _read(base + "wchan"),
            "syscall": args[0] if args else "",
            "syscall_name": name,
        }
    return states


def _ctxt_switches(tid):
    status = _read("/proc/self/task/%d/status" % tid)
    return " ".join(
        line.replace("\t", " ")
        for line in status.splitlines()
        if "ctxt_switches" in line
    )


def _nccl_ras_status(timeout):
    # NCCL's RAS service at its default address: per-rank collective progress.
    socket = sys.modules.get("socket")
    if socket is None:
        return "nccl ras: socket module not loaded"
    chunks, error, limit = [], "", 65536
    try:
        with socket.create_connection(("localhost", 28028), timeout=timeout) as s:
            s.sendall(b"CLIENT PROTOCOL 2\nVERBOSE STATUS\n")
            deadline = time.monotonic() + timeout
            while (got := sum(map(len, chunks))) <= limit:
                s.settimeout(max(0.1, deadline - time.monotonic()))
                chunk = s.recv(min(4096, limit + 1 - got))
                if not chunk:
                    break
                chunks.append(chunk)
    except Exception as e:
        # a socket the server keeps open after answering is fine
        if not (chunks and isinstance(e, TimeoutError)):
            error = " (%r)" % e
    answer = b"".join(chunks)
    if len(answer) > limit:
        error += " (truncated at %d bytes)" % limit
    return "nccl ras%s:\n%s" % (error, answer[:limit].decode(errors="replace"))


def _hang_state():
    # Each part on its own: one failing must not cost the others.
    lines = [
        "engine steps: %s"
        % (
            _worker_state["count"]
            if _worker_state["patched"]
            else "not counted (hook not attached)"
        )
    ]

    def part(fn, *args):
        try:
            return fn(*args)
        except Exception as e:
            lines.append("%s failed: %r" % (fn.__name__, e))

    t0 = time.monotonic()
    first = part(_thread_states)
    main_ctxt = part(_ctxt_switches, os.getpid())
    time.sleep(max(0.0, t0 + 1.0 - time.monotonic()))
    second = part(_thread_states)
    if main_ctxt is not None:
        lines.append(
            "main thread %s -> %s" % (main_ctxt, part(_ctxt_switches, os.getpid()))
        )
    if first is not None and second is not None:
        lines.append(
            "threads (tid comm state d_utime d_stime [clock ticks] wchan syscall), %.1f s apart:"
            % (time.monotonic() - t0)
        )
        for tid, s in second.items():
            f = first.get(tid, s)
            lines.append(
                "  %s %s %s %d %d %s %s %s"
                % (
                    tid,
                    s["comm"],
                    s["state"],
                    s["utime"] - f["utime"],
                    s["stime"] - f["stime"],
                    s["wchan"],
                    s["syscall"],
                    s["syscall_name"],
                )
            )
    return "\n".join(lines) + "\n"


def _write_probes(f):
    # Runs after the dump is committed, safest probe first: a hung probe never
    # returns, and a CUDA call that hangs holding the GIL stops every thread.
    # mem_get_info kept answering in the MEMSTATS poller through past hangs.
    def line(text):
        f.write(text + "\n")
        f.flush()

    mp = sys.modules.get("multiprocessing")
    if mp and mp.current_process().name == "VllmWorker-0":
        line(_nccl_ras_status(5.0))
    torch = sys.modules.get("torch")
    dev, stream, spare = (
        _worker_state["device"],
        _worker_state["stream"],
        _worker_state["spare"],
    )
    if torch is None or dev is None:
        line("cuda probes: no CUDA device recorded")
    else:

        def main_stream_query():
            # driver API through ctypes: releases the GIL while it waits
            ctypes = sys.modules["ctypes"]
            with torch.cuda.device(dev):
                rc = ctypes.CDLL("libcuda.so.1").cuStreamQuery(
                    ctypes.c_void_p(stream.cuda_stream)
                )
            return {_CUDA_SUCCESS: "done", _CUDA_ERROR_NOT_READY: "not ready"}.get(
                rc, "CUresult %d" % rc
            )

        def spare_stream_kernel():
            if isinstance(spare, Exception):
                raise spare
            with torch.cuda.stream(spare):
                torch.zeros(1, device=dev).add_(1)
            spare.synchronize()
            return "done"

        for name, fn in [
            ("mem_get_info", lambda: torch.cuda.mem_get_info(dev)),
            ("main stream query", main_stream_query),
            ("kernel on a spare stream", spare_stream_kernel),
        ]:
            t0 = time.monotonic()
            try:
                result = "ok %r" % (fn(),)
            except Exception as e:
                result = "error %r" % e
            line("cuda probe %s: %s (%.2f s)" % (name, result, time.monotonic() - t0))
    line("probes done")


def _stack_dump_loop():
    # Runs only in spawned multiprocessing children (see the start below) and
    # dumps only in vLLM workers (process name check below).
    # Answers a request of the parent process (the EngineCore, below) with the
    # stacks of all threads and the hang state. A thread and a file, not a
    # signal: in a deployed worker, SIGUSR1 with faulthandler printed nothing.
    # Must not import anything here to avoid deadlocks on interpreter start.
    request = _stack_dump_request(os.getppid())
    dump = "%s.%d" % (request, os.getpid())
    answered = None
    while True:
        time.sleep(0.5)
        try:
            if not _worker_state["patched"] and _is_vllm_worker():
                _count_steps()
            requested = os.stat(request).st_mtime
            if requested == answered:
                continue
            if not _is_vllm_worker():
                continue
            with open(dump + ".tmp", "w") as f:
                faulthandler.dump_traceback(file=f, all_threads=True)
                f.flush()
                try:
                    f.write(_hang_state())
                except Exception as e:
                    f.write("hang state failed: %r\n" % e)
            os.rename(dump + ".tmp", dump)
            answered = requested
            with open(dump + ".probes", "w") as f:
                try:
                    _write_probes(f)
                except Exception as e:
                    f.write("probes failed: %r\n" % e)
        except Exception:
            pass


# multiprocessing's spawn start method runs its children with this flag
if "--multiprocessing-fork" in sys.argv:
    threading.Thread(target=_stack_dump_loop, daemon=True).start()

_real_handle = logging.Logger.handle


def _dump_workers_on_rpc_timeout(self, record):
    # Monkeypatched wrapper for `logging.Logger.handle`.
    # Requests the workers' stack dumps whenever a TimeoutError "RPC call to ...
    # timed out" is logged. vLLM logs it with "EngineCore encountered a fatal
    # error." before shutting the workers down.
    try:
        exc = record.exc_info[1] if record.exc_info else None
        if isinstance(exc, TimeoutError) and str(exc).startswith("RPC call to"):
            import multiprocessing

            workers = sorted(
                (
                    p
                    for p in multiprocessing.active_children()
                    if p.name.startswith("VllmWorker")
                ),
                key=lambda p: p.name,
            )
            if not workers:
                sys.stderr.write("worker stack dump: no VllmWorker child processes\n")
            request = _stack_dump_request(os.getpid())
            for p in workers:  # a late dump of an earlier round
                for suffix in ("", ".probes"):
                    try:
                        os.remove("%s.%d%s" % (request, p.pid, suffix))
                    except OSError:
                        pass
            open(request, "w").close()
            deadline = time.monotonic() + _DUMP_WAIT_SECONDS
            for p in workers:
                dump = "%s.%d" % (request, p.pid)
                # look for the file even after the deadline: one silent worker
                # must not cost the others their dumps
                while not os.path.exists(dump) and time.monotonic() < deadline:
                    time.sleep(0.1)
                try:
                    with open(dump) as f:
                        stacks = f.read()
                    os.remove(dump)
                except OSError:
                    stacks = "no dump within %d s\n" % _DUMP_WAIT_SECONDS
                sys.stderr.write(
                    "worker stack dump: %s pid %d\n%s" % (p.name, p.pid, stacks)
                )
            for p in workers:
                probes_file = "%s.%d.probes" % (request, p.pid)
                probes = ""
                while True:
                    try:
                        with open(probes_file) as f:
                            probes = f.read()
                    except OSError:
                        pass
                    if "probes done\n" in probes or time.monotonic() >= deadline:
                        break
                    time.sleep(0.1)
                if "probes done\n" not in probes:
                    probes += (
                        "probes stopped here: the next probe hung or the %d s wait ended\n"
                        % _DUMP_WAIT_SECONDS
                    )
                sys.stderr.write(
                    "worker probes: %s pid %d\n%s" % (p.name, p.pid, probes)
                )
                try:
                    os.remove(probes_file)
                except OSError:
                    pass
            os.remove(request)
    except Exception as e:  # never break logging
        sys.stderr.write("worker stack dump failed: %r\n" % e)
    return _real_handle(self, record)


logging.Logger.handle = _dump_workers_on_rpc_timeout

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
