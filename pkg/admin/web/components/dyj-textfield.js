// dyj-textfield：带 label 的文本/数字/密码输入框，error attribute 渲染内联
// 校验文案（红色 + aria-invalid）。发 dyj-input / dyj-change。
class DyjTextfield extends HTMLElement {
  static observedAttributes = ['label', 'type', 'value', 'placeholder', 'error', 'disabled', 'min', 'max'];

  #input; #err; #label;

  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; }
  label { display: block; font-size: var(--dyj-fs-m); font-weight: 600; margin-bottom: 4px; }
  input { width: 100%; font: inherit; color: var(--dyj-text);
    background: var(--dyj-card); padding: 9px 10px;
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius);
    transition: border-color .15s; }
  input:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 1px; }
  :host([error]) input { border-color: var(--dyj-danger); }
  .err { margin: 6px 0 0; font-size: var(--dyj-fs-s); color: var(--dyj-danger); }
  :host(:not([error])) .err { display: none; }
</style>
<label></label>
<input aria-describedby="dyj-err">
<p class="err" id="dyj-err" role="alert"></p>`;
    this.#label = this.shadowRoot.querySelector('label');
    this.#input = this.shadowRoot.querySelector('input');
    this.#err = this.shadowRoot.querySelector('.err');
    this.#input.addEventListener('input', () => {
      if (this.getAttribute('value') !== this.#input.value) this.setAttribute('value', this.#input.value);
      this.dispatchEvent(new CustomEvent('dyj-input', { bubbles: true }));
    });
    this.#input.addEventListener('change', () => {
      this.dispatchEvent(new CustomEvent('dyj-change', { bubbles: true }));
    });
  }

  attributeChangedCallback(name, _old, val) {
    const i = this.#input;
    switch (name) {
      case 'label': this.#label.textContent = val || ''; break;
      case 'type': i.type = val || 'text'; break;
      case 'value': if (i.value !== (val ?? '')) i.value = val ?? ''; break;
      case 'placeholder': i.placeholder = val || ''; break;
      case 'error': this.#err.textContent = val || ''; i.setAttribute('aria-invalid', val ? 'true' : 'false'); break;
      case 'disabled': i.disabled = this.hasAttribute('disabled'); break;
      case 'min': val == null ? i.removeAttribute('min') : i.min = val; break;
      case 'max': val == null ? i.removeAttribute('max') : i.max = val; break;
    }
  }

  get value() { return this.#input.value; }
  set value(v) { this.setAttribute('value', v ?? ''); }

  get error() { return this.getAttribute('error') || ''; }
  set error(v) { v ? this.setAttribute('error', v) : this.removeAttribute('error'); }
}

customElements.define('dyj-textfield', DyjTextfield);
