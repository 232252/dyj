// dyj-select：下拉。options=[{value,label,desc}]；选中项的描述以小字显示在
// 下方（名称+描述两行信息）；原生 select 保证键盘可达。发 dyj-change。
class DyjSelect extends HTMLElement {
  static observedAttributes = ['label', 'disabled'];

  #select; #desc; #label; #opts = [];

  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; }
  label { display: block; font-size: var(--dyj-fs-m); font-weight: 600; margin-bottom: 4px; }
  select { width: 100%; font: inherit; color: var(--dyj-text);
    padding: 9px 10px; border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); background: var(--dyj-card); cursor: pointer; }
  select:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 1px; }
  .desc { margin: 6px 0 0; font-size: var(--dyj-fs-s); color: var(--dyj-text-dim); min-height: 1.2em; }
</style>
<label hidden></label>
<select></select>
<p class="desc"></p>`;
    this.#label = this.shadowRoot.querySelector('label');
    this.#select = this.shadowRoot.querySelector('select');
    this.#desc = this.shadowRoot.querySelector('.desc');
    this.#select.addEventListener('change', () => {
      this.#updateDesc();
      this.dispatchEvent(new CustomEvent('dyj-change', { detail: { value: this.value }, bubbles: true }));
    });
  }

  attributeChangedCallback() {
    this.#label.textContent = this.getAttribute('label') || '';
    this.#label.hidden = !this.getAttribute('label');
    this.#select.disabled = this.hasAttribute('disabled');
  }

  get options() { return this.#opts.slice(); }
  set options(list) {
    this.#opts = Array.isArray(list) ? list.slice() : [];
    this.#select.replaceChildren(...this.#opts.map((o) => {
      const el = document.createElement('option');
      el.value = o.value;
      el.textContent = o.label;
      return el;
    }));
    this.#updateDesc();
  }

  get value() { return this.#select.value; }
  set value(v) { this.#select.value = v; this.#updateDesc(); }

  #updateDesc() {
    const cur = this.#opts.find((o) => o.value === this.#select.value);
    this.#desc.textContent = cur?.desc || '';
  }
}

customElements.define('dyj-select', DyjSelect);
