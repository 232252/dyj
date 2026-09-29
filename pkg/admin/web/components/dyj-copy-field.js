// dyj-copy-field：只读值 + 复制按钮 + “已复制”反馈。导出 copyText 供其他
// 组件复用（剪贴板 API 不可用时回退 execCommand，覆盖局域网 http 场景）。
import './dyj-icon.js';
import { t } from '../i18n/index.js';

export async function copyText(str) {
  try {
    await navigator.clipboard.writeText(str);
    return true;
  } catch {
    const ta = document.createElement('textarea');
    ta.value = str;
    ta.style.cssText = 'position:fixed;opacity:0';
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch { /* unsupported */ }
    ta.remove();
    return ok;
  }
}

class DyjCopyField extends HTMLElement {
  static observedAttributes = ['value'];

  #btn; #code; #icon; #label; #timer;

  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; }
  .row { display: flex; align-items: center; gap: 8px; }
  code { flex: 1; min-width: 0; font-family: var(--dyj-mono); font-size: var(--dyj-fs-m);
    background: var(--dyj-bg); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); padding: 8px 10px;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  button { display: inline-flex; align-items: center; gap: 5px; font: inherit;
    font-size: var(--dyj-fs-s); padding: 8px 10px; flex: none;
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius);
    background: var(--dyj-card); color: var(--dyj-text); cursor: pointer;
    transition: border-color .15s; }
  button:hover { border-color: var(--dyj-primary); }
  button:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 2px; }
  button.ok { color: var(--dyj-success); border-color: var(--dyj-success); }
  dyj-icon { width: 13px; height: 13px; }
</style>
<div class="row"><code></code><button type="button"><dyj-icon name="copy"></dyj-icon><span></span></button></div>`;
    this.#code = this.shadowRoot.querySelector('code');
    this.#btn = this.shadowRoot.querySelector('button');
    this.#icon = this.shadowRoot.querySelector('dyj-icon');
    this.#label = this.shadowRoot.querySelector('span');
    this.#label.textContent = t('btn_copy');
    this.#btn.addEventListener('click', async () => {
      clearTimeout(this.#timer);
      const ok = await copyText(this.getAttribute('value') || '');
      this.#icon.setAttribute('name', ok ? 'check' : 'error');
      this.#label.textContent = t(ok ? 'btn_copied' : 'copy_failed');
      this.#btn.classList.toggle('ok', ok);
      this.#timer = setTimeout(() => {
        this.#icon.setAttribute('name', 'copy');
        this.#label.textContent = t('btn_copy');
        this.#btn.classList.remove('ok');
      }, 1600);
    });
  }

  attributeChangedCallback() {
    const v = this.getAttribute('value') || '';
    if (this.#code) { this.#code.textContent = v; this.#code.title = v; }
  }
}

customElements.define('dyj-copy-field', DyjCopyField);
