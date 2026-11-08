// dyj 官网 Worker —— https://dyj.qdzwwqd.top
// 职责：1) 官网介绍页（GET /）  2) GitHub Release 资产的边缘加速下载代理（/download/...）
// 无任何绑定 / secret；上游为仓库 GitHub Release；License 见仓库 ADR-0003（Apache-2.0）。
//
// 设计：浅色克制工具风 —— 白底墨字 + 单一强调色（与管理页 --dyj-primary 同源），
// 无 emoji，图标统一为内联 SVG；页面结构 = Hero / 三步 / 能力 / 下载 / 快速开始。
// 渲染拆成按区块的函数，样式集中在 STYLE 常量，便于后续单点调整。

const REPO = "232252/dyj";
const RELEASES_URL = `https://github.com/${REPO}/releases`;

// 平台别名 → 资产文件名模板函数（v = 版本号，不带 v 前缀）
const PLATFORMS = {
  "windows":           () => ["dyj-setup-x64.exe"],
  "windows-portable":  () => ["dyj-windows-amd64.exe"],
  "windows-arm64":     () => ["dyj-windows-arm64.exe"],
  "windows-32":        () => ["dyj-windows-386.exe"],
  "windows-legacy":    () => ["dyj-legacy-win7-amd64.zip"],
  "windows-legacy-32": () => ["dyj-legacy-win7-386.zip"],
  "mac":               () => ["dyj-darwin-arm64.tar.gz"],
  "mac-intel":         () => ["dyj-darwin-amd64.tar.gz"],
  "linux":             (v) => [`dyj-${v}-linux-amd64.deb`],
  "linux-rpm":         (v) => [`dyj-${v}-linux-amd64.rpm`],
  "linux-arm64":       (v) => [`dyj-${v}-linux-arm64.deb`],
  "linux-arm64-rpm":   (v) => [`dyj-${v}-linux-arm64.rpm`],
  "linux-tgz":         () => ["dyj-linux-amd64.tar.gz"],
  "linux-arm64-tgz":   () => ["dyj-linux-arm64.tar.gz"],
};

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const path = url.pathname.replace(/\/+$/, "") || "/";
    if (request.method !== "GET" && request.method !== "HEAD") {
      return plain(405, "仅支持 GET / HEAD");
    }
    try {
      if (path === "/" || path === "/index.html") return await renderHome(ctx);
      if (path === "/download") return redirect("/");
      if (path.startsWith("/download/")) return await handleDownload(request, ctx, path);
      return plain(404, `页面不存在：${path}`);
    } catch (err) {
      return plain(502, `请求失败：${err && err.message ? err.message : String(err)}`);
    }
  },
};

function redirect(to) {
  return new Response(null, { status: 302, headers: { Location: to } });
}

function plain(status, text) {
  return new Response(`${text}\n`, {
    status,
    headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store" },
  });
}

// ---------- GitHub Release 元数据（边缘缓存 5 分钟，降低 rate limit 风险） ----------
async function getRelease(ctx) {
  const apiUrl = `https://api.github.com/repos/${REPO}/releases/latest`;
  const cache = caches.default;
  const cacheKey = new Request(apiUrl);
  const hit = await cache.match(cacheKey);
  if (hit) return hit.json();

  const res = await fetch(apiUrl, {
    headers: { "User-Agent": "dyj-site", Accept: "application/vnd.github+json" },
    cf: { cacheTtl: 300, cacheEverything: true },
  });
  if (!res.ok) return null; // 页面走降级渲染；下载路由会明确报错
  const data = await res.json();
  const slim = {
    tag: data.tag_name,
    version: String(data.tag_name || "").replace(/^v/, ""),
    url: data.html_url,
    assets: (data.assets || []).map((a) => ({ name: a.name, size: a.size })),
  };
  const cached = new Response(JSON.stringify(slim), {
    headers: { "Content-Type": "application/json", "Cache-Control": "max-age=300" },
  });
  ctx.waitUntil(cache.put(cacheKey, cached.clone()));
  return slim;
}

// ---------- 下载代理：流式透传 + Cloudflare 边缘缓存 ----------
async function proxyAsset(ctx, method, tag, asset) {
  const upstream = `https://github.com/${REPO}/releases/download/${encodeURIComponent(tag)}/${encodeURIComponent(asset)}`;
  const res = await fetch(upstream, { cf: { cacheTtl: 2592000, cacheEverything: true } });
  if (res.status === 404) {
    return plain(404, `资产不存在：${asset} @ ${tag}。请到 GitHub Release 页面确认文件名。`);
  }
  if (!res.ok) return plain(502, `GitHub 下载失败：HTTP ${res.status}`);

  const headers = new Headers();
  for (const h of ["content-type", "content-length", "last-modified", "etag"]) {
    const v = res.headers.get(h);
    if (v) headers.set(h, v);
  }
  if (!headers.has("content-type")) headers.set("content-type", "application/octet-stream");
  headers.set("content-disposition", `attachment; filename*=UTF-8''${encodeURIComponent(asset)}`);
  headers.set("cache-control", "public, max-age=86400");
  return new Response(method === "HEAD" ? null : res.body, { status: res.status, headers });
}

async function handleDownload(request, ctx, path) {
  const parts = path.slice("/download/".length).split("/").filter(Boolean).map(decodeURIComponent);

  // /download/{tag}/{filename}：显式版本 + 具体资产
  if (parts.length >= 2) {
    const [tag, asset] = [parts[0], parts.slice(1).join("-")];
    if (!/^[A-Za-z0-9._-]+$/.test(tag) || !/^[A-Za-z0-9._-]+$/.test(asset)) {
      return plain(400, "非法的下载路径");
    }
    return proxyAsset(ctx, request.method, tag, asset);
  }

  // /download/{platform}：按 token 到最新 Release 找资产
  const token = parts[0] || "";
  const maker = PLATFORMS[token];
  if (!maker) {
    return plain(404, `未知平台：${token}。可用别名：${Object.keys(PLATFORMS).join(" / ")}`);
  }
  const rel = await getRelease(ctx);
  if (!rel || !rel.tag) {
    return plain(502, "暂时无法解析最新 Release 信息（GitHub API 不可用或限流），请稍后重试，或直接到 GitHub Release 下载。");
  }
  return proxyAsset(ctx, request.method, rel.tag, maker(rel.version)[0]);
}

// ---------- 图标（内联 SVG，替代 emoji） ----------
// logo = 仓库 assets/brand/dyj-mark.svg 的源图形（打印机 + 广播波纹），
//        品牌位专用（Lucide 图形不作品牌用，见 docs/research/11 §3.3/§4.2）。
// 其余 = Lucide 图形（ISC），手工内联以保持零 npm、零构建链（同 research/11 §3.2）。
const ICON = {
  logo: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5.6 6.75a8.5 8.5 0 0 1 12.8 0"/><path d="M8.5 6.75a4.95 4.95 0 0 1 7 0"/><path d="M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2"/><rect x="6" y="14" width="12" height="8" rx="1"/></svg>',
  wifi: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h.01"/><path d="M2 8.82a15 15 0 0 1 20 0"/><path d="M5 12.86a10 10 0 0 1 14 0"/><path d="M8.5 16.43a5 5 0 0 1 7 0"/></svg>',
  key: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15.5 7.5 2.3 2.3a1 1 0 0 0 1.4 0l2.1-2.1a1 1 0 0 0 0-1.4L19 4"/><path d="m21 2-9.6 9.6"/><circle cx="7.5" cy="15.5" r="5.5"/></svg>',
  printer: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2"/><path d="M6 9V3h12v6"/><rect x="6" y="14" width="12" height="8" rx="1"/></svg>',
  layers: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/><path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/></svg>',
  globe: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/></svg>',
  wrench: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>',
  download: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/></svg>',
  arrowRight: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>',
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>',
  sun: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>',
  moon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>',
};

// ---------- 站点样式 ----------
const STYLE = `
:root{
  color-scheme: light;
  --bg:#ffffff; --bg-soft:#fbfbfc;
  --surface:#ffffff; --surface-2:#f7f7f8;
  --text:#18181b; --text-2:#3f3f46; --dim:#6b7280; --faint:#a1a1aa;
  --line:#e4e4e7; --line-2:#eeeef1;
  --accent:#2563eb; --accent-2:#1d4ed8; --accent-soft:#eff4ff;
  --code-bg:#f7f8fa; --code-text:#3f3f46;
  --shadow-sm:0 1px 2px rgba(24,24,27,.05);
  --shadow-md:0 14px 34px -14px rgba(24,24,27,.20), 0 2px 8px rgba(24,24,27,.04);
  --radius:10px; --max:1120px;
}
[data-theme="dark"]{
  color-scheme: dark;
  --bg:#0b0b0d; --bg-soft:#0f0f11;
  --surface:#131316; --surface-2:#1a1a1e;
  --text:#f4f4f5; --text-2:#d4d4d8; --dim:#a1a1aa; --faint:#71717a;
  --line:#27272a; --line-2:#1f1f23;
  --accent:#7aa2ff; --accent-2:#9db9ff; --accent-soft:#16203a;
  --code-bg:#141417; --code-text:#d4d4d8;
  --shadow-sm:0 1px 2px rgba(0,0,0,.45);
  --shadow-md:0 18px 44px -18px rgba(0,0,0,.75), 0 2px 8px rgba(0,0,0,.45);
}
*{box-sizing:border-box}
html{-webkit-text-size-adjust:100%;scroll-behavior:smooth;scroll-padding-top:84px}
@media (prefers-reduced-motion:reduce){html{scroll-behavior:auto}}
body{
  margin:0;background:var(--bg);color:var(--text);
  font:400 16px/1.75 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Hiragino Sans GB","Microsoft YaHei","Source Han Sans SC",sans-serif;
  -webkit-font-smoothing:antialiased;text-rendering:optimizeLegibility;
}
a{color:inherit;text-decoration:none}
img,svg{display:block}
svg{width:18px;height:18px;flex:none}
::selection{background:var(--accent);color:#fff}
:focus-visible{outline:2px solid var(--accent);outline-offset:3px;border-radius:6px}
.wrap{max-width:var(--max);margin:0 auto;padding:0 24px}
.ic{width:20px;height:20px;flex:none}

/* ---------- 顶栏 ---------- */
.top{
  position:sticky;top:0;z-index:30;border-bottom:1px solid var(--line);
  background:rgba(255,255,255,.86);
  backdrop-filter:saturate(180%) blur(12px);-webkit-backdrop-filter:saturate(180%) blur(12px);
}
[data-theme="dark"] .top{background:rgba(11,11,13,.82)}
.top .wrap{display:flex;align-items:center;justify-content:space-between;gap:20px;height:60px}
.brand{display:inline-flex;align-items:center;gap:9px;font-size:16px;font-weight:650;letter-spacing:-.01em}
.brand .mark{width:22px;height:22px;color:var(--accent);display:inline-flex}
.brand .mark svg{width:100%;height:100%}
.brand em{font-style:normal;font-weight:450;color:var(--dim)}
nav{display:flex;align-items:center;gap:2px}
nav a{color:var(--dim);font-size:14.5px;padding:7px 11px;border-radius:8px;transition:color .18s,background .18s}
nav a:hover{color:var(--text);background:var(--surface-2)}
.top-actions{display:flex;align-items:center;gap:8px}
.btn-ghost{
  display:inline-flex;align-items:center;gap:7px;font-size:14px;color:var(--text-2);
  border:1px solid var(--line);background:var(--surface);padding:7px 12px;border-radius:8px;
  transition:border-color .18s,color .18s,background .18s;
}
.btn-ghost:hover{border-color:var(--faint);color:var(--text)}
.btn-ghost .ic{width:16px;height:16px}
.icon-btn{
  width:34px;height:34px;display:grid;place-items:center;cursor:pointer;
  border:1px solid var(--line);background:var(--surface);border-radius:8px;color:var(--dim);
  transition:border-color .18s,color .18s,background .18s;
}
.icon-btn:hover{border-color:var(--faint);color:var(--text)}
.icon-btn .ic{width:17px;height:17px}
[data-theme="dark"] .ic-sun{display:none}
[data-theme="dark"] .ic-moon{display:block}
.ic-moon{display:none}

/* ---------- 按钮 ---------- */
.btn{
  display:inline-flex;align-items:center;justify-content:center;gap:8px;
  font-size:15px;font-weight:550;padding:10px 18px;border-radius:var(--radius);
  border:1px solid transparent;cursor:pointer;white-space:nowrap;
  transition:background .18s,border-color .18s,color .18s,transform .12s;
}
.btn:active{transform:translateY(1px)}
.btn .ic{width:18px;height:18px}
.btn-primary{background:var(--accent);color:#fff;box-shadow:0 1px 2px rgba(37,99,235,.22)}
.btn-primary:hover{background:var(--accent-2)}
.btn-quiet{background:var(--surface);border-color:var(--line);color:var(--text)}
.btn-quiet:hover{border-color:var(--faint)}
.btn-lg{font-size:16px;padding:12px 22px}

/* ---------- Hero ---------- */
.hero{padding:80px 0 72px;border-bottom:1px solid var(--line-2)}
.hero-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,430px);gap:64px;align-items:center}
.hero h1{margin:0 0 18px;font-size:clamp(2.3rem,4.6vw,3.4rem);line-height:1.14;font-weight:700;letter-spacing:-.025em}
.hero .lead{margin:0 0 14px;font-size:18.5px;line-height:1.7;color:var(--text-2);max-width:33em}
.hero .note{margin:0 0 28px;font-size:14.5px;color:var(--dim);max-width:33em}
.hero-cta{display:flex;flex-wrap:wrap;gap:12px;margin-bottom:26px}
.hero-meta{display:flex;flex-wrap:wrap;gap:8px 20px;font-size:13.5px;color:var(--dim)}
.hero-meta span{display:inline-flex;align-items:center;gap:7px;white-space:nowrap}
.hero-meta .ic{width:15px;height:15px;color:var(--accent)}

/* ---------- Hero 界面预览 ---------- */
.preview{border:1px solid var(--line);border-radius:14px;background:var(--surface);box-shadow:var(--shadow-md);overflow:hidden}
.pv-bar{display:flex;align-items:center;gap:6px;padding:9px 13px;border-bottom:1px solid var(--line-2);background:var(--surface-2)}
.pv-dot{width:9px;height:9px;border-radius:50%;background:var(--line)}
.pv-url{margin-left:8px;font:12.5px/1 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--faint);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.pv-body{padding:20px}
.pv-label{font-size:13px;color:var(--dim);margin:0 0 12px}
.pv-cards{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.pv-card{border:1px solid var(--line);border-radius:10px;padding:12px 13px;background:var(--bg)}
.pv-card.on{border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-soft)}
.pv-card h4{margin:0 0 3px;font-size:14.5px;font-weight:600}
.pv-card p{margin:0;font-size:12.5px;line-height:1.6;color:var(--dim)}
.pv-code{display:flex;align-items:center;gap:12px;margin-top:16px;padding-top:14px;border-top:1px solid var(--line-2)}
.pv-code .lab{font-size:13px;color:var(--dim)}
.pv-pin{font:600 21px/1 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;letter-spacing:.3em;color:var(--text)}
.pv-live{display:flex;align-items:center;gap:8px;margin-top:12px;font-size:12.5px;color:var(--dim)}
.pv-live .dot{width:7px;height:7px;border-radius:50%;background:#16a34a;flex:none}

/* ---------- 区块通用 ---------- */
section{padding:72px 0;border-bottom:1px solid var(--line-2)}
.eyebrow-sm{font-size:13px;font-weight:600;color:var(--accent);letter-spacing:.02em;margin:0 0 10px}
h2{margin:0 0 10px;font-size:clamp(1.45rem,2.8vw,1.85rem);line-height:1.32;font-weight:650;letter-spacing:-.015em}
.sub2{margin:0 0 40px;font-size:16px;color:var(--dim);max-width:46em}

/* ---------- 三步 ---------- */
.steps{display:grid;grid-template-columns:repeat(3,1fr);gap:40px}
.step{border-top:1px solid var(--line);padding-top:20px}
.step-top{display:flex;align-items:center;justify-content:space-between;margin-bottom:14px}
.step .n{font:600 12.5px/1 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--accent);letter-spacing:.1em}
.step .ic{width:22px;height:22px;color:var(--text)}
.step h3{margin:0 0 6px;font-size:16.5px;font-weight:600}
.step p{margin:0;font-size:14.5px;line-height:1.75;color:var(--dim)}

/* ---------- 能力 ---------- */
.feats{display:grid;grid-template-columns:repeat(2,1fr);gap:0 64px}
.feat{display:grid;grid-template-columns:22px 1fr;gap:15px;padding:24px 0;border-top:1px solid var(--line)}
.feat .ic{width:20px;height:20px;color:var(--accent);margin-top:3px}
.feat h3{margin:0 0 5px;font-size:16px;font-weight:600}
.feat p{margin:0;font-size:14.5px;line-height:1.75;color:var(--dim)}
.feat code{font:13px/1.6 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:var(--surface-2);border:1px solid var(--line-2);border-radius:5px;padding:1px 5px;color:var(--text-2)}

/* ---------- 下载 ---------- */
.dl-bar{display:flex;flex-wrap:wrap;align-items:center;gap:12px;margin-bottom:32px}
.dl-bar label{font-size:14px;color:var(--dim)}
.dl-select{
  appearance:none;font:inherit;font-size:14px;color:var(--text);
  border:1px solid var(--line);border-radius:8px;background:var(--surface);
  padding:7px 30px 7px 11px;cursor:pointer;
  background-image:linear-gradient(45deg,transparent 50%,currentColor 50%),linear-gradient(135deg,currentColor 50%,transparent 50%);
  background-position:calc(100% - 15px) 50%,calc(100% - 10px) 50%;
  background-size:5px 5px,5px 5px;background-repeat:no-repeat;
}
.dl-select:hover{border-color:var(--faint)}
.dl-note{font-size:13.5px;color:var(--dim)}
.dl-groups{display:grid;grid-template-columns:repeat(3,1fr);gap:48px}
.dl-group h3{margin:0 0 4px;font-size:15px;font-weight:600;display:flex;align-items:baseline;gap:8px}
.dl-group .chip{font-size:12px;color:var(--dim);font-weight:400}
.dl-list{list-style:none;margin:14px 0 0;padding:0;border-top:1px solid var(--line)}
.dl-list li{border-bottom:1px solid var(--line-2)}
.dl-link{display:flex;align-items:center;gap:12px;padding:11px 2px;transition:color .16s}
.dl-link .t{flex:1;font-size:14.5px}
.dl-link .m{font-size:12.5px;color:var(--faint);font-variant-numeric:tabular-nums}
.dl-link .ic{width:16px;height:16px;color:var(--faint);transition:color .16s,transform .16s}
.dl-link:hover{color:var(--accent)}
.dl-link:hover .ic{color:var(--accent);transform:translateY(2px)}
.dl-fallback{border:1px solid var(--line);border-radius:var(--radius);padding:20px;font-size:14.5px;color:var(--dim)}
.dl-fallback a{color:var(--accent);font-weight:550}
.dl-foot{margin:36px 0 0;font-size:13.5px;color:var(--dim);max-width:52em}

/* ---------- 快速开始 ---------- */
.quick-grid{display:grid;grid-template-columns:minmax(0,1fr) 300px;gap:48px;align-items:start}
pre{
  margin:0;background:var(--code-bg);border:1px solid var(--line);border-radius:12px;
  padding:20px 22px;overflow:auto;
  font:13.5px/1.95 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--code-text);
}
pre .cm{color:var(--faint)}
.quick-side h3{margin:0 0 10px;font-size:14px;font-weight:600}
.quick-side p{margin:0 0 16px;font-size:14px;line-height:1.75;color:var(--dim)}
.quick-side ul{margin:0;padding-left:18px;font-size:14px;line-height:1.9;color:var(--dim)}
.quick-side a{color:var(--accent);font-weight:550}

/* ---------- 页脚 ---------- */
footer{border-top:1px solid var(--line);padding:36px 0;font-size:13.5px;color:var(--dim)}
footer .wrap{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:14px 24px}
footer .links{display:flex;flex-wrap:wrap;gap:8px 22px}
footer a{transition:color .16s}
footer a:hover{color:var(--text)}

/* ---------- 滚动淡入 ---------- */
.reveal{opacity:0;transform:translateY(10px);transition:opacity .5s ease,transform .5s ease}
.reveal.in{opacity:1;transform:none}
@media (prefers-reduced-motion:reduce){.reveal{opacity:1;transform:none;transition:none}}

/* ---------- 响应式 ---------- */
@media (max-width:980px){
  .hero{padding:64px 0 56px}
  .hero-grid{grid-template-columns:1fr;gap:40px}
  .preview{max-width:480px}
  .steps{grid-template-columns:1fr;gap:28px}
  .feats{grid-template-columns:1fr}
  .dl-groups{grid-template-columns:1fr;gap:36px}
  .quick-grid{grid-template-columns:1fr;gap:28px}
}
@media (max-width:640px){
  .wrap{padding:0 18px}
  nav{display:none}
  .hero h1{font-size:2rem}
  .hero .lead{font-size:17px}
  section{padding:56px 0}
  .pv-cards{grid-template-columns:1fr}
}
`;

// ---------- 页面区块 ----------
function renderTopbar() {
  return `<header class="top">
  <div class="wrap">
    <a class="brand" href="/"><span class="mark">${ICON.logo}</span>dyj <em>局域网打印机共享</em></a>
    <nav>
      <a href="#how">怎么用</a>
      <a href="#features">能力</a>
      <a href="#download">下载</a>
      <a href="#quickstart">快速开始</a>
    </nav>
    <div class="top-actions">
      <a class="btn-ghost" href="https://github.com/${REPO}" target="_blank" rel="noopener">GitHub <span class="ic">${ICON.arrowRight}</span></a>
      <button id="theme-toggle" class="icon-btn" type="button" aria-label="切换深浅色主题" title="切换深浅色主题">
        <span class="ic ic-sun">${ICON.sun}</span><span class="ic ic-moon">${ICON.moon}</span>
      </button>
    </div>
  </div>
</header>`;
}

function renderHero(rel) {
  const version = rel && rel.version ? ` v${escapeHtml(rel.version)}` : "";
  const cta = rel && rel.tag
    ? `<a id="dl-primary" class="btn btn-primary btn-lg" href="/download/windows" data-token="windows"><span class="ic">${ICON.download}</span>下载 dyj${version}</a>`
    : `<a class="btn btn-primary btn-lg" href="${RELEASES_URL}" target="_blank" rel="noopener"><span class="ic">${ICON.download}</span>前往 GitHub 下载</a>`;
  return `<section class="hero">
  <div class="wrap hero-grid">
    <div>
      <h1>把一台打印机，<br>共享给全屋设备</h1>
      <p class="lead">Windows、Linux、macOS 免装驱动直接打印。<br>双击 dyj，浏览器里只有两张卡：服务端和客户端。</p>
      <p class="note">服务端一键共享本机打印机，客户端输 4 位码就能装好。<br>全程不用命令行，也不用装驱动。</p>
      <div class="hero-cta">
        ${cta}
        <a class="btn btn-quiet btn-lg" href="${RELEASES_URL}" target="_blank" rel="noopener">查看全部版本</a>
      </div>
      <div class="hero-meta">
        <span><span class="ic">${ICON.check}</span>单个静态二进制 8–11 MB</span>
        <span><span class="ic">${ICON.check}</span>无运行时依赖</span>
        <span><span class="ic">${ICON.check}</span>Apache-2.0 开源</span>
      </div>
    </div>
    <div class="preview" aria-hidden="true">
      <div class="pv-bar">
        <span class="pv-dot"></span><span class="pv-dot"></span><span class="pv-dot"></span>
        <span class="pv-url">127.0.0.1:6631/admin</span>
      </div>
      <div class="pv-body">
        <p class="pv-label">选择你的方向</p>
        <div class="pv-cards">
          <div class="pv-card on">
            <h4>服务端</h4>
            <p>共享这台机器上的打印机</p>
          </div>
          <div class="pv-card">
            <h4>客户端</h4>
            <p>添加局域网里的打印机</p>
          </div>
        </div>
        <div class="pv-code">
          <span class="lab">配对码</span>
          <span class="pv-pin">4827</span>
        </div>
        <div class="pv-live"><span class="dot"></span>办公室打印机 · 局域网可见</div>
      </div>
    </div>
  </div>
</section>`;
}

function renderSteps() {
  const steps = [
    { n: "01", ic: ICON.wifi, t: "扫描", d: "打开管理页，dyj 自动列出局域网里的打印机，不用手填地址。" },
    { n: "02", ic: ICON.key, t: "配对", d: "输入服务端显示的 4 位码，确认后自动装成系统打印队列。" },
    { n: "03", ic: ICON.printer, t: "打印", d: "打印机出现在系统队列里，WPS、Word、记事本直接选它打印。" },
  ];
  return `<section id="how" class="reveal">
  <div class="wrap">
    <p class="eyebrow-sm">怎么用</p>
    <h2>三步，从打开到打印</h2>
    <p class="sub2">没有安装向导，也没有设置页——把该自动的都自动化了。</p>
    <div class="steps">
      ${steps.map((s) => `<div class="step">
        <div class="step-top"><span class="ic">${s.ic}</span><span class="n">${s.n}</span></div>
        <h3>${s.t}</h3>
        <p>${s.d}</p>
      </div>`).join("")}
    </div>
  </div>
</section>`;
}

function renderFeatures() {
  const feats = [
    { ic: ICON.printer, t: "免装驱动", d: "走 IPP 2.0 与 IPP Everywhere，三大平台原生支持；只声明真能渲染的格式，不做假兼容。" },
    { ic: ICON.wifi, t: "自动发现", d: "同一网络里一开机就能看到；组播不通时，回退到扫描与地址直连。" },
    { ic: ICON.key, t: "4 位码配对", d: "本机回环一律放行，局域网默认拒绝；换设备发个码就行，随时能撤销。" },
    { ic: ICON.layers, t: "多台打印机", d: "服务端逐台开关共享，客户端自动装成系统队列，互不干扰。" },
    { ic: ICON.globe, t: "跨网段可用", d: "两台机器只靠 mesh 或 VPN 相连、组播过不去时，填对方地址照样直连。" },
    { ic: ICON.wrench, t: "自检与诊断", d: "<code>dyj doctor</code> 跑一遍自检，一键导出诊断包，发给能帮你的人即可。" },
  ];
  return `<section id="features" class="reveal">
  <div class="wrap">
    <p class="eyebrow-sm">能力</p>
    <h2>省下来的，都是配置的麻烦</h2>
    <p class="sub2">端口、防火墙、发现协议、驱动安装——这些事不该占用你的时间。</p>
    <div class="feats">
      ${feats.map((f) => `<div class="feat">
        <span class="ic">${f.ic}</span>
        <div>
          <h3>${f.t}</h3>
          <p>${f.d}</p>
        </div>
      </div>`).join("")}
    </div>
  </div>
</section>`;
}

function renderDownloads(rel) {
  if (!rel || !rel.tag) {
    return `<section id="download" class="reveal">
  <div class="wrap">
    <p class="eyebrow-sm">下载</p>
    <h2>暂时读不到 Release 信息</h2>
    <p class="sub2">GitHub 接口这会儿没响应，稍后刷新页面通常就能恢复。</p>
    <div class="dl-fallback">也可以直接去 <a href="${RELEASES_URL}" target="_blank" rel="noopener">GitHub Releases 页面</a> 下载，文件名和版本号都在那里。</div>
  </div>
</section>`;
  }

  const v = rel.version;
  const groups = [
    {
      os: "Windows", chip: "Win10 / 11",
      rows: [
        { token: "windows", asset: "dyj-setup-x64.exe", label: "安装版（推荐）" },
        { token: "windows-portable", asset: "dyj-windows-amd64.exe", label: "便携版 x64" },
        { token: "windows-arm64", asset: "dyj-windows-arm64.exe", label: "便携版 ARM64" },
        { token: "windows-legacy", asset: "dyj-legacy-win7-amd64.zip", label: "Win7 / 8 / 8.1 x64" },
        { token: "windows-legacy-32", asset: "dyj-legacy-win7-386.zip", label: "Win7 / 8 / 8.1 32 位" },
      ],
    },
    {
      os: "macOS", chip: "10.14+",
      rows: [
        { token: "mac", asset: "dyj-darwin-arm64.tar.gz", label: "Apple 芯片（M 系列）" },
        { token: "mac-intel", asset: "dyj-darwin-amd64.tar.gz", label: "Intel" },
      ],
    },
    {
      os: "Linux", chip: "amd64 / arm64",
      rows: [
        { token: "linux", asset: `dyj-${v}-linux-amd64.deb`, label: "deb（amd64）" },
        { token: "linux-rpm", asset: `dyj-${v}-linux-amd64.rpm`, label: "rpm（amd64）" },
        { token: "linux-arm64", asset: `dyj-${v}-linux-arm64.deb`, label: "deb（ARM64）" },
        { token: "linux-arm64-rpm", asset: `dyj-${v}-linux-arm64.rpm`, label: "rpm（ARM64）" },
        { token: "linux-tgz", asset: "dyj-linux-amd64.tar.gz", label: "tar.gz（amd64）" },
        { token: "linux-arm64-tgz", asset: "dyj-linux-arm64.tar.gz", label: "tar.gz（ARM64）" },
      ],
    },
  ];

  const sizeOf = (name) => {
    const a = (rel.assets || []).find((x) => x.name === name);
    return a ? humanSize(a.size) : "";
  };
  const has = (name) => (rel.assets || []).some((a) => a.name === name);

  const groupHtml = groups.map((g) => {
    const rows = g.rows.filter((r) => has(r.asset)).map((r) => {
      const gh = `https://github.com/${REPO}/releases/download/${encodeURIComponent(rel.tag)}/${encodeURIComponent(r.asset)}`;
      return `<li><a class="dl-link" href="/download/${r.token}" data-token="${r.token}" data-gh="${escapeHtml(gh)}" rel="nofollow">
            <span class="t">${escapeHtml(r.label)}</span>
            <span class="m">${escapeHtml(sizeOf(r.asset))}</span>
            <span class="ic">${ICON.download}</span>
          </a></li>`;
    }).join("");
    return `<div class="dl-group">
      <h3>${escapeHtml(g.os)}<span class="chip">${escapeHtml(g.chip)}</span></h3>
      <ul class="dl-list">${rows}</ul>
    </div>`;
  }).join("");

  return `<section id="download" class="reveal">
  <div class="wrap">
    <p class="eyebrow-sm">下载</p>
    <h2>下载 dyj v${escapeHtml(v)}</h2>
    <p class="sub2">单个二进制，无需安装依赖。Windows 用户选安装版，其余平台选对应压缩包或安装包。</p>
    <div class="dl-bar">
      <label for="channel-select">下载通道</label>
      <select id="channel-select" class="dl-select" aria-label="选择下载通道">
        <option value="site" selected>本站加速（Cloudflare 边缘）</option>
        <option value="direct">GitHub 直连</option>
        <option value="https://hk.gh-proxy.org/">gh-proxy.org · 香港</option>
        <option value="https://cdn.gh-proxy.org/">gh-proxy.org · CDN</option>
        <option value="https://v6.gh-proxy.org/">gh-proxy.org · IPv6</option>
        <option value="https://edgeone.gh-proxy.org/">gh-proxy.org · EdgeOne</option>
      </select>
      <span class="dl-note" id="channel-note">经本站 Cloudflare 边缘节点代理并缓存。</span>
    </div>
    <div class="dl-groups">${groupHtml}</div>
    <p class="dl-foot">Win7 SP1 / 8 / 8.1 用户请选 Win7 通道（仅打印服务器角色；客户端 driverless 需要 Win8+ 或 Win10 21H2+）。全部版本与变更记录见 <a href="${escapeHtml(rel.url || RELEASES_URL)}" target="_blank" rel="noopener" style="color:var(--accent)">GitHub Release</a>。</p>
  </div>
</section>`;
}

function renderQuickstart() {
  return `<section id="quickstart" class="reveal">
  <div class="wrap">
    <p class="eyebrow-sm">快速开始</p>
    <h2>命令行也很短</h2>
    <p class="sub2">管理页是给不想看命令行的用户准备的；命令行则是给想确认细节的人准备的。</p>
    <div class="quick-grid">
<pre><span class="cm"># 服务端：装着打印机的那台机器</span>
dyj serve -name "办公室打印机" -printer "EPSON L3250" -port 6631

<span class="cm"># 客户端：同一 WiFi，或仅 mesh / VPN 相连</span>
dyj browse                                <span class="cm"># 找到局域网里的打印机</span>
dyj status  192.168.1.10:6631             <span class="cm"># 查询能力</span>
dyj print   192.168.1.10:6631 doc.pdf     <span class="cm"># 直接打印（-copies -media -color）</span>
dyj doctor  --client --json               <span class="cm"># 接收端诊断</span></pre>
      <div class="quick-side">
        <h3>更多用法</h3>
        <p>完整命令列表、参数说明与排错步骤，都在仓库文档里。</p>
        <ul>
          <li><a href="https://github.com/${REPO}#readme" target="_blank" rel="noopener">README 快速上手</a></li>
          <li><a href="https://github.com/${REPO}/blob/main/docs/troubleshooting.md" target="_blank" rel="noopener">排错手册</a></li>
          <li><a href="https://github.com/${REPO}/issues" target="_blank" rel="noopener">提交问题</a></li>
        </ul>
      </div>
    </div>
  </div>
</section>`;
}

function renderFooter() {
  return `<footer>
  <div class="wrap">
    <div class="links">
      <a href="https://github.com/${REPO}" target="_blank" rel="noopener">GitHub</a>
      <a href="https://github.com/${REPO}/issues" target="_blank" rel="noopener">Issues</a>
      <a href="${RELEASES_URL}" target="_blank" rel="noopener">Releases</a>
      <a href="https://www.apache.org/licenses/LICENSE-2.0" target="_blank" rel="noopener">Apache-2.0</a>
    </div>
    <div>dyj — 局域网打印机共享工具 · 由 Cloudflare Workers 加速分发</div>
  </div>
</footer>`;
}

// ---------- 官网页面 ----------
async function renderHome(ctx) {
  const rel = await getRelease(ctx);

  const html = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>dyj — 局域网打印机共享工具（免装驱动）</title>
<meta name="description" content="dyj 把一台机器上的打印机经局域网共享出去，Windows / Linux / macOS 免装驱动直接打印。单个静态二进制 8–11 MB，无运行时依赖。">
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Cdefs%3E%3ClinearGradient id='dyj-tile' x1='0' y1='0' x2='0' y2='1'%3E%3Cstop offset='0' stop-color='%233B82F6'/%3E%3Cstop offset='1' stop-color='%232563EB'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='24' height='24' rx='5.4' fill='url(%23dyj-tile)'/%3E%3Cg transform='translate(3.6 3) scale(0.7)' fill='none' stroke='%23fff' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M5.6 6.75a8.5 8.5 0 0 1 12.8 0'/%3E%3Cpath d='M8.5 6.75a4.95 4.95 0 0 1 7 0'/%3E%3Cpath d='M6 18H4a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2h-2'/%3E%3Crect x='6' y='14' width='12' height='8' rx='1'/%3E%3C/g%3E%3C/svg%3E">
<script>(function(){try{var t=localStorage.getItem("dyj-theme");if(t==="light"||t==="dark"){document.documentElement.setAttribute("data-theme",t)}}catch(e){}})();</script>
<style>${STYLE}</style>
</head>
<body>
${renderTopbar()}
<main>
${renderHero(rel)}
${renderSteps()}
${renderFeatures()}
${renderDownloads(rel)}
${renderQuickstart()}
</main>
${renderFooter()}
<script>
(function () {
  var GH = ${JSON.stringify(
    rel && rel.tag
      ? Object.fromEntries(
          ["windows", "windows-portable", "windows-arm64", "windows-legacy", "windows-legacy-32", "mac", "mac-intel",
           "linux", "linux-rpm", "linux-arm64", "linux-arm64-rpm", "linux-tgz", "linux-arm64-tgz"].map((t) => {
            const maker = PLATFORMS[t];
            const asset = maker ? maker(rel.version)[0] : null;
            return [t, asset ? `https://github.com/${REPO}/releases/download/${encodeURIComponent(rel.tag)}/${encodeURIComponent(asset)}` : null];
          }).filter(([, u]) => u)
        )
      : {}
  )};

  // 主题切换（localStorage 持久化；head 内脚本已提前应用，避免闪烁）
  var root = document.documentElement, btn = document.getElementById("theme-toggle");
  if (btn) {
    btn.addEventListener("click", function () {
      var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("dyj-theme", next); } catch (e) {}
    });
  }

  // 下载按钮按 UA 预判平台
  var ua = navigator.userAgent, target = "windows";
  if (/iPhone|iPad|Android|Linux/i.test(ua)) target = "linux";
  if (/Macintosh|Mac OS X/i.test(ua)) target = "mac";
  if (/Windows NT/i.test(ua)) {
    target = /Windows NT [5-6]\\./.test(ua) ? "windows-legacy" : "windows";
  }
  var b = document.getElementById("dl-primary");
  if (b) {
    b.setAttribute("data-token", target);
    if (GH[target]) b.setAttribute("data-gh", GH[target]);
  }

  // 下载通道：本站加速 / GitHub 直连 / 第三方公共镜像（选择记忆在 localStorage）
  var channelSel = document.getElementById("channel-select");
  var CHANNEL_NOTES = {
    "site": "经本站 Cloudflare 边缘节点代理并缓存。",
    "direct": "GitHub Releases 直连（不经任何加速）。",
    "https://hk.gh-proxy.org/": "第三方公共镜像（gh-proxy.org 香港节点），可用性不受本站控制。",
    "https://cdn.gh-proxy.org/": "第三方公共镜像（gh-proxy.org，Cloudflare CDN）。",
    "https://v6.gh-proxy.org/": "第三方公共镜像（gh-proxy.org），需要本机有 IPv6 网络。",
    "https://edgeone.gh-proxy.org/": "第三方公共镜像（gh-proxy.org，腾讯 EdgeOne）。"
  };
  function applyChannel(ch) {
    var note = document.getElementById("channel-note");
    if (note) note.textContent = CHANNEL_NOTES[ch] || "第三方公共镜像（可用性不受本站控制）。";
    document.querySelectorAll("a[data-token]").forEach(function (a) {
      if (ch === "site") {
        a.setAttribute("href", "/download/" + a.getAttribute("data-token"));
        return;
      }
      var gh = a.getAttribute("data-gh");
      if (!gh) return; // 资产未解析出时保持本站链接
      a.setAttribute("href", ch === "direct" ? gh : ch + gh);
    });
  }
  if (channelSel) {
    var savedCh = null;
    try { savedCh = localStorage.getItem("dyj-channel"); } catch (e) {}
    if (savedCh && channelSel.querySelector('option[value="' + savedCh + '"]')) channelSel.value = savedCh;
    applyChannel(channelSel.value);
    channelSel.addEventListener("change", function () {
      try { localStorage.setItem("dyj-channel", channelSel.value); } catch (e) {}
      applyChannel(channelSel.value);
    });
  }

  // 卡片滚动淡入（系统关动效时 CSS 已禁用）
  var io, els = document.querySelectorAll(".reveal");
  if ("IntersectionObserver" in window && !window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    io = new IntersectionObserver(function (es) {
      es.forEach(function (e) { if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); } });
    }, { threshold: 0.12 });
    els.forEach(function (el) { io.observe(el); });
  } else {
    els.forEach(function (el) { el.classList.add("in"); });
  }
})();
</script>
</body>
</html>`;

  return new Response(html, {
    headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "max-age=300" },
  });
}

function humanSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "";
  const mb = bytes / 1024 / 1024;
  return mb >= 1 ? `${mb.toFixed(1)} MB` : `${Math.round(bytes / 1024)} KB`;
}

function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
