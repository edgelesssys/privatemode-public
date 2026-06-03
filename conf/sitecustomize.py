# sitecustomize.py - auto-loaded by CPython on startup if on sys.path.
#
# Suppresses a narrow, known-noisy print() from the remote-code tokenizer
# path. The conversion failure is benign and does not affect inference, but
# the upstream code emits it via print(), which routes the full traceback
# to stdout. Replace print() with a filter that drops this specific message.
import builtins
import sys

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
