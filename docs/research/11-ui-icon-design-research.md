# 11 — UI 图标与品牌图标调研（选型 + 更换清单）

> 调研时间：2026-09-29。
> **实施状态：本报告 §5 清单已于同日落地**（Lucide vendoring、品牌 mark/favicon、
> 托盘重绘、go-winres exe 资源、Linux hicolor；实施中额外发现并修复了 `/admin`
> 无尾斜杠白屏与 stepper/diagnostic 缺 attachShadow 两个存量 bug，见 CHANGELOG）。
> 范围：Web 管理页的界面图标、系统托盘图标、Windows exe 资源图标、Linux .desktop 图标、favicon，
> 以及对应的免费/开源图标库选型与品牌（logo）方案。
> 结论将沉淀为 ADR 后再动工；本文保留完整论据。
> 约束回顾（承 docs/research/10 §2）：管理页零 npm、零构建链、`go:embed` 离线内嵌，
> 任何"运行时从 CDN 拉图标"的方案一律排除（LAN 工具必须断网可用）。

## 0. 结论速览

| 决策点 | 结论 | 一句话理由 |
| --- | --- | --- |
| 界面线性图标 | **Lucide**（v1.48.0，ISC，≈1,850 枚） | 现有 13 枚图标本来就是 Lucide/Feather 血统的 24px/stroke-2 线稿，换库=零风格切换成本，且补齐语义缺口 |
| 引入方式 | **不引入 npm 包、不用 CDN**：把实际用到的 ~30 枚 path 手工 vendor 进 `dyj-icon.js`，文件头标注出处与版本 | 延续现有架构；图标总量小，手工同步成本 ≈ 0，断网可用 |
| 品牌 logo / 系统图标 | **自绘**一枚矢量 mark（打印机 + 电波两个母题），一源多出 | 所有主流图标库都不适合当产品标识（Lucide 明确不收品牌 logo，详见 §4.1）；自绘 SVG 是唯一可控路径 |
| Windows exe 图标/版本信息 | `tc-hib/go-winres` 生成 `.syso`，`go build` 自动嵌入多尺寸 ICO + RT_VERSION | 现状 dyj.exe/dyj-tray.exe 在资源管理器/任务栏/卸载列表里显示的是 **Go 默认图标**，无版本信息 |
| Linux .desktop 图标 | 自有图标装入 `/usr/share/icons/hicolor/`，`Icon=printer` → `Icon=dyj` | 现状用的是系统主题通用打印机图标，与系统其它打印应用撞脸且无品牌 |
| 托盘图标 | 保留"运行时绘制、零资源文件"架构，把纯色圆点升级为 **品牌 glyph + 状态角标** | 现状圆点像"状态灯"而非 dyj；架构优点（无资产文件、状态驱动）必须保留 |
| favicon | 补一个（SVG + ICO 双格式） | `index.html` 目前没有任何 `<link rel="icon">`，浏览器显示默认地球/404 |
| macOS .icns | v1 不做（无 .app 壳不触发），v2 做 .app/cask 时一并出 | 与 docs/research/10 的 macOS 结论一致 |

---

## 1. 现状盘点（每一处 UI 图标，逐处体检）

### 1.1 界面图标：`pkg/admin/web/components/dyj-icon.js`

唯一的界面图标源，Shadow DOM 内联 SVG，`stroke="currentColor" stroke-width="2"`，24px 网格。
现存 14 个名字（`print` 与 `printer` 内容完全相同，实为 13 枚）：

| name | 实际形状 | 血统 | 使用处 | 语义评价 |
| --- | --- | --- | --- | --- |
| `print` / `printer` | 打印机 | Lucide printer（逐点一致） | sidebar 品牌+导航"作业"、多处 | ✅ 好 |
| `pause` / `play` | 双竖线 / 三角 | Feather 血统 | 作业操作 | ✅ 好 |
| `copy` | 双叠纸 | Lucide copy | dyj-copy-field | ✅ 好 |
| `log` | 文档+行 | Feather file-text | 导航"日志"、dyj-log-viewer | ⚠️ 可，`file-text` 更准 |
| `wizard` | 罗盘 | Feather compass | 导航"向导" | ⚠️ 可，罗盘≈"引导"成立 |
| `network` | 地球经纬 | Feather globe | 导航"网络" | ❌ "网络页"讲的是 IP/mDNS/接口，globe 易误读为"互联网/WAN"，建议换 Lucide `network`（节点树） |
| `check` | 对勾 | Feather check | **导航"状态"页**、stepper、toast 成功 | ❌ 导航用"对勾"指代状态页语义弱（对勾=完成而非状态），建议 `activity`/`gauge` |
| `warn` / `error` / `info` | 警告三角/圆叹号/圆i | Feather alert-* | dyj-diagnostic、toast、状态徽章 | ✅ 好 |
| `chevron` | 下箭头 | Feather chevron-down | dyj-disclosure | ✅ 好 |

**结构性问题（与具体图形无关）：**

1. **手工抄写、无出处**：path 逐点抄自 Lucide/Feather，但文件头只写"内联 SVG 图标集"，没有来源库名、版本号，将来想补图标或对齐上游改版没有依据。
2. **覆盖面窄**：随着页面长出会有稳定的新需求——`refresh`（刷新日志/作业）、`external-link`（打开管理页/文档）、`wifi`（网络页）、`settings`（设置）、`download`（导出日志）、`pause-circle`/`play-circle`（暂停共享按钮）——现在只能继续手抄或瞎画。
3. 无 favicon 消费方登记（见 1.5）。

### 1.2 系统托盘：`pkg/tray/tray.go:166-224`

- 现状：运行时逐像素画一个 **32×32 纯色圆点**（绿/灰/红三态），包一层 ICO 容器给 `systray.SetIcon`。
- 优点必须保留：**零资源文件、确定性输出、状态驱动换图**（测试 `TestIconBytesIsICOWithPNG` 依赖这个行为）。
- 问题：圆点无任何品牌信息——用户在任务栏里看到的只是一个"彩色指示灯"，和任何监控类软件的托盘无差别；Windows 11 浅色任务栏下 160 透明度软边也略糊。
- 相邻问题：托盘菜单项（tray_windows.go:59-64）纯文字，Windows 托盘菜单支持带图标（可选，P2）。

### 1.3 Windows 可执行文件资源：无 `.syso`（全仓库确认无）

- `dyj.exe` / `dyj-tray.exe` 目前**没有嵌入任何图标/版本资源**：资源管理器、任务栏、Alt-Tab、开始菜单、以及安装器 `UninstallDisplayIcon={app}\dyj-tray.exe`（packaging/windows/dyj.iss:32）显示的全部是 **Go 编译器默认图标**。
- 右键属性也没有产品名/版本/版权——对一款要装到用户机器上的工具来说观感扣分明显。
- `scripts/build-all.sh` 目前没有资源嵌入步骤。

### 1.4 Linux 桌面入口：`packaging/linux/dyj-admin.desktop:9`

- `Icon=printer` → 桌面环境按主题名解析，显示的是**系统主题自带的通用打印机图标**；nfpm.yaml 的 `contents` 也没有安装任何图标文件。
- 后果：与系统中其它打印相关应用图标雷同，且无法通过图标认出 dyj。

### 1.5 Web 管理页 favicon：缺失

- `pkg/admin/web/index.html` 没有 `<link rel="icon">`，浏览器标签页显示默认图标；对"管理页是产品门面"的定位来说是最显眼的缺口。

### 1.6 设计 token（`theme.css`）——不需要动，但要作为选型约束

- 色板即 Tailwind 系（primary `#2563eb` = blue-600，success/warning/danger 同系），8px 圆角，system-ui 字体，注释自述"安静、克制、过渡 ≤ .15s"。
- **结论**：图标选型必须延续 24px/stroke-2/圆角线稿风格，任何面性、拟物、duotone 风格都会破坏现有语言。这直接排除了 Material Symbols、Remix Icon 等面性系。

---

## 2. 候选图标库对比（2026-09-29 实测数据）

版本号与发布时间为当日 GitHub API 实查（见 §7 来源），非转抄。

| 库 | 最新版（发布日） | 数量 | 许可 | 风格 | 维护活跃度 | 与本项目适配度 |
| --- | --- | --- | --- | --- | --- | --- |
| **Lucide** | v1.48.0（2026-09-24） | ≈1,850 | **ISC**（宽于等于 MIT；自带 Feather 衍生声明清单） | 24px stroke-2 线稿 | ★★★★★（5 天前发版，24.7k star） | **★★★★★ 完全同风格，零迁移成本** |
| **Tabler Icons** | v3.48.0（2026-09-22） | 5,800+（outline+filled 双套） | MIT | 24px stroke≈2 线稿，另有 filled | ★★★★★（21.8k star） | ★★★★ 风格兼容；体量大但只 vendor 用到的就无影响 |
| **Phosphor** | v2.0.8（**2024-02-01**） | ≈9,000（6 字重含 duotone） | MIT | 线稿+面性多字重 | ★★ 近两年半未发版 | ★★★ 最丰富但"安静克制"用不上 duotone；停更风险 |
| **Heroicons** | v2.2.0（2024-11-18） | ≈1,300（outline/solid/mini/micro） | MIT | Tailwind 官方系 | ★★★ 更新放缓 | ★★★ 色板同源（Tailwind），但集小、16px micro 对桌面页无增益 |
| **Feather**（现状血统来源） | v4.29（2020 后实际停更，510 个 open issue） | 287 | MIT | 24px stroke-2 | ★ 已停 | ★ 现状问题的根源，即"无上游可跟" |
| Material Symbols | 持续更新 | 2,500+ | Apache-2.0 | 变量字体/面性 | ★★★★★ | ★✗ 风格不合；字体方案与零构建链冲突 |
| Remix Icon | 持续更新 | ≈3,000 | Apache-2.0 | 面性为主 | ★★★★ | ★✗ 面性，风格不合 |
| Iconify | （聚合器） | 20 万+ | 各集随源 | — | ★★★★★ | ★★ 只适合**设计期检索**（可预览全库挑图形），不做运行时依赖 |
| HugeIcons Free | 持续更新 | 4,000+ 免费档 | **CC-BY 4.0（需署名）/双许可** | 线稿 | ★★★★ | ★✗ 署名义务+升级到付费档的许可陷阱，规避 |

**排除逻辑小结**：风格约束（stroke-2 线稿、与 Tailwind 系色板共存）+ 零构建链 + 断网可用，
实际上只剩 Lucide 与 Tabler 两个严肃候选。

## 3. 界面图标选型：Lucide（Tabler 为备选）

### 3.1 理由

1. **现状就是 Lucide**——`dyj-icon.js` 的 printer/copy/compass/globe/alert 系路径与 Lucide 逐点一致（printer 已比对确认），换 Lucide 等于"从手抄升级为有名有姓"，用户端观感零突变。
2. **活跃度顶级**：v1.48.0 发布于 2026-09-24（5 天前），是 Feather 事实上的社区继任者（Feather 自 2020 年停更）。
3. **语义缺口全补齐**：`activity`/`gauge`（状态页）、`network`（真正的网络拓扑图形）、`refresh-cw`、`external-link`、`wifi`、`settings`、`download`、`circle-pause`/`circle-play` 等 1,850 枚里全有。
4. **ISC 许可**，宽松度等同 MIT，附带明确的 Feather 衍生图标清单，合规干净；产品可以闭源分发不冲突。
5. **生态默认值**：2025–2026 年 shadcn/ui、主流 dashboard 模板的既定默认，"安静克制"的现代审美与 theme.css 自述气质一致。

选 Tabler 的唯一场景：将来想要 **filled 实底变体**（如选中态导航图标加底）。届时两者同网格同笔宽，中途迁移成本也很低。

### 3.2 引入方式（保持零 npm、零构建链）

- 在 `dyj-icon.js` 中把 PATHS 扩到实际使用的 ~30 枚；文件头加注释块：来源 `lucide-icons/lucide vX.Y.Z`、ISC、刷新方法（去 lucide.dev 搜图标 → 复制 SVG → 摘 path）。
- 图标总量小（<30 枚），手工同步成本趋近于零；**不**引入 npm 包/子模块/Iconify 运行时 API。
- `print`/`printer` 重复项合并为一个 `printer`；改名映射：

| 现名 | 建议改为（Lucide 名） | 原因 |
| --- | --- | --- |
| `check`（导航"状态"处） | `activity`（或 `gauge`） | 对勾≠状态；activity 心电图线是"运行状态"的通用隐喻 |
| `network`（globe） | `network`（节点树图形） | 网络页讲 IP/mDNS/接口，不是"国际互联网" |
| `log` | `file-text`（名字规范化，图形不变） | 与上游名对齐，便于将来 diff |
| `print` | 并入 `printer` | 去重 |
| 其余 | 保持现名（图形即 Lucide 原样） | 改动最小 |

### 3.3 品牌标识限制（选库时容易踩的坑）

Lucide 明确**不接受也不用于品牌 logo**（仓库有 BRAND_LOGOS_STATEMENT），侧栏品牌位、favicon、安装器图标都不该拿图标库图形充当产品标识 → 引出自绘方案（§4）。

---

## 4. 品牌 logo / 系统图标：自绘方案

### 4.1 为什么自绘

- 各主流库的许可都**允许**商用/修改，但用现成图标当产品 logo 会撞脸（同一个 Lucide printer 全世界几百万产品在用）且无识别度；
- Lucide 官方立场就是"图标库不服务品牌场景"；
- 免费插画/logo 生成器（SVG Repo、各 AI logo 站）产出许可参差，不可控。

### 4.2 设计方向（三选一，建议 A）

统一母题：**打印机（产品本体）+ 电波/波纹（LAN 无驱动共享）**，24px 网格矢量，先画单色线稿再出全彩。

- **A. 打印机出纸 + 出纸处三道波纹**（推荐）：业务一眼可读；波纹既是"无线共享"也是 mDNS 广播的隐喻；小尺寸（16px favicon）退化为"打印机+一个点"仍可辨识。
- B. 圆角方形底 + 抽象"纸→波"箭头：更极简但太抽象，普通用户不识字拼不出含义。
- C. 字标"dyj"几何化：品牌感强，但非拉丁用户记不住，且打印机品类信息丢失。

产出两个变体（都先在仓库里以 `.svg` 源文件形式维护，如 `assets/brand/mark.svg`、`assets/brand/logo-color.svg`）：

| 变体 | 形态 | 消费方 |
| --- | --- | --- |
| `mark`（单色线稿，currentColor） | 24px 网格，stroke-2，与 Lucide 同语法 | Web 侧栏品牌位（替换 `dyj-sidebar.js:51` 的 `printer`）、favicon.svg、文档 |
| `logo-color`（全彩圆角方） | 1024px 母版，圆角方形底 + 主色 | 派生 ICO / .icns / hicolor PNG 的唯一源头 |

免费工具链：绘制用 **Inkscape**（GPL）或 **Figma 免费档**；完全可用本仓库"自行设计"路径在纯 SVG 文本层面完成（24px 网格线稿 handcode 是现实的，彩色母版建议 Inkscape/Figma）。

### 4.3 一源多出的生成管线（全部免费/开源）

| 目标 | 工具 | 产出 | 接入点 |
| --- | --- | --- | --- |
| Windows exe 图标+版本信息 | [`tc-hib/go-winres`](https://github.com/tc-hib/go-winres)（MIT，rsrc/goversioninfo 的现代继任者，单 JSON 配置，跨平台出 `.syso`） | `rsrc_windows_amd64.syso` / `…arm64.syso`（16/20/24/32/48/256 ICO + RT_VERSION + manifest） | 放进 `cmd/dyj`、`cmd/dyj-tray` 包目录，`go build` 自动拾取；`build-all.sh` 加一行 `go-winres make --in … --out …`；dyj.iss 的 `UninstallDisplayIcon` 自动变好看，无需改 |
| Linux hicolor 图标 | 1024px 母版缩放导出 PNG（16/32/48/64/128/256）+ 原始 SVG | `/usr/share/icons/hicolor/<size>/apps/dyj.png`、`…/scalable/apps/dyj.svg` | nfpm.yaml `contents` 增加条目；`dyj-admin.desktop:9` `Icon=printer` → `Icon=dyj` |
| macOS .icns | `iconutil`（macOS 自带）或开源 `png2icns` | `dyj.icns` | v2 做 .app/cask 时使用（v1 挂起） |
| Web favicon | `mark.svg` + 由母版缩的 `favicon.ico`（<3KB） | `pkg/admin/web/favicon.svg`、`favicon.ico`（进 go:embed） | `index.html` 增加 `<link rel="icon" …>` 两行 |
| 托盘 | 不引资源文件：把 `tray.go` 的 `dotPNG` 升级为"mark 线稿（白，适配深浅任务栏用亮度判断或统一白+描边）+ 右下角状态角标（绿/灰/红）"的运行时矢量光栅化；或构建期把 mark 预渲染成 3 张 32px PNG 常量嵌入 | ICO 字节流（行为与现状同） | `pkg/tray/tray.go:166-224` 重写绘制函数，`IconBytes(s State)` 签名与测试语义不变 |

> 托盘细节注意：Windows 托盘实际渲染 16px（125%/150% DPI 下 20/24px），图形必须在大对比度下可读——"白线稿+彩点"组合在深浅两种任务栏都成立；三态仍由角标颜色+tooltip 文本双通道表达（延续现有无障碍设计）。

---

## 5. 需要更换的位置清单（实施以本表为准）

优先级：P0=用户天天看见的品牌/门面缺口；P1=安装分发观感；P2=锦上添花。

| # | 位置 | 现状 | 改成 | 级别 |
| --- | --- | --- | --- | --- |
| 1 | `pkg/admin/web/components/dyj-icon.js`（整个 PATHS 表） | 13 枚手抄无出处 | vendor Lucide v1.48.x 的 ~30 枚（含 §3.2 改名与新增：`activity`、`network`、`refresh-cw`、`external-link`、`wifi`、`settings`、`download`、`circle-pause`、`circle-play` 等），文件头标来源/版本/许可 | **P0** |
| 2 | `pkg/admin/web/components/dyj-sidebar.js:51` 品牌位 + `:53-54` 导航 | 品牌位用通用 `printer`；导航 `check`/`globe` 语义弱 | 品牌位换自绘 `mark`；导航"状态"→`activity`、"网络"→`network` | **P0** |
| 3 | `pkg/admin/web/index.html`（`<head>`） | 无 favicon | `favicon.svg` + `favicon.ico` 两行 `<link>`（go:embed 已覆盖 web 目录则零后端改动，需在 `pkg/admin/web.go` 确认 embed 模式与 MIME） | **P0** |
| 4 | `pkg/tray/tray.go:166-224`（`dotPNG` 及 `statusRGB`） | 纯色圆点 | 品牌线稿 + 状态角标；`IconBytes(s)` 签名/ICO 容器/测试语义不变 | **P0** |
| 5 | `cmd/dyj`、`cmd/dyj-tray`（新增 `rsrc_windows_*.syso` + `winres.json`）、`scripts/build-all.sh` | 无资源嵌入，Go 默认图标、无版本信息 | go-winres 生成 `.syso`（多尺寸 ICO=logo-color 派生 + RT_VERSION），build-all.sh 增加生成步骤（产物可提交，避免 CI 装工具） | **P1** |
| 6 | `packaging/linux/dyj-admin.desktop:9` + `packaging/nfpm.yaml`（contents） | `Icon=printer`（主题通用图标） | `Icon=dyj`；nfpm 增装 hicolor PNG/SVG 条目 | **P1** |
| 7 | `pkg/admin/web/components/dyj-empty-state.js` / `dyj-toast.js` / `dyj-diagnostic.js` / `dyj-status-badge.js` | 引用旧图标名 | 跟随 #1 的改名表机械替换（`error`/`warn`/`info` 可选对齐 Lucide 命名 `circle-alert`/`triangle-alert`/`info`，建议保持现名以减小 diff） | P2 |
| 8 | `assets/brand/*.svg`（新增）+ 各派生产物 | 无 | mark.svg（线稿）、logo-color.svg（母版）、favicon、hicolor、ICO、.icns 的生成脚本或一次性产物 | **P1**（是 #3/#5/#6 的前置） |
| 9 | macOS `.icns` | 无 | v2 .app/cask 时再做 | P2（挂起） |
| 10 | 托盘菜单项带图标（`tray_windows.go` AddMenuItem 二参） | 纯文字 | Windows 支持菜单图标，观感增益有限 | P2 |

工作量预估：#1/#2/#3/#7 纯前端替换 ≈ 半天；#4 托盘重绘 ≈ 半天（含测试）；#8 品牌绘制 ≈ 半天；#5/#6 打包接入 ≈ 半天。合计 2–2.5 个工作日。

## 6. 落地顺序建议

1. **先画 `mark.svg` + `logo-color.svg`**（一切派生的源头）。
2. 换 `dyj-icon.js` → Lucide 全量替换 + 改名（#1/#2/#7，一次 PR，纯路径替换零风险）。
3. favicon（#3）。
4. 托盘重绘（#4，含 `tray_test.go` 颜色断言更新）。
5. go-winres `.syso` + build-all.sh（#5）。
6. Linux hicolor + desktop + nfpm（#6）。
7. 每步产物在真实机器上的验收项：Explorer/任务栏图标、卸载列表图标、GNOME/KDE 应用网格图标、浏览器标签页图标（本仓库已知待办：物理机验收清单，见 memory/docs 10）。

## 7. 来源（2026-09-29 实查）

- Lucide 官网/许可（ISC）/Feather 衍生清单：https://lucide.dev/license 、https://github.com/lucide-icons/lucide
- 版本数据（GitHub Releases API，2026-09-29）：lucide v1.48.0（09-24）、tabler/tabler-icons v3.48.0（09-22）、phosphor-icons/core v2.0.8（2024-02-01）、tailwindlabs/heroicons v2.2.0（2024-11-18）、feathericons/feather（510 open issues，实际停更）
- Lucide 品牌限制声明：https://github.com/lucide-icons/lucide/blob/main/BRAND_LOGOS_STATEMENT.md
- go-winres（Windows 资源嵌入）：https://github.com/tc-hib/go-winres
- 选型对比佐证：https://allsvgicons.com 、https://www.moonb.io（17 best icon libraries，Lucide 居首）、dev.to《10+ Best Open Source Icon Libraries》
