"""NP4 匿名聊天 — PyQt6 桌面客户端 (Windows / macOS / Linux)。

Layering: pages (this file, pure rendering) ← signals ← ChatController
(np4_controller.py, all state & business logic) ← BridgeWorker
(np4_worker.py, native calls on one thread) ← np4bridge (Go).

Run:  python main.py
Demo: NP4_BOOTSTRAP=<multiaddr> NP4_AUTOCONNECT=1 python main.py
"""

from __future__ import annotations

import sys
from datetime import datetime

from PyQt6.QtCore import Qt, pyqtSignal
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

from np4_controller import AUTOCONNECT, BOOTSTRAP_ENV, ChatController, validate_bootstrap


def _stamp() -> str:
    return datetime.now().strftime("%H:%M:%S")


class ConnectPage(QWidget):
    connect_requested = pyqtSignal(str, int)  # bootstrap multiaddr, hops

    def __init__(self) -> None:
        super().__init__()
        layout = QVBoxLayout(self)
        layout.addStretch(1)

        title = QLabel("NP4 匿名聊天（PyQt6）")
        title.setStyleSheet("font-size: 20px; font-weight: 600;")
        title.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(title)

        intro = QLabel("消息经定长 cell + 洋葱路由 + 批量混洗发送，对方只能看到 anonymous。")
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
        self.connect_btn.clicked.connect(self._emit_connect)
        layout.addWidget(self.connect_btn)

        self.status = QLabel("")
        self.status.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(self.status)

        hint = QLabel("提示：尽力送达，对方离线即丢失；“已发送”仅表示已进入匿名队列。")
        hint.setWordWrap(True)
        layout.addWidget(hint)
        layout.addStretch(2)

    def _emit_connect(self) -> None:
        bootstrap = self.bootstrap.text().strip()
        if error := validate_bootstrap(bootstrap):
            QMessageBox.warning(self, "NP4", error)
            return
        try:
            hops = int(self.hops.text().strip() or "1")
        except ValueError:
            QMessageBox.warning(self, "NP4", "跳数必须是数字")
            return
        self.connect_btn.setEnabled(False)
        self.status.setText("正在创建节点…")
        self.connect_requested.emit(bootstrap, hops)

    def set_busy(self, status: str) -> None:
        self.status.setText(status)

    def mark_failed(self, message: str) -> None:
        self.connect_btn.setEnabled(True)
        self.status.setText("")
        QMessageBox.critical(self, "连接失败", message)


class ChatPage(QWidget):
    send_requested = pyqtSignal(str, str)  # dest, text
    refresh_requested = pyqtSignal()

    def __init__(self, peer_id: str) -> None:
        super().__init__()
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
        refresh_btn.clicked.connect(self.refresh_requested)
        dest_row.addWidget(refresh_btn)
        layout.addLayout(dest_row)

        input_row = QHBoxLayout()
        self.input = QTextEdit()
        self.input.setFixedHeight(56)
        self.input.setPlaceholderText("输入消息… (Ctrl+Enter 发送)")
        input_row.addWidget(self.input, 1)
        self.send_btn = QPushButton("发送")
        self.send_btn.clicked.connect(self._emit_send)
        input_row.addWidget(self.send_btn)
        layout.addLayout(input_row)

    def _emit_send(self) -> None:
        dest = self.dest.currentText().strip()
        text = self.input.toPlainText().strip()
        if not dest or not text:
            return
        self.send_requested.emit(dest, text)

    # -- rendering (called from controller signals) ---------------------------

    def set_state(self, state: str) -> None:
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

    def add_outgoing(self, text: str) -> None:
        self.messages.addItem(
            QListWidgetItem(f"{text}\n{_stamp()} · 我（已进入匿名队列）")
        )
        self.messages.scrollToBottom()

    def clear_input(self) -> None:
        self.input.clear()


class MainWindow(QMainWindow):
    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("NP4 匿名聊天")
        self.resize(640, 560)
        self.controller = ChatController(self)
        self._chat: ChatPage | None = None

        self.stack = QStackedWidget()
        self.connect_page = ConnectPage()
        self.stack.addWidget(self.connect_page)
        self.setCentralWidget(self.stack)

        # UI → controller
        self.connect_page.connect_requested.connect(self._on_connect_requested)

        # controller → UI
        self.controller.node_ready.connect(self._on_node_ready)
        self.controller.state_changed.connect(self._on_state)
        self.controller.message_received.connect(self._on_message)
        self.controller.peers_updated.connect(self._on_peers)
        self.controller.send_completed.connect(self._on_send_completed)
        self.controller.connect_failed.connect(self.connect_page.mark_failed)
        self.controller.delivery_risk.connect(self._on_delivery_risk)

        if AUTOCONNECT:
            self.connect_page._emit_connect()

    def _on_connect_requested(self, bootstrap: str, hops: int) -> None:
        self.controller.connect(bootstrap, hops)

    def _on_node_ready(self, node: dict) -> None:
        self._chat = ChatPage(node["peer_id"])
        # UI → controller (chat actions)
        self._chat.send_requested.connect(self.controller.send)
        self._chat.refresh_requested.connect(self.controller.refresh_peers)
        self.stack.addWidget(self._chat)
        self.stack.setCurrentWidget(self._chat)

    def _on_state(self, state: str) -> None:
        if self._chat is not None:
            self._chat.set_state(state)

    def _on_message(self, sender: str, content: str) -> None:
        print(f"[np4] message received from {sender}: {content}", flush=True)
        if self._chat is not None:
            self._chat.add_incoming(sender, content)

    def _on_peers(self, peers: list) -> None:
        if self._chat is not None:
            self._chat.set_peers(peers)

    def _on_send_completed(self, text: str, error: str) -> None:
        if self._chat is None:
            return
        if error:
            QMessageBox.warning(self, "发送失败", error)
            return
        # Echo exactly what was sent — never whatever is in the box now.
        self._chat.add_outgoing(text)
        self._chat.clear_input()

    def _on_delivery_risk(self, dest: str) -> None:
        QMessageBox.warning(
            self,
            "可能无法送达",
            f"对方 {dest[:24]}… 不在当前在线列表中：\n"
            "对方可能已离线，或地址已过期。\n"
            "消息仍会进入匿名队列，但大概率丢失——请从下拉框选择在线的对方。",
        )

    def closeEvent(self, event) -> None:  # noqa: N802 (Qt naming)
        self.controller.shutdown()
        super().closeEvent(event)


def main() -> int:
    app = QApplication(sys.argv)
    window = MainWindow()
    window.show()
    return app.exec()


if __name__ == "__main__":
    raise SystemExit(main())
