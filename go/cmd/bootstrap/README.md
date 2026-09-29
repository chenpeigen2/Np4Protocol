# np4bootstrap

Np4Protocol 引导节点：**DHT 种子服务器兼任 mix relay（消息分发）**。为单服务器部署设计——所有客户端出站连接到它，无需公网 IP；洋葱的最后一跳复用这条既有连接送达。

## 构建

```bash
cd go
go build -o bin/bootstrap ./cmd/bootstrap/
# 交叉编译到 Linux 服务器：
GOOS=linux GOARCH=amd64 go build -o bin/bootstrap ./cmd/bootstrap/
```

## 使用

### 启动引导节点

```bash
./bin/bootstrap start --port 4000 --identity ./boot.id
# 输出:
# Bootstrap node started (DHT seed + mix relay)
# Peer ID: 12D3KooW...
# Addresses:
#   /ip4/127.0.0.1/tcp/4000/p2p/12D3KooW...
# [bootstrap] relay advertisement starts once the first client joins the DHT
# （第一个客户端加入后）:
# [bootstrap] advertised as mix relay (np4-relay rendezvous)
```

relay 广告需要 DHT 路由表非空（记录存储在对等节点上），所以首次广告发生在第一个客户端加入之后——节点会自动重试，无需干预。

### 部署到公网服务器

1. 交叉编译（见上）并上传二进制 + `--identity` 指定的身份文件（**身份文件丢失 = Peer ID 改变 = 所有客户端的 bootstrap 地址失效**）。
2. 防火墙放行 TCP 4000（DHT + relay）；`--web` 端口按需放行或 `--web 0` 关闭仪表盘。
3. **云服务器注意**：1:1 NAT 环境下程序打印的是内网 IP，发给客户端前必须手动把 multiaddr 里的 IP 换成公网 IP。
4. 建议配 systemd 常驻（`Restart=always`）。

### 查看节点信息

```bash
./bin/bootstrap id
# 输出 Peer ID 和 multiaddr（与 start 一致）
```

## 搭配 np4cli 使用

```bash
BOOT=/ip4/<公网IP>/tcp/4000/p2p/12D3KooW...

# 客户端（--hops 1：唯一的 relay 就是 bootstrap）
./bin/np4cli --port 4004 --bootstrap $BOOT --hops 1 --identity ./a.id chat
./bin/np4cli --port 4005 --bootstrap $BOOT --hops 1 --identity ./b.id chat
```

添加更多 relay 进程后，可相应提高 `--hops`（relay 数 ≥ hops）。

## 信任边界（重要）

单 relay 部署下，bootstrap 运营者能看到网络中**全部流量的进入与离开时序**，可以关联收发双方——匿名性完全取决于对运营者的信任，等价于传统代理模型。要获得 mixnet 的 unlinkability 保证，需要多个互不信任的 relay 参与（`--hops ≥ 2`，relay 由不同方运营）。

## 命令

| 命令 | 说明 |
|------|------|
| `start` | 启动引导节点（DHT Server 模式 + mix relay） |
| `id` | 显示节点 Peer ID 和 multiaddr |

## 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--port` | `4000` | TCP 监听端口 |
| `--identity` | `~/.np4/identity` | 持久身份文件 |
| `--web` | `8080` | Web 仪表盘端口（0 关闭；`/api/status`、`/api/peers`、`/api/relays`） |
