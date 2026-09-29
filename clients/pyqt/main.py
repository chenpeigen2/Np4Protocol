"""NP4 匿名聊天 — PyQt6 桌面客户端 (Windows / macOS / Linux)。

Run:  python main.py
Demo: NP4_BOOTSTRAP=<multiaddr> NP4_AUTOCONNECT=1 python main.py
"""

from __future__ import annotations

import os
import sys
from datetime import datetime
from pathlib import Path

from PyQt6.QtCore import QStandardPaths, Qt
from PyQt6.QtWidgets import (
    QApplication,
    QComboBox,
    QFrame,
    QHBoxLayout,
    QLabel,
    QLineEdit,
    QListWidget,
    QListWidgetItem,
    QMainWindow,
    QMessageBox,
    QPushButton,
    QStackedWidget,
    QTextEdit,
    QVBoxLayout,
    QWidget,
)

from np4_worker import BridgeWorker

BOOTSTRAP_ENV = os.environ.get("NP4_BOOTSTRAP", "").strip()
AUTOCONNECT = os.environ.get("NP4_AUTOCONNECT") == "1" and bool(BOOTSTRAP_ENV)
# Multi-instance / self-test hooks:
#   NP4_IDENTITY_PATH    per-instance identity file (default: shared app-data path)
#   NP4_SELFTEST_SEND_TO peer ID — once it appears in the peer list, send one
#                        test message to it and log the result
IDENTITY_ENV = os.environ.get("NP4_IDENTITY_PATH", "").strip()
SELFTEST_TARGET = os.environ.get("NP4_SELFTEST_SEND_TO", "").strip()


def default_identity_path() -> str:
    if IDENTITY_ENV:
        return IDENTITY_ENV
    base = QStandardPaths.writableLocation(
        QStandardPaths.StandardLocation.AppDataLocation
    )
    return str(Path(base) / "np4_identity")


def _stamp() -> str:
    return datetime.now().strftime("%H:%M:%S")


class ConnectPage(QWidget):
    def __init__(self, on_connect) -> None:
        super().__init__()
        self._on_connect = on_connect
        layout = QVBoxLayout(self)
        layout.addStretch(1)

        title = QLabel("NP4 匿名聊天（PyQt6）")
        title.setStyleSheet("font-size: 20px; font-weight: 600;")
        title.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(title)

        intro = QLabel(
            "消息经定长 cell + 洋葱路由 + 批量混洗发送，对方只能看到 anonymous。"
        )
        intro.setAlignment(Qt.AlignmentFlag.AlignCenter)
        intro.setWordWrap(True)
        layout.addWidget(intro)
        layout.addSpacing(24)

        layout.addWidget(QLabel("Bootstrap 节点 multiaddr（云服务器需换成公网 IP）"))
        self.bootstrap = QLineEdit(BOOTSTRAP_ENV)
        self.bootstrap.setPlaceholderText("/ip4/203.0.113.7/tcp/4000/p2p/12D3KooW...")
        layout.addWidget(self.bootstrap)

        layout.addWidget(QLabel("洋葱跳数（需 ≤ 在线 relay 数；单服务器部署用 1）"))
        self.hops = QLineEdit("1")
        layout.addWidget(self.hops)

        self.connect_btn = QPushButton("连接")
        self.connect_btn.clicked.connect(self._go)
        layout.addWidget(self.connect_btn)

        self.status = QLabel("")
        self.status.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(self.status)

        hint = QLabel("提示：尽力送达，对方离线即丢失；“已发送”仅表示已进入匿名队列。")
        hint.setWordWrap(True)
        layout.addWidget(hint)
        layout.addStretch(2)

    def _go(self) -> None:
        bootstrap = self.bootstrap.text().strip()
        if not bootstrap:
            QMessageBox.warning(self, "NP4", "请填写 bootstrap 节点的 multiaddr")
            return
        hops = int(self.hops.text().strip() or "1")
        self.connect_btn.setEnabled(False)
        self.status.setText("正在创建节点…")
        self._on_connect(bootstrap, hops)

    def mark_failed(self, message: str) -> None:
        self.connect_btn.setEnabled(True)
        self.status.setText("")
        QMessageBox.critical(self, "连接失败", message)


class ChatPage(QWidget):
    def __init__(self, peer_id: str, send_callable, refresh_callable) -> None:
        super().__init__()
        self._send_callable = send_callable
        self._refresh_callable = refresh_callable

        layout = QVBoxLayout(self)

        banner = QFrame()
        banner.setStyleSheet("background: palette(alternate-base); border-radius: 6px;")
        banner_layout = QHBoxLayout(banner)
        banner_layout.setContentsMargins(10, 6, 10, 6)
        self.state = QLabel("连接中…")
        peer_label = QLabel(f"我：{peer_id[:20]}…")
        peer_label.setToolTip(peer_id)
        banner_layout.addWidget(self.state)
        banner_layout.addStretch(1)
        banner_layout.addWidget(peer_label)
        layout.addWidget(banner)

        self.messages = QListWidget()
        self.messages.setWordWrap(True)
        self.messages.setSelectionMode(QListWidget.SelectionMode.NoSelection)
        layout.addWidget(self.messages, 1)

        dest_row = QHBoxLayout()
        dest_row.addWidget(QLabel("对方"))
        self.dest = QComboBox()
        self.dest.setEditable(True)
        self.dest.setInsertPolicy(QComboBox.InsertPolicy.NoInsert)
        self.dest.setMinimumContentsLength(26)
        self.dest.setSizeAdjustPolicy(
            QComboBox.SizeAdjustPolicy.AdjustToMinimumContentsLengthWithIcon
        )
        dest_row.addWidget(self.dest, 1)
        refresh_btn = QPushButton("⟳")
        refresh_btn.setFixedWidth(40)
        refresh_btn.setToolTip("刷新在线节点列表")
        refresh_btn.clicked.connect(self._refresh_callable)
        dest_row.addWidget(refresh_btn)
        layout.addLayout(dest_row)

        input_row = QHBoxLayout()
        self.input = QTextEdit()
        self.input.setFixedHeight(56)
        self.input.setPlaceholderText("输入消息… (Ctrl+Enter 发送)")
        input_row.addWidget(self.input, 1)
        self.send_btn = QPushButton("发送")
        self.send_btn.clicked.connect(self.try_send)
        input_row.addWidget(self.send_btn)
        layout.addLayout(input_row)

    def try_send(self) -> None:
        dest = self.dest.currentText().strip()
        text = self.input.toPlainText().strip()
        if not dest or not text:
            return
        self._send_callable(dest, text)

    def mark_online(self, state: str) -> None:
        self.state.setText(state)

    def set_peers(self, peers: list) -> None:
        current = self.dest.currentText().strip()
        self.dest.clear()
        for peer_id, _addrs in peers:
            self.dest.addItem(peer_id)
        if current:
            # Keep a manually typed or previously selected destination.
            self.dest.setCurrentText(current)

    def add_incoming(self, sender: str, content: str) -> None:
        self.messages.addItem(QListWidgetItem(f"{content}\n{_stamp()} · {sender}"))
        self.messages.scrollToBottom()

    def add_outgoing(self, content: str) -> None:
        self.messages.addItem(
            QListWidgetItem(f"{content}\n{_stamp()} · 我（已进入匿名队列）")
        )
        self.messages.scrollToBottom()


class MainWindow(QMainWindow):
    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("NP4 匿名聊天")
        self.resize(640, 560)
        self.worker: BridgeWorker | None = None
        self._chat: ChatPage | None = None
        self._selftest_sent = False

        self.stack = QStackedWidget()
        self.connect_page = ConnectPage(self.start)
        self.stack.addWidget(self.connect_page)
        self.setCentralWidget(self.stack)

        if AUTOCONNECT:
            self.connect_page._go()

    def start(self, bootstrap: str, hops: int) -> None:
        config = {
            "port": 0,
            "identity_path": default_identity_path(),
            "bootstrap": bootstrap,
            "hops": hops,
            "rendezvous": "np4-network",
        }
        self.worker = BridgeWorker(config, self)
        self.worker.node_ready.connect(self._on_node_ready)
        self.worker.state_changed.connect(self._on_state)
        self.worker.message_received.connect(self._on_message)
        self.worker.send_done.connect(self._on_send_done)
        self.worker.peers_ready.connect(self._on_peers)
        self.worker.failed.connect(self._on_failed)
        self.worker.start()

    def _on_node_ready(self, node: dict) -> None:
        peer_id = node["peer_id"]
        print(f"[np4] connected as {peer_id}", flush=True)
        self._chat = ChatPage(
            peer_id,
            send_callable=self.worker.send,
            refresh_callable=self.worker.refresh_peers,
        )
        self.stack.addWidget(self._chat)
        self.stack.setCurrentWidget(self._chat)

    def _on_peers(self, peers: list) -> None:
        if self._chat is not None:
            self._chat.set_peers(peers)
        # Self-test: as soon as the target shows up in the real discovery
        # list, send one message through the app's own send path.
        if SELFTEST_TARGET and not self._selftest_sent:
            if any(pid == SELFTEST_TARGET for pid, _ in peers):
                self._selftest_sent = True
                text = f"selftest from pid {os.getpid()} via peer list"
                print(f"[np4] selftest: sending to {SELFTEST_TARGET}", flush=True)
                if self.worker is not None:
                    self.worker.send(SELFTEST_TARGET, text)

    def _on_state(self, state: str) -> None:
        if self._chat is not None:
            self._chat.mark_online(state)

    def _on_message(self, sender: str, content: str) -> None:
        print(f"[np4] message received from {sender}: {content}", flush=True)
        if self._chat is not None:
            self._chat.add_incoming(sender, content)

    def _on_send_done(self, error: str) -> None:
        if self._chat is None:
            return
        if error:
            QMessageBox.warning(self, "发送失败", error)
            return
        text = self._chat.input.toPlainText().strip()
        if text:
            self._chat.add_outgoing(text)
            self._chat.input.clear()

    def _on_failed(self, message: str) -> None:
        self.stack.setCurrentWidget(self.connect_page)
        self.connect_page.mark_failed(message)

    def closeEvent(self, event) -> None:  # noqa: N802 (Qt naming)
        if self.worker is not None:
            self.worker.shutdown()
            self.worker.wait(3000)
        super().closeEvent(event)


def main() -> int:
    app = QApplication(sys.argv)
    window = MainWindow()
    window.show()
    return app.exec()


if __name__ == "__main__":
    raise SystemExit(main())
