# 调研 16：鸿蒙接收端（React Native 鸿蒙化 / RNOH）

- **日期**：2026-11-08
- **范围**：给 dyj 局域网打印共享工具补一个手机端接收端（发现 → 配对 → 打印）
- **结论**：技术路线定为 **React Native 鸿蒙化（RNOH，New Architecture）**，版本线
  **RN 0.84.1 + RNOH 0.84.5**；首版支持图片 + 文本 + PDF；**服务端零改动**。
- **决策**：[ADR-0012](../adr/0012-harmony-receiver-rnoh.md)

## 1. 需求边界

只做**接收端**（手机 → 共享端）。共享端（服务端）不因这个功能改一行代码：

- 配对：手机 `POST /pair` 换令牌，共享端现有接口已经够用。
- 打印：手机 `POST /p/<令牌>/ipp/print/<打印机>`，走现成的 IPP 数据面。
- 不碰 `/admin/api/*`（那是给网页管理页用的，要管理令牌）。

## 2. 技术路线选型

| 方案 | 结论 | 理由 |
| --- | --- | --- |
| **RNOH（RN 鸿蒙化）** | ✅ 采纳 | 协议编解码写 TS（可单测、黄金字节对齐），平台能力写 ArkTS TurboModule；两端都能复用现有仓库资产（IPP/mDNS 逻辑在 TS 侧重写一遍即可对齐 Go 侧） |
| 原生 ArkTS 全写 | ❌ | 协议层没法单测，黄金字节对齐要另写一套工具；工作量翻倍 |
| ArkWeb 薄壳（内嵌管理页） | ❌ | 管理页是给共享端机器看的；手机端需要文件选择、相机、相册、系统分享，Web 壳做不了 |
| 纯原生 + C++ 直连 | ❌ | 与仓库既有 Go 侧协议实现脱节，维护两套 wire 逻辑 |

## 3. 版本矩阵（实测）

| 组件 | 版本 | 备注 |
| --- | --- | --- |
| React Native | 0.84.1 | 用户指定「0.84 最新线」 |
| @react-native-oh/react-native-harmony | 0.84.5 | RNOH 本体 |
| @react-native-oh/react-native-harmony-cli | 0.84.5 | `init-harmony` / `codegen-harmony` / `bundle-harmony` |
| @react-native-community/cli | 20.1.0 | RN 0.84 配套 |
| react | 19.2.3 | RN 0.84 配套 |
| metro | 0.83.8 | RN 0.84 配套 |

**RNOH har 体积实测**（0.84.5，与 0.82.35 的记录值不同，以实测为准）：

- `react_native_openharmony_release.har` —— **13.0 MB**
- `react_native_openharmony.har`（debug）—— **76.4 MB**

首版走 **release har**：体积小一个数量级，debug har 只在需要调试原生时再换。

## 4. 服务端协议事实（照抄，不得改）

从 `pkg/` 代码里核实并抄下来，手机端按此实现：

| 事实 | 出处 |
| --- | --- |
| `POST /pair` 免鉴权，体 `{pin,device_id,device_name}` → `{token,server_name,printers:[{key,name,uri_path}]}` | `pkg/pairing/http.go` |
| 错误码：400 `pin_required` / 401 `bad_pin` / 403 `locked`+`retry_after_sec` / 500 `internal` | 同上 |
| 配对码 10 分钟滚动有效；同源 10 分钟 10 次失败锁 30 分钟 | `pkg/pairing/pairing.go` |
| 同 device_id 重配会轮换令牌，旧令牌立即失效 | 同上 |
| 打印路径 `/p/<令牌>/ipp/print/<打印机>`（令牌在路径里，不在请求头） | `pkg/ipp/router.go` |
| 服务端**不校验** `printer-uri` 的主机部分 | `pkg/ipp/router.go` |
| 属性编码：`value-tag(1)｜name-len(2 BE)｜name｜value-len(2 BE)｜value`，**value-len 在 name 之后** | `pkg/attributes/writer.go` |
| 多值属性第 2+ 个值的 name-len = 0（additional-value） | 同上 |
| 每个属性组前必须有 attributes-charset + attributes-natural-language | `pkg/ipp/client.go` `writeGroup` |
| 请求体上限 128 MiB；`compression=gzip` 由服务端解压 | `pkg/ipp/server.go`、`pkg/ipp/operations.go` |
| mDNS 通告 `_ipp._tcp.local.`，TXT 含 `dyj=1` / `pair=1` / `dyjver=3`；多网卡时每个接口一个 SRV 主机名 | `cmd/dyj/node.go`、`pkg/discovery/mdns.go` |

**mDNS 要问两轮**：服务端对「有哪些打印服务」只回 PTR，对「这个服务在哪」才回
SRV/TXT/A。手机端先扫一轮拿实例名，再带着实例名扫第二轮。

## 5. SDK 能力取证（本机 SDK 的 .d.ts，非猜测）

路径：`C:\Users\sq199\HarmonyOS\command-line-tools-26.0.0.621\sdk`

| 能力 | 取证结论 |
| --- | --- |
| 组播 | `socket.constructMulticastSocketInstance()`、`bind(NetAddress)`、`addMembership(NetAddress)`、`send({data:ArrayBuffer,address})`、`on('message',{message:ArrayBuffer,remoteInfo})` |
| `NetAddress` | 只有 `address` / `family?` / `port?` —— **没有 netmask**（踩过坑，见 §7） |
| HTTP | `http.createHttp()` → `request(url,{method,header,extraData,expectDataType:ARRAY_BUFFER,connectTimeout,readTimeout})` |
| 图片 | `image.createImageSource(path)` → `getImageInfo()` / `createPixelMap({desiredSize,editable})`；`createImagePacker().packing(pixelMap,{format:'image/jpeg',quality})` |
| PDF | **只在 HMS SDK**：`@kit.PDFKit` → `pdfService.PdfDocument`（`loadDocument`/`getPageCount`/`getPage`/`getPagePixelMap`/`releaseDocument`）。OpenHarmony SDK 无 PDF Kit |
| 文件选择 | `@kit.CoreFileKit` → `picker.DocumentViewPicker(ctx).select({maxSelectNumber,fileSuffixFilters})` → URI 数组 |
| 文件读 | `fileIo`（= `@ohos.file.fs`）：**没有 readFileSync**，得 `openSync` + `statSync` + `readSync` + `closeSync` |
| 键值存储 | `@kit.ArkData` → `getPreferences(ctx,{name})` |
| base64 | `util.Base64Helper` 的 `encodeToStringSync` / `decodeSync` 是**实例方法**，不是静态方法 |

## 6. 关键设计决定

### 6.1 协议在 TS，平台在 ArkTS

IPP 编解码、mDNS 解析、去重、格式协商全在 TS 侧：

- 能单测（现在 28 项 jest 用例，含与 Go 侧共用的黄金字节）。
- wire 格式改动能被测试挡住，不靠人眼比对。

ArkTS 侧只提供「原子能力」：收包、收字节、发 HTTP、选文件、转 JPEG、渲染 PDF、
读写键值。

### 6.2 跨桥只传字符串

codegen 对 ArrayBuffer / 复杂对象支持有限，所以六个 TurboModule 一律返回字符串
（JSON 串或 base64）。大文档用「头 base64 + 文档 base64」两段过桥，ArkTS 侧解码
后拼接再发出——大文件不必先整份复制进 JS 内存。

### 6.3 黄金字节单一事实源

`pkg/client/testdata/print_job.hex`（小写十六进制、无分隔符）由 Go 侧生成，Go 与
TS 各自读它断言。任何一侧漂移，两边同时变红。

### 6.4 PDF 默认端上渲染

服务端**只有装了 poppler（pdftoppm）才声明 `application/pdf`**（`pkg/driver/pdf.go`、
`cmd/dyj/serve.go`）。所以：

- 默认路径：手机用 PDF Kit 把 PDF 逐页渲染成 JPEG，逐页一个作业送出去。任何服务端
  都能用，不依赖服务端能力。
- 「保持原样直传」是可选开关，**默认关闭**，且只在服务端明确声明支持时才出现。

## 7. 踩过的坑（都留了痕）

### 7.1 Node ≥22 移除 `dirent.path` → RNOH CLI 崩溃

`init-harmony` 在 Node 26 上崩：`this.nodeFSDirent.path` 已废弃。修法写在
`harmony/dyj-harmony/scripts/patch-rnoh-cli.mjs`（幂等，postinstall 自动跑）：

```diff
- this.nodeFSDirent.path
+ (this.nodeFSDirent.parentPath ?? this.nodeFSDirent.path)
```

### 7.2 Windows MAX_PATH → ninja 记录依赖失败

现象：编译通过，但**增量构建**报

```
ninja: error: Stat(.../oh_modules/.ohpm/@rnoh+react-native-openharmony@ghi+pk3thbszpbmpbmrv6uea42tx+cmxk5eh0jodn88=/oh_modules/@rnoh/react-native-openharmony/include/RNOHCorePackage/ComponentBinders/DrawerLayoutAndroidJSIBinder.h): Filename longer than 260 characters
```

根因：ohpm 生成的 `react-native-openharmonyConfig.cmake` 把 **.ohpm 虚拟 store 的
绝对路径**写进了导入目标的 `INTERFACE_INCLUDE_DIRECTORIES`（194 字符），加上头文件
名就超过 Windows 的 260 上限。

修法（`harmony/entry/src/main/cpp/CMakeLists.txt`）：`include()` 完官方 cmake 片段
之后，把导入目标里的 store 绝对路径换回**目录联接路径**
`oh_modules/@rnoh/react-native-openharmony/...`（同一份内容，短 80 字符）。改完
clean build 与增量 build 都通过。

### 7.3 codegen 的 ETS 输出目录被套两层

RNOH codegen 用正斜杠正则 `/\/generated\/?$/` 判断输出目录；Windows 上若路径写成
反斜杠就匹配不上，会再追加一层变成 `generated/generated/`。因此 `etsOutputPath`
写 `./entry/src/main/ets`（让 codegen 自己拼 `generated`），命令里传
`--ets-output-path ./harmony/entry/src/main/ets/generated`（正斜杠）。

### 7.4 ArkTS 严格模式下的 API 误用

编译期报错 → 逐条按 SDK .d.ts 纠正：

| 误用 | 正确 |
| --- | --- |
| `fileIo.readFileSync` | `fileIo.openSync` + `statSync` + `readSync` + `closeSync` |
| `util.Base64Helper.encodeToString`（静态） | `new util.Base64Helper().encodeToStringSync(...)`（实例方法） |
| `addMembership({address, netmask})` | `addMembership({address})`（NetAddress 无 netmask） |
| `fileUri.getPathFromUri` | SDK 里没有这个函数；文件名从 URI 末段取，`copyFileSync` 直接吃 URI |

## 8. 验收口径

软件侧可自动验证的：

1. `go test ./pkg/client/...` 绿（黄金字节生成与断言）。
2. `npx jest` 绿（28 项：IPP 编解码、mDNS 解析与去重、格式协商、禁用词）。
3. `npx tsc --noEmit` 无错。
4. `hvigorw assembleHap` 出 HAP（含 JS bundle，可脱离 metro 运行）。

**须现场人工验收**（签名归用户，机器上才能装）：

5. 真机装机 → 扫描到共享端 → 输 4 位码配对 → 打印图片/文字/PDF。
6. 令牌失效（共享端重配）后回到配对页。
7. 配对码错 10 次后显示锁与倒计时。

## 9. 仍然开放的问题

| # | 问题 | 备注 |
| --- | --- | --- |
| 1 | HarmonyOS NEXT 上 `@kit.PDFKit` 的实际渲染效果与内存占用 | 只在 SDK 层面核实了 API，未在真机跑过多页大 PDF。**已知限制**：`DyjPdf.render` 一次返回全部页的 base64（ArkTS 侧逐页渲染、逐页释放 PixelMap，但 base64 字符串会累积），百页级 PDF 的峰值内存待实测 |
| 2 | 组播在部分路由器/热点下不通 | 已有「按地址添加」兜底；跨网段发现是服务端议题（调研 15） |
| 3 | 签名与上架 | 归用户；不上架应用市场 |
| 4 | 相机直拍 | v1 只走系统选择器，不接相机 |
