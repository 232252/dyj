// dyj-toggle：开关，role="switch"。点击 / Enter / Space 切换；乐观更新并
// 发 dyj-change {checked}，调用方失败时把 .checked 设回即可回滚。
class DyjToggle extends HTMLElement {
  static observedAttributes = ['checked', 'disabled', 'label'];

  constructor() {
    super();
    this.tabIndex = 0;
    this.setAttribute('role', 'switch');
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: inline-flex; align-items: center; gap: 10px;
    cursor: pointer; user-select: none; font-size: var(--dyj-fs-m); }
  :host(:focus-visible) { outline: 2px solid var(--dyj-primary);
    outline-offset: 2px; border-radius: 12px; }
  :host([disabled]) { opacity: .55; cursor: not-allowed; }
  .track { width: 38px; height: 22px; border-radius: 11px; flex: none;
    background: var(--dyj-border); position: relative; transition: background .15s; }
  :host([checked]) .track { background: var(--dyj-success); }
  .knob { position: absolute; top: 3px; left: 3px; width: 16px; height: 16px;
    border-radius: 50%; background: #fff;
    box-shadow: 0 1px 2px rgb(0 0 0 / 30%); transition: left .15s; }
  :host([checked]) .knob { left: 19px; }
</style>
<span class="track"><span class="knob"></span></span>
<slot></slot>`;
    this.addEventListener('click', () => this.#toggle());
    this.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); this.#toggle(); }
    });
  }

  attributeChangedCallback() { this.#sync(); }

  #sync() {
    this.setAttribute('aria-checked', String(this.hasAttribute('checked')));
    const lb = this.getAttribute('label');
    if (lb) this.setAttribute('aria-label', lb);
  }

  get checked() { return this.hasAttribute('checked'); }
  set checked(v) { this.toggleAttribute('checked', !!v); }

  #toggle() {
    if (this.hasAttribute('disabled')) return;
    this.checked = !this.checked;
    this.dispatchEvent(new CustomEvent('dyj-change', { detail: { checked: this.checked }, bubbles: true }));
  }
}

customElements.define('dyj-toggle', DyjToggle);
