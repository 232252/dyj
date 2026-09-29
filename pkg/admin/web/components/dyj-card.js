// dyj-card：卡片容器。slot "title" / "action" 组成可选头部（都为空时隐藏），
// 默认 slot 为正文。首页指标栅格与表单页复用。
class DyjCard extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; background: var(--dyj-card);
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius);
    box-shadow: var(--dyj-shadow); }
  :host([hidden]) { display: none; }
  header { display: flex; align-items: center; justify-content: space-between;
    gap: 8px; padding: 12px 16px; border-bottom: 1px solid var(--dyj-border); }
  header[hidden] { display: none; }
  header ::slotted([slot=title]) { font-size: var(--dyj-fs-m); font-weight: 600;
    color: var(--dyj-text-dim); }
  .body { padding: 16px; }
</style>
<header hidden><slot name="title"></slot><slot name="action"></slot></header>
<div class="body"><slot></slot></div>`;
    // 只有 title/action 都为空才隐藏头部；正文 slot 不参与判断
    const head = this.shadowRoot.querySelector('header');
    const slots = this.shadowRoot.querySelectorAll('slot[name]');
    const check = () => {
      head.hidden = ![...slots].some((s) => s.assignedNodes({ flatten: true }).length);
    };
    slots.forEach((s) => s.addEventListener('slotchange', check));
  }

  connectedCallback() {
    // upgrade 之后 slotchange 不再补发，手动判一次头显隐
    const head = this.shadowRoot.querySelector('header');
    const slots = this.shadowRoot.querySelectorAll('slot[name]');
    head.hidden = ![...slots].some((s) => s.assignedNodes({ flatten: true }).length);
  }
}

customElements.define('dyj-card', DyjCard);
