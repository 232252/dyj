// dyj-disclosure：折叠面板。summary attribute 为标题，open attribute 反映
// 展开态；“高级设置”与“如何添加打印机”复用。
import './dyj-icon.js';

class DyjDisclosure extends HTMLElement {
  static observedAttributes = ['summary'];

  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; }
  button.head { display: flex; align-items: center; gap: 6px; width: 100%;
    font: inherit; font-size: var(--dyj-fs-m); font-weight: 600;
    color: var(--dyj-text); background: none; border: none;
    padding: 8px 0; cursor: pointer; text-align: left; }
  button.head:focus-visible { outline: 2px solid var(--dyj-primary);
    outline-offset: 2px; border-radius: 4px; }
  dyj-icon { width: 14px; height: 14px; flex: none; color: var(--dyj-text-dim);
    transition: transform .15s; }
  :host([open]) dyj-icon { transform: rotate(180deg); }
  .body { padding: 4px 0 8px; }
  .body[hidden] { display: none; }
</style>
<button type="button" class="head" aria-expanded="false">
  <dyj-icon name="chevron"></dyj-icon><span></span>
</button>
<div class="body" hidden><slot></slot></div>`;
    const btn = this.shadowRoot.querySelector('button');
    const body = this.shadowRoot.querySelector('.body');
    btn.addEventListener('click', () => {
      const open = !this.hasAttribute('open');
      this.toggleAttribute('open', open);
      btn.setAttribute('aria-expanded', String(open));
      body.hidden = !open;
    });
  }

  attributeChangedCallback(_n, _o, val) {
    const span = this.shadowRoot?.querySelector('span');
    if (span) span.textContent = val || '';
  }

  connectedCallback() {
    const open = this.hasAttribute('open');
    const btn = this.shadowRoot.querySelector('button');
    btn.setAttribute('aria-expanded', String(open));
    this.shadowRoot.querySelector('.body').hidden = !open;
    const span = this.shadowRoot.querySelector('span');
    span.textContent = this.getAttribute('summary') || '';
  }
}

customElements.define('dyj-disclosure', DyjDisclosure);
