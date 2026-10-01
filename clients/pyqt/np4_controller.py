"""ChatController — owns the worker, the demo hooks, and all chat state.

The UI layer (main.py) only renders signals it hears from here; every
mutation of chat state flows through this controller. This keeps main.py
free of business logic and makes the environment-driven self-test hooks a
controller concern instead of something smeared across widgets.
"""

from __future__ import annotations

import os
import re
import sys
import threading

from PyQt6.QtCore import QLockFile, QObject, QStandardPaths, pyqtSignal

from np4_worker import BridgeWorker

BOOTSTRAP_ENV = os.environ.get("NP4_BOOTSTRAP", "").strip()
AUTOCONNECT = os.environ.get("NP4_AUTOCONNECT") == "1" and bool(BOOTSTRAP_ENV)
IDENTITY_ENV = os.environ.get("NP4_IDENTITY_PATH", "").strip()
SELFTEST_TARGET = os.environ.get("NP4_SELFTEST_SEND_TO", "").strip()

_MULTIADDR_RE = re.compile(r"^/ip[46]/.+/tcp/\d+/p2p/.+$")


def default_identity_path() -> str:
    if IDENTITY_ENV:
        return IDENTITY_ENV
    base = QStandardPaths.writableLocation(
        QStandardPaths.StandardLocation.AppDataLocation
    )
    return os.path.join(base, "np4_identity")


def validate_bootstrap(addr: str) -> str | None:
    """Returns an error message, or None if the multiaddr is plausible."""
    if not addr:
        return "请填写 bootstrap 节点的 multiaddr"
    if not _MULTIADDR_RE.match(addr):
        return "multiaddr 形如 /ip4/1.2.3.4/tcp/4000/p2p/12D3KooW...（云服务器记得换成公网 IP）"
    return None


class ChatController(QObject):
    node_ready = pyqtSignal(dict)  # {handle, peer_id, addrs}
    state_changed = pyqtSignal(str)
    message_received = pyqtSignal(str, str)  # sender, content
    peers_updated = pyqtSignal(list)  # [(peer_id, addrs)]
    send_completed = pyqtSignal(str, str)  # text, error ('' = success)
    # The destination is not in the current online list: delivery is
    # best-effort and this message will most likely be lost. UI should warn.
    delivery_risk = pyqtSignal(str)  # dest
    connect_failed = pyqtSignal(str)

    def __init__(self, parent=None) -> None:
        super().__init__(parent)
        self._worker: BridgeWorker | None = None
        self._selftest_sent = False
        self._selftest_lock = threading.Lock()
        self._contacts: list[str] = []  # online, deduped, relay-free
        self._lock: QLockFile | None = None

    # -- lifecycle -----------------------------------------------------------

    def connect(self, bootstrap: str, hops: int) -> None:
        if self._worker is not None:
            return  # already connected
        identity_path = default_identity_path()
        # One identity, one window: two processes sharing an identity file
        # produce one peer ID twice — messages split unpredictably.
        self._lock = QLockFile(identity_path + ".lock")
        if not self._lock.tryLock(0):
            self.connect_failed.emit(
                "该身份已被另一个窗口使用。\n多开请为每个窗口设置独立的 NP4_IDENTITY_PATH。"
            )
            return
        config = {
            "port": 0,
            "identity_path": identity_path,
            "bootstrap": bootstrap,
            "hops": hops,
            "rendezvous": "np4-network",
        }
        self._worker = BridgeWorker(config, self)
        self._worker.node_ready.connect(self._on_node_ready)
        self._worker.state_changed.connect(self.state_changed)
        self._worker.message_received.connect(self.message_received)
        self._worker.peers_ready.connect(self._on_peers)
        self._worker.send_done.connect(self._on_send_done)
        self._worker.failed.connect(self._on_failed)
        self._worker.start()

    def shutdown(self) -> None:
        if self._worker is not None:
            self._worker.shutdown()
            self._worker.wait(3000)
            self._worker = None

    # -- user actions --------------------------------------------------------

    def send(self, dest: str, text: str) -> None:
        if self._worker is None:
            self.send_completed.emit(text, "尚未连接")
            return
        if dest not in self._contacts:
            # Silent-loss territory: the mix accepts the packet, but a dead
            # or stale destination cannot be dialed — say so up front.
            self.delivery_risk.emit(dest)
        self._worker.send(dest, text)

    def refresh_peers(self) -> None:
        if self._worker is not None:
            self._worker.refresh_peers()

    # -- worker slots --------------------------------------------------------

    def _on_node_ready(self, node: dict) -> None:
        print(f"[np4] connected as {node['peer_id']}", flush=True)
        self.node_ready.emit(node)

    def _on_peers(self, peers: list) -> None:
        # Relays are infrastructure, not chat contacts — keep them out of the
        # destination picker (messaging the sole relay is impossible anyway;
        # the protocol now says so explicitly). Dedupe by peer ID: duplicate
        # records must not stack up in the picker.
        contacts: list[tuple[str, list]] = []
        seen: set[str] = set()
        for pid, addrs, is_relay in peers:
            if is_relay or pid in seen:
                continue
            seen.add(pid)
            contacts.append((pid, addrs))
        self._contacts = [pid for pid, _ in contacts]
        self.peers_updated.emit(contacts)
        self._maybe_selftest(contacts)

    def _on_send_done(self, dest: str, text: str, error: str) -> None:
        self.send_completed.emit(text, error)

    def _on_failed(self, message: str) -> None:
        self.shutdown()
        self.connect_failed.emit(message)

    # -- self-test hook ------------------------------------------------------

    def _maybe_selftest(self, peers: list) -> None:
        """NP4_SELFTEST_SEND_TO: once the target shows up in the real
        discovery list, send one message through this controller's own send
        path (drives the unattended two-window test)."""
        if not SELFTEST_TARGET or self._selftest_sent:
            return
        if not any(pid == SELFTEST_TARGET for pid, _ in peers):
            return
        with self._selftest_lock:
            if self._selftest_sent:
                return
            self._selftest_sent = True
        text = f"selftest from pid {os.getpid()} via peer list"
        print(f"[np4] selftest: sending to {SELFTEST_TARGET}", flush=True)
        self.send(SELFTEST_TARGET, text)


def log_stderr(message: str) -> None:
    print(message, file=sys.stderr, flush=True)
