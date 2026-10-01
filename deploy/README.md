# NP4 Bootstrap 服务器部署（Ubuntu 24.04 + Docker）

把 bootstrap（DHT 种子 + mix relay 单进程）部署到公网服务器。一条脚本完成安装、构建、启动、防火墙和客户端地址输出。

## 快速开始

```bash
git clone https://github.com/chenpeigen2/Np4Protocol.git
cd Np4Protocol/deploy
sudo ./deploy.sh            # 公网 IP 自动探测；也可显式指定：sudo ./deploy.sh 1.2.3.4
```

脚本结束时打印**客户端可直接使用的 multiaddr**（已替换为公网 IP）。

## 部署包做了什么

| 项 | 说明 |
|---|---|
| 镜像 | 多阶段构建：golang:1.26-alpine 编译静态二进制（CGO_ENABLED=0）→ alpine:3.20 运行 |
| 身份持久化 | `/data/boot.id` 存在 `np4-identity` 卷里。**丢了它 = Peer ID 变了 = 所有客户端地址作废**，务必备份（脚本输出里有备份命令） |
| 端口 | 仅 `4000/tcp` 公开（DHT + relay）；仪表盘 `8080` 只绑 `127.0.0.1`——它无鉴权且 CORS 全开，绝不暴露公网，用 SSH 隧道访问 |
| 健康检查 | `wget /api/status`，compose 依赖它决定重启；deploy.sh 等到 healthy 才打印地址 |
| 重启策略 | `unless-stopped` + docker 开机自启；日志 10MB×3 轮转 |
| 防火墙 | ufw active 时只放行 4000/tcp |

## 日常运维

```bash
docker logs -f np4-bootstrap                  # 日志
sudo docker compose restart np4-bootstrap     # 重启
git pull && sudo docker compose up -d --build # 升级代码
docker run --rm -v np4-identity:/data -v $PWD:/backup alpine cp /data/boot.id /backup/   # 备份身份
```

## 仪表盘接口（SSH 隧道访问 `http://localhost:8080`）

| 接口 | 内容 |
|---|---|
| `/api/status` | 节点状态：peer_id、监听地址、uptime、DHT 路由表大小 |
| `/api/directory` | **通讯录**：每个 mix 可达节点一条记录 —— `peer_id` / `addrs` / `ecdh_pub`（X25519 洋葱公钥）/ `connected`（在线）/ `is_relay`（能否当中继）。面板每 5 秒刷新，每条目可一键复制 JSON（即客户端添加联系人所需的全部信息） |
| `/api/peers` | 原始 libp2p 连接视图（含尚未发布 key、不可达的节点） |
| `/api/relays` | 当前 advertise 为 relay 的节点及其 X25519 公钥 |

## 信任边界（部署前必读）

单 relay 部署下，这台服务器能看到**全网流量的进出时序**并关联收发双方——匿名性完全取决于对服务器运营者（你）的信任，等价于传统代理模型（见 `docs/protocol.md` 威胁模型）。要真正的 mix 匿名性，需要多个互不信任的 relay：在别的机器上再跑几个 `np4cli relay`，客户端把 `--hops` 提到 relay 总数。协议侧已支持多个 bootstrap 地址（`WithBootstrap` 接受列表），后续做多 bootstrap 高可用不需要改客户端。

## 已知限制

- 容器内看到的地址是内部 IP——客户端用的 multiaddr 一律以 deploy.sh 打印的公网版本为准。
- 镜像不在 CI 构建：首次 `--build` 在服务器上拉取 Go 依赖。国内服务器把 `docker-compose.yml` 里的 `GOPROXY` 改成 `https://goproxy.cn,direct`（已是构建参数，改一行即可）。
