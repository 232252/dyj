// 首次设置向导：欢迎 → 打印机设置（复用打印机表单字段）→ 诊断 → 完成
// （三平台添加指引 + URI 复制）。完成后写 dyj_seen_wizard 并回首页。
import { getConfig, saveConfig, getStatus, getDiagnostics, errMessage } from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import '../components/dyj-stepper.js';
import '../components/dyj-diagnostic.js';
import '../components/dyj-copy-field.js';
import '../components/dyj-textfield.js';
import '../components/dyj-select.js';
import '../components/dyj-button.js';
import '../components/dyj-card.js';

const BACKENDS = [
  { value: '', label: 'backend_auto', desc: 'backend_auto_desc' },
  { value: 'wingdi', label: 'backend_wingdi', desc: 'backend_wingdi_desc' },
  { value: 'cups', label: 'backend_cups', desc: 'backend_cups_desc' },
  { value: 'file', label: 'backend_file', desc: 'backend_file_desc' },
  { value: 'null', label: 'backend_null', desc: 'backend_null_desc' },
];

class DyjPageWizard extends HTMLElement {
  #cur = 0;
  #cfg = null;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-wizard .body { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow); padding: 24px; }
  dyj-page-wizard .form { display: flex; flex-direction: column; gap: 14px; max-width: 480px; }
  dyj-page-wizard .nav { display: flex; justify-content: space-between; margin-top: 16px; }
  dyj-page-wizard .uris { display: flex; flex-direction: column; gap: 8px; margin: 12px 0 16px; max-width: 640px; }
  dyj-page-wizard .guide p { color: var(--dyj-text-dim); max-width: 46em; }
  dyj-page-wizard pre.cmd { background: var(--dyj-bg); border: 1px solid var(--dyj-border);
    border-radius: 6px; padding: 8px 12px; overflow-x: auto; font-size: var(--dyj-fs-s); }
  dyj-page-wizard .diag-list { margin-top: 12px; }
</style>
<h1>${t('page_wizard_title')}</h1>
<dyj-stepper id="stepper"></dyj-stepper>
<div class="body" id="body"></div>
<div class="nav">
  <dyj-button id="prev" variant="secondary">${t('btn_prev')}</dyj-button>
  <dyj-button id="next" variant="primary"></dyj-button>
</div>`;
    const steps = [t('wiz_step_welcome'), t('wiz_step_printer'), t('wiz_step_diag'), t('wiz_step_done')];
    const st = this.querySelector('#stepper');
    st.steps = steps;
    st.addEventListener('dyj-goto', (ev) => this.#show(ev.detail.index));
    this.querySelector('#prev').addEventListener('click', () => this.#show(Math.max(0, this.#cur - 1)));
    this.querySelector('#next').addEventListener('click', () => this.#advance());
    this.#show(0);
    getConfig().then(({ config }) => { this.#cfg = config || {}; }).catch(() => { this.#cfg = {}; });
  }

  #show(i) {
    this.#cur = i;
    this.querySelector('#stepper').step = i;
    this.querySelector('#prev').hidden = i === 0;
    const next = this.querySelector('#next');
    next.textContent = i === 1 ? t('wiz_printer_save') : i === 3 ? t('btn_finish') : t('btn_next');
    const body = this.querySelector('#body');
    if (i === 0) this.#renderWelcome(body);
    else if (i === 1) this.#renderPrinter(body);
    else if (i === 2) this.#renderDiag(body);
    else this.#renderDone(body);
  }

  async #advance() {
    if (this.#cur === 1 && !(await this.#savePrinter())) return;
    if (this.#cur === 3) {
      this.#finish();
      return;
    }
    this.#show(this.#cur + 1);
  }

  #renderWelcome(el) {
    el.innerHTML = `
      <h2>${t('wiz_welcome_title')}</h2>
      <p>${t('wiz_welcome_body')}</p>
      <p class="dim">${t('wiz_welcome_body2')}</p>`;
  }

  #renderPrinter(el) {
    el.innerHTML = `
      <h2>${t('wiz_printer_title')}</h2>
      <div class="form">
        <dyj-textfield id="w-name" label="${t('field_name')}"></dyj-textfield>
        <p class="dim">${t('field_name_desc')}</p>
        <dyj-textfield id="w-printer" label="${t('field_queue')}"></dyj-textfield>
        <p class="dim">${t('field_queue_desc')}</p>
        <dyj-select id="w-backend" label="${t('field_backend')}"></dyj-select>
      </div>`;
    const sel = el.querySelector('#w-backend');
    sel.options = BACKENDS.map((b) => ({ value: b.value, label: t(b.label), desc: t(b.desc) }));
    const cfg = this.#cfg || {};
    el.querySelector('#w-name').value = cfg.name || '';
    el.querySelector('#w-printer').value = cfg.printer || '';
    sel.value = cfg.backend || '';
  }

  async #savePrinter() {
    const el = this.querySelector('#body');
    const name = el.querySelector('#w-name');
    const printer = el.querySelector('#w-printer');
    name.error = name.value.trim() ? '' : t('err_required');
    printer.error = printer.value.trim() ? '' : t('err_required');
    if (name.error || printer.error) return false;
    const body = {
      ...(this.#cfg || {}),
      name: name.value.trim(),
      printer: printer.value.trim(),
      backend: el.querySelector('#w-backend').value,
    };
    try {
      await saveConfig(body);
      this.#cfg = body;
      showToast(t('toast_saved'));
      return true;
    } catch (e) {
      showToast(errMessage(e), 'error');
      return false;
    }
  }

  #renderDiag(el) {
    el.innerHTML = `
      <h2>${t('wiz_diag_title')}</h2>
      <p class="dim">${t('wiz_diag_hint')}</p>
      <div class="diag-list" id="diag-list">${t('wiz_diag_running')}</div>
      <p id="diag-ok" hidden>${t('wiz_diag_ok')}</p>`;
    const run = async () => {
      const list = el.querySelector('#diag-list');
      try {
        const { checks } = await getDiagnostics();
        list.replaceChildren(...checks.map((c) => {
          const d = document.createElement('dyj-diagnostic');
          d.check = c;
          return d;
        }));
        el.querySelector('#diag-ok').hidden = !checks.every((c) => c.level === 'ok');
      } catch (e) {
        list.textContent = errMessage(e);
      }
    };
    run();
    const re = document.createElement('dyj-button');
    re.setAttribute('variant', 'secondary');
    re.textContent = t('btn_recheck');
    re.addEventListener('click', run);
    el.appendChild(re);
  }

  async #renderDone(el) {
    el.innerHTML = `
      <h2>${t('wiz_done_title')}</h2>
      <p>${t('wiz_done_body')}</p>
      <div class="uris" id="wz-uris"></div>
      <div class="guide">
        <h3>Windows</h3><p>${t('guide_windows')}</p>
        <h3>macOS</h3><p>${t('guide_macos')}</p>
        <h3>Linux</h3><p>${t('guide_linux')}</p>
        <pre class="cmd"><code>${t('guide_linux_cmd')}</code></pre>
      </div>`;
    try {
      const s = await getStatus();
      el.querySelector('#wz-uris').replaceChildren(...(s.uris || []).map((u) => {
        const cf = document.createElement('dyj-copy-field');
        cf.setAttribute('value', u);
        return cf;
      }));
    } catch { /* 地址列表拿不到时不阻塞完成页 */ }
  }

  #finish() {
    localStorage.setItem('dyj_seen_wizard', '1');
    location.hash = '#/status';
  }
}

customElements.define('dyj-page-wizard', DyjPageWizard);
