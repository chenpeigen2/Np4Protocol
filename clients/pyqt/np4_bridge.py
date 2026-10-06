"""ctypes bindings for the np4bridge native library (go/cmd/np4bridge).

The same four-symbol JSON contract the Flutter client uses (see
clients/flutter/lib/bridge/np4_ffi_raw.dart): every call is synchronous and
returns {"ok": bool, "result"/"error"}; content bytes travel base64-encoded.
Events are polled — pointers are only ever dereferenced inside a call.
"""

from __future__ import annotations

import base64
import ctypes
import json
import os
import platform
import sys
from pathlib import Path

_LIB_NAMES = {
    "Darwin": "libnp4bridge.dylib",
    "Windows": "np4bridge.dll",
    "Linux": "libnp4bridge.so",
}


class Np4BridgeError(RuntimeError):
    """Raised when the bridge envelope carries ok=false."""


def _candidate_dirs() -> list[Path]:
    here = Path(__file__).resolve().parent
    dirs = [here / "native"]
    # PyInstaller bundle: --add-binary lands the dylib under np4bridge/, but
    # WHERE depends on the PyInstaller major: 6.x onedir puts deps in
    # _internal/ while sys._MEIPASS points at the app top-level. Cover both.
    meipass = getattr(sys, "_MEIPASS", None)
    if meipass:
        base = Path(meipass)
        dirs.append(base / "np4bridge")
        dirs.append(base / "_internal" / "np4bridge")
    sub = {"Darwin": "macos", "Windows": "windows", "Linux": "linux"}.get(platform.system())
    if sub:
        dirs.append(here / "native" / sub)
        # Dev convenience: reuse the Flutter client's already-built library.
        dirs.append(here / ".." / "flutter" / "native" / sub)
    dirs.append(Path.cwd())
    return dirs


def load_bridge() -> ctypes.CDLL:
    name = _LIB_NAMES.get(platform.system())
    if name is None:
        raise Np4BridgeError(f"unsupported platform: {platform.system()} (iOS/Android 用 Flutter 客户端)")
    if env := os.environ.get("NP4_LIB"):
        return ctypes.CDLL(env)
    for d in _candidate_dirs():
        p = d / name
        if p.exists():
            return ctypes.CDLL(str(p))
    raise Np4BridgeError(
        f"{name} not found — run clients/flutter/tool/build_native.sh (或设置 NP4_LIB 指向库文件)"
    )


class Bridge:
    def __init__(self) -> None:
        lib = load_bridge()
        lib.np4_create.argtypes = [ctypes.c_char_p, ctypes.c_int]
        lib.np4_create.restype = ctypes.c_void_p
        lib.np4_call.argtypes = [ctypes.c_int64, ctypes.c_char_p, ctypes.c_int]
        lib.np4_call.restype = ctypes.c_void_p
        lib.np4_stop.argtypes = [ctypes.c_int64]
        lib.np4_stop.restype = None
        lib.np4_free.argtypes = [ctypes.c_void_p]
        lib.np4_free.restype = None
        self._lib = lib

    def _invoke(self, fn, payload: str) -> dict:
        raw = payload.encode()
        p = fn(raw, len(raw))
        try:
            text = ctypes.string_at(p)  # copy BEFORE freeing
        finally:
            self._lib.np4_free(p)
        env = json.loads(text)
        if not env.get("ok"):
            raise Np4BridgeError(env.get("error", "unknown error"))
        return env.get("result") or {}

    def create(self, config: dict) -> dict:
        return self._invoke(self._lib.np4_create, json.dumps(config))

    def call(self, handle: int, method: str, args: dict | None = None) -> dict:
        return self._invoke(
            lambda raw, n: self._lib.np4_call(handle, raw, n),
            json.dumps({"method": method, "args": args or {}}),
        )

    def stop(self, handle: int) -> None:
        self._lib.np4_stop(handle)

    def poll(self, handle: int) -> tuple[list[dict], int]:
        res = self.call(handle, "poll")
        return res.get("events", []), res.get("dropped", 0)


def decode_event(ev: dict) -> tuple[str, str, bool]:
    """Returns (sender, content, verified). verified=True means the message
    carried a valid sender-auth tag attributable to a known contact; the
    sender field is then that contact's peer ID, else 'anonymous'."""
    return (
        ev.get("sender", "anonymous"),
        base64.b64decode(ev.get("content_b64", "")).decode("utf-8", "replace"),
        bool(ev.get("verified", False)),
    )
