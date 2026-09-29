// dyj-stepper：向导步骤条。steps=[标签...]、step=当前序号；已完成步骤可点
// 击回退，发 dyj-goto {index}。
import './dyj-icon.js';

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

class DyjStepper extends HTMLElement {
  #steps = [];
  #cur = 0;

  set steps(v) { this.#steps = Array.isArray(v) ? v : []; this.#render(); }
  get steps() { return this.#steps.slice(); }
  set step(i) { this.#cur = Math.max(0, i | 0); this.#render(); }
  get step() { return this.#cur; }

  #render() {
    this.shadowRoot.innerHTML = `
<style>
  :host { display: block; }
  ol { display: flex; list-style: none; margin: 0 0 20px; padding: 0; }
  li { flex: 1; display: flex; align-items: center; min-width: 0; }
  li + li { padding-left: 10px; }
  li + li::before { content: ''; height: 2px; background: var(--dyj-border);
    flex: 1; margin-right: 10px; order: -1; }
  li.done + li::before { background: var(--dyj-primary); }
  button.step { display: flex; align-items: center; gap: 8px; font: inherit;
    font-size: var(--dyj-fs-s); color: var(--dyj-text-dim); background: none;
    border: none; padding: 4px; cursor: default; min-width: 0; border-radius: 6px; }
  button.step.done { cursor: pointer; color: var(--dyj-text); }
  button.step.done:hover .lbl { color: var(--dyj-primary); }
  button.step:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 2px; }
  .dot { width: 26px; height: 26px; border-radius: 50%; flex: none;
    display: flex; align-items: center; justify-content: center;
    font-size: var(--dyj-fs-s); font-weight: 600;
    border: 2px solid var(--dyj-border); color: var(--dyj-text-dim);
    background: var(--dyj-card); }
  .cur .dot { border-color: var(--dyj-primary); color: var(--dyj-primary); }
  .done .dot { background: var(--dyj-primary); border-color: var(--dyj-primary); color: #fff; }
  .done .dot dyj-icon { width: 13px; height: 13px; }
  .lbl { overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    transition: color .15s; }
</style>
<ol></ol>`;
    const ol = this.shadowRoot.querySelector('ol');
    ol.replaceChildren(...this.#steps.map((label, i) => {
      const li = document.createElement('li');
      const cls = i < this.#cur ? 'done' : i === this.#cur ? 'cur' : '';
      if (cls) li.className = cls;
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'step ' + cls;
      btn.disabled = i >= this.#cur;
      if (i === this.#cur) btn.setAttribute('aria-current', 'step');
      const dot = document.createElement('span');
      dot.className = 'dot';
      dot.innerHTML = i < this.#cur ? '<dyj-icon name="check"></dyj-icon>' : String(i + 1);
      const lbl = document.createElement('span');
      lbl.className = 'lbl';
      lbl.textContent = label;
      btn.append(dot, lbl);
      if (i < this.#cur) {
        btn.addEventListener('click', () => {
          this.dispatchEvent(new CustomEvent('dyj-goto', { detail: { index: i }, bubbles: true }));
        });
      }
      li.appendChild(btn);
      return li;
    }));
  }
}

customElements.define('dyj-stepper', DyjStepper);
