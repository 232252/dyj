// dyj-modal：模态确认框。confirmDialog({title,body,danger,...}) 返回
// Promise<boolean>；Esc / 点遮罩 / 取消 → false，确定 → true。
// danger 时确认按钮为红色且默认焦点落在取消上。
import './dyj-button.js';
import { t } from '../i18n/index.js';

export function confirmDialog({ title, body = '', confirmText, cancelText, danger = false } = {}) {
  const m = document.createElement('dyj-modal');
  m.setAttribute('title', title || '');
  m.setAttribute('body', body);
  if (danger) m.setAttribute('danger', '');
  m.setAttribute('ok-text', confirmText || t('modal_confirm'));
  m.setAttribute('cancel-text', cancelText || t('modal_cancel'));
  document.body.appendChild(m);
  return new Promise((resolve) => {
    m.addEventListener('dyj-close', (e) => { m.remove(); resolve(e.detail === 'ok'); }, { once: true });
  });
}

class DyjModal extends HTMLElement {
  connectedCallback() {
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { position: fixed; inset: 0; z-index: 900; }
  .backdrop { position: absolute; inset: 0; background: rgb(0 0 0 / 40%);
    display: flex; align-items: center; justify-content: center; }
  .box { position: relative; background: var(--dyj-card); color: var(--dyj-text);
    border-radius: var(--dyj-radius); box-shadow: 0 8px 30px rgb(0 0 0 / 25%);
    width: min(92vw, 440px); padding: 20px; }
  h2 { margin: 0 0 8px; font-size: var(--dyj-fs-l); }
  p { margin: 0 0 16px; color: var(--dyj-text-dim); font-size: var(--dyj-fs-m); line-height: 1.6; }
  p[hidden] { display: none; }
  .acts { display: flex; justify-content: flex-end; gap: 8px; }
</style>
<div class="backdrop"><div class="box" role="dialog" aria-modal="true">
  <h2></h2><p hidden></p>
  <div class="acts">
    <dyj-button class="no"></dyj-button>
    <dyj-button class="yes"></dyj-button>
  </div>
</div></div>`;
    const root = this.shadowRoot;
    root.querySelector('h2').textContent = this.getAttribute('title') || '';
    root.querySelector('.box').setAttribute('aria-label', this.getAttribute('title') || '');
    const body = this.getAttribute('body') || '';
    const p = root.querySelector('p');
    p.textContent = body;
    p.hidden = !body;
    const no = root.querySelector('.no');
    const yes = root.querySelector('.yes');
    no.textContent = this.getAttribute('cancel-text') || t('modal_cancel');
    yes.textContent = this.getAttribute('ok-text') || t('modal_confirm');
    if (this.hasAttribute('danger')) yes.setAttribute('variant', 'danger');
    no.addEventListener('click', () => this.#close('cancel'));
    yes.addEventListener('click', () => this.#close('ok'));
    root.querySelector('.backdrop').addEventListener('click', (e) => {
      if (e.target === root.querySelector('.backdrop')) this.#close('cancel');
    });
    this.#esc = (e) => { if (e.key === 'Escape') this.#close('cancel'); };
    document.addEventListener('keydown', this.#esc);
    (this.hasAttribute('danger') ? no : yes).focus();
  }

  disconnectedCallback() {
    document.removeEventListener('keydown', this.#esc);
  }

  #esc = null;

  #close(detail) {
    document.removeEventListener('keydown', this.#esc);
    this.dispatchEvent(new CustomEvent('dyj-close', { detail }));
  }
}

customElements.define('dyj-modal', DyjModal);
