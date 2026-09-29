// dyj-button：按钮。variant=primary|secondary|danger；loading 态禁点并显示
// 转圈；:focus-visible 焦点环；暴露 focus() 供模态定位焦点。
class DyjButton extends HTMLElement {
  static observedAttributes = ['variant', 'loading', 'disabled'];

  #btn;

  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: inline-block; }
  :host([hidden]) { display: none; }
  button {
    display: inline-flex; align-items: center; gap: 6px;
    font: inherit; font-size: var(--dyj-fs-m); line-height: 1.2;
    padding: 9px 14px; border-radius: var(--dyj-radius);
    border: 1px solid var(--dyj-border);
    background: var(--dyj-card); color: var(--dyj-text); cursor: pointer;
    transition: background .15s, border-color .15s, opacity .15s;
  }
  button:hover:not(:disabled) { border-color: var(--dyj-primary); }
  button:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 2px; }
  button:disabled { opacity: .55; cursor: not-allowed; }
  :host([variant=primary]) button { background: var(--dyj-primary); border-color: var(--dyj-primary); color: var(--dyj-primary-contrast); }
  :host([variant=danger]) button { background: var(--dyj-danger); border-color: var(--dyj-danger); color: #fff; }
  .spin { width: 13px; height: 13px; border: 2px solid currentColor;
    border-right-color: transparent; border-radius: 50%; flex: none;
    animation: dyj-spin .7s linear infinite; }
  @keyframes dyj-spin { to { transform: rotate(360deg); } }
</style>
<button type="button"><span class="spin" hidden></span><slot></slot></button>`;
    this.#btn = this.shadowRoot.querySelector('button');
  }

  attributeChangedCallback() {
    this.#btn.disabled = this.hasAttribute('disabled') || this.hasAttribute('loading');
    this.#btn.setAttribute('aria-busy', String(this.hasAttribute('loading')));
    this.shadowRoot.querySelector('.spin').hidden = !this.hasAttribute('loading');
  }

  focus() { this.#btn.focus(); }
}

customElements.define('dyj-button', DyjButton);
