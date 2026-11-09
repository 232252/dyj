// dyj-diagnostic：单条诊断。level=ok|warn|fail|info → 级别图标+彩色左边线+
// 级别词（不只靠颜色）；可选 fix 建议命令渲染为代码块并附一键复制；
// can_fix 的检查再附“自动修复”按钮，点击冒泡 dyj-repair 事件
// （composed，穿过 shadow 边界），busy 置位期间按钮禁用。
import './dyj-icon.js';
import { copyText } from './dyj-copy-field.js';
import { esc } from '../api.js';
import { t } from '../i18n/index.js';

const LVL = {
  ok:   ['check', 'diag_ok',   'ok'],
  warn: ['warn',  'diag_warn', 'warn'],
  fail: ['error', 'diag_fail', 'bad'],
  info: ['info',  'diag_info', 'dim'],
};

// tone → design-token suffix（--dyj-<token>）
const TONE_TOKEN = { ok: 'success', warn: 'warning', bad: 'danger', dim: 'text-dim' };
const TONE_LINE = { ok: 'success', warn: 'warning', bad: 'danger', dim: 'border' };

class DyjDiagnostic extends HTMLElement {
  #check = null;
  #busy = false;

  constructor() { super(); this.attachShadow({ mode: 'open' }); }

  set check(v) { this.#check = v || {}; this.#render(); }
  get check() { return this.#check; }

  set busy(v) {
    this.#busy = !!v;
    const btn = this.shadowRoot.querySelector('.fixbtn');
    if (btn) {
      btn.disabled = this.#busy;
      btn.textContent = this.#busy ? t('help_fix_running') : t('help_fix_btn');
    }
  }

  get busy() { return this.#busy; }

  #render() {
    const c = this.#check || {};
    const [icon, key, tone] = LVL[c.level] || LVL.info;
    this.shadowRoot.innerHTML = `
<style>
  :host { display: block; }
  .row { display: flex; gap: 10px; padding: 12px 14px; margin-bottom: 8px;
    background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-left: 3px solid var(--dyj-${TONE_LINE[tone]});
    border-radius: var(--dyj-radius); }
  dyj-icon { width: 17px; height: 17px; flex: none; margin-top: 2px;
    color: var(--dyj-${TONE_TOKEN[tone]}); }
  .txt { min-width: 0; flex: 1; }
  .l1 { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .name { font-family: var(--dyj-mono); font-size: var(--dyj-fs-s);
    background: var(--dyj-bg); border: 1px solid var(--dyj-border);
    border-radius: 4px; padding: 1px 6px; color: var(--dyj-text-dim); }
  .lv { font-size: var(--dyj-fs-s); font-weight: 600;
    color: var(--dyj-${TONE_TOKEN[tone]}); }
  .detail { margin: 4px 0 0; font-size: var(--dyj-fs-m); line-height: 1.6; }
  .fix { margin-top: 8px; display: flex; align-items: center; gap: 8px; }
  .fix pre { margin: 0; flex: 1; min-width: 0; overflow-x: auto;
    background: var(--dyj-bg); border: 1px solid var(--dyj-border);
    border-radius: 6px; padding: 6px 10px; font-size: var(--dyj-fs-s); }
  .fix code { font-family: var(--dyj-mono); }
  .cp { font: inherit; font-size: var(--dyj-fs-s); padding: 4px 10px; flex: none;
    border: 1px solid var(--dyj-border); border-radius: 6px;
    background: var(--dyj-card); color: var(--dyj-text); cursor: pointer; }
  .cp:hover { border-color: var(--dyj-primary); }
  .cp:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 1px; }
  .cp:disabled { opacity: .55; cursor: default; }
  .fixbtn { color: var(--dyj-primary); border-color: var(--dyj-primary); }
</style>
<div class="row">
  <dyj-icon name="${icon}"></dyj-icon>
  <div class="txt">
    <div class="l1"><code class="name">${esc(c.name || '')}</code><span class="lv">${t(key)}</span></div>
    <p class="detail">${esc(c.detail || '')}</p>
    ${c.fix || c.can_fix ? `<div class="fix">
      ${c.fix ? `<pre><code>${esc(c.fix)}</code></pre>` : ''}
      ${c.fix ? `<button type="button" class="cp">${t('btn_copy')}</button>` : ''}
      ${c.can_fix ? `<button type="button" class="cp fixbtn" ${this.#busy ? 'disabled' : ''}>${this.#busy ? t('help_fix_running') : t('help_fix_btn')}</button>` : ''}
    </div>` : ''}
  </div>
</div>`;
    const cp = this.shadowRoot.querySelector('.fix .cp:not(.fixbtn)');
    if (cp) {
      cp.addEventListener('click', async () => {
        await copyText(c.fix);
        cp.textContent = t('btn_copied');
        setTimeout(() => { cp.textContent = t('btn_copy'); }, 1600);
      });
    }
    const fbtn = this.shadowRoot.querySelector('.fixbtn');
    if (fbtn) {
      fbtn.addEventListener('click', () => {
        if (this.#busy) return;
        this.dispatchEvent(new CustomEvent('dyj-repair', {
          bubbles: true, composed: true, detail: { name: c.name },
        }));
      });
    }
  }
}

customElements.define('dyj-diagnostic', DyjDiagnostic);
