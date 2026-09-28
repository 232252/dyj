# ADR-0007：渲染管线与"只声明能渲染的格式"

- **状态**：已接受
- **日期**：2026-09-29
- **依据**：[ADR-0002 D3 设备接入](0002-stack-and-architecture.md)、
  [PWG 5102.4 PWG Raster]、CUPS `cups/raster-stream.c` 一手源码（2026-09 核对）

## 背景

ADR-0002 D3 决定"把 IPP 端点架在已有打印队列之上"。真正落进代码时必须回答：
**客户端发来的文档，服务端拿什么变成像素？** 这决定了
`document-format-supported` 声明什么——而声明直接决定客户端发什么。

调研阶段（附录 E / 调研索引 P0 #2）遗留过乐观声明：
`application/pdf`、`image/urf` 都出现在默认表里。写渲染代码时必须纠正。

## 决策

### D1. 格式声明 = 渲染能力，一字不差

`document-format-supported` 只声明 `pkg/render` 能用**标准库**解码的格式：

| 格式 | 渲染路径 | 状态 |
| --- | --- | --- |
| `image/pwg-raster` | 自研解码器（PWG 5102.4） | ✅ IPP Everywhere 必需 |
| `image/jpeg` | `image/jpeg` 解码 → 缩放适配页面 | ✅ 彩色机 PWG 必需 |
| `image/png` | `image/png` 解码 → 缩放适配页面 | ✅ |
| `text/plain` | 自研布局引擎（7x13 位图字体 + 自动换行分页） | ✅ |

**不声明** `application/pdf`（无渲染器；Ghostscript 是 AGPL 传染 + 数十 MB，
直接违背 ADR-0002 的许可证与体积决策）与 `image/urf`（Apple 专有规范，
无权威原文可依，无法在不编造字节布局的情况下实现——项目价值观禁止编造）。

**虚假声明的代价**：声明 PDF 却渲染不了 → 客户端把作业发来 → 纸没出来。
这比"不支持"糟糕得多，因为它不可诊断。声明表必须等于能力表。

### D2. PWG 光栅实现逐字节对齐 CUPS

唯一权威参照是 CUPS 的实现（`OpenPrinting/cups` `cups/raster-stream.c`，
2026-09 从一手源码核对）。文件格式：

```
"RaS2"(4) { 1796 字节网络序 v2 页头（大部分置零，MediaClass="PwgRaster"）,
            行数据: [行重复计数-1(1B)] + modified-PackBits(行) }*
```

modified-PackBits 与经典 PackBits 的差异已锁定进代码与测试：
单像素用 `0x00 + bpp` 字节、`0x80` = 行余下置白、literal 编码 `257-count`
（恒 ≥2 像素）。行重复计数上限 256（开发中实际抓到过溢出 bug，测试锁定）。

### D3. Windows 投递走 GDI（复用厂商驱动），页位图经 StretchDIBits

`driver/wingdi`：`OpenPrinter → DocumentProperties → CreateDC("WINSPOOL")`
→ `StartDoc → {StartPage → StretchDIBits → EndPage}* → EndDoc`。
厂商驱动负责打印机语言编码；dyj 自身零驱动、零 PPD。
纸张/份数通过 DEVMODE 字节偏移 patch（不重定义整个结构体，杜绝布局漂移）。

`driver/file` 把每页存成 PNG（调试/E2E/"打印成图片"三用）；`driver/null` 供测试。
**明确砍掉** raw `/dev/usb/lp0` 后端：我们声明的都是光栅格式，
把光栅流灌进期望 PCL/ESC-P 的哑打印机只会出废纸——与 D1 同一条诚实原则。

### D4. 内存模型：文档流式、页面物化

文档级永远流式（IPP 解码器 → spool 落盘 → 后端重开文件），作业大小无内存上限；
页面级物化单页位图（A4@300dpi sRGB ≈ 26 MB），因为 GDI 投递本来就需要整页位图。
解码端有尺寸上限（2^20 px/边）防恶意头。

## 后果

- **正面**：声明即可信；标准库之外仅 `golang.org/x/image`（位图字体/缩放）；
  Windows 路径复用一切已装驱动；E2E 用 file 后端验证全管线。
- **负面**：iOS/macOS 的 AirPrint（URF/PDF）客户端暂不能打（后续若取得 URF 权威规范，
  可按 D1 程序补上声明）；PDF 打印需求暂时要客户端先转成图片。
- **已知弱点**：文本布局是位图字体（ASCII 为主），复杂排版请用图片格式；
  JPEG 解码到 300 dpi 位的内存峰值 ~30 MB/页，低端设备上可降到 200 dpi。

## 参考

- CUPS `raster-stream.c` / `raster.h`（OpenPrinting，一手源码）
- PWG 5102.4 (PWG Raster / IPP Everywhere 自描述光栅格式)
- RFC 8011 §5.1.10（resolution 语法）、PWG 5100.14（格式要求）
