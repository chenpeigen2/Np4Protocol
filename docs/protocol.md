# Np4Protocol Specification

> 状态图例：**[impl]** 已实现 / **[must]** 本 milestone 交付 / **[v2]** 明确计划 / **[cut]** 明确放弃。
> 计划文档：`docs/plans/2026-09-13-anonymity-milestone.md`。

## Overview

Np4Protocol is a Mixnet-based anonymous communication protocol designed for metadata protection. It provides sender/receiver anonymity by batching, shuffling, and delaying fixed-size cells through relay nodes.

## Threat Model（两层声明）

**协议设计目标**：对抗**主动 relay**——relay 可重放、篡改、说谎（发布伪造 key 记录）、拒绝转发、注入任意字节流。网络观察者保持被动（**不防 GPA 全局时序分析**）。

**当前验收底线**：协议不崩溃、不泄露明文、重放与 DHT 毒化被拒；发往已知联系人的消息可验证归属（伪造/篡改被拒），未验证消息有显式徽标而非静默丢弃。

**已知边界（不解决，见 [v2]）**：

- Cover traffic 只 flatten 发送侧速率，不防 GPA 时序关联（威胁模型声明的边界）。
- 前向保密窗口 = 7 天保留期：超过 7 天前录制的流量在密钥文件泄露后不可解，但**主密钥（peer ID）本身不轮换**——针对主密钥的主动持久攻击者仍可冒充未来流量（无 post-compromise security）。
- 开放准入模式（无 --allowlist）下无抗 Sybil：攻击者可注册大量 relay 吸路径。
- Replay 防线依赖内存缓存（eph_pub LRU 100k + msg_id 去重 100k）：重启清零、淘汰后窗口重开；去重保证不重复投递，残余风险为容量挤占，评估低危（2026-10-01 审查备案）。

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
│  认证层    │  sender-auth tag：pairwise ECDH→HMAC-128   │ [must]
├─────────────────────────────────────────────────────────┤
│  Cell 层  │  定长 4096B cell：id‖type‖tag‖len‖content  │ [must]
├─────────────────────────────────────────────────────────┤
│  匿名层    │  MixEngine 批量混洗；入口 500ms / relay 200ms│ [must]
├─────────────────────────────────────────────────────────┤
│  洋葱层    │  定长填充层；TTL 逐跳递减；replay 缓存      │ [must]
├─────────────────────────────────────────────────────────┤
│  加密层    │  X25519 + HKDF-SHA256 + ChaCha20-Poly1305 │
├─────────────────────────────────────────────────────────┤
│  传输层    │  libp2p TCP + Noise + yamux；长度前缀帧；  │
│            │  bootstrap 可选 ConnectionGater 准入       │
└─────────────────────────────────────────────────────────┘
```

## Cell 格式 v2 **[must]**

Cell 是定长 **4096 字节**的协议常量（上线后不可协商）：

```
msg_id (16B) ‖ type (1B) ‖ tag (16B) ‖ content_len (2B, big-endian) ‖ content ‖ 0x00 padding
```

- `msg_id`：随机 16 字节；接收端按 `msg_id` 幂等去重。
- `type`：`0x00` = dummy（cover traffic，[M3] 注入），`0x01` = text。仅接收端可见；relay 无法区分。
- `tag`：sender-auth 标签（见「消息认证」）；dummy cell 全零。
- 最大 content = **4061 字节**；更大内容 **[v2]** 分块协议。
- 真实长度/类型/标签只编码在信封内部（随洋葱最内层加密）；外层任何尺寸差必须为零。

## 消息认证 **[must]**

目标：**匿名但可验真**——接收端确认消息出自已知联系人，网络与 relay 全程不可见、第三方不可证明。

- 发送端：`shared = X25519(自己的静态 X25519 私钥, 接收端已发布 ECDH pub)`；`key = HKDF-SHA256(shared, salt="np4-auth-v1", info=排序后的双方 peer ID)`；`tag = HMAC-SHA256(key, msg_id ‖ content)[:16]`。
- 接收端不知发送者是谁，遍历自己的联系人缓存（peer ID → 已验证发布的 ECDH pub）重算并常量时间比对；首个匹配即归属（`Verified=true`，`SenderID` = 该联系人）。
- 无匹配 → 消息**照常投递**、标记 `Verified=false`（UI 显示"未验证来源"徽标，不静默丢弃）——冷缓存中的新联系人仍可达。
- 性质：对称 MAC → **可否认**（接收方自己也算得出，第三方持完整转录无法证明发送者）；info 绑定双方 ID → 标签不可移植到其他对话；伪造/篡改在常量时间比对下拒绝。
- 已知边界：静态长期密钥，无前向保密（[M4] 时间桶轮换：24h 轮换 / 7d 保留）。

## 准入与限速 **[must]**

- **Allowlist 准入**（可选，单服务器部署）：bootstrap `--allowlist` 文件（每行一个 peer ID，热加载）。名单外节点在连接层即被 `ConnectionGater` 拒绝（无法加入 DHT——连接门是唯一密封闸点，记录校验挡不住节点本地自存记录）；validator 层同样拒绝名单外 `/np4/ecdh` 发布作纵深防御。**空/未配置 = 开放准入**（开发模式）。bootstrap 自身 ID 永远豁免。
- **Relay 入口限速**：per-peer 令牌桶，默认 10 cell/s、burst 50（`--relay-rate` 可调，0=不限）。在洋葱流入口、任何密码学处理之前执行，防单客户端灌满 relay mix（容量 256、drop-oldest）挤占他人流量。

## 入口与资源防护 **[impl]**

九轮安全审查（2026-10）落地的防御，全部带回归测试：

- **读取上界**：onion 流读取以 `2×WireSize` 为界——超长长度前缀在
  4 字节头即被拒，永不按声明值分配内存（分配放大防护）。
- **入站 TTL 上界**：入站包 `ttl > MaxInitialTTL(25)` 即判伪造丢弃
  （合法发送端 ≤24 且逐跳递减），封死单包环路放大。
- **发现候选上限**：rendezvous 遍历每调用最多 64 个候选（每个候选
  一次 DHT 往返），provider 洪泛无法放大发现成本。
- **有界记录存储**：ModeServer 节点的 DHT 记录存储上限 100k 条
  （kad-dht 默认为无界内存 map，且 /kad 流绕过 onion 限速）；满时
  拒绝新 key、更新放行。根因缓解仍是 allowlist 准入。
- **入口预算**：relay 默认 10 cell/s（burst 50，`--relay-rate`），
  客户端/np4cli 默认 100/s（burst 200，`--ingress-rate`），
  direct 协议 5 msg/s（burst 20）——全部 per-peer 令牌桶。
- **direct 协议加固**：严格 `DestID == 自身`（拒绝空目标广播注入）、
  线路字段不信任（`Verified` 强制 false、消息按协议认可字段重建，
  `SessionKey` 等遗留槽位永不透传）——direct 无鉴权，消息恒为
  "未验证"徽标。
- **失效模式**：allowlist 文件损坏/不可读时 **fail-closed**（仅
  bootstrap 自身可用），热加载恢复后自动放开；侧车文件损坏降级为
  全新子密钥窗口（不 brick 身份）。

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

## Key 记录（DHT）v2

```
/np4/ecdh/<base32(peer multihash)>  →  value = master_marshaled(36B) ‖ bucket(8B BE) ‖ x25519_pub(32B) ‖ sig(64B)
```

- `master_marshaled`：节点 **ed25519 主公钥**（libp2p 编码）。Validator 强制 `IDFromPublicKey(master) == 记录中的 peerID`——DHT 不验签名，此绑定仍是唯一防毒化机制，**不可移除**；配置 allowlist 时，validator 额外拒绝名单外 peer 的发布（纵深防御，连接门为主）。主密钥永不轮换（peer ID = 通讯录/allowlist 的锚）。
- `x25519_pub`：**当前轮换桶的 X25519 子密钥**（见「密钥轮换」），发送端直接用于洋葱末跳与 sender-auth tag。
- `sig`：主私钥对 `"np4-rotation-v1" ‖ bucket ‖ x25519_pub` 的 Ed25519 签名——防止任何人替别人发布子密钥。
- Validator 与 GetKey 端侧均走同一解析：框架 → 绑定 → 验签，全过才算数。

## 密钥轮换 **[must]**

前向保密通过时间桶子密钥实现：

- **周期**：每 24h 生成**全新随机** X25519 密钥对（非从主种子派生——派生会让历史子密钥可从种子重算，前向保密失效）；私钥保留 7 天后销毁。
- **性质**：录流攻击者今日窃取密钥文件，最多解密近 7 天的流量；离线 ≤7 天的接收端回来仍能解密发往旧子密钥的消息。peer ID、地址簿、allowlist 全部不受影响。
- **接收路径**：洋葱解密与 tag 验证都尝试保留窗内全部子密钥（当前优先，热路径代价一次 X25519+AEAD）。
- **发布**：节点每 15 分钟检查桶边界并重发布记录——同时修掉了 v1 的隐患（记录 EOL 有限，长跑节点 24h 后不可达）。
- **已知边界**：跨桶瞬间（联系缓存 30s 刷新窗口内）的 tag 可能 miss → 显示"未验证"徽标；洋葱机密性不受影响。
- 持久化：身份文件旁 `<path>.keys` sidecar（0600，原子写），崩溃/重启后保留窗完整。

## Node Types

- **Client**：发送/接收；需 `PublishKeys` 使他人可寻址。
- **Relay**：`ServeRelay` 广告 + 运行 relay 侧 MixEngine（10 条 / 200ms）。
- **Bootstrap**：独立 DHT server（种子节点）**兼任 mix relay**（单服务器部署模式）。客户端对它的连接是出站的，最后一跳复用该既有连接送达——NAT 后的客户端无需公网 IP 即可接收；单 relay 时客户端 `--hops 1`。信任边界：全路径唯一 relay 时，运营者可见全部时序（等价代理模型，见威胁模型两层声明）。

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
| cover traffic | Poisson 0.5 cell/s/节点 | 生产入口默认；`--dummy-rate` 可调，0=关 |
| flush 并发上限 | 64 | 反压而非无限 goroutine |

延迟预算：最坏 ≈ 500ms + 3×200ms ≈ 1.1s。

混洗随机源：**无状态 crypto/rand**——每次 Fisher-Yates 交换直接从
crypto/rand 抽取索引，不使用可种子化的 PRNG（批次排列是可观测输出，
可种子化 PRNG 的状态可被离线暴力恢复，进而反推全部历史混洗映射）。

## Cover Traffic **[must]**

无 cover 时，发送速率本身即活跃度指纹。所有启用 DHT 的节点以 **Poisson 过程**注入 dummy cell（均值 `--dummy-rate`，生产默认 0.5 cell/s ≈ 每 2 秒 1 条；0=关闭，库默认关闭以保证测量确定性）：

- **不可区分性**：真实路径选择 + 真实洋葱结构 + 恒定 wire 尺寸；与真实包唯一的差异是最内层 cell 的 `type=0x00`，任何 relay 不可见。receiver 在去重缓存**之前**静默丢弃，不产生事件。
- **目的地选择**：随机联系人（排除自身与 bootstrap——终止于 bootstrap 的包会被它百分百识别为 dummy，见威胁模型）。
- **让位语义**：mix 队列满时 dummy 被拒、真实流量优先；注入路径上任何失败静默跳过，绝不产生用户可见错误。
- **入口点默认**：np4cli / bridge（嵌入客户端）/ bootstrap 均默认 0.5；bridge 传负值显式关闭。

## [v2] Roadmap

- 端到端 ACK（回程 onion 路径）。
- 文件分块协议（>4061B 目前硬失败 ErrTooLarge）。
- Presence / 离线存储转发。
- 多 relay 部署下的 cover traffic relay 侧注入（当前仅入口侧；单 relay 拓扑下 relay 侧注入无增益）。
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
- Relay 发现：Kademlia DHT rendezvous（`np4-relay`）；mDNS helper 存在但无生产调用。
