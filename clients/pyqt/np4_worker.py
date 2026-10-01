"""QThread worker owning the native bridge — the Qt UI thread never blocks.

Same architecture as the Flutter engine isolate: every ctypes call happens on
this thread; the controller talks to it through signals and a request queue.
"""

from __future__ import annotations

import base64
import queue
import threading
import time

from PyQt6.QtCore import QThread, pyqtSignal

from np4_bridge import Bridge, Np4BridgeError, decode_event

POLL_MS = 25
PEERS_REFRESH_S = 30


class BridgeWorker(QThread):
    node_ready = pyqtSignal(dict)  # {handle, peer_id, addrs}
    state_changed = pyqtSignal(str)
    message_received = pyqtSignal(str, str, bool)  # sender, content, verified
    # dest, text, error ('' = success). Carrying the exact text through the
    # worker lets the UI echo what was SENT, not whatever happens to be in
    # the input box by the time the ack comes back.
    send_done = pyqtSignal(str, str, str)
    peers_ready = pyqtSignal(list)  # [(peer_id, addrs)]
    failed = pyqtSignal(str)

    def __init__(self, config: dict, parent=None) -> None:
        super().__init__(parent)
        self._config = config
        self._requests: "queue.Queue[tuple[str, tuple[str, str]]]" = queue.Queue()
        self._stop = threading.Event()

    def send(self, dest: str, text: str) -> None:
        self._requests.put(("send", (dest, text)))

    def refresh_peers(self) -> None:
        self._requests.put(("refresh_peers", ("", "")))

    def shutdown(self) -> None:
        self._stop.set()

    def run(self) -> None:
        try:
            bridge = Bridge()
        except Np4BridgeError as e:
            self.failed.emit(str(e))
            return
        try:
            node = bridge.create(self._config)
            handle = node["handle"]
            self.node_ready.emit(node)
            self.state_changed.emit("正在向 DHT 发布密钥…")
            bridge.call(handle, "publish_keys")
            self.state_changed.emit("在线（匿名模式，尽力送达）")
            self._refresh_peers(bridge, handle)
            last_peers = time.monotonic()
            while not self._stop.is_set():
                try:
                    kind, (dest, text) = self._requests.get_nowait()
                except queue.Empty:
                    kind, dest, text = None, "", ""
                if kind == "send":
                    error = ""
                    try:
                        content = base64.b64encode(text.encode()).decode()
                        bridge.call(handle, "send", {"dest": dest, "content_b64": content})
                    except Np4BridgeError as e:
                        error = str(e)
                        print(f"[np4] send to {dest[:20]}… failed: {error}", flush=True)
                    self.send_done.emit(dest, text, error)
                elif kind == "refresh_peers":
                    self._refresh_peers(bridge, handle)
                    last_peers = time.monotonic()
                events, _dropped = bridge.poll(handle)
                for ev in events:
                    sender, content, verified = decode_event(ev)
                    self.message_received.emit(sender, content, verified)
                if time.monotonic() - last_peers > PEERS_REFRESH_S:
                    self._refresh_peers(bridge, handle)
                    last_peers = time.monotonic()
                self.msleep(POLL_MS)
            bridge.stop(handle)
        except Np4BridgeError as e:
            self.failed.emit(str(e))

    def _refresh_peers(self, bridge: Bridge, handle: int) -> None:
        try:
            res = bridge.call(handle, "list_peers")
        except Np4BridgeError:
            return
        peers = [
            (p["peer_id"], p.get("addrs", []), p.get("is_relay", False))
            for p in res.get("peers", [])
        ]
        relays = sum(1 for *_, r in peers if r)
        print(f"[np4] peers online: {len(peers) - relays} contacts, {relays} relays", flush=True)
        self.peers_ready.emit(peers)
