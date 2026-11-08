// 部署 dyj 官网 Worker 到 Cloudflare（基于已连接的 Cloudflare App / mcode-tools CLI）。
// 用法：node website/deploy.mjs
// 依赖：Node ≥ 20（零 npm 依赖）。授权走 MiniMax Cloudflare App，无需 wrangler login。
//
// Windows 传参陷阱：mcode-tools 经 .cmd 垫片会剥掉命令行里内嵌 JSON 的双引号，
// 因此统一走 --args-file（把 {code:...} 写成临时文件再传入），用完即删。

import { readFileSync, writeFileSync, unlinkSync, existsSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const ACCOUNT_ID = "dfd02bbb6a7a4ed67f15cb7531137ae5"; // Qdzwwqd@gmail.com's Account
const SCRIPT_NAME = "dyj";
const COMPATIBILITY_DATE = "2026-09-15";

const workerSource = readFileSync(new URL("./worker.js", import.meta.url), "utf8");
const metadata = { main_module: "worker.js", compatibility_date: COMPATIBILITY_DATE, bindings: [] };
const putPath = `/accounts/${ACCOUNT_ID}/workers/scripts/${SCRIPT_NAME}`;

const code = `async () => {
  const metadata = ${JSON.stringify(metadata)};
  const putPath = ${JSON.stringify(putPath)};
  const form = new FormData();
  form.append("metadata", new Blob([JSON.stringify(metadata)], { type: "application/json" }));
  form.append("worker.js", new Blob([${JSON.stringify(workerSource)}], { type: "application/javascript+module" }), "worker.js");
  return cloudflare.request({ method: "PUT", path: putPath, body: form });
}`;

const argsPath = new URL("./.deploy-args.json", import.meta.url);
writeFileSync(argsPath, JSON.stringify({ account_id: ACCOUNT_ID, code }));

try {
  const argsFile = fileURLToPath(argsPath);
  const r = spawnSync(
    "mcode-tools",
    ["connector", "call", "connector__cloudflare__execute", "--args-file", argsFile],
    { shell: true, encoding: "utf8", maxBuffer: 20 * 1024 * 1024 }
  );
  const raw = (r.stdout || "") + (r.stderr && !r.stdout ? r.stderr : "");
  if (r.error) throw new Error(`无法启动 mcode-tools：${r.error.message}`);
  if (r.status !== 0) throw new Error(`mcode-tools 退出码 ${r.status}\n${(r.stderr || raw || "").slice(0, 800)}`);

  const envelope = parseConnectorOutput(raw);
  if (!envelope || envelope.success !== true) {
    throw new Error(`Cloudflare 返回失败：${JSON.stringify(envelope && envelope.errors)}`);
  }
  const s = envelope.result || {};
  console.log(`✓ Worker "${s.id || SCRIPT_NAME}" 已上传（modified_on=${s.modified_on || "?"}）`);
} catch (err) {
  console.error(`✗ 部署失败：${err.message}`);
  process.exitCode = 1;
} finally {
  const onDisk = fileURLToPath(argsPath);
  if (existsSync(onDisk)) {
    try { unlinkSync(onDisk); } catch {}
  }
}

function parseConnectorOutput(raw) {
  const trimmed = String(raw || "").trim();
  if (!trimmed) throw new Error("mcode-tools 无输出");
  try {
    const outer = JSON.parse(trimmed);
    if (Array.isArray(outer) && outer[0] && typeof outer[0].text === "string") {
      return JSON.parse(outer[0].text); // {"type":"text","text":"<envelope-json>"}
    }
    return outer;
  } catch (e) {
    throw new Error(`输出解析失败：${trimmed.slice(0, 300)}`);
  }
}
