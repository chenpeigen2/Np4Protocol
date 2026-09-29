# np4_pyqt — PyQt6 桌面客户端

NP4Protocol 的 **Windows / macOS / Linux** 桌面客户端。复用与 Flutter 客户端完全相同的原生库（`go/cmd/np4bridge` 的四符号 JSON ABI），Python 侧用 `ctypes` 直连——协议逻辑零复制、无需任何 SDK/Xcode，解释器直接跑。

> 移动端（Android/iOS）用 `clients/flutter`。

## 运行

```bash
cd clients/pyqt
python3 -m venv .venv && source .venv/bin/activate    # Windows: .venv\Scripts\activate
pip install -r requirements.txt
# 国内加速：pip install -r requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple

python main.py
```

首次运行前需要原生库（任选其一）：

```bash
../flutter/tool/build_native.sh          # 全部平台里能编的都编（自动复用）
NP4_LIB=/path/to/libnp4bridge.dylib python main.py   # 或手动指定
```

加载顺序：环境变量 `NP4_LIB` → `clients/pyqt/native/<os>/` → `clients/flutter/native/<os>/`（开发时复用 Flutter 客户端的产物）。

## 与服务器配对

公网服务器跑 `bootstrap start`（bootstrap 兼任 relay，见 `go/cmd/bootstrap/README.md`），连接页填 multiaddr、hops 填 1。对方 Peer ID 填进输入框上方的栏位即可互发；消息来自 mix，对方显示 **anonymous**。

演示模式（跳过手动连接）：

```bash
NP4_BOOTSTRAP=/ip4/1.2.3.4/tcp/4000/p2p/12D3KooW... NP4_AUTOCONNECT=1 python main.py
```

## 结构

| 文件 | 职责 |
|---|---|
| `np4_bridge.py` | ctypes 绑定：四符号、JSON envelope、错误翻译、库查找 |
| `np4_worker.py` | `QThread` 工作者：所有原生调用在此线程（UI 永不阻塞），25ms 轮询事件 → Qt 信号 |
| `main.py` | 连接页 + 聊天页 UI、匿名横幅、状态栏 |

匿名语义与协议一致：`send` 走 mix 且硬失败，无静默直连回退；"已发送"仅表示进入匿名队列（端到端 ACK 是 [v2]）。

## 打包分发（可选）

PyInstaller 可打出免依赖的可执行文件（原生库用 `--add-binary` 带上）：

```bash
pyinstaller --windowed --name np4chat \
  --add-binary "$(../flutter/tool/build_native.sh macos >/dev/null; ls ../flutter/native/macos/libnp4bridge.dylib)" \
  main.py
```
