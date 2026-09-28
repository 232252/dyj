# ADR-0005：单一 RFC 8010 编码层（消除重复实现的裁决）

- **状态**：已接受（Accepted）
- **日期**：2026-02（会话日期）
- **决策者**：项目发起人（价值观条款的适用裁决）
- **依据**：[ADR-0002 D4 模块划分](0002-stack-and-architecture.md)、[附录 E](../research/07-go-ipp-server-capability.md)、[RFC 8010](https://www.rfc-editor.org/rfc/rfc8010)

## 背景

P0 实证阶段并行开发时，`pkg/attributes/` 与 `pkg/ipp/` 各自实现了一套 RFC 8010 属性编码：

| | `pkg/attributes/writer.go` | `pkg/ipp/wire.go` |
| --- | --- | --- |
| tag 常量表 | ✅ 导出 | ✅ **重复一套**（`TagInteger`…`TagMimeType`，取值相同） |
| 属性结构 | `attributes.Attribute` | `ipp.Attr`（**结构完全相同**） |
| group tag 常量 | ✅ | ✅ **重复** |
| 编码循环 | ✅ | ✅ **重复**（374 行） |
| 对 `pkg/attributes` 的引用 | — | **零**（`grep` 全仓仅 1 处命中，在一个探针里） |

## 决策

**`pkg/attributes` 是本仓库唯一的 RFC 8010 编码层。** `pkg/ipp` 必须删除重复的 wire 层并消费它。

职责重新划清：

| 包 | 职责 |
| --- | --- |
| `pkg/attributes` | tag 常量、`Attribute` 类型、RFC 8010 序列化、IPP Everywhere 属性表。**唯一编码实现。** |
| `pkg/ipp` | HTTP 传输层、操作码路由与语义、**用 `go-ipp` 解码请求**。**不再自行编码属性。** |

## 理由：重复已经造成了真实损害，不是理论洁癖

这不是"代码不优雅"的问题，而是一个**已发生的正确性事故**：

同一个 tag `0x23`（enum）在两份实现里出现了**两种宽度** ——
`pkg/ipp/wire.go` 与 `pkg/attributes/writer.go` 的 `Enums()` 写 **2 字节**，
而 `Enum()`/`Int(TagEnum,…)` 写 **4 字节**，**同一份响应内自相矛盾**。

**重复的代价在这里具体表现为：同一类错误被复制了两份，而不是被集中发现一次。**
若只有一份编码器，这个矛盾在写第二处时就会暴露。

### 规范事实（已从 RFC 8010 一手原文确认，非二手摘要）

enum 值在 wire 上**必须 4 字节**，四处独立证据：

1. §3：*"A four-byte integer, henceforth called a **SIGNED-INTEGER, is used for value fields**"*
2. §3.8（规范性 MUST）：*"For any of the types represented by binary signed integers,
   **the sender MUST encode the value in exactly four octets.**"*
3. Table 7（Attribute Value Encoding）：`integer and enum` → **a SIGNED-INTEGER**
4. **Appendix A.2 真实线格式样例**（决定性 —— 它是可逐字节核对的实例）：
   ```
   0x23     enum type     value-tag
   0x0009                 name-length
   job-state              name
   0x0004                 value-length      ← 4 octets
   0x0003   pending       value
   ```

→ 实现中的 2 字节写法**违反 MUST**。
⚠️ 注意 "type2 enum"（RFC 8011 §5.4.15 对 `operations-supported` 的标注）说的是**语义类型**，
**不是 wire 编码宽度**；宽度由 RFC 8010 §3.8 统一规定为 4 字节。这两者被此前混淆了。

## 备选方案与拒绝理由

| 方案 | 拒绝理由 |
| --- | --- |
| **(a) `pkg/ipp` 删除重复层，消费 `pkg/attributes`**（已采纳） | 唯一满足"无巨石/好维护"；让错误只可能存在于一处 |
| (b) 在 `pkg/attributes` 加适配器，让 `pkg/ipp` 保留 `Attr` 名字 | 只是给重复换了个名字，编码仍是两份 —— 治标不治本 |
| (c) 有意接受重复 | 直接违反 README 价值观硬约束，且已造成上面的事故 |

## 后果

- **正面**：错误只有一个可能来源；tag 常量与属性语义集中；后续新增属性（IPP Everywhere 表会持续增长）
  只改一处；与 ADR-0002 D4 的模块划分重新一致。
- **负面**：需要一次跨包重构，`pkg/ipp` 有合并期改动量。
- **要求（防回归）**：
  1. `pkg/attributes` 的公开 API 必须稳定并附**最小接入示例**，否则 `pkg/ipp` 会再次倾向于自己写；
  2. **黄金字节测试必须覆盖 `1setOf enum`（`operations-supported`）** —— 这正是出过错的地方；
  3. 交叉实现验证优先：手抄 RFC 8010 Appendix A 的样例字节并喂给独立实现（`go-ipp` 解码器）比对，
     比自证一致性更强。

## 参考

- [RFC 8010 §3 / §3.8 / Table 7 / Appendix A.2](https://www.rfc-editor.org/rfc/rfc8010)
- [RFC 8011 §5.4.15 `operations-supported`](https://www.rfc-editor.org/rfc/rfc8011)
- [ADR-0002 D4 模块划分](0002-stack-and-architecture.md)
