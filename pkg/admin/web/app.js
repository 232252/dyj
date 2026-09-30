// app.js：v0.4 极简路由（research/14 §3.1）——#/ #/server #/client #/help
// 四条路由、无侧栏。旧 v0.3 hash 就地重定向保书签；首屏按服务端 role 落地
// （share/both→server、use→client、""→两卡片首页）。5s 状态轮询（页面隐藏
// 时暂停）+ 401 时弹出轻量令牌输入条。
import { getStatus, getConfig, setToken } from './api.js';
import { t, locale } from './i18n/index.js';
import './components/dyj-topbar.js';
import './pages/page-home.js';
import './pages/page-server.js';
import './pages/page-client.js';
import './pages/page-help.js';

const ROUTES = {
  '': 'dyj-page-home',
  server: 'dyj-page-server',
  client: 'dyj-page-client',
  help: 'dyj-page-help',
};

// v0.3 书签 → v0.4 路由（调研 14 §7.4）。
const REDIRECT = {
  status: '',
  setup: '',
  share: 'server',
  add: 'client',
  printer: 'server',
  network: 'help',
  jobs: 'server',
  logs: 'help',
};

document.documentElement.lang = locale === 'en' ? 'en' : 'zh-CN';
document.title = t('app_title');

function currentRoute() {
  let r = location.hash.replace(/^#\/?/, '');
  if (r in REDIRECT) {
    r = REDIRECT[r];
    history.replaceState(null, '', '#' + r);
  }
  return r in ROUTES ? r : '';
}

function render() {
  const r = currentRoute();
  document.getElementById('page').replaceChildren(document.createElement(ROUTES[r]));
  window.dispatchEvent(new CustomEvent('dyj-route', { detail: r }));
}

// ---- status polling: one fetch feeds topbar badge + pages ----
let pollTimer = 0;

async function poll() {
  try {
    window.dispatchEvent(new CustomEvent('dyj-status', { detail: await getStatus() }));
  } catch { /* 掉线时徽章保持原样，令牌条由 dyj-unauthorized 事件唤起 */ }
}

function startPolling() {
  if (pollTimer) return;
  poll();
  pollTimer = setInterval(() => { if (!document.hidden) poll(); }, 5000);
}

document.addEventListener('visibilitychange', () => {
  if (!document.hidden && pollTimer) poll();
});

// ---- lightweight token bar for LAN access (401) ----
function showTokenBar() {
  if (document.getElementById('dyj-token-bar')) return;
  const bar = document.createElement('div');
  bar.id = 'dyj-token-bar';
  bar.innerHTML = `
    <div class="tok-text"><strong>${t('token_title')}</strong><span>${t('token_body')}</span></div>
    <input type="password" placeholder="${t('token_placeholder')}" autocomplete="off" aria-label="${t('token_title')}">
    <button type="button" class="tok-btn">${t('token_save')}</button>
    <span class="tok-err" role="alert"></span>`;
  document.body.prepend(bar);
  const input = bar.querySelector('input');
  const err = bar.querySelector('.tok-err');
  const attempt = async () => {
    const tok = input.value.trim();
    if (!tok) return;
    setToken(tok);
    try {
      await getStatus();
      bar.remove();
      startPolling();
      render(); // 重挂当前页，让它带着令牌重新拉数据
    } catch {
      err.textContent = t('token_invalid');
      input.classList.add('bad');
      input.focus();
      input.select();
    }
  };
  bar.querySelector('button').addEventListener('click', attempt);
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') attempt(); });
  input.focus();
}

window.addEventListener('dyj-unauthorized', showTokenBar);

// ---- landing: 默认落地页按 role 决定（已配置的机器没有导航这件事） ----
async function landingRole() {
  if (currentRoute() !== '') return; // 用户已明确选择页面，不打扰
  try {
    const { config } = await getConfig();
    const role = config?.role || '';
    if (role === 'use') location.hash = '#/client';
    else if (role === 'share' || role === 'both') location.hash = '#/server';
  } catch { /* 401 / 掉线：留在两卡片首页 */ }
}

function boot() {
  if (!location.hash) history.replaceState(null, '', '#/');
  render();
  window.addEventListener('hashchange', render);
  getStatus()
    .then(() => startPolling())
    .catch((e) => {
      if (e?.status === 401) showTokenBar();
      startPolling(); // 循环里继续试，拿到权限后广播自然恢复
    });
  landingRole();
}

boot();
