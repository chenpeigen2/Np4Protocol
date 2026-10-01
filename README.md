# Np4Protocol

基于 **Mixnet（混洗网络）** 的匿名通信协议，以元数据保护为核心目标：通过对定长 cell 进行批量混洗、延迟与洋葱式逐跳加密，为收发双方提供匿名性，同时保证消息**可验证归属**（匿名但可验真）。

完整协议规范见 **[docs/protocol.md](docs/protocol.md)**（实现状态以 `[impl]`/`[must]`/`[v2]`/`[cut]` 标记声明）。

## 核心特性

- **元数据保护**：定长 4096B cell + 恒定 8192B wire 包，外部观察者只见恒定尺寸、恒定节奏的流量。
- **洋葱加密**：X25519 + HKDF-SHA256 + ChaCha20-Poly1305，逐跳剥离；TTL 逐跳递减防循环 DoS；eph_pub LRU 缓存防重放。
- **混洗（Mix）**：发送端 10 条 / 500ms，中间 relay 10 条 / 200ms 批量混洗。已度量基线：中间 relay 时序关联攻击 precision 0.45，与 chance 水平（0.37 ± 0.08）在统计噪声内。
- **可否认的 sender-auth**：pairwise ECDH → HMAC-128 标签；接收端遍历联系人常量时间比对，验证通过标 `Verified=true`，未验证消息照常投递并显示显式徽标（不静默丢弃）。
- **威胁模型**：对抗**主动 relay**（重放/篡改/说谎/拒绝转发）；网络观察者保持被动（不防 GPA 全局时序分析）。已知边界与 [v2] 规划见 spec。

## 架构

```
┌──────────────────────────────────────────────┐
│  应用层   │  消息（异步）；presence/离线 [v2]  │
├──────────────────────────────────────────────┤
│  认证层   │  sender-auth tag：ECDH→HMAC-128  │
├──────────────────────────────────────────────┤
│  Cell 层  │  定长 4096B cell                 │
├──────────────────────────────────────────────┤
│  匿名层   │  MixEngine 批量混洗               │
├──────────────────────────────────────────────┤
│  洋葱层   │  定长填充；TTL；replay 缓存       │
├──────────────────────────────────────────────┤
│  加密层   │  X25519 + HKDF-SHA256 + ChaCha20 │
├──────────────────────────────────────────────┤
│  传输层   │  libp2p TCP + Noise + yamux；    │
│          │  DHT rendezvous + mDNS 发现      │
└──────────────────────────────────────────────┘
```

**节点角色**：Client（收发消息）、Relay（`ServeRelay` 广告 + mix）、Bootstrap（DHT 种子节点，可兼任 mix relay——单服务器部署模式下 NAT 后的客户端无需公网 IP 即可接收）。

## 仓库结构

```
go/            Go 协议栈（核心实现）
  cmd/bootstrap/  DHT 种子节点 + 可选 mix relay（含 /api/directory 地址簿）
  cmd/np4cli/     CLI 客户端（chat / connect / id / path / peers / relay / send）
  cmd/np4bridge/  桥接二进制（供桌面/移动客户端嵌入）
  pkg/            cell / onion / mix / message / identity / pathsel /
                  auth / p2p / np4 / bridge / proto
clients/
  flutter/     Flutter 桌面 + Android 客户端（加载编译进 dylib/so 的原生协议栈）
  pyqt/        PyQt6 客户端（QThread worker 隔离原生调用）
deploy/        Dockerfile + docker-compose 单服务器部署
docs/          协议规范（protocol.md）与计划文档
proto/         共享 proto/消息定义
```

## 快速开始

要求：Go 1.26+，Go 模块代理建议 `GOPROXY=https://goproxy.cn,direct`（国内网络）。

```bash
# 1. 构建
cd go
go build -o bin/bootstrap ./cmd/bootstrap/
go build -o bin/np4cli    ./cmd/np4cli/

# 2. 启动 bootstrap（单服务器模式：DHT 种子 + relay + 准入/限速可选）
./bin/bootstrap start --port 4000 --identity ./boot.id --web 8080
#    可选准入：--allowlist peers.txt（每行一个 peer ID，热加载）
#    可选限速：--relay-rate 10（per-peer 令牌桶 cell/s，0=不限）

# 3. 两个终端各起一个 CLI 客户端
./bin/np4cli connect /ip4/127.0.0.1/tcp/4000/p2p/<bootstrap-peer-id> --hops 1
./bin/np4cli chat --self <peer-id-A>
./bin/np4cli send <peer-id-B> "hello over the mix"
```

端到端验证标准：接收端日志出现 `[np4] message received`（客户端发送失败会写 `[np4] send ... failed`，排查先看日志）。

更多：`np4cli` 各子命令见 [go/cmd/np4cli/README.md](go/cmd/np4cli/README.md)；bootstrap 参数见 [go/cmd/bootstrap/README.md](go/cmd/bootstrap/README.md)；容器化部署见 [deploy/README.md](deploy/README.md)。

## GUI 客户端

GUI 客户端加载的是**编译进 dylib/dll/so 的 Go 协议栈**——修改 `go/` 后必须重建原生库，否则客户端永远跑旧协议：

```bash
clients/flutter/tool/build_native.sh   # Android 3 ABI + macOS dylib（Windows/Linux 有工具链时一并）
```

桥 ABI 纪律：四个符号（`np4_create / np4_call / np4_stop / np4_free`）永不增减；加功能 = 加 JSON 字段。客户端 UI 测试不依赖原生库（Flutter 用 FakeTransport；PyQt 用演示钩子环境变量）。

## 测试

```bash
cd go
go build ./... && go vet ./...
go test -race ./pkg/cell/ ./pkg/onion/ ./pkg/mix/ ./pkg/message/ ./pkg/pathsel/ ./pkg/identity/ ./pkg/bridge/
go test ./pkg/np4/            # ~100s 重量级集成测试，禁止加 -race（不收敛）
```

注意：`pkg/np4`、`pkg/p2p` 的 libp2p 集成测试加 `-race` 不收敛；`TestMDNSDiscovery` 在 macOS 偶发超时（组播抖动，失败先单独重跑）；Android 构建必须加 `-ldflags="-checklinkname=0"`。

## 协议常量速查

| 参数 | 值 | 说明 |
|---|---|---|
| `cell_size` | 4096 B | cell 定长（协议常量） |
| `WireSize` | 8192 B | 链路恒定包尺寸 |
| 最大 content | 4061 B | 更大内容 [v2] 分块 |
| 入口 batch | 10 条 / 500 ms | 发送端 |
| relay batch | 10 条 / 200 ms | 中间 relay |
| replay 缓存 | 100k 条 LRU | ~3.2 MB |
| `MaxInitialTTL` | 25 | 防无限循环 |
| 延迟预算 | ≈ 1.1 s | 最坏 500ms + 3×200ms |

错误码、key 记录格式（`/np4/ecdh/<base32(peerID)>`，validator 强制 ID 绑定防 DHT 毒化）、准入与限速语义等详见 [docs/protocol.md](docs/protocol.md)。

## 贡献与开发规范

`go/` 下任何改动必须按序完成（详见 [AGENTS.md](AGENTS.md)）：

1. 重建所有原生产物（`build_native.sh` + bootstrap/np4cli 二进制）；
2. 测试全绿（含触碰解码路径时的 fuzz ≥30s）；
3. 桥 ABI 纪律（四符号不变，两客户端同步改）；
4. 端到端验证到「日志里看到送达」；
5. 语义变化同步 `docs/protocol.md`；
6. 提交即推送，工作区与远端任何时刻保持同步。

TDD：新行为与缺陷修复一律先写失败测试再实现，垂直切片推进，经行为观测断言于既有公共接缝。

## License

[LICENSE](LICENSE)
