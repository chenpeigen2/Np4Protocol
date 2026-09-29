"""QThread worker owning the native bridge — the Qt UI thread never blocks.

Same architecture as the Flutter engine isolate: every ctypes call happens on
this thread; the UI talks to it through signals and a request queue.
"""

from __future__ import annotations

import base64
import queue
import threading

from PyQt6.QtCore import QThread, pyqtSignal

from np4_bridge import Bridge, Np4BridgeError, decode_event

POLL_MS = 25


class BridgeWorker(QThread):
    node_ready = pyqtSignal(dict)  # {handle, peer_id, addrs}
    state_changed = pyqtSignal(str)
    message_received = pyqtSignal(str, str)  # sender, content
    send_done = pyqtSignal(str)  # error text, empty on success
    failed = pyqtSignal(str)

    def __init__(self, config: dict, parent=None) -> None:
        super().__init__(parent)
        self._config = config
        self._requests: "queue.Queue[tuple[str, tuple[str, str]]]" = queue.Queue()
        self._stop = threading.Event()

    def send(self, dest: str, text: str) -> None:
        self._requests.put(("send", (dest, text)))

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
            while not self._stop.is_set():
                try:
                    kind, (dest, text) = self._requests.get_nowait()
                except queue.Empty:
                    kind = None
                if kind == "send":
                    try:
                        content = base64.b64encode(text.encode()).decode()
                        bridge.call(handle, "send", {"dest": dest, "content_b64": content})
                        self.send_done.emit("")
                    except Np4BridgeError as e:
                        self.send_done.emit(str(e))
                events, _dropped = bridge.poll(handle)
                for ev in events:
                    sender, content = decode_event(ev)
                    self.message_received.emit(sender, content)
                self.msleep(POLL_MS)
            bridge.stop(handle)
        except Np4BridgeError as e:
            self.failed.emit(str(e))
