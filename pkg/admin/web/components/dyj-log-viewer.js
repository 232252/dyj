// dyj-log-viewer：SSE 日志流视图。等宽字体、自动滚底、可“暂停跟随”
// （手动按钮或向上滚动自动暂停）；行数封顶防内存膨胀；断线显示重连状态。
import { openLogStream } from '../api.js';
import { t } from '../i18n/index.js';
import './dyj-icon.js';

const MAX_LINES = 2000;

class DyjLogViewer extends HTMLElement {
  #pre; #stateTxt; #stateIco; #followBtn; #follow = true; #stop = null;

  connectedCallback() {
    if (this.#stop) return;
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; background: var(--dyj-card);
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius); }
  .bar { display: flex; align-items: center; gap: 10px; padding: 8px 12px;
    border-bottom: 1px solid var(--dyj-border); font-size: var(--dyj-fs-s);
    color: var(--dyj-text-dim); }
  .grow { flex: 1; }
  .state { display: inline-flex; align-items: center; gap: 5px; }
  dyj-icon { width: 13px; height: 13px; }
  button { font: inherit; font-size: var(--dyj-fs-s); padding: 4px 10px;
    border: 1px solid var(--dyj-border); border-radius: 6px;
    background: var(--dyj-card); color: var(--dyj-text); cursor: pointer;
    transition: border-color .15s; }
  button:hover { border-color: var(--dyj-primary); }
  button:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: 1px; }
  pre { margin: 0; padding: 12px; height: 60vh; overflow: auto;
    font-family: var(--dyj-mono); font-size: 13px; line-height: 1.5;
    white-space: pre-wrap; word-break: break-all; }
  pre:focus-visible { outline: 2px solid var(--dyj-primary); outline-offset: -2px; }
  .live { color: var(--dyj-success); }
  .warn { color: var(--dyj-warning); }
</style>
<div class="bar">
  <span class="state"><dyj-icon name="info"></dyj-icon><span class="sttxt"></span></span>
  <span class="grow"></span>
  <button type="button" class="follow"></button>
  <button type="button" class="clear">${t('logs_clear')}</button>
</div>
<pre tabindex="0"></pre>`;
    this.#pre = this.shadowRoot.querySelector('pre');
    this.#stateTxt = this.shadowRoot.querySelector('.sttxt');
    this.#stateIco = this.shadowRoot.querySelector('dyj-icon');
    this.#followBtn = this.shadowRoot.querySelector('.follow');
    this.#followBtn.textContent = t('logs_follow_pause');
    this.#followBtn.addEventListener('click', () => this.#setFollow(!this.#follow));
    this.shadowRoot.querySelector('.clear').addEventListener('click', () => this.#pre.replaceChildren());
    this.#pre.addEventListener('scroll', () => {
      const bottom = this.#pre.scrollTop + this.#pre.clientHeight >= this.#pre.scrollHeight - 30;
      if (this.#follow && !bottom) this.#setFollow(false);
    });
    this.#setState('connecting');
    this.#stop = openLogStream({
      onOpen: () => this.#setState('live'),
      onClose: () => this.#setState('reconnecting'),
      onLine: (line) => this.#append(line),
    });
  }

  disconnectedCallback() {
    this.#stop?.();
    this.#stop = null;
  }

  #setFollow(v) {
    this.#follow = v;
    this.#followBtn.textContent = t(v ? 'logs_follow_pause' : 'logs_follow_resume');
    if (v) this.#pre.scrollTop = this.#pre.scrollHeight;
  }

  #setState(k) {
    const map = {
      connecting: ['logs_connecting', '', 'dim'],
      live: ['logs_live', 'check', 'live'],
      reconnecting: ['logs_reconnecting', 'warn', 'warn'],
    };
    const [key, icon, cls] = map[k];
    this.#stateTxt.textContent = t(key);
    this.#stateIco.setAttribute('name', icon || 'info');
    this.#stateIco.className = cls;
  }

  #append(line) {
    const pre = this.#pre;
    pre.appendChild(document.createTextNode(line + '\n'));
    while (pre.childNodes.length > MAX_LINES) pre.removeChild(pre.firstChild);
    if (this.#follow) pre.scrollTop = pre.scrollHeight;
  }
}

customElements.define('dyj-log-viewer', DyjLogViewer);
