// dyj-empty-state：空态（图标 + 文案 + 可选动作插槽 slot="action"）。
import './dyj-icon.js';

class DyjEmptyState extends HTMLElement {
  static observedAttributes = ['icon', 'text'];

  constructor() { super(); this.attachShadow({ mode: 'open' }); }

  attributeChangedCallback() { this.#render(); }
  connectedCallback() { this.#render(); }

  #render() {
    const icon = this.getAttribute('icon') || 'info';
    const text = this.getAttribute('text') || '';
    this.shadowRoot.innerHTML = `
<style>
  :host { display: block; text-align: center; padding: 40px 16px; }
  :host([hidden]) { display: none; }
  .wrap { display: flex; flex-direction: column; align-items: center; gap: 10px; }
  dyj-icon { width: 36px; height: 36px; color: var(--dyj-text-dim); }
  p { margin: 0; color: var(--dyj-text-dim); font-size: var(--dyj-fs-m); max-width: 42em; }
  .act { margin-top: 4px; }
  .act[hidden] { display: none; }
</style>
<div class="wrap">
  <dyj-icon name="${icon}"></dyj-icon>
  <p></p>
  <div class="act" hidden><slot name="action"></slot></div>
</div>`;
    this.shadowRoot.querySelector('p').textContent = text;
    const slot = this.shadowRoot.querySelector('slot');
    slot.addEventListener('slotchange', () => this.#syncAct());
    this.#syncAct();
  }

  #syncAct() {
    const slot = this.shadowRoot.querySelector('slot');
    const has = slot.assignedNodes({ flatten: true }).length > 0;
    this.shadowRoot.querySelector('.act').hidden = !has;
  }
}

customElements.define('dyj-empty-state', DyjEmptyState);
