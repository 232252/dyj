// 打印机页：正在共享的打印机清单（多打印机模型，v0.3）。改配置走共享
// 向导（#/share），这里负责"看见现状"。
import { getConfig, errMessage } from '../api.js';
import { t } from '../i18n/index.js';
import '../components/dyj-button.js';

class DyjPagePrinter extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-printer .body { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow); padding: 24px; }
  dyj-page-printer table { width: 100%; border-collapse: collapse; }
  dyj-page-printer th, dyj-page-printer td { text-align: left; padding: 8px 10px;
    border-bottom: 1px solid var(--dyj-border); font-size: var(--dyj-fs-m); }
  dyj-page-printer th { color: var(--dyj-text-dim); font-weight: 500; }
  dyj-page-printer .empty { color: var(--dyj-text-dim); }
  dyj-page-printer .acts { margin-top: 16px; }
</style>
<h1>${t('page_printer_title')}</h1>
<div class="body">
  <h2>${t('printer_list_title')}</h2>
  <div id="list"></div>
  <div class="acts">
    <dyj-button id="rerun" variant="primary">${t('printer_rerun')}</dyj-button>
  </div>
</div>`;
    this.querySelector('#rerun').addEventListener('click', () => { location.hash = '#/share'; });
    getConfig().then(({ config }) => {
      const printers = config?.printers || [];
      const list = this.querySelector('#list');
      if (!printers.length) {
        list.innerHTML = `<div class="empty">${t('printer_list_empty')}</div>`;
        return;
      }
      const tbl = document.createElement('table');
      tbl.innerHTML = `
        <thead><tr>
          <th>${t('field_name')}</th><th>${t('col_queue')}</th>
          <th>${t('field_backend')}</th><th>${t('col_key')}</th>
        </tr></thead><tbody></tbody>`;
      const tb = tbl.querySelector('tbody');
      for (const p of printers) {
        const tr = document.createElement('tr');
        for (const v of [p.name, p.queue || '—', p.backend || t('backend_auto'), p.key || '—']) {
          const td = document.createElement('td');
          td.textContent = v;
          tr.append(td);
        }
        tb.append(tr);
      }
      list.replaceChildren(tbl);
    }).catch((e) => {
      this.querySelector('#list').innerHTML = `<div class="empty">${errMessage(e)}</div>`;
    });
  }
}

customElements.define('dyj-page-printer', DyjPagePrinter);
