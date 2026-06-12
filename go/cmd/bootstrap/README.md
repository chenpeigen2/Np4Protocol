# np4bootstrap

Np4Protocol DHT 引导节点。作为长期运行的 DHT 服务器，帮助其他节点发现彼此。

## 构建

```bash
cd go
go build -o bin/bootstrap ./cmd/bootstrap/
```

## 使用

### 启动引导节点

```bash
./bin/bootstrap start --port 4000
# 输出:
# Bootstrap node started
# Peer ID: 12D3KooW...
# Addresses:
#   /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW...
#
# Use the multiaddr above with np4cli --bootstrap flag
# Press Ctrl+C to stop
```

### 查看节点信息

```bash
./bin/bootstrap id --port 4000
# 输出:
# Peer ID: 12D3KooW...
# Addresses:
#   /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW...
```

## 命令

| 命令 | 说明 |
|------|------|
| `start` | 启动引导节点（DHT Server 模式） |
| `id` | 显示节点 Peer ID 和 multiaddr |

## 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--port` | `4000` | TCP 监听端口 |

## 搭配 np4cli 使用

```bash
# 1. 启动 bootstrap
./bin/bootstrap start --port 4000

# 2. 复制输出的 multiaddr，用于 np4cli
./bin/np4cli --port 4002 --bootstrap /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW... chat
```
