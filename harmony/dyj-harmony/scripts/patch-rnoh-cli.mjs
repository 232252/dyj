#!/usr/bin/env node
/**
 * 给 RNOH CLI 打 Node ≥22 兼容补丁（幂等）。
 *
 * 背景：Node 22 移除了 `fs.Dirent#path`，改用 `parentPath`。RNOH CLI 的
 * `dist/io/RealFS.js` 里 `RealFSDirent.path` 直接读 `nodeFSDirent.path`，
 * 在 Node ≥22 上取到 undefined，于是 `init-harmony` / `run-harmony` 崩在
 * "paths[0] argument must be of type string"。
 *
 * 上游 0.82.35 与 0.84.5 的这段代码完全相同。补丁只把读取改成
 * `parentPath ?? path`（新 API 优先，旧 Node 回退），不改变任何行为。
 *
 * 为什么需要这个文件：补丁改的是 node_modules 里的上游产物，不能进仓库；
 * 但「每次装依赖都要手动改一遍」对别人不友好，于是把「怎么改」固化成脚本，
 * 由 package.json 的 postinstall 钩子自动执行。上游修好后本脚本会变成空操作。
 */

import {readFileSync, writeFileSync, existsSync} from 'node:fs';
import {dirname, join} from 'node:path';
import {fileURLToPath} from 'node:url';

const projectRoot = dirname(dirname(fileURLToPath(import.meta.url)));

const target = join(
  projectRoot,
  'node_modules',
  '@react-native-oh',
  'react-native-harmony-cli',
  'dist',
  'io',
  'RealFS.js',
);

const BROKEN = 'this.nodeFSDirent.path';
const FIXED = '(this.nodeFSDirent.parentPath ?? this.nodeFSDirent.path)';

function warn(message) {
  console.warn(`[patch-rnoh-cli] ${message}`);
}

if (!existsSync(target)) {
  warn(
    `未找到 ${target}。若 CLI 未安装可忽略；` +
      `若随后 init-harmony 报 "paths[0] argument must be of type string"，` +
      `说明 Node ≥22 与该版本 RNOH CLI 不兼容，见 harmony/README.md。`,
  );
  process.exit(0);
}

const original = readFileSync(target, 'utf8');

if (original.includes(FIXED)) {
  warn('已是补丁状态，跳过。');
  process.exit(0);
}

if (!original.includes(BROKEN)) {
  warn('上游代码结构已变（未找到预期片段），未改动。');
  process.exit(0);
}

const patched = original.split(BROKEN).join(FIXED);
writeFileSync(target, patched, 'utf8');
warn(`已修补 ${target}`);
