# AGENTS.md

本文件是 agent 与协作者在本仓库工作的强制规范。`CLAUDE.md` 是它的子集视图。

## 协议修改强制清单（`go/` 下任何改动都必须执行）

背景教训（2026-09-30 实录）：Go 侧修好了 bug，但 GUI 客户端窗口还在跑旧的
dylib，出现"客户端根本没有效果"的假象；同日还发现一处"修复"实际无效
（kad-dht 的 discovery.TTL 不控制 provider 记录有效期）。**协议代码与客户端
产物不同步、以及"修复未验证到真实效果"，是本仓库最高频的两类事故。**

每次修改 `go/` 下任何代码，按顺序完成以下全部步骤，缺一不可：

### 1. 重新构建所有原生产物

GUI 客户端加载的是**编译进 dylib/dll/so 里的 Go 协议栈**——不重建，客户端
就永远跑旧协议：

```bash
clients/flutter/tool/build_native.sh        # Android 3 ABI + macOS dylib（Windows/Linux 有工具链时一并）
cd go && go build -o bin/bootstrap ./cmd/bootstrap/ && go build -o bin/np4cli ./cmd/np4cli/
```

冒烟测试用的 bootstrap/np4cli 二进制也必须基于最新代码重建。

### 2. 测试全绿（顺序执行）

```bash
go build ./... && go vet ./...
go test -race -skip 'TestMDNSDiscovery' ./pkg/identity/ ./pkg/auth/ ./pkg/cell/ \
  ./pkg/onion/ ./pkg/mix/ ./pkg/message/ ./pkg/pathsel/ ./pkg/bridge/ \
  ./pkg/p2p/ ./cmd/bootstrap/
go test ./pkg/np4/            # ~180s 重量级集成测试，放后台跑
```

触碰 `cell/`、`onion/`、`wire`、`pathsel` 解析、`auth` 的路径时，另跑对应
fuzz 目标各 ≥15s：`go test -fuzz FuzzWireUnwrap -fuzztime 15s ./pkg/onion/`、
`go test -fuzz FuzzParseRotationRecord -fuzztime 15s ./pkg/pathsel/`、
`go test -fuzz FuzzTagVerify -fuzztime 15s ./pkg/auth/` 等。

CI（`.github/workflows/ci.yml`）在每个 push/PR 上自动执行同一矩阵
（Go 三段 + Flutter analyze/test + PyQt 编译冒烟）——本地跳过的步骤
CI 会补跑。改 CI 配置后观察 GitHub Actions 首跑再收工。

### 3. 桥 ABI 纪律

四个符号（`np4_create / np4_call / np4_stop / np4_free`）**永不增减**；加功能
= 加 JSON 字段。任何桥层变更必须同时改两个客户端（`clients/pyqt` +
`clients/flutter`），并回到第 1 步重建。

### 4. 端到端验证到"日志里看到送达"

单元测试全绿 ≠ 系统工作。起本地 bootstrap + 两个客户端（或 `np4cli send`），
在接收端日志里确认 `[np4] message received`，才算验证完成。客户端发送失败
会写日志（`[np4] send ... failed`），排查先看日志。

### 5. spec 同步

语义变化（报错含义、节点角色、wire/peer 行为、超时参数）必须同步
`docs/protocol.md`，使用其 [impl]/[must]/[v2]/[cut] 标记体系。

### 6. 提交即推送

commit 后立即 `git push origin main`；工作区与远端任何时刻保持同步。

## 测试环境须知（踩过的坑）

- **`-race` 的边界（2026-10-05 更新）**：全部轻量包 + `pkg/p2p` +
  `cmd/bootstrap` 现在都跑 `-race`（CI 同步）；`pkg/np4` 全量加 `-race`
  约 3.5 分钟且**当前零竞争**（第九轮审计 206s 通过）——重大并发改动后
  值得本地跑一次全量 race 审计。
- **`TestMDNSDiscovery` 在 macOS 偶发超时**（组播环境抖动）：失败先单独重跑
  再排查，不要当真回归。
- **Android 构建必须加 `-ldflags="-checklinkname=0"`**：libp2p 的
  wlynxg/anet 依赖 Go 1.23+ 默认拒绝的 linkname，不加必然链接失败。
- **kad-dht 的 `discovery.TTL` 不控制 provider 记录有效期**——它只是
  Advertise 的返回值；真实有效期由 `ProviderManagerOpts(ProvideValidity(...))`
  设定（当前 5min，见 `go/pkg/p2p/discovery.go`）。改 rendezvous 逻辑前先读
  该文件注释。
- **Flutter 桌面构建无法从 macOS 交叉编译**；Windows/Linux 应用层测试需在
  对应系统或 CI 上做。
- **演示/开发环境身份放 `~/.np4demo`，不要放 `/tmp`**：macOS 会清理
  /tmp，身份文件丢失 = Peer ID 变化 = 全部客户端地址作废（2026-10-05 实录）。
- **PyQt 运行用 `clients/pyqt/.venv/bin/python`**（系统 python3 无 PyQt6）；
  `main.py` 已装 excepthook——slot 异常打 traceback 不再 qFatal 崩进程
  （此前两次窗口崩溃正是 PyQt slot 未捕获异常 abort 所致）。
- **QListWidget 的 item widget 上禁用 QGraphicsOpacityEffect**（与 viewport
  painter 冲突，刷 QPainter not active）：入场动效用
  `_expand_into_list`（纯几何高度展开）。
- **UI 验收截图**：PyQt 用 `NP4_SCREENSHOT=<path>`（配
  `NP4_SCREENSHOT_DELAY_MS`），模拟器用 `adb exec-out screencap`。
- **国内网络镜像**：Go 用 GOPROXY=goproxy.cn；Flutter SDK/pub 用
  flutter-io.cn；gradle 用阿里云/腾讯镜像（已焊入 clients/flutter 的
  gradle 配置与 deploy/Dockerfile 的 GOPROXY 参数）。
- 客户端 UI 测试策略：PyQt6 用 QThread worker 隔离原生调用 + 演示钩子
  （`NP4_BOOTSTRAP`/`NP4_AUTOCONNECT`/`NP4_IDENTITY_PATH`/
  `NP4_SELFTEST_SEND_TO`）；Flutter 用 FakeTransport；不依赖原生库。
