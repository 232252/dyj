// 角色选择页（#/setup）：首启只给两个方向——共享本机打印机 / 添加网络
// 打印机。已配置的机器显示当前角色，可随时重新选择（research/13 §3.1）。
import { getConfig } from '../api.js';
import { t } from '../i18n/index.js';
import '../components/dyj-button.js';
import '../components/dyj-card.js';

class DyjPageSetup extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-setup .cards { display: grid; gap: 16px;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); max-width: 760px; }
  dyj-page-setup .card { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow);
    padding: 22px; display: flex; flex-direction: column; gap: 10px; }
  dyj-page-setup .card h2 { font-size: var(--dyj-fs-l); }
  dyj-page-setup .card p { color: var(--dyj-text-dim); flex: 1; }
  dyj-page-setup .cur { margin: 4px 0 14px; color: var(--dyj-text-dim); }
  dyj-page-setup .cur b { color: var(--dyj-text); }
</style>
<h1>${t('setup_title')}</h1>
<div class="cur" id="cur" hidden></div>
<div class="cards">
  <div class="card">
    <h2>${t('setup_share_title')}</h2>
    <p>${t('setup_share_body')}</p>
    <dyj-button id="goShare" variant="primary">${t('setup_share_btn')}</dyj-button>
  </div>
  <div class="card">
    <h2>${t('setup_add_title')}</h2>
    <p>${t('setup_add_body')}</p>
    <dyj-button id="goAdd" variant="primary">${t('setup_add_btn')}</dyj-button>
  </div>
</div>`;
    this.querySelector('#goShare').addEventListener('click', () => { location.hash = '#/share'; });
    this.querySelector('#goAdd').addEventListener('click', () => { location.hash = '#/add'; });
    getConfig().then(({ config }) => {
      const role = config?.role || '';
      if (!role) return;
      const key = role === 'share' ? 'role_share' : role === 'use' ? 'role_use' : 'role_both';
      const cur = this.querySelector('#cur');
      cur.innerHTML = `${t('setup_current')}：<b>${t(key)}</b>`;
      cur.hidden = false;
    }).catch(() => {});
  }
}

customElements.define('dyj-page-setup', DyjPageSetup);
