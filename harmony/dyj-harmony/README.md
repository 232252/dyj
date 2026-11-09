# dyj 鸿蒙接收端（React Native 工程）

这是 dyj 局域网打印共享工具的**鸿蒙手机端**：发现共享端 → 输入 4 位配对码 →
选内容 → 打印。

**构建、签名、装机、联调的完整步骤在 [../README.md](../README.md)** —— 先看那份。

本目录是 React Native 工程（RNOH 0.84.5 / RN 0.84.1），`harmony/` 子目录是
ArkTS 工程。代码分工：

| 位置 | 放什么 |
| --- | --- |
| `src/ipp`、`src/mdns`、`src/format` | 协议编解码 —— 可单测，与 Go 侧共用黄金字节 |
| `src/client`、`src/state`、`src/screens` | 配对、打印、发现与界面 |
| `src/specs` | TurboModule 规格（codegen 的输入） |
| `harmony/entry/src/main/ets/native` | 六个 TurboModule 的 ArkTS 实现 |

设计与取舍：[ADR-0012](../../docs/adr/0012-harmony-receiver-rnoh.md)。
