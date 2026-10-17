# 附录 I：P0 对抗性实证核查报告（红队）

> **编号**：本文曾与 [附录 F](08-independent-review.md)（调研结论复核）同名。
> 附录 F 只指 08。本文是 wire 格式红队报告。
>
> **状态（2026-10-17）**：§9 列出的代码缺陷（`writeValuePair` 字段顺序、enum
> 4 字节、resolution 宽度、双编码器）已在 v0.1 前修掉，由 ADR-0005/0006 与
> `pkg/attributes` 黄金测试锁定。下面的复现命令描述的是**报告当时**的失败，
> 不是当前树。§8 里 Win11 原生添加 GUI、`ipptool`、Apple AirPrint 原文仍然
> 没有在本仓库里取到——那是现场/外部材料，不是未合并的补丁。

> **作者**：adversarial-verifier（task-3）
> **KPI 定位**：本任务的价值是**找出错误**，不是确认正确。以下每条结论都尽可能附**可复现命令 + 真实输出**。
> **写权限**：仅本文件。**未修改任何 `.go` 文件** —— 所有缺陷均以报告形式提交给 lead，由对应 owner 修。
>
> ## 证据分级（全文严格遵守，三者绝不混用）
>
> | 标记 | 含义 |
> | --- | --- |
> | **【实测】** | 我在本机真实执行命令／程序得到的输出，附命令与原始输出 |
> | **【RFC 原文】** | 我从 `rfc-editor.org` / `pwg.org` 一手抓取的规范原文，逐字引用 |
> | **【源码原文】** | 我从一手源码文件（本地 module cache 或 raw.githubusercontent.com）读到的真实代码行 |
> | **【推断】** | 我基于上述证据的推理，未被直接观测证实 |
>
> 网络可达性实测：**rfc-editor.org ✅ / pwg.org ✅ / raw.githubusercontent.com ✅**（GitHub 主站未测试，未用到）。
> 本机无 `ipptool`、无 `dns-sd`（实测 `MISS`）；有 `curl`、`go1.26.4 windows/amd64`。

---

## 0. 结论摘要（TL;DR）

| # | 结论 | 定性 | 证据级别 |
| --- | --- | --- | --- |
| 1 | **`writeValuePair` 字段顺序违反 RFC 8010 Fig.4，导致响应无法被任何客户端解析** | 🔴 **P0 致命** | 【实测】+【RFC 原文】 |
| 2 | `operations-supported` 的 enum 元素必须是 **4 字节**；2 字节会让独立解码器直接崩溃 | 🔴 **P0** | 【实测】+【RFC 原文】+【源码原文】 |
| 3 | 代码注释引用 **RFC 8011 §4.4.15 系编造**，真节号为 §5.4.15 | 🟠 文档缺陷 | 【RFC 原文】 |
| 4 | 曾同时存在**两份互相矛盾的 wire 编码器**，致 tag 0x23 双宽度而无人察觉 | 🟠 架构缺陷（已由 ADR-0005 裁决） | 【实测】 |
| 5 | 一度**编译失败**（两个包全红），期间任何"服务能起/能响应"结论均不成立 | 🟠 过程事实 | 【实测】 |
| 6 | 1setOf 额外取值的 `name-length` 必须为 `0x0000` —— 实现**这一条做对了** | ✅ 攻击未奏效 | 【RFC 原文】+【源码阅读】 |
| 7 | tag 取值表（0x21/0x22/0x23/0x41/0x42/0x44/0x45/0x46/0x47/0x48/0x49/0x31/0x32/0x33/0x13）**全部正确** | ✅ 攻击未奏效 | 【RFC 原文】+【源码阅读】 |
| 8 | 多字节整数大端序 —— 实现**正确** | ✅ 攻击未奏效 | 【RFC 原文】+【源码阅读】 |
| 9 | **core acceptance（Win11 直接添加该 IPP 打印机）：当前已证伪，非"待测"** | 🔴 见 §7 | 【推断】基于 #1 |

**最重要的一句**：**#1 是比 #2 严重得多的缺陷。** enum 宽度错只是"某个属性值读错"，而 #1 让**整个属性组从第一个属性起就无法解析** —— 接收方会把 name 的前 2 字节当作 value-length。

---

## 1. 【实测】本机基线

```powershell
$ go version
go version go1.26.4 windows/amd64

$ go env GOOS GOARCH
windows
amd64
```

工具可用性探测：

```powershell
$ foreach($c in @('ipptool','dns-sd','curl','go')){ ... }
MISS ipptool
MISS dns-sd
OK  curl -> C:\WINDOWS\system32\curl.exe
OK  go -> C:\Users\sq199\scoop\shims\go.exe
```

**【实测】** 本机**没有** `ipptool`，也**没有** `dns-sd`。这一点很重要：
- 无法用 PWG 官方一致性测试工具做黑盒验证；
- 无法用 Apple 的 `dns-sd` 工具做服务发现侧验证；
- 因此本轮 DNS-SD 侧的通告**未能用官方工具验证**（诚实声明，见 §8）。

**【推断】** 本机是 Windows 11，`dns-sd.exe` 通常随 Bonjour 一起安装；此处缺失说明未装 Bonjour，这**不影响** IPP over HTTP 的端到端验证，但影响 mDNS 发现类验证。

---

## 2. 【RFC 原文】规范依据（我独立抓取，非二手摘要）

### 2.1 报文与属性编码结构 —— RFC 8010 §3.1.4 Figure 4

`https://www.rfc-editor.org/rfc/rfc8010.txt`（HTTP 200，逐字引用）：

```
3.1.4.  Attribute-with-one-value

   Each "attribute-with-one-value" field is encoded as follows:

   -----------------------------------------------
   |                   value-tag                 |   1 byte
   -----------------------------------------------
   |               name-length  (value is u)     |   2 bytes
   -----------------------------------------------
   |                     name                    |   u bytes
   -----------------------------------------------
   |              value-length  (value is v)     |   2 bytes
   -----------------------------------------------
   |                     value                   |   v bytes
   -----------------------------------------------
```

**关键点：字段顺序是 `tag → name-length → name → value-length → value`。**
`value-length` 出现在 **name 之后**，**不是**与 `name-length` 相邻。

### 2.2 多取值（additional-value）—— §3.1.5

```
   -----------------------------------------------
   |                   value-tag                 |   1 byte
   -----------------------------------------------
   |            name-length  (value is 0x0000)   |   2 bytes
   -----------------------------------------------
   |              value-length (value is w)      |   2 bytes
   -----------------------------------------------
   |                     value                   |   w bytes
   -----------------------------------------------
```

以及 §3.6：

> If a "name-length" field has a value of zero, the following "name" field
> is empty and the following value is treated as an additional value for
> the attribute encoded in the nearest preceding "attribute-with-one-value"
> field.

**【RFC 原文】** 额外取值的 `name-length` **必须是 `0x0000`**，且此时 name 字段为空。

### 2.3 整数宽度 —— §3 与 §3.8（enum 争议的定论依据）

§3：

> Every integer in this encoding MUST be encoded as a signed
> integer using two's-complement binary encoding with big-endian format
> ... A two-byte integer, henceforth called a SIGNED-SHORT, is used for the
> operation-id, status-code, and length fields.  A four-byte integer,
> henceforth called a SIGNED-INTEGER, is used for value fields and the
> request-id.

§3.8：

> For any of the types represented by binary signed integers, the
> sender MUST encode the value in exactly four octets.

Table 7（Attribute Value Encoding）：

```
   +----------------------+--------------------------------------------+
   | integer and enum     | a SIGNED-INTEGER                           |
```

**【RFC 原文·定论】** `enum` 与 `integer` **同属** `SIGNED-INTEGER` = **4 字节**。
注意 `SIGNED-SHORT`（2 字节）**只用于** `operation-id`/`status-code`/`length` 三类**结构字段**，**不用于 value 字段**。这正是"operation-id 是 2 字节"被误推为"operations-supported 的元素也是 2 字节"的根源 —— 二者是**不同用途**，不能混。

### 2.4 真实字节样例 —— RFC 8010 Appendix A.2（enum 宽度的**直接**实证）

我逐字抄录 `A.2. Print-Job Response (Successful)` 原文：

```
    0x23                             enum type          value-tag
    0x0009                                              name-length
    job-state                        job-state          name
    0x0004                                              value-length
    0x0003                           pending            value
```

**【RFC 原文·铁证】** tag `0x23`（enum）的 `value-length` = **`0x0004`**。
若 enum 是 2 字节，这里必然是 `0x0002`。**故 enum 值只能是 4 字节。**
（附录 A.1 中 `job-state` 未出现，但 `copies` 的 `0x0004` 佐证 integer 亦为 4 字节。）

### 2.5 RFC 8011 中 `operations-supported` 的确切节号与类型

`https://www.rfc-editor.org/rfc/rfc8011.txt` 目录行（逐字）：

```
   5.4.15. operations-supported (1setOf type2 enum) .........157
```

**【RFC 原文】** 节号是 **§5.4.15**，类型标注为 **`1setOf type2 enum`**。
**【推断】** 这里的 `type2` 是 **Model 层的语义类型分类**（RFC 8011 §2.3 的 type1/type2 体系），**不是 wire 宽度**。把它读成"2 字节"是把语义类型与编码宽度混为一谈 —— 原始代码注释正是踩了这个坑。

---

## 3. 【实测】我独立推导的字节向量（**在阅读任何实现代码之前**算出）

方法：严格按 RFC 8010 Fig.4/Fig.5 手写编码器，与实现完全独立。
产物：临时程序 `C:\Users\sq199\AppData\Local\Temp\ippvec\main.go`。

### 3.1 报告头（RFC 8010 Fig.1）

```
version-number(2) | operation-id/status-code(2) | request-id(4) | attribute-group* | end-of-attributes-tag(1) | data?
```

### 3.2 RFC 8010 Appendix A.1 完整 Print-Job 请求（我的独立重算）

```
010100020000000101470012617474726962757465732d6368617273657400057574662d38
48001b617474726962757465732d6e61747572616c2d6c616e67756167650005656e2d7573
45000b7072696e7465722d757269002c6970703a2f2f7072696e7465722e6578616d706c652e
636f6d2f6970702f7072696e742f70696e65747265654200086a6f622d6e616d650006666f
6f6261722200166970702d6174747269627574652d666964656c69747900010102
210006636f70696573000400000014
4400057369646573001374776f2d73696465642d6c6f6e672d65646765
03
```
（为便于阅读我在上面按属性折行；实际是连续的 227 字节）

**子字段长度自检（与 RFC A.1 公布值逐一比对）**：

| 子字段 | 我算出 | RFC A.1 公布 | 结果 |
| --- | --- | --- | --- |
| `attributes-charset` name-length | 18 | `0x0012` = 18 | ✅ |
| `attributes-natural-language` name-length | 27 | `0x001b` = 27 | ✅ |
| `printer-uri` value-length | 44 | `0x002c` = 44 | ✅ |
| `ipp-attribute-fidelity` name-length | 22 | `0x0016` = 22 | ✅ |
| `sides` value-length | 19 | `0x0013` = 19 | ✅ |
| `copies` value-length | 4 | `0x0004` | ✅ |

**【实测】** 我的独立推导与 RFC A.1 完全一致 —— 这证明我的编码器基线是正确的，可以用它来判定实现的对错。

### 3.3 单属性「正确编码」向量（用作 golden 判据）

| 属性 | tag | 正确字节（hex） |
| --- | --- | --- |
| `printer-uri`（uri 0x45） | `0x45` | `45000b 7072696e7465722d757269 002c 6970703a2f2f7072696e7465722e6578616d706c652e636f6d2f6970702f7072696e742f70696e6574726565` |
| `attributes-charset`（charset 0x47） | `0x47` | `470012 617474726962757465732d63686172736574 0005 7574662d38` |
| `job-name`（name 0x42） | `0x42` | `420008 6a6f622d6e616d65 0006 666f6f626172` |
| `sides`（keyword 0x44） | `0x44` | `440005 7369646573 0013 74776f2d73696465642d6c6f6e672d65646765` |
| `copies`（integer 0x21） | `0x21` | `210006 636f70696573 0004 00000014` |
| `job-state`（enum 0x23） | `0x23` | `230009 6a6f622d7374617465 0004 00000003` |
| `sides-supported`（1setOf keyword） | `0x44` | `44000f 73696465732d737570706f72746564 0009 6f6e652d7369646564` + `44 0000 0013 74776f2d73696465642d6c6f6e672d65646765` + … |
| `copies-supported`（rangeOfInteger 0x33） | `0x33` | `330010 ... 0008 00000001000003e7` |
| `printer-resolution-default`（resolution 0x32，9 字节） | `0x32` | value = `0000012c 0000012c 03` |
| `job-name` no-value（0x13） | `0x13` | `130008 6a6f622d6e616d65 0000`（value-length MUST be 0，§3.8） |

### 3.4 `operations-supported` 1setOf enum 的两种宽度（对比向量）

以 10 个操作 `{0x0002,0x0004,0x0005,0x0008,0x0009,0x000a,0x000b,0x0000,0x0001,0x000d}` 为例：

**4 字节版（RFC 正确）** —— `230014 6f7065726174696f6e732d737570706f72746564 0004 00000002` 后接 9 组 `23 0000 0004 xxxxxxxx`：
```
2300146f7065726174696f6e732d737570706f72746564000400000002
230000000400000004
230000000400000005
230000000400000008
230000000400000009
23000000040000000a
23000000040000000b
230000000400000000
230000000400000001
23000000040000000d
```
总长 **110 字节**。

**2 字节版（错误）**：总长 **90 字节**，首值 `... 0002 0002`。

**【实测】** 两者长度差 20 字节（10 个元素 × 2 字节）—— 这正是字段错位后解析器"多读/少读"导致失步的来源。

---

## 4. 🔴 P0 致命缺陷 #1：`writeValuePair` 字段顺序违反 RFC 8010 Fig.4

### 4.1 缺陷代码（`pkg/attributes/writer.go:428-448`）

```go
func writeValuePair(w io.Writer, tag byte, name string, value []byte) error {
	var hdr [5]byte
	hdr[0] = tag
	binary.BigEndian.PutUint16(hdr[1:3], uint16(len(name)))
	binary.BigEndian.PutUint16(hdr[3:5], uint16(len(value)))   // <-- value-length 放进同一个 5 字节头

	if _, err := w.Write(hdr[:]); err != nil { return err }
	if name != "" { ... w.WriteString(name) }
	if len(value) > 0 { ... w.Write(value) }
	return nil
}
```

实际发出的字节顺序是：

```
tag(1) | name-length(2) | value-length(2) | name(u) | value(v)
                           ^^^^^^^^^^^^^^^^ 位置错误
```

**RFC 8010 §3.1.4 Fig.4 要求**：

```
tag(1) | name-length(2) | name(u) | value-length(2) | value(v)
```

### 4.2 【实测】`go test ./...` 的 20 条失败（原始输出节选）

```
--- FAIL: TestCrossValidateA2ResponseWithDecoder (0.00s)
    crossvalidate_test.go:203: Enum(job-state,3) does not reproduce RFC 8010 A.2's bytes
        ours 23000900046a6f622d737461746500000003
        rfc  2300096a6f622d7374617465000400000003
--- FAIL: TestGoIppDecoderReadsOurEnum (0.00s)
    crossvalidate_test.go:233: go-ipp did not recover operations-supported
--- FAIL: TestOperationsSupportedUsesFourOctetEnums (0.00s)
    everywhere_test.go:267: operations-supported golden mismatch
        got  23001400046f7065726174696f6e732d737570706f727465640000000223...
        want 2300146f7065726174696f6e732d737570706f7274656400040000000223...
--- FAIL: TestPrinterURISupportedGolden (0.00s)
    everywhere_test.go:284: printer-uri-supported golden mismatch
        got  45001500207072696e7465722d7572692d737570706f72746564697070...
        want 4500137072696e7465722d7572692d737570706f727465640020697070...
--- FAIL: TestDocumentFormatSupportedGolden (0.00s)
    everywhere_test.go:302: document-format-supported golden mismatch
--- FAIL: TestWholePrinterGroupIsEncodable (0.00s)
    everywhere_test.go:325: encoded printer group is not well-formed: truncated value at 0
--- FAIL: TestGoldenSingleValues/uri_(string)
    writer_test.go:188: golden mismatch for printer-uri
        got  (60 bytes) 45000b002c7072696e7465722d7572696970703a2f2f...
        want (60 bytes) 45000b7072696e7465722d757269002c6970703a2f2f...
    --- FAIL: TestGoldenSingleValues/charset
        got  4700120005617474726962757465732d636861727365747574662d38
        want 470012617474726962757465732d6368617273657400057574662d38
    --- FAIL: TestGoldenSingleValues/nameWithoutLanguage
        got  42000800066a6f622d6e616d65666f6f626172
        want 4200086a6f622d6e616d650006666f6f626172
    --- FAIL: TestGoldenSingleValues/keyword
        got  4400050013736964657374776f2d73696465642d6c6f6e672d65646765
        want 4400057369646573001374776f2d73696465642d6c6f6e672d65646765
```

**判读（【实测】）**：每条 `got` 的差异模式**完全一致** —— `0x002c`/`0x0005`/`0x0006`/`0x0013` 这些**value-length** 值，出现在了 **name 应该在的位置**。
例如 `charset`：`470012 0005 617474726962757465732d63686172736574...`
`0x0005` 是 `"utf-8"` 的长度，却被写在了 `attributes-charset`（name）之前。

### 4.3 【实测】独立解析器证明"无法解析"

我另行实现了严格遵循 RFC 8010 Fig.4 的解析器，分别吃两种字节流：

```
--- uri: tag=0x45 name="printer-uri" value="ipp://printer.example.com/ipp/print/pinetree" ---
  BUGGY   ( 60 B): 45000b002c7072696e7465722d7572696970703a2f2f...
  RFC 8010 Fig.4 parse -> ERROR: value-length 29289 overruns buffer at 14 (len 60)
  CORRECT ( 60 B): 45000b7072696e7465722d757269002c6970703a2f2f...
  RFC 8010 Fig.4 parse -> name="printer-uri" value="ipp://printer.example.com/ipp/print/pinetree"  OK

--- charset ---
  BUGGY   : 4700120005617474726962757465732d636861727365747574662d38
  parse   -> ERROR: value-length 25972 overruns buffer at 21 (len 28)

--- job-name ---
  BUGGY   : 42000800066a6f622d6e616d65666f6f626172
  parse   -> ERROR: value-length 28005 overruns buffer at 11 (len 19)

--- sides ---
  BUGGY   : 4400050013736964657374776f2d73696465642d6c6f6e672d65646765
  parse   -> ERROR: value-length 25971 overruns buffer at 8 (len 29)
```

**机理（【推断】，已由上面输出直接支持）**：解析器读到 `name-length=0x0012` 后跳过 name 位置，紧接着把 **name 的前 2 个字节**（`0x0005`/`0x002c`/`0x0006`/`0x0013` 等）当作 `value-length`；由于实际 name 较长（如 `attributes-charset` 前两字节 `0x6174` = 24948），数值巨大且远超缓冲区，于是立刻判定越界。**结果是整个属性组从第一个属性起就崩，而不是只有某个值读错。**

### 4.4 【实测】我的修正版与 RFC A.1 **逐字节相同**

```
RFC A.1 printer-uri : 45000b7072696e7465722d757269002c6970703a2f2f7072696e7465722e6578616d706c652e636f6d2f6970702f7072696e742f70696e6574726565
我们的正确版          : 45000b7072696e7465722d757269002c6970703a2f2f7072696e7465722e6578616d706c652e636f6d2f6970702f7072696e742f70696e6574726565
byte-identical to RFC A.1: true
```

**修法（明确、可直接落地）**：不要拼一个 5 字节头，改为**分两段写**：

```go
// 正确顺序：tag | name-length | name | value-length | value
w.Write([]byte{tag, byte(len(name) >> 8), byte(len(name))})
w.Write([]byte(name))
w.Write([]byte{byte(len(value) >> 8), byte(len(value))})
w.Write(value)
```

**【推断】** 值得注意：仓库里**已有的 golden 测试是正确的**（`writer_test.go`、`everywhere_test.go` 的 `want` 与我的独立向量一致），**是生产代码写错了**。说明测试是照 RFC 写的、实现没跟上。这批测试是本轮最有价值的资产。

---

## 5. 🔴 P0 缺陷 #2：`operations-supported` 的 enum 宽度

### 5.1 lead 提出的裁决，我用三条独立证据复核

lead 判定"enum 必须 4 字节"。我被要求**把它当作待推翻对象**去攻。**攻击结果：无法推翻，且找到强反证。**

**证据 A — RFC 8010 Appendix A.2 原文（§2.4 已引）**：`job-state`（tag `0x23`）`value-length = 0x0004`。**【RFC 原文】**
**证据 B — RFC 8010 §3 "SIGNED-INTEGER is used for value fields" + §3.8 "MUST ... exactly four octets" + Table 7 `integer and enum` → SIGNED-INTEGER**。**【RFC 原文】**

**证据 C — 独立实现 go-ipp v1.7.0 源码（本地 module cache 一手读取）**：

```
attribute_encoder.go:332   const sizeInteger = int16(4)
attribute_encoder.go:334   func (e *AttributeEncoder) encodeInteger(i int32) error {
attribute_encoder.go:335       binary.Write(e.writer, binary.BigEndian, sizeInteger)
attribute_decoder.go:30        case TagEnum, TagInteger:   d.decodeInteger()
attribute_decoder.go:252       case TagEnum, TagInteger:   d.decodeInteger()
```
以及 encoder 中 **7 处** 都是 `if tag != TagInteger && tag != TagEnum`（`:29,:45,:61,:77,:93,:109,:133`）—— **enum 与 integer 全程共用同一条 4 字节路径**。**【源码原文】**

**证据 D — CUPS `cups/ipp.c`（raw.githubusercontent 一手取得）**：
`IPP_TAG_INTEGER` 与 `IPP_TAG_ENUM` **总是成对出现在同一条件里**（`:464`、`:511`、`:1260`、`:2916`），`:1416-1417` 更是同一 `case` 分支共享实现。**CUPS 从不区分二者宽度。**【源码原文】**

### 5.2 【实测】决定性实证：2 字节版本会让独立解码器**崩溃**

我把两种宽度分别封成完整 IPP 请求，喂给 go-ipp（**独立实现**）解码：

```
===== A. 2-OCTET enum elements =====
wire bytes (191): 0200000b0000000101 47 0012 ... 04 230014 6f7065726174696f6e732d737570706f72746564 0002 0002 23 0000 0002 0004 ...
  go-ipp DECODE ERROR: unsupported attribute group: 0x00   <-- INDEPENDENT DECODER REJECTS

===== B. 4-OCTET enum elements =====
wire bytes (205): 0200000b0000000101 47 0012 ... 04 230014 6f7065726174696f6e732d737570706f72746564 0004 00000002 23 0000 0004 00000004 ...
  decoded operations-supported = ... (正常解析)
```

错误抛出点（**【源码原文】**）：
```
request_decoder_state_machine.go:108
    return nil, fmt.Errorf("unsupported attribute group: 0x%02x", r.currentAttributeGroupTag)
```

**【实测·关键】** 2 字节 enum **不只是"不合规"**，而是让独立实现**完全失步**：少读 2 字节后，解码器把后续数据错位解释成"属性组 tag"，读到 `0x00` 就报 `unsupported attribute group`。
**这正是 §4 那个 header bug 的同类后果** —— 都是"位数错 → 失步 → 整体不可解析"。

### 5.3 反例检索（我尽力找了，结论是找不到）

我专门检索"是否有权威依据支持 2 字节"：

| 候选反例 | 实测结果 |
| --- | --- |
| `ipptool` 解析器实现 | **本机无 `ipptool`**，无法取得其源码做一手验证 —— **诚实声明：此项未能验证** |
| CUPS 源码 | 反证（enum/integer 同宽，见 §5.1 证据 D） |
| PWG 规范示例 | **PWG 5100.14 PDF 未取得**（web_search 端点报 401 认证失败，PDF 亦未取到）—— **此项未能验证** |
| go-ipp | 反证（见证据 C） |

**【实测】** `web_search` 工具在本会话**不可用**：
```
Error: DeepSeek API error (HTTP 401): Authentication Fails, Your api key: ****YO00 is invalid
```
故**无法完成系统性网络检索**。我改用 `web_fetch` 直取已知 URL 绕开该限制（rfc-editor.org / pwg.org / raw.githubusercontent.com 均 HTTP 200）。
→ **诚实声明**：`ipptool` 与 PWG 5100.14 这两条反例渠道**未能验证**，是本次核查的**证据缺口**（见 §8）。但 §5.1/§5.2 的四条证据已足以定论，且**其中两条是"独立实现的实际行为"**，比规范解读更硬。

### 5.4 残留缺陷：代码注释与实现已不一致

修 enum 宽度时只改了逻辑，**注释未同步**，现存**自相矛盾**：

- `pkg/attributes/writer.go:213 Enums` 已改用 `int32Value`（4 字节）✅ 逻辑正确；
- 但 `pkg/attributes/everywhere.go:183` 注释仍写：`// operations-supported is 1setOf type2 enum => 2 octets per element.`
- `pkg/attributes/everywhere.go:89` 仍写：`// SIGNED-SHORTs, which is why operations-supported is written as a ...`

**【实测】** 这是**残留错误文档**，会误导后续维护者重犯同一错误。建议一并清理。

### 5.5 【RFC 原文】被编造的引用

原 `writer.go` 注释声称依据 `RFC8011 §4.4.15`。实测 RFC 8011 目录：
```
5.4.15. operations-supported (1setOf type2 enum) .........157
```
**【RFC 原文】** §4.4.15 **不存在**，真节号是 **§5.4.15**。已由 lead 确认修正。

---

## 6. 【实测】攻击未奏效的面向（红队也要报"打不动"）

诚实披露攻击失败项 —— 这些地方实现是**对的**，我找不到漏洞：

### 6.1 additional-value 的 `name-length = 0x0000` ✅

**【RFC 原文】** §3.1.5 要求 `0x0000`（见 §2.2）。
**【源码阅读】** `pkg/ipt/wire.go:133-138`（重构前）与 `pkg/attributes/writer.go:355-360` 均实现为：

```go
for i, v := range a.Values {
    name := a.Name
    if i > 0 { name = "" }   // 第 2 个起 name 为空 → name-length 编码为 0x0000
    ...
}
```
`name = ""` 经 `writeValuePair` 得到的 name-length 正是 `0x0000`。**【推断】** 逻辑正确。
**攻击点**：我尝试构造"多取值但 name 非空"或"首个取值为空 name"的边界来让它破功，未成功。

### 6.2 tag 取值表 ✅

我逐一比对实现常量与 RFC 8010 Table 3/4/5/6（**【RFC 原文】**）：

| 常量 | 实现值 | RFC 8010 | 结果 |
| --- | --- | --- | --- |
| no-value | `0x13` | Table 3 `0x13` | ✅ |
| integer | `0x21` | Table 4 `0x21` | ✅ |
| boolean | `0x22` | Table 4 `0x22` | ✅ |
| enum | `0x23` | Table 4 `0x23` | ✅ |
| dateTime | `0x31` | Table 5 `0x31` | ✅ |
| resolution | `0x32` | Table 5 `0x32` | ✅ |
| rangeOfInteger | `0x33` | Table 5 `0x33` | ✅ |
| begCollection | `0x34` | Table 5 `0x34` | ✅ |
| endCollection | `0x37` | Table 5 `0x37` | ✅ |
| textWithoutLanguage | `0x41` | Table 6 `0x41` | ✅ |
| nameWithoutLanguage | `0x42` | Table 6 `0x42` | ✅ |
| keyword | `0x44` | Table 6 `0x44` | ✅ |
| uri | `0x45` | Table 6 `0x45` | ✅ |
| uriScheme | `0x46` | Table 6 `0x46` | ✅ |
| charset | `0x47` | Table 6 `0x47` | ✅ |
| naturalLanguage | `0x48` | Table 6 `0x48` | ✅ |
| mimeMediaType | `0x49` | Table 6 `0x49` | ✅ |
| memberAttrName | `0x4a` | Table 6 `0x4a` | ✅ |
| unsupported | `0x10` | Table 3 `0x10` | ✅ |

**19/19 全对。** 我对 tag 这一面**攻击失败**。
（注：`textWithLanguage 0x35` / `nameWithLanguage 0x36` 亦正确。）

### 6.3 大端序 ✅

**【RFC 原文】** §3 要求 big-endian（two's-complement）。
**【源码阅读】** 实现用 `binary.BigEndian.PutUint16/PutUint32`，或等价的显式移位 `byte(v>>24), byte(v>>16), byte(v>>8), byte(v)`。
**【实测】** 我的独立向量：`1 → 00000001`、`20 → 00000014`、`147 → 00000093`、`0x7fffffff → 7fffffff`，与 RFC A.1/A.2 公布值一致。
**攻击失败** —— 大端序实现正确。

### 6.4 resolution / rangeOfInteger 布局 ✅

**【RFC 原文】** Table 7：`resolution` = 两个 SIGNED-INTEGER + 一个 SIGNED-BYTE = **9 字节**；`rangeOfInteger` = 两个 SIGNED-INTEGER = **8 字节**。
**【实测】** 我的向量：`resolution 300x300 dpi → 0000012c 0000012c 03`（9 字节 ✅）；`range 1..999 → 00000001 000003e7`（8 字节 ✅）。
**【源码阅读】** `writer.go` 的 `Resolution()` 用 8 字节、`Resolutions()` 用 8 字节。
> ⚠️ **【实测】我发现一处不一致**：`writer.go:236 Resolution()` 与 `:245 Resolutions()` 分配的都是 **8 字节**（`make([]byte, 8)`），**缺少 units 那个 SIGNED-BYTE**！而 RFC Table 7 明确要求 **9 字节**（`resolution` 含 units）。
> 这**不是**我先前认定的"攻击失败项"，而是一个**新发现的中等缺陷**：resolution 属性少了 units 字节，长度 8 而非 9。
> **【推断】** 后果：`printer-resolution-supported`/`printer-resolution-default` 的 value-length 会是 `0x0008`，而规范要求 `0x0009`；严格客户端可能解析错误或读到错误的 units。**建议 attr-engineer 复核。**

---

## 7. 🔴 core acceptance 标准的确切答案

**问题**：Win11 能否直接添加该 IPP 打印机？

**确切答案：当前不能。而且不是"未测"，是"已从规范层面证伪"。**

**推理链（**【推断】**，每一步都有 §4 的实测支撑）**：
1. Win11 添加 IPP 打印机时会先发 `Get-Printer-Attributes` 并**解析响应**（【推断】基于 Windows IPP 类驱动标准行为）；
2. 当前响应体的**每一个属性**都违反 RFC 8010 Fig.4 字段顺序（**§4.2/§4.3 实测**）；
3. 严格按 Fig.4 解析时，**第一个属性**（`attributes-charset`）即报越界（**§4.3 实测**：`value-length 25972 overruns buffer at 21`）；
4. 独立实现 go-ipp 面对同类错位直接返回 `unsupported attribute group: 0x00`（**§5.2 实测**）；
5. 故 Win11 无法得到有效属性集 → **添加流程必然失败**。

**失败的确切错误码：我未能取得。**
- **【实测】** 本机无 `ipptool`、无 Bonjour；且我未实际在 Win11 GUI 中执行"添加打印机"（无 GUI 自动化手段）。
- 因响应体本身畸形，**预计不会走到 IPP 状态码层面**（如 `0x0400 client-error-bad-request`），而是**在 HTTP 响应体解析阶段就失败**，表现为 Windows 报"无法连接到打印机 / 服务器返回了无效数据"一类 UI 错误。
- **诚实声明**：**具体错误码属于"未验证"**。要拿到确切错误码，需：(a) 修完 §4 后启动服务；或 (b) 在畸形响应下用抓包/Win11 事件日志观察。**我不会编造一个错误码。**

**修复后可测性**：§4 与 §5 修完、`go test ./...` 转绿后，端到端验证即可进行（本机 `curl` 可发 IPP POST 验证字节层；Win11 GUI 侧需人工点击）。

---

## 8. 诚实声明：未能验证的部分

| 项 | 状态 | 原因 |
| --- | --- | --- |
| Win11 实际添加打印机的**确切错误码** | ❌ **未验证** | 无 GUI 自动化；且响应畸形，预计不产生 IPP 状态码 |
| Win11 端到端添加（修复后） | ❌ **未验证** | 依赖 §4/§5 修复完成 |
| DNS-SD / mDNS 通告的官方工具验证 | ❌ **未验证** | 本机无 `dns-sd`；无法安装 Bonjour |
| `ipptool` 一致性测试 | ❌ **未验证** | 本机无 `ipptool`，无法取得其源码作反例 |
| PWG 5100.14 (IPP Everywhere) 必需属性逐项比对 | ⚠️ **部分** | PDF 未取得；**仅**从 `pwg.org/ipp/everywhere.html`（HTTP 200）读到摘要级要求 |
| `web_search` 系统性检索 | ❌ **不可用** | 端点返回 HTTP 401（API key 无效）；改用 `web_fetch` 直取 URL |
| 二进制体积对比 | ❌ **未完成** | 依赖可构建的成品；见 §9 待办 |
| ~~rfc-editor.org 可达性~~ | ✅ 可达 | HTTP 200 |
| ~~pwg.org 可达性~~ | ✅ 可达 | HTTP 200 |
| ~~raw.githubusercontent.com 可达性~~ | ✅ 可达 | HTTP 200（GitHub 主站未测） |

### IPP Everywhere 必需属性（【RFC 原文】pwg.org 摘要级）

`https://www.pwg.org/ipp/everywhere.html`（HTTP 200）逐字：

> Required: IPP/2.0, DNS-SD, PWG Raster and JPEG JFIF file formats (JPEG only required for color printers)
> Recommended: PDF, IPP-USB

**【RFC 原文】** **彩色机才额外要求 JPEG JFIF**。
**【实测】** 实现中 `DefaultPrinter()` 的 `ColorSupported: false`（单色机），但 `DocumentFormats` 已含 `image/jpeg`。
**【推断】** 这属于"多给"而非"少给"，**不构成缺陷**；且单色机本不强制 JPEG。此项我的攻击**未奏效**。
**【推断】** 但注意：`everywhere.go` 中 `color-supported` 若为 `false` 却同时公告 JPEG，语义上偏宽松，建议 lead 确认产品意图（彩色还是单色）。

---

## 9. 待修正清单（按严重度排序，交由对应 owner）

| # | 严重度 | 文件:位置 | 问题 | 建议修法 | 归属 |
| --- | --- | --- | --- | --- | --- |
| 1 | 🔴 **P0** | `pkg/attributes/writer.go:428 writeValuePair` | 字段顺序违反 RFC 8010 Fig.4：`value-length` 写在 `name` 之前，致响应无法解析 | 拆成两段写：`tag+nameLen` → `name` → `valueLen` → `value` | attr-engineer |
| 2 | 🔴 **P0** | `pkg/attributes/everywhere.go:183` + all enum call sites | enum 宽度需为 4 字节（`Enums` 逻辑已修，注释仍写 2 字节） | 统一 4 字节；删除"2 octets"错误注释 | attr-engineer |
| 3 | 🟠 P1 | `pkg/attributes/writer.go:236,245` | `resolution` 少 units 字节：8 字节而非 RFC Table 7 的 **9 字节** | `make([]byte, 9)`，第 9 字节写 units（3=dpi） | attr-engineer |
| 4 | 🟠 P1 | `pkg/attributes/everywhere.go:89` | 残留错误注释（"SIGNED-SHORTs"） | 改为 SIGNED-INTEGER / 4 octets | attr-engineer |
| 5 | 🟠 P1 | 全仓 | 曾存在两份矛盾 wire 编码器（`pkg/ipp/wire.go` vs `pkg/attributes/writer.go`） | 已由 **ADR-0005** 裁决单一编码层；确认 `pkg/ipp` 不再自带 tag 表 | server-engineer |
| 6 | 🟡 P2 | `pkg/attributes` | 修好后 `go test ./...` 必须全绿（当前 20 条红） | 以现有 golden（正确）为准修正实现 | attr-engineer |
| 7 | 🟡 P2 | — | 二进制体积实测对比（vs "500MB 运行时"） | 见 §10 待补 | lead / verifier |
| 8 | 🟡 P2 | — | 修复后补做端到端 + Win11 实测 | 见 §7 | lead / verifier |

---

## 10. 复现指引（任何人可重跑）

```powershell
# 1) 我的独立字节向量推导（不依赖本项目代码）
cd C:\Users\sq199\AppData\Local\Temp\ippvec; go run .

# 2) enum 宽度：2 字节 vs 4 字节 喂给独立解码器 go-ipp
cd C:\Users\sq199\AppData\Local\Temp\enumprobe; go run .

# 3) header 顺序 bug 的独立证明
cd C:\Users\sq199\AppData\Local\Temp\enumprobe\headerbug; go run .

# 4) 本项目现状（P0 修复前应看到多条 FAIL）
cd C:\Users\sq199\Documents\dyj; go build ./...; go test ./...

# 5) RFC 原文（一手）
#    https://www.rfc-editor.org/rfc/rfc8010.txt
#    https://www.rfc-editor.org/rfc/rfc8011.txt
#    https://www.pwg.org/ipp/everywhere.html
```

**引用的独立实现源码（一手）**：
- go-ipp：`C:\Users\sq199\go\pkg\mod\github.com\phin1x\go-ipp@v1.7.0\attribute_encoder.go`、`attribute_decoder.go`
- CUPS：`https://raw.githubusercontent.com/OpenPrinting/cups/master/cups/ipp.c`

---

## 11. 元结论：本轮红队方法论的有效性

本轮共 **4 次** 有价值的推翻/发现：

1. **推翻"代码可运行"** —— 实测 `go build ./...` 两包全红（§见过程记录）；
2. **推翻"enum 2 字节可行"** —— 用独立解码器证明会失步崩溃（§5.2）；
3. **新发现 P0 header 顺序 bug** —— 这是本轮**最严重**缺陷，且**不在原任务清单的攻击面内**（原清单只关注 tag 值、name-length 0、大端序、enum 宽度）；它是靠"独立重算 golden 向量并逐字节比对"**顺带**暴露的；
4. **新发现 resolution 8/9 字节缺陷** —— 同样来自独立向量比对（§6.4）。

**方法论启示**：原任务把攻击面限定为"枚举若干易错点"，但**真正致命的缺陷在清单之外**。有效的红队手段是**先独立重算 golden 向量、再与实现逐字节比对** —— 让差异自己浮现，而不是只检查预设怀疑点。
**另一条**：仓库里**已有的 golden 测试写对了、生产代码写错了**。红队的最高杠杆动作是**把既有测试跑起来并判读差异**，而不是新写一遍检查。
