# 附录 E：`go-ipp` 服务端能力实测（含**已证实的属性编码缺口**）

> **结论速览**：
> - ✅ `go-ipp v1.7.0` **可以**用作 IPP 服务端（请求解码 + 响应编码 + 库内往返均实测通过）；
> - ❌ **但 `AttributeTagMapping` 只认识 70 个属性名**，**缺少几乎所有 IPP Everywhere / AirPrint 必需的
>   `-supported` / `-default` 属性**，且 `Encode` 遇到未知属性名会**直接返回错误**；
> - ⚠️ 因此**必须自研一个"带显式 tag 的 RFC 8010 属性写入器"**，`go-ipp` 只能承担一部分工作。
>
> 采集方式：Team Lead 在本机安装 `github.com/phin1x/go-ipp@v1.7.0`，编写并运行真实 Go 程序实测。
> 本节**修正并细化了** [附录 A](03-verified-blockers.md) 的结论（附录 A 只判断"具备服务端能力"，未发现此缺口）。

## E.1 已实测通过的部分（正面结论）

用库**自身**的编码器产出请求、再用自身解码器读回，实测输出：

```
*** A. REQUEST ENCODE OK ***
    112 bytes: 02 00 00 0B 00 00 00 01 01 47 00 12 61 74 74 72 ... 03
*** A. REQUEST DECODE OK ***
    version=2.0 op=0x000B reqId=1
    charset=utf-8 natural-language=en-us printer-uri=ipp://localhost:631/p
```

**解析 `Get-Printer-Attributes`（op=0x000B）请求 → 完全正确。**
这印证了附录 A：`go-ipp` 的**服务端方向（RequestDecoder + Response + Response.Encode）是可用的**，
**不需要 fork**（附录 A 曾担心 `newResponseEncoder` 是私有函数 —— 实际 `Response.Encode()` 已导出，
封装了它）。

**修正附录 A**：附录 A 写的"可能需要自行补齐导出入口（fork 后加一行导出包装）"**不成立**，
`Response.Encode()` 已导出可用。这一条应从"风险"降级为"已解决"。

## E.2 已证实的**硬缺口**（负面结论，影响工作量估算）

### 缺口 1：属性名映射表只有 70 项

`constants.go` 中 `AttributeTagMapping` 的**完整**键集合（已解析为实际 wire 名称）共 **70 个**：

```
attributes-charset, attributes-natural-language, copies, device-uri, document-format,
document-name, document-number, document-state, finishings, first-job-id, hold-job-until,
job-hold-until, job-id, job-impressions-completed, job-name, job-printer-state-message,
job-printer-state-reasons, job-printer-uri, job-priority, job-sheets, job-state,
job-state-message, job-state-reason, job-state-reasons, job-uri, last-document, limit,
media, media-bottom-margin, media-col, media-col-database, media-col-default,
media-col-ready, media-color, media-left-margin, media-right-margin, media-size,
media-size-name, media-source, media-top-margin, media-type, member-uris, my-jobs,
number-of-documents, number-up, orientation-requested, outputorder, ppd-make-and-model,
ppd-name, print-quality, print-scaling, printer-error-policy, printer-info,
printer-is-accepting-jobs, printer-is-shared, printer-is-temporary, printer-location,
printer-name, printer-resolution, printer-state, printer-state-reasons, printer-uri,
purge-jobs, requested-attributes, requesting-user-name, sides, status-message,
which-jobs, x-dimension, y-dimension
```

### 缺口 2：**IPP Everywhere / AirPrint 必需属性大面积缺失**

对 14 个关键属性逐一核对（这是客户端能否接受我们的打印机的核心）：

| 属性 | 是否在映射表中 | 重要性 |
| --- | --- | --- |
| `attributes-charset` | ✅ YES | 必需（且须排第一） |
| `attributes-natural-language` | ✅ YES | 必需（且须排第二） |
| `printer-name` | ✅ YES | 必需 |
| `printer-state` | ✅ YES | 必需 |
| **`document-format-supported`** | ❌ **缺失** | **必需** —— 决定客户端敢不敢发作业 |
| **`operations-supported`** | ❌ **缺失** | **必需** |
| **`printer-uri-supported`** | ❌ **缺失** | **必需** |
| **`ipp-versions-supported`** | ❌ **缺失** | **必需** |
| **`charset-configured` / `charset-supported`** | ❌ **缺失** | **必需** |
| **`media-supported` / `media-default`** | ❌ **缺失** | **必需** |
| **`sides-supported`** | ❌ **缺失** | **必需** |
| **`color-supported`** | ❌ **缺失** | AirPrint 相关 |
| **`printer-make-and-model`** | ❌ **缺失** | AirPrint 相关 |
| **`urf-supported`** | ❌ **缺失** | **AirPrint 核心** |
| **`pwg-raster-document-type-supported`** | ❌ **缺失** | IPP Everywhere 核心 |

**缺失是系统性的**：映射表里几乎没有 `-supported` / `-default` 系列（除了 `printer-state-reasons`）。
这恰好是"打印机能力声明"的那一半 —— 而**没有能力声明，客户端根本不会发作业**。

### 缺口 3：`Encode` 遇到未知属性**直接报错**，无绕过参数

实测：

```
=== TEST 2: unmapped IPP Everywhere attribute (document-format-supported) ===
    result -> cannot get tag of attribute document-format-supported
    => CONFIRMED BLOCKED: library cannot emit this attribute

=== TEST 3: the escape hatch ===
    ipp.NewAttributeEncoder(w).Encode(name, value) has the SAME mapping lookup,
    so it also fails.
```

源码依据（`attribute_encoder.go`）：

```go
func (e *AttributeEncoder) Encode(attribute string, value any) error {
	tag, ok := AttributeTagMapping[attribute]
	if !ok {
		return fmt.Errorf("cannot get tag of attribute %s", attribute)
	}
	...
```

**没有** `WithTag(...)` 之类的参数可以显式指定 tag。
而且注意 `switch v := value.(type)` 里的**类型-标签耦合校验**：
`int` 只能配 `TagInteger`/`TagEnum`，传入类型与映射 tag 不符也会报错
（`tag for attribute %s does not match with value type`）。

## E.3 可行的绕过方案（已实测编码可行）

**自研一个"带显式 tag 的属性写入器"**，绕过映射表。实测手写 RFC 8010 编码成功：

```
=== TEST 4: hand-rolled RFC 8010 encoding of the missing set ===
    hand-rolled 154 bytes: 49 00 19 64 6F 63 75 6D 65 6E 74 2D 66 6F 72 6D 61 74 2D
    73 75 70 70 6F 72 74 65 64 00 0F 61 70 70 6C 69 63 61 74 69 6F 6E 2F 70 64 66
    49 00 00 00 09 69 6D 61 67 65 2F 75 72 66 ...
```

确认可行的编码规则（RFC 8010）：
- 每个属性：`tag(1B)` + `name-length(2B)` + `name` + `value-length(2B)` + `value`；
- **1setOf 的额外取值**：重复 `tag(1B)` + **零长度 name(0x0000)** + `value-length` + `value`；
- `operations-supported` 是 **1setOf enum**（值 2 字节），不是字符串；
- `document-format-supported` 是 **1setOf mimeMediaType**（tag 0x49，字符串）。

**注意**：`operations-supported` 的 enum 值必须写成**大端 2 字节二进制**（如 `0x0002` = Print-Job），
**不能**当字符串写 —— 这是手写编码最容易错的地方，必须用 `ipptool` 验证。

## E.4 对工作量与架构的影响（**这是本附录存在的意义**）

| 项 | 修正前（附录 A） | 修正后（本附录） |
| --- | --- | --- |
| `go-ipp` 能否做服务端 | ✅ 具备 | ✅ **具备，且无需 fork** |
| 响应编码 | ⚠️ "可能需 fork 补齐" | ✅ **不需要**（`Response.Encode()` 已导出） |
| IPP Everywhere 属性表 | 未评估 | ❌ **需自研带 tag 的写入器**（约 70 项映射之外全部要手写） |
| `ipp/` 模块定位 | "基于 go-ipp，不足部分补齐" | **go-ipp 负责：请求解析、属性值类型解码、报文框架；自研负责：带显式 tag 的能力属性序列化** |

**结论**：`go-ipp` 仍然值得用（省掉请求解析与类型解码），
但**"IPP Everywhere 必需属性表 + 带 tag 的序列化"是一块独立且必须自研的工作**，
必须在 `pkg/attributes/` 中实现，并用 `ipptool` 的 `get-printer-attributes-suite.test` 对拍验收。

**这加强了 ADR-0002 的判断**：IPP 服务端生态确实空缺，**选 Go 不等于省掉 IPP 工作量**，
只是省掉了"从零写二进制协议解析器"这一层。

## E.5 复现方式

```powershell
# 装库并运行探针
mkdir $env:TEMP\ippproof; cd $env:TEMP\ippproof
go mod init ippproof
go get github.com/phin1x/go-ipp@latest      # -> v1.7.0
# 放入探针 main.go（见本会话），然后：
go run .

# 阅读关键源码（模块缓存）
$dir = "$(go env GOPATH)\pkg\mod\github.com\phin1x\go-ipp@v1.7.0"
Get-Content "$dir\constants.go"          # AttributeTagMapping：注意只有 70 项
Get-Content "$dir\attribute_encoder.go"  # Encode 的映射查表与报错
Get-Content "$dir\response.go"           # Response.Encode() 已导出
Get-Content "$dir\response_encoder.go"   # encodeOperationAttributes 强制 charset 优先
```

**额外发现（正面）**：`response_encoder.go` 的 `encodeOperationAttributes` 内置了**强制顺序**：

```go
order := []string{
	AttributeCharset,
	AttributeNaturalLanguage,
	AttributePrinterURI,
	AttributeJobID,
}
```

即库**自动保证** `attributes-charset` 排第一、`attributes-natural-language` 排第二 ——
这正是 RFC 8010 / IPP Everywhere 的强制要求（见简报 §"编码强制顺序"）。
**这是一个真实的收益**：用 `Response.Encode()` 就不必自己维护属性顺序。

## 参考来源

- `phin1x/go-ipp` v1.7.0（Apache-2.0）：<https://github.com/phin1x/go-ipp>
- RFC 8010（IPP 编码，属性与 1setOf 的线格式）：<https://www.rfc-editor.org/rfc/rfc8010>
- RFC 8011（IPP 语义，操作与必需属性）：<https://www.rfc-editor.org/rfc/rfc8011>
- PWG 5100.14 IPP Everywhere（必需属性清单）：<https://www.pwg.org/ipp/everywhere.html>
