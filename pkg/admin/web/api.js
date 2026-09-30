// fetch 封装：/admin/api/* 的类型化客户端。
// - token 注入：localStorage("dyj_admin_token") → X-Dyj-Token 头
// - 错误归一化：throw ApiError({status, message})，401 时广播 dyj-unauthorized
// - SSE 帮手：EventSource 无法带自定义头，故用 fetch 流式解析（3s 自动重连）
import { t } from './i18n/index.js';

const TOKEN_KEY = 'dyj_admin_token';

export const getToken = () => localStorage.getItem(TOKEN_KEY) || '';

export function setToken(tok) {
  if (tok) localStorage.setItem(TOKEN_KEY, tok);
  else localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

async function request(path, { method = 'GET', body } = {}) {
  const headers = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const tok = getToken();
  if (tok) headers['X-Dyj-Token'] = tok;
  let res;
  try {
    res = await fetch('/admin/api/' + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(t('err_network'), 0);
  }
  let data = null;
  try { data = await res.json(); } catch { /* non-JSON body */ }
  if (!res.ok) {
    if (res.status === 401) window.dispatchEvent(new CustomEvent('dyj-unauthorized'));
    const err = new ApiError(data?.error || 'http_' + res.status, res.status);
    err.data = data; // structured body (e.g. retry_after_sec) for callers
    throw err;
  }
  return data;
}

// errMessage: human text for a thrown ApiError (server 400s are already human).
export function errMessage(e) {
  if (!e || e.status === undefined) return t('err_generic');
  if (e.status === 0) return t('err_network');
  if (e.status === 401) return t('err_auth');
  if (/^http_/.test(e.message || '')) return `${t('err_generic')} (HTTP ${e.status})`;
  return e.message || t('err_generic');
}

export const getStatus = () => request('status');
// getJobs(which, limit)：limit 只取最近 N 条（服务端"最近打印"块，最新在前）。
export const getJobs = (which = 'all', limit = 0) =>
  request('jobs?which=' + encodeURIComponent(which) + (limit > 0 ? '&limit=' + limit : ''));
export const cancelJob = (id) => request(`jobs/${id}/cancel`, { method: 'POST' });
export const pausePrinter = () => request('pause', { method: 'POST' });
export const resumePrinter = () => request('resume', { method: 'POST' });
export const getConfig = () => request('config');
export const saveConfig = (config) => request('config', { method: 'PUT', body: config });
export const getDiagnostics = () => request('diagnostics');

// ---- v0.3: role wizard / pairing / receiver APIs ----
export const getLocalPrinters = () => request('local-printers');
export const scanServices = () => request('scan');
export const pairWith = (body) => request('pair', { method: 'POST', body });
export const installQueue = (body) => request('queue/install', { method: 'POST', body });
export const removeQueue = (body) => request('queue/remove', { method: 'POST', body });
export const testQueue = (body) => request('queue/test', { method: 'POST', body });
export const getAddedPrinters = () => request('added-printers');
export const getPairing = () => request('pairing');
export const rotatePairing = () => request('pairing/rotate', { method: 'POST' });
export const getDevices = () => request('devices');
export const revokeDevice = (id) => request('devices/' + encodeURIComponent(id), { method: 'DELETE' });

// exportDiag：诊断包下载（research/14 §6.3）。zip 是二进制，走 blob 而非
// request() 的 JSON 通道；令牌头照常注入，文件名取 Content-Disposition。
export async function exportDiag() {
  const headers = {};
  const tok = getToken();
  if (tok) headers['X-Dyj-Token'] = tok;
  const res = await fetch('/admin/api/diag/export', { headers });
  if (!res.ok) {
    if (res.status === 401) window.dispatchEvent(new CustomEvent('dyj-unauthorized'));
    throw new ApiError('http_' + res.status, res.status);
  }
  const blob = await res.blob();
  const m = (res.headers.get('Content-Disposition') || '').match(/filename=(dyj-diag-[\d-]+\.zip)/);
  const name = m ? m[1] : 'dyj-diag.zip';
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 3000);
  return name;
}

// openLogStream: SSE over fetch so the token header rides along.
// Calls onOpen() when live, onLine(line) per data line, onClose() when the
// connection drops (then retries every 3s). Returns a stop() function.
export function openLogStream({ onOpen, onLine, onClose }) {
  let stopped = false;
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  (async () => {
    while (!stopped) {
      try {
        const headers = {};
        const tok = getToken();
        if (tok) headers['X-Dyj-Token'] = tok;
        const res = await fetch('/admin/api/logs', { headers });
        if (!res.ok) {
          if (res.status === 401) window.dispatchEvent(new CustomEvent('dyj-unauthorized'));
          throw new Error('http_' + res.status);
        }
        onOpen?.();
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          let i;
          while ((i = buf.indexOf('\n\n')) >= 0) {
            for (const line of buf.slice(0, i).split('\n')) {
              if (line.startsWith('data:')) onLine?.(line.slice(5).replace(/^ /, ''));
            }
            buf = buf.slice(i + 2);
          }
        }
      } catch { /* fall through to retry */ }
      if (stopped) break;
      onClose?.();
      await sleep(3000);
    }
  })();
  return () => { stopped = true; };
}
