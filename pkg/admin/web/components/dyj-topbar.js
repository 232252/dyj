// dyj-topbar：极简顶栏（v0.4 取代 7 项侧栏，research/14 §3.1）。
// 品牌 + 全局状态徽章 + 帮助入口 [?]——技术信息只住在帮助页，导航不存在：
// 角色即主页，页面间跳转靠页脚链接。statusBadgeState 也从这里导出
// （app.js 与各页共用）。
import './dyj-icon.js';
import './dyj-status-badge.js';
import { t } from '../i18n/index.js';

export function statusBadgeState(s) {
  const reasons = s.reasons || [];
  if (reasons.includes('not-configured')) return 'unknown'; // 未配置 ≠ 故障
  if (s.accepting) return 'ok';
  if (!reasons.length || reasons.includes('paused')) return 'paused';
  return 'error';
}

class DyjTopbar extends HTMLElement {
  #onStatus;

  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; background: var(--dyj-card);
    border-bottom: 1px solid var(--dyj-border); }
  .bar { max-width: 980px; margin: 0 auto; display: flex; align-items: center;
    gap: 10px; padding: 12px 24px; }
  .brand { display: flex; align-items: baseline; gap: 7px; }
  .brand dyj-icon { width: 20px; height: 20px; color: var(--dyj-primary);
    align-self: center; flex: none; }
  .brand b { font-size: var(--dyj-fs-xl); }
  .brand span { font-size: var(--dyj-fs-s); color: var(--dyj-text-dim); }
  .badge { padding: 0 8px; }
  .grow { flex: 1; }
  a.help { display: flex; align-items: center; justify-content: center;
    width: 30px; height: 30px; border-radius: 50%; color: var(--dyj-text-dim);
    text-decoration: none; font-weight: 600; transition: background .15s; }
  a.help:hover { background: var(--dyj-bg); color: var(--dyj-primary); }
  a.help:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 2px; }
  a.help dyj-icon { width: 17px; height: 17px; }
</style>
<div class="bar">
  <div class="brand"><dyj-icon name="dyj"></dyj-icon><b>dyj</b><span>${t('app_subtitle')}</span></div>
  <div class="badge"><dyj-status-badge id="badge" state="unknown"></dyj-status-badge></div>
  <span class="grow"></span>
  <a class="help" href="#/help" title="${t('help_title')}" aria-label="${t('help_title')}"><dyj-icon name="info"></dyj-icon></a>
</div>`;
    this.shadowRoot.querySelector('a.help').addEventListener('click', (e) => {
      e.preventDefault();
      location.hash = '#/help';
    });
    this.#onStatus = (ev) => {
      const s = ev.detail || {};
      this.shadowRoot.getElementById('badge').setAttribute('state', statusBadgeState(s));
    };
    window.addEventListener('dyj-status', this.#onStatus);
  }

  disconnectedCallback() {
    window.removeEventListener('dyj-status', this.#onStatus);
  }
}

customElements.define('dyj-topbar', DyjTopbar);
