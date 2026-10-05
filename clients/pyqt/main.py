"""NP4 匿名聊天 — PyQt6 桌面客户端 (Windows / macOS / Linux)。

Layering: pages (this file, pure rendering) ← signals ← ChatController
(np4_controller.py, all state & business logic) ← BridgeWorker
(np4_worker.py, native calls on one thread) ← np4bridge (Go).

Visual language: deep-space dark surface, a single emerald accent, calm
motion (opacity fades, a breathing status dot, a pulsing shield) — same
as the Flutter client and the dashboard.

Run:  python main.py
Demo: NP4_BOOTSTRAP=<multiaddr> NP4_AUTOCONNECT=1 python main.py
"""

from __future__ import annotations

import os
import sys
from datetime import datetime
from pathlib import Path

from PyQt6.QtCore import (
    QAbstractAnimation,
    QEasingCurve,
    QPropertyAnimation,
    Qt,
    QTimer,
    pyqtSignal,
)
from PyQt6.QtGui import QColor, QIcon, QPalette
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

# -- design tokens (mirror lib/app/theme.dart and the dashboard CSS) --------
BG = "#0A0F1C"
SURFACE = "#121A2C"
SURFACE_HIGH = "#1A2338"
BORDER = "#202C47"
ACCENT = "#34D399"
ACCENT_CONTAINER = "#12301F"
ON_ACCENT = "#052018"
TEXT = "#E9EDF5"
MUTED = "#8B94AB"
FAINT = "#5A6378"
WARN = "#F3B94D"

GLOBAL_QSS = f"""
QMainWindow, QWidget#page {{ background: {BG}; }}
QLabel {{ color: {TEXT}; font-size: 13px; }}
QLabel#title {{ font-size: 24px; font-weight: 700; color: {TEXT}; }}
QLabel#subtitle {{ color: {MUTED}; font-size: 13px; }}
QLabel#hint {{ color: {FAINT}; font-size: 11.5px; }}
QLabel#origin {{ font-size: 11px; font-weight: 600; }}
QLabel#ts {{ color: {FAINT}; font-size: 10px; }}
QLabel#fieldLabel {{ color: {MUTED}; font-size: 12px; font-weight: 600; }}

QFrame#card {{
    background: {SURFACE};
    border: 1px solid {BORDER};
    border-radius: 16px;
}}
QFrame#bubbleMine {{
    background: {ACCENT_CONTAINER};
    border: 1px solid rgba(52, 211, 153, 0.28);
    border-radius: 14px;
}}
QFrame#bubbleTheirs {{
    background: {SURFACE_HIGH};
    border: 1px solid {BORDER};
    border-radius: 14px;
}}
QFrame#mine QLabel#origin {{ color: {ACCENT}; }}
QFrame#theirs QLabel#origin {{ color: {WARN}; }}

QLineEdit, QTextEdit, QComboBox {{
    background: {SURFACE_HIGH};
    color: {TEXT};
    border: 1px solid {BORDER};
    border-radius: 10px;
    padding: 8px 12px;
    font-size: 13px;
    selection-background-color: {ACCENT};
    selection-color: {ON_ACCENT};
}}
QLineEdit:focus, QTextEdit:focus, QComboBox:focus {{
    border: 1px solid {ACCENT};
}}
QComboBox QAbstractItemView {{
    background: {SURFACE};
    color: {TEXT};
    border: 1px solid {BORDER};
    selection-background-color: {ACCENT_CONTAINER};
    selection-color: {ACCENT};
}}

QPushButton {{
    background: {SURFACE_HIGH};
    color: {MUTED};
    border: 1px solid {BORDER};
    border-radius: 10px;
    padding: 8px 14px;
    font-size: 13px;
}}
QPushButton:hover {{ color: {ACCENT}; border-color: rgba(52,211,153,.4); }}
QPushButton:disabled {{ color: {FAINT}; }}
QPushButton#primary {{
    background: {ACCENT};
    color: {ON_ACCENT};
    font-size: 14px;
    font-weight: 600;
    padding: 12px;
    border: none;
    border-radius: 12px;
}}
QPushButton#primary:hover {{ background: #45E0A8; }}
QPushButton#primary:disabled {{ background: {SURFACE_HIGH}; color: {FAINT}; }}
QPushButton#send {{
    background: {ACCENT};
    color: {ON_ACCENT};
    border: none;
    border-radius: 21px;
    font-size: 16px;
    font-weight: 700;
}}
QPushButton#send:pressed {{ background: #2BBF8A; }}

QListWidget {{
    background: transparent;
    border: none;
    outline: none;
}}
QListWidget::item {{ background: transparent; }}
QListWidget::item:selected {{ background: transparent; }}

QScrollBar:vertical {{
    background: transparent; width: 8px; margin: 2px;
}}
QScrollBar::handle:vertical {{
    background: {BORDER}; border-radius: 4px; min-height: 30px;
}}
QScrollBar::add-line, QScrollBar::sub-line {{ height: 0; }}
QFrame#header {{
    background: {SURFACE};
    border: 1px solid {BORDER};
    border-radius: 12px;
}}
QFrame#footer {{
    background: {SURFACE};
    border: 1px solid {BORDER};
    border-radius: 14px;
}}
"""


def _dark_palette(app: QApplication) -> None:
    """Fusion + dark palette so native widgets (menus, dialogs) match too."""
    app.setStyle("Fusion")
    p = QPalette()
    p.setColor(QPalette.ColorRole.Window, QColor(BG))
    p.setColor(QPalette.ColorRole.WindowText, QColor(TEXT))
    p.setColor(QPalette.ColorRole.Base, QColor(SURFACE))
    p.setColor(QPalette.ColorRole.AlternateBase, QColor(SURFACE_HIGH))
    p.setColor(QPalette.ColorRole.Text, QColor(TEXT))
    p.setColor(QPalette.ColorRole.Button, QColor(SURFACE))
    p.setColor(QPalette.ColorRole.ButtonText, QColor(TEXT))
    p.setColor(QPalette.ColorRole.Highlight, QColor(ACCENT))
    p.setColor(QPalette.ColorRole.HighlightedText, QColor(ON_ACCENT))
    p.setColor(QPalette.ColorRole.ToolTipBase, QColor(SURFACE))
    p.setColor(QPalette.ColorRole.ToolTipText, QColor(TEXT))
    p.setColor(QPalette.ColorRole.PlaceholderText, QColor(FAINT))
    app.setPalette(p)


def _fade_in(widget: QWidget, ms: int = 240) -> None:
    """One-shot top-level fade. Window opacity only — no paint hacks; widgets
    inside layouts use _expand_into_list (pure geometry)."""
    widget.setWindowOpacity(0.0)
    anim = QPropertyAnimation(widget, b"windowOpacity", widget)
    anim.setDuration(ms)
    anim.setStartValue(0.0)
    anim.setEndValue(1.0)
    anim.setEasingCurve(QEasingCurve.Type.OutCubic)
    anim.start(QAbstractAnimation.DeletionPolicy.DeleteWhenStopped)


def _expand_into_list(messages: QListWidget, row: QWidget, ms: int = 260) -> None:
    """Row entrance as a vertical expand: pure geometry, no paint effects —
    QGraphicsOpacityEffect inside a QListWidget viewport fights its painter.
    The item's size hint and the row's maximum height are driven together."""
    from PyQt6.QtCore import QSize, QVariantAnimation

    full = row.sizeHint().height()
    width = row.sizeHint().width()
    item = messages.item(messages.count() - 1)
    anim = QVariantAnimation(messages)
    anim.setStartValue(24.0)
    anim.setEndValue(float(max(full, 24)))
    anim.setDuration(ms)
    anim.setEasingCurve(QEasingCurve.Type.OutCubic)

    def apply(value) -> None:
        h = int(value)
        item.setSizeHint(QSize(width, h))
        row.setMaximumHeight(h)

    anim.valueChanged.connect(apply)
    anim.start(QAbstractAnimation.DeletionPolicy.DeleteWhenStopped)


def _pulse_color(widget: QWidget, base: str, glow: str, ms: int = 1600) -> None:
    """Two-state color breathing via a timer — no paint effects involved."""
    state = {"on": False}

    def tick() -> None:
        state["on"] = not state["on"]
        widget.setStyleSheet(
            f"color: {glow if state['on'] else base}; font-size: 11px;")

    timer = QTimer(widget)
    timer.timeout.connect(tick)
    timer.start(ms // 2)
    tick()


def _stamp() -> str:
    return datetime.now().strftime("%H:%M")


class ConnectPage(QWidget):
    connect_requested = pyqtSignal(str, int)  # bootstrap multiaddr, hops

    def __init__(self) -> None:
        super().__init__()
        self.setObjectName("page")
        layout = QVBoxLayout(self)
        layout.setContentsMargins(28, 24, 28, 24)
        layout.addStretch(2)

        # Shield: the brand mark, gently pulsing — the page is alive.
        icon_path = Path(__file__).resolve().parent / "assets" / "icon.png"
        shield = QLabel()
        if icon_path.exists():
            shield.setPixmap(_pixmap_icon(icon_path, 96))
        shield.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(shield)

        title = QLabel("NP4 匿名聊天")
        title.setObjectName("title")
        title.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(title)

        subtitle = QLabel("定长 cell · 洋葱路由 · 批量混洗\n对方看到的只有一个词：anonymous")
        subtitle.setObjectName("subtitle")
        subtitle.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(subtitle)
        layout.addSpacing(22)

        card = QFrame()
        card.setObjectName("card")
        card_layout = QVBoxLayout(card)
        card_layout.setContentsMargins(18, 18, 18, 18)
        card_layout.setSpacing(8)

        field_label = QLabel("Bootstrap 节点")
        field_label.setObjectName("fieldLabel")
        card_layout.addWidget(field_label)
        self.bootstrap = QLineEdit(BOOTSTRAP_ENV)
        self.bootstrap.setPlaceholderText("/ip4/203.0.113.7/tcp/4000/p2p/12D3KooW...")
        card_layout.addWidget(self.bootstrap)

        addr_hint = QLabel("服务器上 ./bootstrap start 输出的地址；云服务器把内网 IP 换成公网 IP")
        addr_hint.setObjectName("hint")
        addr_hint.setWordWrap(True)
        card_layout.addWidget(addr_hint)

        hops_label = QLabel("洋葱跳数")
        hops_label.setObjectName("fieldLabel")
        card_layout.addWidget(hops_label)
        self.hops = QLineEdit("1")
        card_layout.addWidget(self.hops)

        hops_hint = QLabel("需 ≤ 在线 relay 数；bootstrap 兼任 relay 的单服务器部署用 1")
        hops_hint.setObjectName("hint")
        card_layout.addWidget(hops_hint)

        card_layout.addSpacing(10)
        self.connect_btn = QPushButton("进入匿名网络")
        self.connect_btn.setObjectName("primary")
        self.connect_btn.setCursor(Qt.CursorShape.PointingHandCursor)
        self.connect_btn.clicked.connect(self._emit_connect)
        card_layout.addWidget(self.connect_btn)
        layout.addWidget(card)

        layout.addSpacing(14)
        self.status = QLabel("")
        self.status.setObjectName("subtitle")
        self.status.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.status.setWordWrap(True)
        layout.addWidget(self.status)

        hint = QLabel("消息为尽力送达，对方离线即丢失；\n\u201c已发送\u201d仅表示已进入匿名队列。")
        hint.setObjectName("hint")
        hint.setWordWrap(True)
        hint.setAlignment(Qt.AlignmentFlag.AlignCenter)
        layout.addWidget(hint)
        layout.addStretch(3)

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


class SendTextEdit(QTextEdit):
    """QTextEdit that honors the placeholder's promise: Ctrl+Enter sends."""

    send_requested = pyqtSignal()

    def keyPressEvent(self, event) -> None:  # noqa: N802 (Qt naming)
        ctrl = event.modifiers() & Qt.KeyboardModifier.ControlModifier
        if ctrl and event.key() in (
            Qt.Key.Key_Return,
            Qt.Key.Key_Enter,
        ):
            self.send_requested.emit()
            return
        super().keyPressEvent(event)


class Bubble(QFrame):
    """One chat message: attribution line, content, timestamp."""

    def __init__(self, text: str, origin: str, mine: bool, origin_color: str) -> None:
        super().__init__()
        self.setObjectName("bubbleMine" if mine else "bubbleTheirs")
        v = QVBoxLayout(self)
        v.setContentsMargins(12, 8, 12, 5)
        v.setSpacing(2)

        origin_label = QLabel(origin)
        origin_label.setObjectName("origin")
        origin_label.setStyleSheet(f"color: {origin_color}; font-size: 11px; font-weight: 600;")
        v.addWidget(origin_label)

        content = QLabel(text)
        content.setWordWrap(True)
        content.setTextInteractionFlags(Qt.TextInteractionFlag.TextSelectableByMouse)
        content.setStyleSheet(f"color: {TEXT}; font-size: 14px;")
        v.addWidget(content)

        ts = QLabel(_stamp())
        ts.setObjectName("ts")
        ts.setAlignment(Qt.AlignmentFlag.AlignRight)
        v.addWidget(ts)


class MessageRow(QWidget):
    """A list row holding one bubble, aligned right (mine) or left (theirs)."""

    def __init__(self, bubble: Bubble, mine: bool) -> None:
        super().__init__()
        h = QHBoxLayout(self)
        h.setContentsMargins(8, 3, 8, 3)
        if mine:
            h.addStretch(1)
            h.addWidget(bubble, 0, Qt.AlignmentFlag.AlignBottom)
        else:
            h.addWidget(bubble, 0, Qt.AlignmentFlag.AlignBottom)
            h.addStretch(1)


class ChatPage(QWidget):
    send_requested = pyqtSignal(str, str)  # dest, text
    refresh_requested = pyqtSignal()

    def __init__(self, peer_id: str) -> None:
        super().__init__()
        self.setObjectName("page")
        self._peer_id = peer_id
        layout = QVBoxLayout(self)
        layout.setContentsMargins(14, 10, 14, 12)
        layout.setSpacing(10)

        # Header card: breathing status dot + identity.
        header = QFrame()
        header.setObjectName("header")
        header_layout = QHBoxLayout(header)
        header_layout.setContentsMargins(14, 8, 14, 8)
        self.dot = QLabel("●")
        self.dot.setStyleSheet(f"color: {ACCENT}; font-size: 11px;")
        header_layout.addWidget(self.dot)
        self.state = QLabel("连接中…")
        self.state.setStyleSheet(f"color: {MUTED}; font-size: 12px;")
        header_layout.addWidget(self.state)
        header_layout.addStretch(1)
        peer_label = QLabel(f"我 · {peer_id[:18]}…")
        peer_label.setToolTip(peer_id)
        peer_label.setStyleSheet(f"color: {FAINT}; font-size: 11px;")
        header_layout.addWidget(peer_label)
        layout.addWidget(header)
        _pulse_color(self.dot, base=MUTED, glow=ACCENT, ms=1600)

        self.messages = QListWidget()
        self.messages.setWordWrap(True)
        self.messages.setSelectionMode(QListWidget.SelectionMode.NoSelection)
        self.messages.setVerticalScrollMode(QListWidget.ScrollMode.ScrollPerPixel)
        layout.addWidget(self.messages, 1)

        footer = QFrame()
        footer.setObjectName("footer")
        footer_layout = QVBoxLayout(footer)
        footer_layout.setContentsMargins(12, 10, 12, 12)
        footer_layout.setSpacing(8)

        dest_row = QHBoxLayout()
        dest_label = QLabel("对方")
        dest_label.setObjectName("fieldLabel")
        dest_row.addWidget(dest_label)
        self.dest = QComboBox()
        self.dest.setEditable(True)
        self.dest.setInsertPolicy(QComboBox.InsertPolicy.NoInsert)
        self.dest.setMinimumContentsLength(26)
        self.dest.setSizeAdjustPolicy(
            QComboBox.SizeAdjustPolicy.AdjustToMinimumContentsLengthWithIcon
        )
        dest_row.addWidget(self.dest, 1)
        refresh_btn = QPushButton("⟳")
        refresh_btn.setFixedWidth(38)
        refresh_btn.setToolTip("刷新在线节点列表")
        refresh_btn.setCursor(Qt.CursorShape.PointingHandCursor)
        refresh_btn.clicked.connect(self.refresh_requested)
        dest_row.addWidget(refresh_btn)
        footer_layout.addLayout(dest_row)

        input_row = QHBoxLayout()
        self.input = SendTextEdit()
        self.input.send_requested.connect(self._emit_send)
        self.input.setFixedHeight(52)
        self.input.setPlaceholderText("输入消息… (Ctrl+Enter 发送)")
        input_row.addWidget(self.input, 1)
        self.send_btn = QPushButton("↑")
        self.send_btn.setObjectName("send")
        self.send_btn.setFixedSize(42, 42)
        self.send_btn.setCursor(Qt.CursorShape.PointingHandCursor)
        self.send_btn.clicked.connect(self._emit_send)
        input_row.addWidget(self.send_btn)
        footer_layout.addLayout(input_row)
        layout.addWidget(footer)

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

    def _append_row(self, row: MessageRow) -> None:
        item = QListWidgetItem(self.messages)
        from PyQt6.QtCore import QSize
        item.setSizeHint(QSize(row.sizeHint().width(), 24))
        self.messages.addItem(item)
        self.messages.setItemWidget(item, row)
        self.messages.scrollToBottom()
        # Entrance: the bubble expands into place (geometry-only motion).
        _expand_into_list(self.messages, row, ms=260)

    def add_incoming(self, sender: str, content: str, verified: bool) -> None:
        # Sender attribution comes from the pairwise auth tag: a verified
        # message shows which contact sent it, an unverified one stays
        # anonymous and is badged so the reader treats it with suspicion.
        if verified:
            origin = f"✓ 已验证 · {sender[:12]}…"
            color = ACCENT
        else:
            origin = "⚠ 未验证来源"
            color = WARN
        self._append_row(MessageRow(Bubble(content, origin, mine=False, origin_color=color), mine=False))

    def add_outgoing(self, text: str) -> None:
        self._append_row(MessageRow(Bubble(text, "我 · 已进入匿名队列", mine=True, origin_color=MUTED), mine=True))

    def clear_input(self) -> None:
        self.input.clear()


def _pixmap_icon(path: Path, size: int):
    from PyQt6.QtGui import QPixmap

    return QPixmap(str(path)).scaled(
        size, size,
        Qt.AspectRatioMode.KeepAspectRatio,
        Qt.TransformationMode.SmoothTransformation,
    )


class MainWindow(QMainWindow):
    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle("NP4 匿名聊天")
        self.resize(680, 640)
        self.setMinimumSize(520, 520)
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
            QTimer.singleShot(350, self.connect_page._emit_connect)

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

    def _on_message(self, sender: str, content: str, verified: bool) -> None:
        print(f"[np4] message received from {sender} (verified={verified}): {content}", flush=True)
        if self._chat is not None:
            self._chat.add_incoming(sender, content, verified)

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


def _ui_excepthook(exc_type, exc, tb) -> None:
    """PyQt6 calls qFatal (abort) when a slot raises an unhandled exception —
    one bad handler would take the whole app down. Replace the hook so the
    traceback is printed and the app keeps running instead."""
    import traceback

    traceback.print_exception(exc_type, exc, tb, file=sys.stderr)


def main() -> int:
    sys.excepthook = _ui_excepthook
    app = QApplication(sys.argv)
    _dark_palette(app)
    app.setStyleSheet(GLOBAL_QSS)
    # Unified NP4 brand icon (clients/tools/gen_icon.py) — window, taskbar
    # and dock all inherit it.
    icon_path = Path(__file__).resolve().parent / "assets" / "icon.png"
    if icon_path.exists():
        app.setWindowIcon(QIcon(str(icon_path)))
    window = MainWindow()
    window.show()
    _fade_in(window, ms=280)
    # Debug/CI hook: NP4_SCREENSHOT=<path> grabs the window then exits
    # (NP4_SCREENSHOT_DELAY_MS to wait for network choreography first).
    shot = os.environ.get("NP4_SCREENSHOT", "").strip()
    if shot:
        delay = int(os.environ.get("NP4_SCREENSHOT_DELAY_MS", "2500"))
        def _grab() -> None:
            window.grab().save(shot)
            app.quit()
        QTimer.singleShot(delay, _grab)
    return app.exec()


if __name__ == "__main__":
    raise SystemExit(main())
