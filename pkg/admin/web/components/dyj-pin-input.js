// dyj-pin-input：4 位配对码输入（v0.4 research/14 §3.4）。四个单字格，
// 数字键盘（inputmode=numeric）、自动跳格、退格回退、整段粘贴分发、
// 输满第 4 位发 dyj-complete。.value 取当前码，clear() 清空并聚焦首位。
//
// t() 必须在本模块导入。构造函数里调用未定义的 t 会抛 ReferenceError，
// 自定义元素升级失败，格子渲染成空（与当年 dyj-toggle / dyj-diagnostic
// 构造函数抛错后整块空白是同一类故障，不是某个内嵌浏览器的特例）。
import { t } from '../i18n/index.js';

const DIGITS = 4;

class DyjPinInput extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: inline-flex; gap: 10px; }
  input {
    width: 3.2em; height: 3.4em; text-align: center;
    font: 700 var(--dyj-fs-xl)/1 var(--dyj-font); font-variant-numeric: tabular-nums;
    color: var(--dyj-text); background: var(--dyj-bg);
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius);
    outline: none; transition: border-color .15s;
  }
  input:focus { border-color: var(--dyj-primary); }
  input.bad { border-color: var(--dyj-danger); }
</style>`;
    this.#boxes = [];
    for (let i = 0; i < DIGITS; i++) {
      const b = document.createElement('input');
      b.type = 'text';
      b.inputMode = 'numeric';
      b.autocomplete = i === 0 ? 'one-time-code' : 'off';
      b.maxLength = 1;
      b.setAttribute('aria-label', t('client_pin_digit', { n: i + 1 }));
      b.addEventListener('input', () => this.#onInput(i, b));
      b.addEventListener('keydown', (e) => this.#onKey(i, b, e));
      b.addEventListener('paste', (e) => this.#onPaste(e));
      this.shadowRoot.append(b);
      this.#boxes.push(b);
    }
  }

  #boxes;

  connectedCallback() {
    // 属性初始化后再聚焦（modal 场景由调用方控制，这里兜底）。
    if (this.hasAttribute('autofocus')) this.focusFirst();
  }

  get value() {
    return this.#boxes.map((b) => b.value).join('');
  }

  set value(v) {
    const s = String(v || '').replace(/\D/g, '').slice(0, DIGITS);
    this.#boxes.forEach((b, i) => { b.value = s[i] || ''; });
    if (s.length === DIGITS) this.#emitComplete();
  }

  clear() {
    this.#boxes.forEach((b) => { b.value = ''; b.classList.remove('bad'); });
    this.focusFirst();
  }

  markBad() {
    this.#boxes.forEach((b) => b.classList.add('bad'));
  }

  focusFirst() {
    this.#boxes[0]?.focus();
  }

  #onInput(i, b) {
    b.value = b.value.replace(/\D/g, '').slice(-1);
    b.classList.remove('bad');
    if (b.value && i < DIGITS - 1) {
      this.#boxes[i + 1].focus();
    }
    if (this.value.length === DIGITS) this.#emitComplete();
  }

  #onKey(i, b, e) {
    if (e.key === 'Backspace' && !b.value && i > 0) {
      this.#boxes[i - 1].focus();
      this.#boxes[i - 1].value = '';
      e.preventDefault();
    }
  }

  #onPaste(e) {
    const s = (e.clipboardData?.getData('text') || '').replace(/\D/g, '').slice(0, DIGITS);
    if (!s) return;
    e.preventDefault();
    this.value = s;
    this.#boxes[Math.min(s.length, DIGITS - 1)].focus();
  }

  #emitComplete() {
    this.dispatchEvent(new CustomEvent('dyj-complete', { bubbles: true }));
  }
}

customElements.define('dyj-pin-input', DyjPinInput);
