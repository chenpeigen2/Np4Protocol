# np4cli

Np4Protocol P2P 匿名通信客户端。基于 libp2p，使用 Noise 协议加密传输（X25519 + ChaCha20-Poly1305）；消息经**定长 cell + 洋葱分层 + 入口/中继两侧 MixEngine 批量混洗**路由，默认永不回退到明文直连。

## 构建

```bash
cd go
go build -o bin/np4cli ./cmd/np4cli/
go build -o bin/bootstrap ./cmd/bootstrap/
```

## 快速开始

最小可用网络 = 1 bootstrap + N 个 relay（N ≥ `--hops`，默认 3）+ 通信双方。

### 1. 启动 bootstrap 节点

```bash
./bin/bootstrap start --port 4000 --web 0 --identity ./boot.id
# 记录输出中的 Peer ID 和 multiaddr
```

### 2. 启动 relays（示例开 3 个，与默认 --hops 3 匹配）

```bash
BOOT=/ip4/127.0.0.1/tcp/4000/p2p/<boot-peer-id>
./bin/np4cli --port 4001 --bootstrap $BOOT --identity ./ra.id relay
./bin/np4cli --port 4002 --bootstrap $BOOT --identity ./rb.id relay
./bin/np4cli --port 4003 --bootstrap $BOOT --identity ./rc.id relay
```

### 3. 双方进入 chat

```bash
# 终端 A（chat 启动时会自动向 DHT 发布 key，使自己可被寻址）
./bin/np4cli --port 4004 --bootstrap $BOOT --identity ./a.id chat

# 终端 B
./bin/np4cli --port 4005 --bootstrap $BOOT --identity ./b.id chat
```

### 4. 发送

终端 A 中（用 B 的 Peer ID）：
```
> send 12D3KooW... 你好，B
Sent (mix) to 12D3KooW...
```

终端 B 中：
```
[14:32:01] anonymous: 你好，B
```

注意显示的发送者是 `anonymous`——这是匿名性的体现。

## 子命令

| 命令 | 说明 |
|------|------|
| `np4cli id` | 显示本节点的 Peer ID 和地址 |
| `np4cli peers` | 通过 DHT rendezvous 发现在线节点 |
| `np4cli relay` | 作为 mix relay 运行（在 DHT 广告，参与路径选择） |
| `np4cli send <peer-id> <消息>` | 走 mix 发送；`--insecure` 显式直连（**无匿名性**，输出有 WARNING） |
| `np4cli chat` | 交互式聊天；mix 模式自动发布 key；`--insecure` 直连模式 |

## 全局参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--port` | `0`（随机） | TCP 监听端口 |
| `--bootstrap` | 无 | Bootstrap 节点的 multiaddr（启用 DHT；**mix 模式必填**） |
| `--hops` | `3` | 洋葱路径的中间 relay 数（需 ≤ 在线 relay 数） |
| `--rendezvous` | `np4-network` | DHT rendezvous 字符串 |
| `--identity` | `~/.np4/identity` | 持久身份文件 |

## 语义与限制（重要）

- **离线即丢**：对方不在线或未完成 key 发布 → 消息丢失（无 presence、无离线队列）。
- **无送达保证**：`Send` 返回 nil 仅表示"已进入 mix"（已交给第一跳 relay），端到端 ACK 是 [v2] 特性。
- **冷启动延迟**：新节点加入后 DHT 路由表需要数秒预热；路径选择会自动重试（最长 25s）。
- 节点发现使用 Kademlia DHT；relay 发现使用 `np4-relay` rendezvous。
