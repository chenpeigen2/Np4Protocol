# Milestone Plan: 让 Metadata Protection 名副其实

> 2026-09-13. 本文档是 grilling 会话的完整决策树固化。所有决策已与项目所有者确认。
> 状态图例（同步重写 `docs/protocol.md`）：**[must]** 本 milestone 交付 / **[v2]** 明确计划 / **[cut]** 明确放弃。

## 1. 威胁模型（两层声明）

**协议设计目标**：对抗**主动 relay**——relay 可重放、篡改、说谎（发布伪造 ECDH key）、拒绝转发、注入任意字节流。网络观察者保持被动（不防 GPA 全局时序分析）。

**本 milestone 验收底线**：协议不崩溃、不泄露明文、重放与 DHT 毒化被拒。

**已知边界（写进 spec，不解决）**：
- Relay 发现无抗 Sybil：攻击者可注册大量 relay 吸路径。
- 静态长期 ECDH key，无 forward secrecy → **[v2]** key rotation（与 DHT record TTL 耦合设计）。
- 无 dummy traffic → 流量速率本身泄露活跃度（[v2] 单独设计轮次）。

## 2. Spec 三分类

| 特性 | 状态 | 备注 |
|---|---|---|
| Padding（定长 4KB cell） | **[must]** | 协议常量，上线后不可协商 |
| Relay 侧 batching/delay | **[must]** | 入口 10条/500ms；中间 relay 10条/200ms |
| TTL | **[must]** | 加密层内嵌，逐跳递减，归零丢弃 |
| Replay protection | **[must]** | Relay 记录已见 eph key（32B），LRU 容量上限 |
| 消息 ID + 接收端去重 | **[must]** | `Send` 硬失败语义的一部分 |
| 端到端 ACK | **[v2]** | 回程 onion 路径设计 |
| Dummy traffic | **[v2]** | 含全局限速，重新评估 cell 尺寸 |
| Key rotation / forward secrecy | **[v2]** | |
| 文件传输（分块协议） | **[v2]** | 须在 padding 之后 |
| 离线存储转发 / presence / 群聊 | **[v2]** | 引入新元数据面，须在 dummy traffic 之后 |
| Protobuf 运行时 | **[cut]** | JSON 保留；wire format 另有轻量二进制信封（见 §4） |
| 消息类型 SyncRequest/Response、Broadcast、FileChunk | **[v2]/[cut]** | v1 仅 Async + Direct |

## 3. 匿名降级语义

- `Send(dest, content)` **默认硬失败**：路径构建失败、入队失败即返回 error，**永不静默 fallback**。
- `SendDirect` 保留为显式 API；CLI 提供 `--insecure` 旗标并在输出中明示"未经匿名保护"。
- 理由（决策记录）：匿名性不作为配置默认值存在——默认安全，显式降级。

## 4. Cell 与 Wire 格式、延迟预算

**Cell = 定长 4096 字节（协议常量）**：`msg_id(16) ‖ content_len(2) ‖ content ‖ 0x00 padding`，内容 ≤ 4078B，更大 **[v2]** 分块。

**Wire = 定长 8192 字节（协议常量）**：`ttl(1) ‖ clen(2) ‖ layer_ciphertext(clen) ‖ random_pad`。等长在 wire 层实现（嵌套层不可能等长——每层必然比内层大 ~65+hopID 字节）。clen 前缀暴露剩余深度给 relay，与明文 TTL 泄露等价（威胁模型已声明），外部观察者只见恒定尺寸。

**延迟预算**：最坏 ≈ 入口 500ms + 3 × 200ms ≈ 1.1s。

**实现注记（与初稿的偏差）**：原计划"每层密文填充至等长"在数学上不成立（外层密文包含整个内层 + 开销），落地时改为 wire 层 re-pad 方案。

## 5. Chat v1 功能边界

**有**：洋葱路径 Send/Receive、消息 ID 去重、`--insecure` direct 模式、多行文本。
**没有**：presence、离线消息、历史记录、群聊、文件传输。
**语义**：双方同时在线 + peer ID 寻址 + 尽力送达；对方离线 = 消息丢失。CLI 启动 banner 明示。

## 6. 执行顺序

1. **spec 重写**：威胁模型两层声明 + 本文件 §2 状态表并入 `docs/protocol.md`
2. **堵安全洞**
   - DHT record 校验：validator 检查记录签名 pubkey 与 key 命名空间中的 peerID 一致；value 长度限 32B
   - TTL：加密层内嵌（`flagRelay` 明文增加 ttl 字段，初值 = 路径长度 + 余量），逐跳递减
   - Relay replay 缓存：已见 eph key 集合，LRU 上限（如 100k 条 ≈ 3.2MB）
   - `flushBatch` 派发信号量上限（如 64），拒绝无界 goroutine
3. **Cell 格式 + padding**：`pkg/cell`（信封编解码 + pad/unpad），onion 层输出定长化
4. **Relay 侧 MixEngine**：`handleOnionStream` 不再即时转发——解密 peel 后入 relay 本地 MixEngine（10条/200ms），flush 时逐包发往下一跳；replay 检查在 peel 时执行
5. **Send API 改造**：`Send` 硬失败；`SendDirect` 显式；`pendingPacket` 携带 cell；接收端 `msg_id` 去重
6. **Chat 走 mix**：`cmd_chat.go` 改用 `Send`；`--insecure` 走 `SendDirect`；banner 声明离线即丢
7. **对抗性集成测试**：恶意 relay 重放（断言不重复投递）、DHT 毒化（伪造他人 key 被拒）、随机字节流 fuzz（不 panic）、截断流（不 panic、不投递）、relay 层篡改（下游解密失败、链终止）

## 7. 验收标准

- `TestEndToEndMix` 保持绿；新增对抗性测试套件全绿
- `go vet` / `go build` 干净
- spec 与代码在 [must] 范围内零落差

## 8. 执行增补（2026-09-13，实测中发现）

真实多进程冒烟测试（bootstrap + 2 relay + chat 双端）暴露的三个测试无法发现的问题：

1. **CLI 从不发布 key** → chat 永远无法被寻址。修复：`chat` 启动时自动 `PublishKeys()` 并明示。
2. **DHT 冷启动竞态**：新节点路由表为空时 `FindProviders` 静默返回空（与"无 relay"不可区分）。修复：`pickPath` 自动重试至 25s 预算。
3. **one-shot 进程退出杀死队列中的包**（入口 mix 500ms flush 定时器未触发进程就没了）。修复：`MixEngine.Pending()` + `Node.WaitFlushed(ctx)`，`send` 命令 flush 后再退出。

冒烟测试最终端到端验证通过：跨进程 chat over mix，接收方显示 `anonymous`。

## 9. [v2] 执行：匿名性维持测试套件（D 项，2026-09-13）

交付 `TestMiddleRelayUnlinkability`（`pkg/np4/anonymity_test.go`）：

- **模型**：R1 → ADV（诚实但好奇的中间 relay）→ R2 → dest。ADV 只见即时邻居，唯一线索是时序。
- **攻击器**："最近未匹配入站"启发式（400ms 预算内最迟 ingress ↔ egress 配对），评分 precision vs 真值。
- **控制组**：`WithImmediateRelayForward()`（测试专用开关）关闭 ADV 混洗 → precision 必须 ≈1.0，证明实验装置能测出关联，mixed 断言非空洞。
- **基线**：对真实 flush 窗口做 Monte Carlo 均匀置换（500 次），自校准 chance 水平，无数学窗口假设。
- **结果**：control=1.00，mixed=0.45，chance=0.37±0.08 → 单中间 relay 的时序关联被压到 chance 水平。
- 过程中的真实 bug：MC 基线第一版把"攻击者猜的配对"当成了 ground truth，基线恒等于 1.00 使断言空洞——修正为置换 egress 携带的 send、保持真值配对。

工程注记：`-race` 对 libp2p 重量级集成测试不可用（>15min 不收敛）；race 覆盖轻量包（cell/onion/identity/mix/message/pathsel）。
