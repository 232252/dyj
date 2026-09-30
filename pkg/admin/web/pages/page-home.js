// 首页（#/，未配置态）：整个产品只有两个方向——服务端 / 客户端
// （research/14 §3.2）。已配置的机器由 app.js 落地到对应主页，不会停在这。
import { t } from '../i18n/index.js';
import '../components/dyj-button.js';

class DyjPageHome extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-home .wrap { max-width: 760px; margin: 6vh auto 0; text-align: center; }
  dyj-page-home h1 { font-size: var(--dyj-fs-xl); }
  dyj-page-home .cards { display: grid; gap: 16px; margin-top: 20px;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); }
  dyj-page-home .card { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow);
    padding: 28px 22px; display: flex; flex-direction: column; gap: 10px; }
  dyj-page-home .card h2 { font-size: var(--dyj-fs-l); margin: 0; }
  dyj-page-home .card p { color: var(--dyj-text-dim); flex: 1; margin: 0; }
  dyj-page-home .foot { margin-top: 22px; color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
</style>
<div class="wrap">
  <h1>${t('home_title')}</h1>
  <div class="cards">
    <div class="card">
      <h2>${t('home_share_title')}</h2>
      <p>${t('home_share_body')}</p>
      <dyj-button id="goServer" variant="primary">${t('home_go')}</dyj-button>
    </div>
    <div class="card">
      <h2>${t('home_use_title')}</h2>
      <p>${t('home_use_body')}</p>
      <dyj-button id="goClient" variant="primary">${t('home_go')}</dyj-button>
    </div>
  </div>
  <div class="foot">${t('home_hint')}</div>
</div>`;
    this.querySelector('#goServer').addEventListener('click', () => { location.hash = '#/server'; });
    this.querySelector('#goClient').addEventListener('click', () => { location.hash = '#/client'; });
  }
}

customElements.define('dyj-page-home', DyjPageHome);
