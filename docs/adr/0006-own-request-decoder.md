# ADR-0006：自研 RFC 8010 请求解码器（弃用 go-ipp 解码路径）

- **状态**：已接受
- **日期**：2026-09-29
- **依据**：[ADR-0005 单一编码层](0005-single-wire-encoder.md)、go-ipp v1.7.0 一手源码（`request_decoder_state_machine.go`）

## 背景

ADR-0005 确立了"`pkg/attributes` 是唯一 RFC 8010 编码层"（响应方向）。请求方向的解码仍走
`phin1x/go-ipp`——附录 E 实证过它的**编码器**不可用，但当时认为"解码是它做得好的部分"。

本轮开发中 `TestRequestedAttributesFilter` 持续失败：CUPS/Windows 客户端把
`requested-attributes` 作为 **1setOf keyword** 发送（多个值），服务端只看到最后一个值。

## 根因（一手源码证据）

go-ipp v1.7.0 `request_decoder_state_machine.go`，属性循环内：

```go
// TODO FIX: handle attributes with array values
// append attribute to list
r.currentAttributes[r.currentAttributeName] = attrib.Value
```

**重复属性名直接覆盖**，前 N-1 个值丢失。这是该库的一个已知未修 TODO。
对客户端库无害（客户端构造请求），对服务端是**协议正确性缺陷**：
驱动无打印（driverless）交换中 `requested-attributes`、`media-supported` 等 1setOf 属性无处不在。

## 决策

**`pkg/attributes.DecodeMessageStream` 是请求方向的唯一解码器。** go-ipp 降级为
**仅测试中的交叉验证工具**，不参与生产路径（`go.mod` 仍保留依赖）。

关键性质：

1. **保留每一个值**：重复属性名聚合为一个 `Attribute` 的多值 `Values`（RFC 8010 §3.6）。
2. **流式**：属性组从 `io.Reader` 增量解析，文档字节留在 `Message.DataReader`
   ——100 MB 的 PWG 光栅作业永不整体进内存。
3. **严格**：`attributes-charset` 必须是操作组第一属性（RFC 8011 §4.1.4）、值越界即错、
   未知 delimiter 即错——服务端对畸形请求的回答是 0x0400 而不是 panic 或失步。
4. **无损往返**：decode→re-encode 逐字节还原，测试锁定。

## 备选方案与拒绝理由

| 方案 | 拒绝理由 |
| --- | --- |
| fork go-ipp 修掉覆盖 bug | 维护一个 fork 比自带 300 行严格解码器贵得多；违背"好维护" |
| 在 go-ipp 之上二次解析 | 它已经把值丢了，无法恢复 |
| 自己限制客户端"别发多值" | 违反 RFC 8011，真实客户端（CUPS）必然发多值 |

## 后果

- **正面**：requested-attributes 过滤正确；解码/编码同一份代码，一类 bug 只可能存在一处；
  流式解码是 spooler 落盘直通的前提。
- **负面**：多 ~300 行自有代码；go-ipp 依赖仅剩测试用途（未来可移除）。
- **防回归**：`TestDecodeKeepsEveryValue`（多值保留）、`TestDecodeRoundTrip`
  （逐字节往返）、`TestDecodeErrors`（7 个畸形输入分支）锁定。
