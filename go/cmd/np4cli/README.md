# np4cli

Np4Protocol P2P 匿名通信客户端。基于 libp2p，使用 Noise 协议加密（X25519 + ChaCha20-Poly1305），支持 DHT 节点发现。

## 构建

```bash
cd go
go build -o bin/np4cli ./cmd/np4cli/
```

## 快速开始

### 1. 启动 bootstrap 节点

```bash
go build -o bin/bootstrap ./cmd/bootstrap/
./bin/bootstrap -port 4000
# 输出:
# Bootstrap node started
# Peer ID: 12D3KooW...
# Addresses:
#   /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW...
```

### 2. 启动客户端

```bash
# 终端 A
./bin/np4cli --port 4002 --bootstrap /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW... chat

# 终端 B
./bin/np4cli --port 4003 --bootstrap /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW... chat
```

### 3. 发现节点并聊天

终端 A 中:
```
> peers
  12D3KooW...  [/ip4/127.0.0.1/tcp/4003/...]
发现 1 个节点

> connect /ip4/127.0.0.1/tcp/4003/p2p/12D3KooW...
已连接到 12D3KooW...

> send 12D3KooW... 来自 A 的消息
已发送到 12D3KooW...
```

终端 B 中:
```
[14:32:01] 12D3KooW...: 来自 A 的消息
>
```

## 子命令

| 命令 | 说明 |
|------|------|
| `np4cli id` | 显示本节点的 Peer ID 和地址 |
| `np4cli peers` | 通过 DHT 发现在线节点 |
| `np4cli connect <multiaddr>` | 连接到指定节点 |
| `np4cli send <peer-id> <消息>` | 发送消息 |
| `np4cli chat` | 进入交互式聊天模式 |

## 全局参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--port` | `0`（随机） | TCP 监听端口 |
| `--bootstrap` | 无 | Bootstrap 节点的 multiaddr（启用 DHT 发现） |
| `--rendezvous` | `np4-network` | DHT rendezvous 字符串，用于节点发现 |

## 交互模式命令

进入 `chat` 模式后可用的命令：

| 命令 | 说明 |
|------|------|
| `peers` | 发现在线节点 |
| `connect <multiaddr>` | 连接到节点 |
| `send <peer-id> <消息>` | 发送消息 |
| `id` | 显示本节点信息 |
| `help` | 显示帮助 |
| `quit` / `exit` | 退出 |

## 架构

```
np4cli (cobra CLI)
  └── np4.Node
        ├── libp2p Host（TCP + Noise 加密）
        ├── Kademlia DHT（节点发现）
        ├── Stream 处理器（/np4/message/1.0.0）
        └── MessageBus（消息发布/订阅）
```

所有连接通过 libp2p 的 Noise 协议自动加密（X25519 密钥交换 + ChaCha20-Poly1305）。节点发现使用 Kademlia DHT，通过 rendezvous 字符串标识同一网络。

## Multiaddr 格式

libp2p 使用 multiaddr 表示网络地址：

```
/ip4/127.0.0.1/tcp/4000/p2p/12D3KooW...
└─ IP ─┘          └端口┘    └─ Peer ID ─┘
```

`id` 命令会输出完整的 multiaddr，用于 `connect` 命令和 `--bootstrap` 参数。
