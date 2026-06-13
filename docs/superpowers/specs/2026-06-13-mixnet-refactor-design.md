# Mixnet 深度重构设计

**日期**：2026-06-13
**状态**：待实施
**前置**：当前代码为 P2P 直连原型（`Send` 绕过 MixEngine），无匿名性。本次重构将 MixEngine 真正接入发送链路，并实现简化版 onion 路由。

## 1. 目标与非目标

### 目标
- **实现真正的 mix 通信**：消息默认走 N 跳 onion 路径，每跳中继只知道上一跳和下一跳，无法关联源/目的。
- **修复致命 BUG**：`bootstrap id` 每次输出不同 Peer ID；`MessageBus.Send` goroutine 爆炸；`Broadcast` 修改入参；`MixEngine` timer 泄漏。
- **代码质量**：消除全局变量、抽 interface、错误日志可见、单元测试覆盖核心加密/路径逻辑。
- **保持 CLI 易用**：`np4cli send` 默认走 mix，调试用 `--direct`。

### 非目标（v1 不做）
- Sphinx 完整实现（replay cache、constant-size padding、tag 检测）——留待 v2。
- Cover traffic / dummy 消息填充。
- 入口/出口防御（ ISP 级别的流量分析）。
- PADDING 等长化（包大小可观测）。
- 与现有 proto 的兼容（proto 暂时不用，JSON 即可）。

## 2. 架构总览

```
cmd/
├── np4cli/         # 客户端：send/chat/id/peers/connect/relay/path
└── bootstrap/      # DHT server + relay 目录服务
        ↓
pkg/np4/           # 高层 Node：wires p2p + mix + onion + identity
        ↓
pkg/mix/           # MixEngine：batch + shuffle + flush（真正生效）
pkg/onion/         # 分层加密：Build / Decode / Forward
pkg/pathsel/       # 路径选择：从 DHT 选 N 跳
pkg/identity/      # 持久化 Ed25519 + X25519 派生
        ↓
pkg/p2p/           # libp2p host/stream/discovery
pkg/message/       # MessageBus：worker pool，无副作用
```

**依赖方向**：上层依赖下层，禁止反向。`pkg/onion` 不依赖 `pkg/np4`，可独立测试。

## 3. 数据结构与协议

### 3.1 持久化身份（pkg/identity）

```go
type Identity struct {
    ed25519Priv crypto.PrivKey   // libp2p 标识
    x25519Pub   []byte           // ECDH 用，从 ed25519 派生
}

func LoadOrCreate(path string) (*Identity, error)
func (i *Identity) PeerID() peer.ID
func (i *Identity) ECDHPub() []byte             // 公开发布
func (i *Identity) ECDH(theirPub []byte) ([]byte, error)  // 共享密钥
```

**存储**：`~/.np4/identity`（0600 权限），protobuf 或 raw bytes。
**派生**：Ed25519 → X25519 用 `golang.org/x/crypto/curve25519` 的标准转换。

### 3.2 Onion 数据结构（pkg/onion）

**单层 wire 格式**（解密后）：
```
[1 byte: is_final]                   (0=relay, 1=final)
[4 bytes: next_hop_len]               (big-endian, 仅 relay 层有效)
[next_hop_len bytes: next_hop_peer_id] (仅 relay 层)
[remaining: inner_payload]            (下一层 onion 或最终消息)
```

**整包**（网络上传输的）：
```
[4 bytes: total_len]                  (length-prefix framing, 复用 pkg/p2p/stream)
[total_len bytes: layer_0_packet]     (见下)
```

**每层密文格式**（AEAD 加密前的 plaintext 包含 `is_final/next_hop/inner`，密文格式如下）：
```
[32 bytes: sender_ephemeral_pub]      (X25519 公钥，每层独立生成)
[12 bytes: nonce]                      (随机)
[N bytes: ciphertext + 16 bytes tag]  (ChaCha20-Poly1305)
```

**ECDH 流程**：
- 发送者构造第 i 层时：生成新的 ephemeral keypair `(eph_priv_i, eph_pub_i)`，存 `eph_pub_i` 在包头
- ECDH 共享密钥：`shared_i = curve25519.X25519(eph_priv_i, relay_i_static_pub)`
- 中继 i 解密：`shared_i = curve25519.X25519(relay_i_static_priv, eph_pub_i)`（相同结果）
- KDF：`key_i = HKDF-SHA256(shared_i, salt="np4-onion-v1", info=relay_i_peer_id)`
- AEAD：`plaintext = ChaCha20Poly1305.Decrypt(nonce, ciphertext, key)`

**核心 API**：
```go
// Build：发送者构造 onion
func Build(path []Hop, finalPayload []byte) (*Onion, error)

type Hop struct {
    PeerID  peer.ID
    ECDHPub []byte
}

// Decode：中继或目的地解一层
func Decode(ciphertext []byte, priv *identity.Identity) (*Decoded, error)

type Decoded struct {
    IsFinal    bool
    NextHop    peer.ID   // 仅 relay 层
    Inner      []byte     // relay: 下层 onion；final: 应用 payload
}
```

**加密细节**：
- 每层 ephemeral keypair：发送者为每一层独立生成 `(eph_priv, eph_pub)`，`eph_pub` 随包传输
- ECDH 共享密钥：`curve25519.X25519(eph_priv, relay_static_pub)`（发送方）等于 `curve25519.X25519(relay_static_priv, eph_pub)`（接收方）
- KDF：`HKDF-SHA256(shared, salt="np4-onion-v1", info=relay_peer_id_bytes)` → 32 字节 key
- AEAD：ChaCha20-Poly1305，随机 nonce 前置（12 字节 nonce + 密文 + 16 字节 tag）
- 每层独立 ephemeral 防止跨层关联

### 3.3 协议 ID 与 DHT 命名空间

libp2p 区分三种"标识"：

**Stream 协议 ID**（用于 `SetStreamHandler`）：
```
/np4/onion/1.0.0    ← onion 转发流，所有节点注册此 handler
/np4/direct/1.0.0   ← 直连消息流（--debug 用）
```

**DHT rendezvous 字符串**（用于 `AdvertiseRendezvous` / `FindPeers`）：
```
"np4-relay"         ← relay 节点宣告自己的 rendezvous
"np4-node"          ← 普通节点宣告（可选，用于 peers 命令）
```

**DHT record key 命名空间**（用于 `PutValue` / `GetValue`）：
```
/np4/ecdh/<base32(peer_id)>  ← relay 的 ECDH 公钥（32 字节）
```

### 3.4 MixEngine（pkg/mix）

```go
type MixEngine[T any] struct { ... }

func NewMixEngine[T any](batchSize int, maxDelay time.Duration, onFlush func([]*T)) *MixEngine[T]
func (m *MixEngine[T]) Add(msg *T) error   // 入队
func (m *MixEngine[T]) Close() error        // 停止 timer，flush 剩余
```

修复点：
- `flush()` 的 `go m.onFlush(batch)` 改为同步调用（由调用方决定是否异步），避免 timer 持锁时启动 goroutine 的调度歧义。
- 加 `Close()`：停止 timer、flush 剩余消息、标记 closed（之后 Add 返回 error）。
- 加种子：`rand.New(rand.NewSource(secureSeed))`，seed 来自 `crypto/rand`。

### 3.5 路径选择（pkg/pathsel）

```go
type Selector struct {
    Hops       int           // 默认 3
    Rendezvous string        // "np4-relay"
    Timeout    time.Duration // DHT 查询超时
}

func (s *Selector) Pick(ctx context.Context, dht *dht.IpfsDHT, exclude ...peer.ID) ([]onion.Hop, error)
```

逻辑：
1. `FindPeers(ctx, dht, "np4-relay")` 收集候选
2. 过滤 `exclude`（通常包含自己 + 目的地）
3. 过滤掉没有 ECDHPub 元数据的候选（DHT record 里没有公钥的不能用）
4. 随机选 N 个，要求互不相同
5. 若候选不足 N，返回 `ErrNotEnoughRelays`

**Relay 公钥分发**：
- Relay 启动时调用 `AdvertiseRendezvous(ctx, dht, "np4-relay")` 宣告
- 同时调用 `dht.PutValue(ctx, "/np4/ecdh/" + base32(peerID), pubBytes)` 存自己的公钥
- Selector 用 `dht.GetValue(ctx, key)` 读取候选 relay 的公钥
- DHT record 自带签名（创建者私钥签名 record 内容），所以公钥不能被篡改

### 3.6 Node（pkg/np4）

```go
type Node struct {
    host      host.Host
    identity  *identity.Identity
    bus       *message.MessageBus
    mix       *mix.MixEngine[*message.Message]
    dht       *dht.IpfsDHT
    pathSel   *pathsel.Selector
    ctx       context.Context
    cancel    context.CancelFunc
    stopOnce  sync.Once
}

func NewNode(opts ...Option) (*Node, error)

// 发送：默认走 mix + onion
func (n *Node) Send(dest peer.ID, content []byte) error

// 直连（调试）：单跳 stream，无 mix
func (n *Node) SendDirect(dest peer.ID, content []byte) error

// 作为 relay 运行：注册 handler + advertise
func (n *Node) ServeRelay(ctx context.Context) error

// 连接（用于 --direct 或 chat 的预连接）
func (n *Node) Connect(info peer.AddrInfo) error

// 关闭：DHT、host、mix
func (n *Node) Close() error
```

**Send 流程**：
1. `pathSel.Pick(ctx, n.dht, n.ID(), dest)` 选 3 个中继
2. 路径 = [relay1, relay2, relay3, dest]
3. `onion.Build(path, content)` 加密
4. `n.mix.Add(onionPacket)` 入队
5. mix flush 时 → `sendToRelay(relay1, packet)`

**接收（onion handler）**：
1. 读包 → `onion.Decode(packet, n.identity)`
2. 若 `IsFinal`：`n.bus.Send(parsed_message)`
3. 若 relay 层：`n.host.NewStream(next_hop, /np4/onion/1.0.0)` → 写 `Inner`

## 4. CLI 变更

### 4.1 np4cli（cmd/np4cli）

```bash
# 默认走 mix
np4cli send <peer-id> <msg>

# 直连（调试）
np4cli send --direct <peer-id> <msg>

# 路径长度
np4cli send --hops 5 <peer-id> <msg>

# 显示实际路径
np4cli path <peer-id>

# 作为 relay 节点运行
np4cli relay --port 5000 --bootstrap <multiaddr>

# 现有命令保留
np4cli id
np4cli peers
np4cli connect <multiaddr>
np4cli chat
```

### 4.2 bootstrap（cmd/bootstrap）

修复：
- `bootstrap start` 用 `identity.LoadOrCreate("~/.np4/identity")`
- `bootstrap id` 读取同一文件，**不创建新 host**，输出稳定 Peer ID
- 新增 `/api/relays`：DHT 查询 `/np4/relay` rendezvous，返回当前已知 relay 列表
- `/api/status` 的 `dht_peers` 改为 `n.dht.RoutingTable().Size()`

## 5. MessageBus 修复

```go
type MessageBus struct {
    handlerCh chan *Message       // 有界 channel
    handlers  []MessageHandler
    mu        sync.RWMutex
    workers   int
    quit      chan struct{}
}

func NewMessageBus(workers int) *MessageBus   // workers = GOMAXPROCS*2
func (b *MessageBus) Start()                   // 启动 worker pool
func (b *MessageBus) Stop()                    // 优雅关闭
func (b *MessageBus) Send(msg *Message) error  // 投递到 channel
func (b *MessageBus) Broadcast(msg *Message) error {
    copy := *msg              // 不修改原 msg
    copy.Type = TypeBroadcast
    copy.DestID = ""
    return b.Send(&copy)
}
```

`Send` 改为投递到 channel，由固定数量的 worker 消费并调用 handlers。channel 满时返回 `ErrBusFull`（而非无限堆积 goroutine）。

## 6. 错误处理改进

- 所有 `if err != nil { return }` 静默吞错的地方改用 `log.Printf` 或返回包装错误。
- `handleStream` 出错时记录 peer ID 和错误类型（不打日志全堆栈）。
- 引入 `pkg/log`（封装标准 log，加前缀 `[np4]`、节点 ID 短哈希）。

## 7. 测试策略

### 单元测试（每个包都要有）

| 包 | 测试点 |
|----|--------|
| `pkg/identity` | LoadOrCreate 幂等、ECDH 对称性 |
| `pkg/onion` | Build/Decode 对称、解密失败、错误 layer、空 payload |
| `pkg/onion` | **Fuzz test**：随机字节不应 panic |
| `pkg/pathsel` | 不足时返回 ErrNotEnoughRelays、exclude 生效、随机性 |
| `pkg/mix` | batch 触发、timeout 触发、Close 后 Add 报错 |
| `pkg/message` | Send/Broadcast 不修改入参、worker 优雅退出 |

### 集成测试（pkg/np4）

```go
// 启动 5 个节点：1 sender + 3 relay + 1 receiver
// 验证：receiver 收到、relay 收到的是 onion 密文（看不到明文）
func TestEndToEndMix(t *testing.T)
```

### 修复 shell 测试

- 删除 `bootstrap id` 作为获取 multiaddr 的方式
- 改用 `bootstrap start` 的输出（打印 multiaddr 到 stdout）
- 启动 relay 节点 `np4cli relay --port 5xxx --bootstrap <multiaddr>`
- 测试 `send` 通过 mix，验证收到

## 8. 实施阶段

| 阶段 | 内容 | 产出 |
|------|------|------|
| 1 | `pkg/identity` + 修 bootstrap id | 持久化身份，bootstrap id 稳定 |
| 2 | `pkg/onion` encode/decode/forward + 单测 + fuzz | onion 包独立可工作 |
| 3 | `pkg/mix` 加 Close、修 goroutine 调度 | MixEngine 安全关闭 |
| 4 | `pkg/pathsel` + DHT pubkey 分发 | 选 N 跳路径 |
| 5 | `pkg/np4` Node 重写、Send 走 mix | 端到端 onion 链路 |
| 6 | `pkg/message` worker pool + 修副作用 | 消息总线稳定 |
| 7 | CLI：np4cli relay/path/send flags + bootstrap /api/relays | 完整功能 |
| 8 | 集成测试 + 修 shell 测试 | 全绿 |

每阶段独立可提交、可回滚。

## 9. 风险与权衡

| 风险 | 缓解 |
|------|------|
| DHT 在局域网/单机不稳定，relay 发现失败 | SendDirect 作为 fallback；CLI 提示"relay 不足，回退直连" |
| Ed25519 → X25519 派生实现细节 | 用 `github.com/libp2p/go-libp2p/core/crypto`（libp2p 的 PrivKey 已支持 `GetPublic()`），或直接用 `curve25519.X25519` 配合 ed25519 私钥的字节转换 |
| MixEngine batch 在低流量下不 flush | 已有 timeout flush（500ms），保持 |
| Onion 包比直连大 N 倍（每层 AEAD 加 16B tag + overhead） | 接受；若 > 1MB 限制需提示 |
| DHT record 的 pubkey 可能被篡改 | libp2p DHT record 自带签名验证，依赖之 |

## 10. 与现有代码的兼容性

- **保留**：`np4cli send/chat/id/peers/connect` 子命令名
- **保留**：libp2p host/Noise/stream framing
- **保留**：DHT discovery 基础设施
- **修改**：`/np4/message/1.0.0` → `/np4/direct/1.0.0`（旧路径用于 --direct）
- **新增**：`/np4/onion/1.0.0`
- **删除**：`printPeerID` 死函数、MessageBus.Broadcast 修改入参的行为、全局 `node` 变量

## 11. 验收标准

- [ ] `bootstrap id` 多次运行输出相同 Peer ID
- [ ] `np4cli send <peer-id> <msg>` 通过 3 跳 relay 后被对方收到
- [ ] 任意中继节点的日志/抓包看不到明文 content
- [ ] `np4cli send --direct` 仍然工作（向后兼容）
- [ ] 所有单元测试通过，包括 fuzz
- [ ] shell 测试套件全部通过（修复后）
- [ ] `go vet ./...` 无警告
- [ ] `gofmt -l .` 无输出
