# Np4Protocol 设计文档

## 1. 概述

Np4Protocol 是一个基于 TCP 的匿名通信协议，采用 Mixnet（混合网络）架构实现元数据保护。协议使用 Protobuf 定义消息格式，支持 Go、Java 等多语言实现。

### 1.1 设计目标

- **元数据保护**：隐藏通信双方身份、时序、频率等元数据
- **多语言支持**：通过 Protobuf 和协议规范确保跨语言一致性
- **小规模高效**：针对 <100 节点的网络优化
- **渐进式扩展**：架构预留扩展能力，可从小规模扩展到更大网络

### 1.2 核心特性

- X25519 + ChaCha20-Poly1305 加密
- 批量混洗重排序，消除时序信息
- 消息填充到固定长度，防止大小分析
- 支持异步消息、同步请求/响应、广播、文件传输

## 2. 架构设计

### 2.1 协议栈

```
┌─────────────────────────────────────────────────────────┐
│  应用层    │  消息类型（异步/同步/广播/文件）              │
├─────────────────────────────────────────────────────────┤
│  匿名层    │  混洗引擎（Mixnet）- 批量混洗、重排序        │
├─────────────────────────────────────────────────────────┤
│  加密层    │  X25519 密钥交换 + ChaCha20-Poly1305 加密   │
├─────────────────────────────────────────────────────────┤
│  传输层    │  TCP 连接池 + Protobuf 序列化                │
└─────────────────────────────────────────────────────────┘
```

### 2.2 核心组件

| 组件 | 职责 |
|------|------|
| Transport（传输层） | 管理 TCP 连接，Protobuf 编解码 |
| Crypto（加密层） | X25519 密钥协商，ChaCha20-Poly1305 加解密 |
| MixEngine（混洗引擎） | 收集消息、批量混洗、延迟重排后转发 |
| Router（路由层） | 维护节点表，选择转发路径 |
| MessageBus（消息总线） | 应用层接口，支持四种消息类型 |

### 2.3 节点角色

- **Client**：发送/接收消息的终端节点
- **MixNode**：中继混洗节点，负责消息混淆
- **Bootstrap**：引导节点，帮助新节点加入网络

## 3. 协议消息格式

### 3.1 Protobuf 定义

```protobuf
syntax = "proto3";
package np4;

// 信封：网络传输的基本单位
message Envelope {
  bytes  payload    = 1;  // 加密后的载荷
  bytes  signature  = 2;  // 签名（可选）
  Header header     = 3;  // 明文头部（路由用）
}

message Header {
  MessageType type       = 1;
  bytes       dest_id    = 2;  // 目标节点 ID（可能是假名）
  bytes       sender_id  = 3;  // 发送者假名 ID
  uint64      timestamp  = 4;
  uint32      ttl        = 5;  // 生存时间
  uint32      version    = 6;  // 协议版本号（当前：1）
}

enum MessageType {
  ASYNC_MSG      = 0;  // 异步消息
  SYNC_REQUEST   = 1;  // 同步请求
  SYNC_RESPONSE  = 2;  // 同步响应
  BROADCAST      = 3;  // 广播
  FILE_CHUNK     = 4;  // 文件分片
  MIX_CTRL       = 5;  // 混洗控制消息
  KEY_EXCHANGE   = 6;  // 密钥交换
  PEER_DISCOVERY = 7;  // 节点发现
}

// 载荷（解密后）
message Payload {
  bytes  content      = 1;  // 实际内容
  bytes  session_key  = 2;  // 会话密钥（用于多跳）
  uint32 hop_count    = 3;  // 已跳数
  bytes  next_hop     = 4;  // 下一跳地址
}
```

### 3.2 混洗批次格式

```protobuf
message MixBatch {
  uint64            batch_id   = 1;
  repeated Envelope messages   = 2;  // 混洗后的消息列表
  bytes             proof      = 3;  // 批次完整性证明
}
```

### 3.3 关键设计点

- Header 是明文的（用于路由），但 dest_id 可以是假名
- Payload 完全加密，MixNode 只能看到 Header
- 批量混洗时，消息顺序被打乱，时序信息被消除

### 3.4 假名 ID 机制

假名 ID 是节点的临时身份标识，用于防止长期身份关联：

- **生成**：每次会话使用 `HKDF(session_secret, "pseudonym", nonce)` 派生
- **有效期**：单次会话或可配置的轮换周期（如 1 小时）
- **关联**：只有通信双方知道假名与真实身份的映射
- **格式**：32 字节随机数据，与真实公钥无数学关联

### 3.5 批次完整性证明

`MixBatch.proof` 字段用于验证批次未被篡改：

- **算法**：使用 HMAC-SHA256
- **输入**：`HMAC(batch_key, batch_id || messages_hash)`
- **batch_key**：由 MixNode 的长期密钥派生
- **作用**：防止中间人删除或重复消息

## 4. 混洗引擎（MixEngine）

### 4.1 混洗策略

```
消息到达 → 缓冲区收集 → 达到阈值/超时 → 混洗重排 → 批量转发
```

### 4.2 核心参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `batch_size` | 10 | 每批混洗的消息数量 |
| `max_delay` | 500ms | 最大等待时间，超时即发送 |
| `min_delay` | 50ms | 最小延迟，防止时序攻击 |
| `padding` | true | 填充到固定长度，防止大小分析 |

### 4.3 混洗算法

```go
type MixEngine struct {
    buffer    []*Envelope
    batchSize int
    maxDelay  time.Duration
    mu        sync.Mutex
}

func (m *MixEngine) AddMessage(env *Envelope) {
    m.mu.Lock()
    m.buffer = append(m.buffer, env)
    if len(m.buffer) >= m.batchSize {
        m.flush()
    }
    m.mu.Unlock()
}

func (m *MixEngine) flush() {
    // Fisher-Yates 洗牌
    rand.Shuffle(len(m.buffer), func(i, j int) {
        m.buffer[i], m.buffer[j] = m.buffer[j], m.buffer[i]
    })
    // 填充到固定长度
    for _, msg := range m.buffer {
        padToFixedLength(msg)
    }
    // 批量转发
    m.forwardBatch(m.buffer)
    m.buffer = nil
}
```

### 4.4 填充函数

```go
const PaddedSize = 4096  // 4KB 固定长度

func padToFixedLength(msg *Envelope) {
    currentSize := proto.Size(msg)
    if currentSize >= PaddedSize {
        return  // 已经超过，不填充（异常情况）
    }
    // 在 payload 末尾添加随机填充字节
    padding := make([]byte, PaddedSize-currentSize)
    rand.Read(padding)
    msg.Payload = append(msg.Payload, padding...)
}
```

### 4.5 抗流量分析

- **填充**：所有消息填充到相同长度（4KB），消除大小特征
- **虚假流量**：每个 MixNode 定期生成 dummy 消息（每 5-10 秒一条）
- **批量转发**：同一时间窗口内的消息一起发出，消除时序关联
- **恒定速率**：无论是否有真实消息，MixNode 以恒定速率发送批次

## 5. 加密与密钥交换

### 5.1 密钥层次

```
长期密钥（Identity Key）
    ↓ X25519 协商
会话密钥（Session Key）
    ↓ 派生
消息密钥（Message Key）← 每条消息唯一
```

### 5.2 密钥交换流程

```
节点 A                              节点 B
  │                                   │
  │  1. 生成临时密钥对 (ephemeral)     │
  │  2. 发送: {ephemeral_pub, nonce}  │
  │ ─────────────────────────────────→ │
  │                                   │  3. 生成自己的临时密钥对
  │                                   │  4. 计算共享密钥
  │  ←─────────────────────────────── │  5. 发送: {ephemeral_pub, nonce}
  │                                   │
  │  6. 计算共享密钥                   │
  │  7. 派生会话密钥 (HKDF)           │
  └───────────────────────────────────┘
```

### 5.3 加密方案

| 操作 | 算法 | 说明 |
|------|------|------|
| 密钥交换 | X25519 | ECDH，32 字节公钥 |
| 对称加密 | ChaCha20-Poly1305 | AEAD，高性能，抗时序攻击 |
| 密钥派生 | HKDF-SHA256 | 从共享密钥派生会话密钥 |
| 签名 | Ed25519 | 节点身份签名 |

### 5.4 消息加密流程

```go
// 发送方
func encryptMessage(msg *Payload, sessionKey []byte) ([]byte, error) {
    nonce := randomBytes(12)
    aead, _ := chacha20poly1305.New(sessionKey)
    ciphertext := aead.Seal(nil, nonce, marshal(msg), nil)
    return append(nonce, ciphertext...), nil
}

// 接收方
func decryptMessage(data []byte, sessionKey []byte) (*Payload, error) {
    nonce, ciphertext := data[:12], data[12:]
    aead, _ := chacha20poly1305.New(sessionKey)
    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    return unmarshal(plaintext), err
}
```

### 5.5 多跳加密（洋葱式）

对于需要经过多个 MixNode 的消息，使用分层加密：

```
原始消息 → 用第3跳密钥加密 → 用第2跳密钥加密 → 用第1跳密钥加密
```

每个 MixNode 只能解密一层，看到下一跳地址。

## 6. 项目结构

### 6.1 目录结构

```
Np4Protocol/
├── proto/                    # Protobuf 定义（语言无关）
│   └── np4.proto
├── go/                       # Go 实现
│   ├── cmd/
│   │   ├── np4d/            # MixNode 守护进程
│   │   └── np4cli/          # 客户端工具
│   ├── pkg/
│   │   ├── transport/       # TCP 传输层
│   │   ├── crypto/          # 加密层
│   │   ├── mix/             # 混洗引擎
│   │   ├── router/          # 路由层
│   │   └── message/         # 消息总线
│   └── go.mod
├── java/                     # Java 实现（后续）
│   └── src/main/java/np4/
└── docs/                     # 文档
    └── protocol.md
```

### 6.2 跨语言保证

1. **Protobuf 统一定义**：所有消息格式在 `proto/np4.proto` 中定义
2. **协议规范文档**：`docs/protocol.md` 描述行为规范（不只是格式）
3. **测试向量**：提供加密测试向量，确保各语言实现一致
4. **版本号**：协议头部包含版本字段，支持向后兼容

### 6.3 Go 核心接口

```go
type Transport interface {
    Connect(addr string) (Conn, error)
    Listen(addr string) (Listener, error)
}

type Crypto interface {
    KeyExchange(remotePub []byte) ([]byte, error)
    Encrypt(plaintext, key []byte) ([]byte, error)
    Decrypt(ciphertext, key []byte) ([]byte, error)
}

type MixEngine interface {
    AddMessage(msg *Envelope)
    OnBatch(callback func([]*Envelope))
}

type MessageBus interface {
    Send(dest NodeID, msg *Payload) error
    Broadcast(msg *Payload) error
    Request(dest NodeID, req *Payload) (*Payload, error)
    OnMessage(handler func(NodeID, *Payload))
}
```

## 7. 错误处理与测试

### 7.1 错误处理原则

- **网络错误**：自动重连，指数退避
- **加密错误**：立即断开连接，不泄露信息
- **协议错误**：发送错误码，关闭连接
- **混洗超时**：发送当前批次，不丢弃消息

### 7.2 错误码定义

```protobuf
enum ErrorCode {
  OK                = 0;
  INVALID_FORMAT    = 1;  // 消息格式错误
  DECRYPT_FAILED    = 2;  // 解密失败
  KEY_MISMATCH      = 3;  // 密钥不匹配
  TTL_EXPIRED       = 4;  // 消息过期
  BATCH_FULL        = 5;  // 批次已满
  UNKNOWN_NODE      = 6;  // 未知节点
}
```

### 7.3 测试策略

| 层级 | 测试内容 | 方法 |
|------|----------|------|
| 单元测试 | 加密/解密、混洗算法 | 标准单元测试 |
| 集成测试 | 多节点通信 | 启动本地测试网络 |
| 一致性测试 | 跨语言实现一致性 | 共享测试向量 |
| 性能测试 | 延迟、吞吐量 | 基准测试 |
| 安全测试 | 抗流量分析 | 模拟攻击场景 |

### 7.4 测试向量示例

```json
{
  "key_exchange": {
    "alice_private": "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf000102030405060708090a0b0c0d0e0f",
    "alice_public": "e6db6867583030db3594c1a424b15f7c726624ec26b3353b10a903a6d0ab1c4c",
    "bob_private": "b0b1b2b3b4b5b6b7b8b9babbbcbdbebf000102030405060708090a0b0c0d0e0f",
    "bob_public": "d0b1c4a6e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a7b8c9d0e1f2",
    "shared_secret": "4a5d9b5f1d6b8e4c2a3f7e9d0c1b5a8e6f4d2c0a9e7b3d5f1c0a8e6d4b2a0f"
  },
  "encrypt": {
    "key": "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
    "nonce": "000000000000000000000000",
    "plaintext": "48656c6c6f20576f726c64",
    "ciphertext": "d31a8d34648e60db7b86afbc53ef7c2b"
  }
}
```

## 8. 实现路线图

### Phase 1：核心协议（Go）

1. Protobuf 消息定义
2. TCP 传输层
3. X25519 + ChaCha20-Poly1305 加密层
4. 基础混洗引擎
5. 简单的点对点通信

### Phase 2：完整功能

1. 多跳路由
2. 广播支持
3. 文件传输
4. 节点发现

### Phase 3：多语言支持

1. 协议规范文档
2. 测试向量
3. Java 实现

### Phase 4：优化与扩展

1. 性能优化
2. 安全审计
3. 更大规模网络支持
