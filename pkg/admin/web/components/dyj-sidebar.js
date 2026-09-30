// dyj-sidebar：品牌区 + 全局状态徽章（监听 dyj-status 实时刷新）+ 导航。
// 链接点击改写 location.hash；dyj-route 事件驱动高亮。
import './dyj-icon.js';
import './dyj-status-badge.js';
import { t } from '../i18n/index.js';

const NAV = [
  ['status', 'activity', 'nav_status'],
  ['setup', 'compass', 'nav_setup'],
  ['add', 'wifi', 'nav_add'],
  ['printer', 'printer', 'nav_printer'],
  ['network', 'network', 'nav_network'],
  ['jobs', 'files', 'nav_jobs'],
  ['logs', 'file-text', 'nav_logs'],
];

export function statusBadgeState(s) {
  if (s.accepting) return 'ok';
  const reasons = s.reasons || [];
  if (!reasons.length || reasons.includes('paused')) return 'paused';
  return 'error';
}

class DyjSidebar extends HTMLElement {
  #onStatus; #onRoute;

  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; background: var(--dyj-card);
    border-right: 1px solid var(--dyj-border); }
  .wrap { position: sticky; top: 0; display: flex; flex-direction: column;
    gap: 14px; padding: 16px 12px; max-height: 100vh; overflow-y: auto; }
  .brand { display: flex; align-items: center; gap: 7px; padding: 0 8px; }
  .brand dyj-icon { width: 20px; height: 20px; color: var(--dyj-primary); flex: none; }
  .brand b { font-size: var(--dyj-fs-xl); }
  .brand span { font-size: var(--dyj-fs-s); color: var(--dyj-text-dim); }
  .badge { padding: 0 8px; }
  nav { display: flex; flex-direction: column; gap: 2px; }
  a { display: flex; align-items: center; gap: 10px; padding: 9px 10px;
    border-radius: var(--dyj-radius); color: var(--dyj-text);
    text-decoration: none; font-size: var(--dyj-fs-m); transition: background .15s; }
  a:hover { background: var(--dyj-bg); }
  a:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: -2px; }
  a.active { background: var(--dyj-primary-weak); color: var(--dyj-primary); font-weight: 600; }
  a dyj-icon { width: 16px; height: 16px; flex: none; }
  .foot { margin-top: auto; padding: 0 8px; font-size: var(--dyj-fs-s);
    color: var(--dyj-text-dim); }
</style>
<div class="wrap">
  <div class="brand"><dyj-icon name="dyj"></dyj-icon><b>dyj</b><span>${t('app_subtitle')}</span></div>
  <div class="badge"><dyj-status-badge id="badge" state="unknown"></dyj-status-badge></div>
  <nav>${NAV.map(([r, ic, k]) =>
    `<a href="#/${r}" data-route="${r}"><dyj-icon name="${ic}"></dyj-icon><span>${t(k)}</span></a>`).join('')}</nav>
  <div class="foot" id="ver"></div>
</div>`;
    this.shadowRoot.addEventListener('click', (e) => {
      const a = e.target.closest('a');
      if (!a) return;
      e.preventDefault();
      location.hash = a.getAttribute('href');
    });
    this.#onStatus = (ev) => {
      const s = ev.detail || {};
      this.shadowRoot.getElementById('badge').setAttribute('state', statusBadgeState(s));
      this.shadowRoot.getElementById('ver').textContent = s.version ? 'v' + s.version : '';
    };
    this.#onRoute = (ev) => {
      this.shadowRoot.querySelectorAll('a').forEach((a) => {
        a.classList.toggle('active', a.dataset.route === ev.detail);
      });
    };
    window.addEventListener('dyj-status', this.#onStatus);
    window.addEventListener('dyj-route', this.#onRoute);
  }

  disconnectedCallback() {
    window.removeEventListener('dyj-status', this.#onStatus);
    window.removeEventListener('dyj-route', this.#onRoute);
  }
}

customElements.define('dyj-sidebar', DyjSidebar);
