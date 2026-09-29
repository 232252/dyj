// 打印机设置页：共享名称 / 队列名 / 后端下拉；保存走 PUT config（整体合并
// 提交），成功后 toast + 提示重启生效，并补 dyj_seen_wizard 标记。
import { getConfig, saveConfig, errMessage } from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import '../components/dyj-card.js';
import '../components/dyj-textfield.js';
import '../components/dyj-select.js';
import '../components/dyj-button.js';

const BACKENDS = [
  { value: '', label: 'backend_auto', desc: 'backend_auto_desc' },
  { value: 'wingdi', label: 'backend_wingdi', desc: 'backend_wingdi_desc' },
  { value: 'cups', label: 'backend_cups', desc: 'backend_cups_desc' },
  { value: 'file', label: 'backend_file', desc: 'backend_file_desc' },
  { value: 'null', label: 'backend_null', desc: 'backend_null_desc' },
];

class DyjPagePrinter extends HTMLElement {
  #cfg = null;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-printer .form { display: flex; flex-direction: column; gap: 16px; max-width: 520px; }
  dyj-page-printer .save-row { display: flex; align-items: center; gap: 12px; }
  dyj-page-printer dyj-button { align-self: flex-start; }
</style>
<h1>${t('page_printer_title')}</h1>
<dyj-card>
  <div class="form">
    <dyj-textfield id="name" label="${t('field_name')}"></dyj-textfield>
    <p class="dim">${t('field_name_desc')}</p>
    <dyj-textfield id="printer" label="${t('field_queue')}"></dyj-textfield>
    <p class="dim">${t('field_queue_desc')}</p>
    <dyj-select id="backend" label="${t('field_backend')}"></dyj-select>
    <div class="save-row">
      <dyj-button id="save" variant="primary">${t('btn_save')}</dyj-button>
      <span class="dim">${t('save_restart_hint')}</span>
    </div>
  </div>
</dyj-card>`;
    this.querySelector('#backend').options = BACKENDS.map((b) => ({
      value: b.value, label: t(b.label), desc: t(b.desc),
    }));
    this.querySelector('#save').addEventListener('click', () => this.#save());
    this.#load();
  }

  async #load() {
    try {
      const { config } = await getConfig();
      this.#cfg = config || {};
      this.querySelector('#name').value = this.#cfg.name || '';
      this.querySelector('#printer').value = this.#cfg.printer || '';
      this.querySelector('#backend').value = this.#cfg.backend || '';
    } catch (e) {
      showToast(errMessage(e), 'error');
    }
  }

  async #save() {
    const name = this.querySelector('#name');
    const printer = this.querySelector('#printer');
    name.error = name.value.trim() ? '' : t('err_required');
    printer.error = printer.value.trim() ? '' : t('err_required');
    if (name.error || printer.error) return;
    const btn = this.querySelector('#save');
    btn.setAttribute('loading', '');
    const body = {
      ...this.#cfg,
      name: name.value.trim(),
      printer: printer.value.trim(),
      backend: this.querySelector('#backend').value,
    };
    try {
      await saveConfig(body);
      this.#cfg = body;
      localStorage.setItem('dyj_seen_wizard', '1');
      showToast(t('toast_saved'));
    } catch (e) {
      showToast(errMessage(e), 'error');
    } finally {
      btn.removeAttribute('loading');
    }
  }
}

customElements.define('dyj-page-printer', DyjPagePrinter);
