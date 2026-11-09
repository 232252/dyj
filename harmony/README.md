# dyj 鸿蒙接收端

> **方向变更（2026-11-09，[ADR-0013](../docs/adr/0013-harmony-system-print-integration.md)）**：
> 打印动作移交系统打印面板（设置 → 打印机和扫描仪 → 添加，系统打印栈直连
> dyj 服务端 IPP）。App 收缩为配对/管理助手——配对一次即信任设备
> （[调研 17](../docs/research/17-harmony-system-print-takeover.md)）。
> 下列"App 内四步闭环"中的选内容/打印两步将被裁撤，配对与发现保留。

手机端把图片、字、PDF 发到局域网里的 dyj 共享端打印。

闭环是四步：**发现共享端 → 输入 4 位配对码 → 选内容 → 打印**。

技术路线与取舍见 [ADR-0012](../docs/adr/0012-harmony-receiver-rnoh.md)，
版本矩阵、SDK 取证与踩坑记录见
[调研 16](../docs/research/16-harmony-receiver-design.md)。

**服务端零改动**：配对走现成的 `POST /pair`，打印走现成的
`POST /p/<令牌>/ipp/print/<打印机>`。

## 目录

```
harmony/
└── dyj-harmony/                 React Native 工程（含 ArkTS 工程）
    ├── src/                     TS 侧：协议、界面
    │   ├── ipp/                 IPP 编解码（与 Go 侧共用黄金字节）
    │   ├── mdns/                DNS/DNS-SD 解析与去重
    │   ├── format/              文档格式协商与降级
    │   ├── client/              配对、打印、发现
    │   ├── state/               本地持久化（设备身份、配对记录）
    │   ├── screens/             四个页面
    │   ├── specs/               TurboModule 规格（codegen 输入）
    │   └── i18n/                文案（禁用词检查扫这里）
    ├── harmony/entry/src/main/ets/
    │   ├── native/              ArkTS 侧六个 TurboModule 实现
    │   └── generated/           codegen 产物（不入库）
    └── scripts/patch-rnoh-cli.mjs   Node ≥22 兼容补丁
```

分工：**协议编解码在 TS 侧**（可单测、与 Go 侧逐字节对齐），**平台能力在
ArkTS 侧**（收包、发 HTTP、选文件、转 JPEG、渲染 PDF、读写键值）。

## 环境

| 依赖 | 版本 | 说明 |
| --- | --- | --- |
| Node.js | ≥ 22.11（实测 26.3） | 低于 22 的 RNOH CLI 有兼容问题，仓库自带补丁兜底 |
| DevEco command-line-tools | 5.0+（实测 26.0.0.621） | 提供 `hvigorw`、`ohpm` 与 SDK |
| ohpm / hvigor | 随 command-line-tools | `ohpm` 装 har，`hvigorw` 出 HAP |
| JDK | 17+（实测 25） | 跑 `hap-sign-tool.jar` 签名 |

构建前把 SDK 指到本工程的 SDK（**别用别的工程留下的环境变量**）：

```powershell
$env:DEVECO_SDK_HOME = 'C:\Users\<你>\HarmonyOS\command-line-tools-26.0.0.621\sdk'
$env:HOS_SDK_HOME   = $env:DEVECO_SDK_HOME
```

本机 npm 配了代理且会掐断大 tarball 时，装依赖要绕开代理：

```powershell
npm --prefix harmony\dyj-harmony install `
  --registry=https://registry.npmmirror.com --proxy=null --https-proxy=null
```

## 从零构建

四步，**顺序不能换**（codegen 产出 ArkTS 规格，bundle 依赖 codegen 后的
工程，hvigor 依赖 bundle 与 codegen 产物）：

```powershell
# 1. 依赖（装 RNOH har、跑 Node 兼容补丁）
npm --prefix harmony\dyj-harmony install --registry=https://registry.npmmirror.com --proxy=null --https-proxy=null

# 2. 装 ArkTS 侧依赖（release har）
cd harmony\dyj-harmony\harmony
ohpm install --all

# 3. 生成 TurboModule 的 C++ 与 ArkTS 代码
cd ..
npm run harmony:codegen

# 4. 打包 JS。必须 --js-engine hermes：Index.ets 只从 rawfile 加载
#    hermes_bundle.hbc（默认 any 引擎产出的是 bundle.harmony.js，装上白屏）
npm run harmony:bundle -- --dev false --js-engine hermes `
  --hermesc-dir ./node_modules/hermes-compiler/hermesc

# 5. 出包
cd harmony
hvigorw --mode module -p product=default -p module=entry@default -p buildMode=release assembleHap
```

产物：`harmony/entry/build/default/outputs/default/entry-default-unsigned.hap`
（release 模式约 19 MB，含 `resources/rawfile/hermes_bundle.hbc`，可脱离 metro
独立运行）。release 出包时 hvigor 插件还会自动重打一份 `bundle.harmony.js` 进
rawfile——`Index.ets` 不加载它，属无害冗余。注意 **debug 模式出包插件会跳过
打包**（`[bundler] skipped`），rawfile 里有什么就打什么。

> `hvigorw` 若不在 PATH，用 SDK 里的：
> `& '<command-line-tools>\bin\hvigorw.bat' --mode module -p product=default -p module=entry@default -p buildMode=release assembleHap`

### 只改 TS 时的快捷路径

```powershell
cd harmony\dyj-harmony
npm run harmony:codegen       # 改了 src/specs/ 才需要
npm run harmony:bundle -- --dev false --js-engine hermes --hermesc-dir ./node_modules/hermes-compiler/hermesc
```

## 签名（归你来做）

仓库里**不放任何签名材料**：`harmony/build-profile.json5` 的 `signingConfigs`
是空数组，构建出来的 HAP 是未签名的。**不要复用本机其它工程的证书或
profile**——那是别人的东西。

### 方式一：DevEco Studio 自动签名（推荐）

1. DevEco Studio 打开 `harmony/dyj-harmony/harmony` 目录。
2. `File → Project Structure → Signing Configs`，勾选 **Automatically generate
   signature**（需登录华为开发者账号）。
3. 填上证书、profile 与密钥库（`.p12`）信息，点 OK。
4. 直接 `Build → Build Hap(s)/APP(s) → Build Hap(s)`，产物已签名，可装机。

### 方式二：命令行签名

工具在 SDK 里（实测路径）：

```
<command-line-tools>\sdk\default\openharmony\toolchains\lib\hap-sign-tool.jar
```

签名 HAP 需要三样东西：**应用证书 `.cer`**、**密钥库 `.p12`/`.jks`**、
**已签名的调试/发布 profile `.p7b`**（profile 由华为开发者后台签发）。

```powershell
$jar = '<command-line-tools>\sdk\default\openharmony\toolchains\lib\hap-sign-tool.jar'
java -jar $jar sign-app `
  -mode localSign `
  -keyAlias "oh-app1-key-v1" `
  -appCertFile "D:\keys\app-release-cert.cer" `
  -profileFile "D:\keys\signed-profile.p7b" `
  -inFile "harmony\dyj-harmony\harmony\entry\build\default\outputs\default\entry-default-unsigned.hap" `
  -signAlg SHA256withECDSA `
  -keystoreFile "D:\keys\app-keypair.p12" `
  -keystorePwd <密钥库口令> `
  -outFile "D:\out\dyj-signed.hap" `
  -compatibleVersion 12 `
  -signCode "1"
```

密钥库与证书可以用同一个工具生成（`generate-keypair` / `generate-csr` /
`generate-app-cert`），`java -jar $jar` 会列出全部子命令与参数。

## 装机与联调

```powershell
hdc list targets                                   # 确认设备在线
hdc install -r D:\out\dyj-signed.hap               # 安装
hdc shell aa force-stop com.dyj.app                # 重启应用
```

调试时看日志：

```powershell
hdc shell hilog | Select-String 'dyj|Dyj'
```

### 走一遍完整流程

1. 共享端机器上跑 `dyj`（共享打印机），管理页上会显示 4 位配对码。
2. 手机装好本 App，点「扫描」，应能扫到共享端。
3. 点共享端 → 输入 4 位码 → 配对成功。
4. 选图片 / 输入文字 / 选 PDF → 打印 → 看共享端的打印队列。

扫不到时用「按地址添加」兜底：填共享端的 `192.168.x.x`（端口默认 6631）。

## 测试

```powershell
cd harmony\dyj-harmony
npx jest            # 28 项：IPP 编解码、mDNS、格式协商、禁用词
npx tsc --noEmit    # 类型检查
cd ..\.. && go test ./pkg/client/...   # Go 侧黄金字节
```

IPP 的 wire 格式由 `pkg/client/testdata/print_job.hex` 锁定：Go 与 TS 各自
读它断言，任何一侧漂移两边同时变红。

## 常见问题

| 现象 | 原因与解法 |
| --- | --- |
| `ninja: error: Stat(...): Filename longer than 260 characters` | ohpm 把 `.ohpm` 虚拟 store 的绝对路径写进了 include 目录，Windows 上超 260 字符。已在本工程的 `harmony/entry/src/main/cpp/CMakeLists.txt` 里把导入目标改回 `oh_modules/@rnoh/...` 的短路径；若仍出现，先删 `harmony/entry/.cxx` 让 CMake 重新生成 |
| `ohpm install` 报 ECONNRESET | pnpm（hvigor 自带）走用户 `~/.npmrc` 里的代理。设 `npm_config_proxy=null`、`npm_config_https_proxy=null` 再试 |
| codegen 后多出一层 `generated/generated/` | codegen 用正斜杠正则判断输出目录，Windows 上反斜杠匹配不上。命令里一律用正斜杠（`npm run harmony:codegen` 已按此写） |
| `init-harmony` 崩在 `dirent.path` | Node ≥22 移除了 `dirent.path`。`npm install` 的 postinstall 会自动打补丁（`scripts/patch-rnoh-cli.mjs`） |
| 编译报「`readFileSync`/`encodeToString`/`netmask` 不存在」 | 这些是按旧文档写的猜测。实际 API 见调研 16 §5：`fileIo` 没有 `readFileSync`、`Base64Helper` 是实例方法、`NetAddress` 没有 `netmask` |
| HAP 装上后白屏 | 两个根因：① bundle 没打进去（debug 出包插件跳过打包，须带 `-p buildMode=release` 或先手动跑 bundle 命令）；② bundle 引擎不对——`Index.ets` 只加载 `hermes_bundle.hbc`，不带 `--js-engine hermes` 打出来的是 `bundle.harmony.js`，装上必白屏。修复：`npm run harmony:bundle -- --dev false --js-engine hermes --hermesc-dir ./node_modules/hermes-compiler/hermesc` 后重新出包 |
