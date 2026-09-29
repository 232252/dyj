// dyj-toast：全局轻提示。showToast(msg, 'success'|'error') 会懒挂载宿主
// #dyj-toasts，气泡自动 3.2s 消退；role="status" 供读屏播报。
import './dyj-icon.js';

export function showToast(message, type = 'success') {
  let host = document.getElementById('dyj-toasts');
  if (!host) {
    host = document.createElement('div');
    host.id = 'dyj-toasts';
    document.body.appendChild(host);
  }
  const el = document.createElement('dyj-toast');
  el.setAttribute('type', type);
  el.textContent = message;
  host.appendChild(el);
  setTimeout(() => {
    el.classList.add('bye');
    setTimeout(() => el.remove(), 250);
  }, 3200);
}

class DyjToast extends HTMLElement {
  connectedCallback() {
    if (!document.getElementById('dyj-toasts-style')) {
      const st = document.createElement('style');
      st.id = 'dyj-toasts-style';
      st.textContent = '#dyj-toasts{position:fixed;top:16px;left:50%;transform:translateX(-50%);' +
        'z-index:1000;display:flex;flex-direction:column;gap:8px;align-items:center;pointer-events:none;}';
      document.head.appendChild(st);
    }
    const bad = this.getAttribute('type') === 'error';
    const tone = bad ? 'danger' : 'success';
    this.attachShadow({ mode: 'open' }).innerHTML = `
<style>
  :host { display: block; pointer-events: auto; }
  .t { display: flex; align-items: center; gap: 8px; max-width: min(90vw, 480px);
    background: var(--dyj-card); color: var(--dyj-text);
    border: 1px solid var(--dyj-border); border-left: 3px solid var(--dyj-${tone});
    border-radius: var(--dyj-radius); box-shadow: 0 4px 16px rgb(0 0 0 / 12%);
    padding: 10px 16px; font-size: var(--dyj-fs-m);
    opacity: 1; transform: translateY(0); transition: opacity .25s, transform .25s; }
  dyj-icon { width: 15px; height: 15px; flex: none; color: var(--dyj-${tone}); }
  :host(.bye) .t { opacity: 0; transform: translateY(-6px); }
</style>
<div class="t" role="status"><dyj-icon name="${bad ? 'error' : 'check'}"></dyj-icon><span></span></div>`;
    this.shadowRoot.querySelector('span').textContent = this.textContent;
  }
}

customElements.define('dyj-toast', DyjToast);
