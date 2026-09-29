# Np4Protocol Specification

> 状态图例：**[impl]** 已实现 / **[must]** 本 milestone 交付 / **[v2]** 明确计划 / **[cut]** 明确放弃。
> 计划文档：`docs/plans/2026-09-13-anonymity-milestone.md`。

## Overview

Np4Protocol is a Mixnet-based anonymous communication protocol designed for metadata protection. It provides sender/receiver anonymity by batching, shuffling, and delaying fixed-size cells through relay nodes.

## Threat Model（两层声明）

**协议设计目标**：对抗**主动 relay**——relay 可重放、篡改、说谎（发布伪造 key 记录）、拒绝转发、注入任意字节流。网络观察者保持被动（**不防 GPA 全局时序分析**）。

**当前验收底线**：协议不崩溃、不泄露明文、重放与 DHT 毒化被拒。

**已知边界（不解决，见 [v2]）**：

- Relay 发现无抗 Sybil：攻击者可注册大量 relay 吸路径。
- 静态长期 ECDH key，无 forward secrecy。
- 无 dummy traffic：流量速率本身泄露活跃度。

**已度量的基线**（`TestMiddleRelayUnlinkability`，60 条流，6 客户端，中间 relay 时序关联攻击）：

| 场景 | 攻击者配对 precision |
|---|---|
| 无混洗（控制组） | 1.00 |
| 正常混洗（实测） | 0.45 |
| 完美混洗（Monte Carlo chance） | 0.37 ± 0.08 |

结论：单个中间 relay 的时序关联攻击被压到 chance 水平（差异在统计噪声内）。此表随 dummy traffic 等 [v2] 特性更新。

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  应用层    │  消息（异步）；presence/离线/群聊 [v2]        │
├─────────────────────────────────────────────────────────┤
│  Cell 层  │  定长 4096B cell：msg_id ‖ len ‖ content    │ [must]
├─────────────────────────────────────────────────────────┤
│  匿名层    │  MixEngine 批量混洗；入口 500ms / relay 200ms│ [must]
├─────────────────────────────────────────────────────────┤
│  洋葱层    │  定长填充层；TTL 逐跳递减；replay 缓存      │ [must]
├─────────────────────────────────────────────────────────┤
│  加密层    │  X25519 + HKDF-SHA256 + ChaCha20-Poly1305 │
├─────────────────────────────────────────────────────────┤
│  传输层    │  libp2p TCP + Noise + yamux；长度前缀帧     │
└─────────────────────────────────────────────────────────┘
```

## Cell 格式 **[must]**

Cell 是定长 **4096 字节**的协议常量（上线后不可协商）：

```
msg_id (16B) ‖ content_len (2B, big-endian) ‖ content ‖ 0x00 padding
```

- `msg_id`：随机 16 字节；接收端按 `msg_id` 幂等去重。
- 最大 content = 4078 字节；更大内容 **[v2]** 分块协议。
- 真实长度只编码在信封内部（随洋葱最内层加密）；外层任何尺寸差必须为零。

## 洋葱层与 Wire 格式 **[must]**

嵌套加密的固有性质：每层密文比内层大 ~65+hopID 字节，等长不可能在层内做。等长在 **wire 层**实现——每个链路上的包都是恒定尺寸：

```
wire packet = ttl(1B) ‖ clen(2B, BE) ‖ layer_ciphertext(clen) ‖ random_pad   — 总长 = WireSize (8192B)
layer_ciphertext = eph_pub(32) ‖ nonce(12) ‖ ChaCha20-Poly1305(flag ‖ routing ‖ inner)
```

- **定长 slot**：发送方和每个 relay 都将包 re-pad 到 `WireSize`。外部观察者只见恒定尺寸恒定节奏的流量，内容大小和路径长度均不可见。
- **clen 前缀**：暴露剩余深度给 relay——但明文 TTL 已暴露同样信息（威胁模型已声明 relay 可知剩余深度），不新增泄露面。
- **TTL**：明文逐跳递减，归零即丢弃（防恶意构造的无限循环 DoS）。`MaxInitialTTL = 25`。
- **Replay**：relay 记录已见 `eph_pub`（LRU 上限 100k 条 ≈ 3.2MB），重复即丢弃；`msg_id` 去重是端到端兜底。只在解密成功后才计入缓存，垃圾洪泛无法驱逐合法条目。
- **降级语义**：`Send` 永不静默 fallback 到 direct；`SendDirect` 是显式 API（CLI `--insecure`）。

## Key 记录（DHT）

- 记录 value = 节点 **ed25519 公钥**（libp2p 编码），key = `/np4/ecdh/<base32(peer multihash)>`。
- Validator 强制 `IDFromPublicKey(value) == key 中的 peerID`——DHT 不验签名，此绑定是唯一防毒化机制，**不可移除**。
- 发送端本地将 ed25519 pub 双有理映射换算为 X25519 pub（与 NaCl 派生兼容）。

## Node Types

- **Client**：发送/接收；需 `PublishKeys` 使他人可寻址。
- **Relay**：`ServeRelay` 广告 + 运行 relay 侧 MixEngine（10 条 / 200ms）。
- **Bootstrap**：独立 DHT server（种子节点）。

## 消息类型

v1 仅使用 `TypeAsync`（mix 送达）与 direct JSON 消息（`--insecure`）。
`TypeSyncRequest/Response`、`Broadcast`、`FileChunk` → **[v2]/[cut]**。

## Mix 参数

| 参数 | 值 | 说明 |
|---|---|---|
| cell_size | 4096 B | 协议常量（cell） |
| WireSize | 8192 B | 协议常量（链路上恒定包尺寸） |
| 入口 batch | 10 条 / 500 ms | 发送端 |
| relay batch | 10 条 / 200 ms | 每个中间 relay |
| relay replay 缓存 | 100k 条 LRU | ~3.2 MB |
| flush 并发上限 | 64 | 反压而非无限 goroutine |

延迟预算：最坏 ≈ 500ms + 3×200ms ≈ 1.1s。

## [v2] Roadmap

- Dummy traffic + 全局限速（届时重新评估 cell 尺寸）。
- 端到端 ACK（回程 onion 路径）。
- Key rotation / forward secrecy（与 DHT record TTL 耦合）。
- 文件传输分块协议。
- Presence / 离线存储转发（依赖 dummy traffic 先行）。
- Protobuf 运行时 **[cut]**：保留 JSON + 二进制 cell 信封。

## Error Codes

| Code | Value | Description |
|------|-------|-------------|
| OK | 0 | Success |
| INVALID_FORMAT | 1 | Malformed message |
| DECRYPT_FAILED | 2 | Decryption failure |
| KEY_MISMATCH | 3 | Key mismatch |
| TTL_EXPIRED | 4 | Message expired |
| BATCH_FULL | 5 | Batch buffer full |
| UNKNOWN_NODE | 6 | Unknown destination |

## Transport

- libp2p：TCP transport，Noise 安全（X25519 + ChaCha20-Poly1305），yamux 多路复用。
- 应用层帧：4-byte big-endian 长度 + payload，上限 1 MB。
- Relay 发现：Kademlia DHT rendezvous（`np4-relay`）+ mDNS（局域网）。
