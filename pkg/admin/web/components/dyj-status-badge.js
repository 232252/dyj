// dyj-status-badge：共享状态徽章。state=ok|paused|error|unknown，图标+文字，
// 不只靠颜色（色盲安全）。size=large 用于首页主状态。
import './dyj-icon.js';
import { t } from '../i18n/index.js';

const MAP = {
  ok:      { icon: 'check', key: 'status_shared',  tone: 'ok' },
  paused:  { icon: 'pause', key: 'status_paused',  tone: 'dim' },
  error:   { icon: 'error', key: 'status_error',   tone: 'bad' },
  unknown: { icon: 'info',  key: 'status_unknown', tone: 'dim' },
};

class DyjStatusBadge extends HTMLElement {
  static observedAttributes = ['state', 'size'];

  constructor() { super(); this.attachShadow({ mode: 'open' }); }

  attributeChangedCallback() { this.#render(); }
  connectedCallback() { this.#render(); }

  #render() {
    const s = MAP[this.getAttribute('state')] || MAP.unknown;
    const large = this.hasAttribute('size') && this.getAttribute('size') === 'large';
    this.shadowRoot.innerHTML = `
<style>
  :host { display: inline-block; }
  .pill { display: inline-flex; align-items: center; gap: 8px;
    padding: ${large ? '8px 18px' : '4px 12px'}; border-radius: 999px;
    font-size: ${large ? 'var(--dyj-fs-l)' : 'var(--dyj-fs-m)'};
    font-weight: 600; border: 1px solid transparent; }
  dyj-icon { width: ${large ? '20px' : '14px'}; height: ${large ? '20px' : '14px'}; flex: none; }
  .ok   { color: var(--dyj-success); background: var(--dyj-success-weak); }
  .bad  { color: var(--dyj-danger); background: var(--dyj-danger-weak); }
  .dim  { color: var(--dyj-text-dim); background: var(--dyj-card); border-color: var(--dyj-border); }
</style>
<span class="pill ${s.tone}"><dyj-icon name="${s.icon}"></dyj-icon>${t(s.key)}</span>`;
  }
}

customElements.define('dyj-status-badge', DyjStatusBadge);
