// app.js：hash 路由（#/status #/printer #/network #/jobs #/logs #/wizard）
// + 侧栏联动（dyj-route 事件）+ 5s 状态轮询（页面隐藏时暂停）+ 401 时弹出
// 轻量令牌输入条 + 首次进入自动跳设置向导。
import { getStatus, getConfig, setToken } from './api.js';
import { t, locale } from './i18n/index.js';
import './components/dyj-sidebar.js';
import './pages/page-dashboard.js';
import './pages/page-printer.js';
import './pages/page-network.js';
import './pages/page-jobs.js';
import './pages/page-logs.js';
import './pages/page-wizard.js';

const ROUTES = {
  status: 'dyj-page-dashboard',
  printer: 'dyj-page-printer',
  network: 'dyj-page-network',
  jobs: 'dyj-page-jobs',
  logs: 'dyj-page-logs',
  wizard: 'dyj-page-wizard',
};

document.documentElement.lang = locale === 'en' ? 'en' : 'zh-CN';
document.title = t('app_title');

function currentRoute() {
  const r = location.hash.replace(/^#\/?/, '');
  return ROUTES[r] ? r : 'status';
}

function render() {
  const r = currentRoute();
  document.getElementById('page').replaceChildren(document.createElement(ROUTES[r]));
  window.dispatchEvent(new CustomEvent('dyj-route', { detail: r }));
}

// ---- status polling: one fetch feeds sidebar badge + dashboard ----
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

// ---- first-run wizard redirect ----
async function checkWizard() {
  try {
    const { config } = await getConfig();
    const noName = !config || !config.name;
    if ((noName || !localStorage.getItem('dyj_seen_wizard')) && currentRoute() !== 'wizard') {
      location.hash = '#/wizard';
    }
  } catch { /* 401 / 掉线：不跳 */ }
}

function boot() {
  if (!location.hash) history.replaceState(null, '', '#/status');
  render();
  window.addEventListener('hashchange', render);
  getStatus()
    .then(() => startPolling())
    .catch((e) => {
      if (e?.status === 401) showTokenBar();
      startPolling(); // 循环里继续试，拿到权限后广播自然恢复
    });
  checkWizard();
}

boot();
