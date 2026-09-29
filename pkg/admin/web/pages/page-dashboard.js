// 首页：大状态徽章 + 打印机名 + URI 复制 + 暂停/恢复 + 指标卡片 + 各系统
// 添加打印机指引（折叠）。数据来自 window "dyj-status" 广播 + 自身首取。
import { getStatus, pausePrinter, resumePrinter, errMessage } from '../api.js';
import { t } from '../i18n/index.js';
import { statusBadgeState } from '../components/dyj-sidebar.js';
import { confirmDialog } from '../components/dyj-modal.js';
import { showToast } from '../components/dyj-toast.js';
import '../components/dyj-status-badge.js';
import '../components/dyj-button.js';
import '../components/dyj-card.js';
import '../components/dyj-copy-field.js';
import '../components/dyj-disclosure.js';

class DyjPageDashboard extends HTMLElement {
  #accepting = true;
  #last = null;
  #onStatus = null;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-dashboard .hero { display: flex; flex-direction: column; gap: 6px;
    background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow);
    padding: 24px; }
  dyj-page-dashboard .pname { font-size: var(--dyj-fs-xl); font-weight: 600; }
  dyj-page-dashboard .acts { margin-top: 12px; }
  dyj-page-dashboard .uris { display: flex; flex-direction: column; gap: 8px; max-width: 640px; }
  dyj-page-dashboard .metrics { display: grid; gap: 12px;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); }
  dyj-page-dashboard .num { font-size: var(--dyj-fs-xl); font-weight: 600; }
  dyj-page-dashboard pre.cmd { background: var(--dyj-bg); border: 1px solid var(--dyj-border);
    border-radius: 6px; padding: 8px 12px; overflow-x: auto; font-size: var(--dyj-fs-s); }
  dyj-page-dashboard .guide p { max-width: 46em; color: var(--dyj-text-dim); }
</style>
<h1>${t('nav_status')}</h1>
<section class="hero">
  <dyj-status-badge id="badge" state="unknown" size="large"></dyj-status-badge>
  <div class="pname" id="pname"></div>
  <div class="dim" id="pinfo"></div>
  <div class="dim" id="reasons" hidden></div>
  <div class="acts"><dyj-button id="toggleBtn" variant="secondary"></dyj-button></div>
</section>
<section>
  <h2>${t('uris_title')}</h2>
  <div class="uris" id="uris"></div>
</section>
<section class="metrics">
  <dyj-card><span slot="title">${t('metric_queued')}</span><div class="num" id="mQueued">–</div></dyj-card>
  <dyj-card><span slot="title">${t('metric_backend')}</span><div class="num" id="mBackend">–</div></dyj-card>
  <dyj-card><span slot="title">${t('metric_mdns')}</span><div class="num" id="mMdns">–</div></dyj-card>
  <dyj-card><span slot="title">${t('metric_version')}</span><div class="num" id="mVersion">–</div></dyj-card>
</section>
<section class="guide">
  <dyj-disclosure summary="${t('guide_title')}">
    <h3>Windows</h3><p>${t('guide_windows')}</p>
    <h3>macOS</h3><p>${t('guide_macos')}</p>
    <h3>Linux</h3><p>${t('guide_linux')}</p>
    <pre class="cmd"><code>${t('guide_linux_cmd')}</code></pre>
  </dyj-disclosure>
</section>`;
    this.querySelector('#toggleBtn').addEventListener('click', () => this.#toggle());
    this.#onStatus = (ev) => this.#update(ev.detail);
    window.addEventListener('dyj-status', this.#onStatus);
    getStatus().then((s) => this.#update(s)).catch((e) => {
      this.querySelector('#pinfo').textContent = errMessage(e);
    });
  }

  disconnectedCallback() {
    if (this.#onStatus) window.removeEventListener('dyj-status', this.#onStatus);
  }

  #update(s) {
    this.#last = s;
    this.#accepting = !!s.accepting;
    this.querySelector('#badge').setAttribute('state', statusBadgeState(s));
    this.querySelector('#pname').textContent = s.printer_name || '—';
    this.querySelector('#pinfo').textContent = s.printer_info || '';
    const reasons = s.reasons || [];
    const rEl = this.querySelector('#reasons');
    if (!s.accepting && reasons.length) {
      rEl.textContent = t('reasons_prefix') + '：' + reasons.map((r) => {
        const v = t('reason_' + r);
        return v === 'reason_' + r ? r : v;
      }).join('、');
      rEl.hidden = false;
    } else {
      rEl.hidden = true;
    }
    this.querySelector('#mQueued').textContent = String(s.queued ?? 0);
    this.querySelector('#mBackend').textContent = s.backend || '—';
    this.querySelector('#mMdns').textContent = s.mdns ? t('mdns_on') : t('mdns_off');
    this.querySelector('#mVersion').textContent = s.version ? 'v' + s.version : '—';
    const uris = this.querySelector('#uris');
    if (uris.childElementCount !== (s.uris || []).length) {
      uris.replaceChildren(...(s.uris || []).map((u) => {
        const cf = document.createElement('dyj-copy-field');
        cf.setAttribute('value', u);
        return cf;
      }));
    }
    const btn = this.querySelector('#toggleBtn');
    btn.textContent = this.#accepting ? t('dash_pause') : t('dash_resume');
  }

  async #toggle() {
    const wasAccepting = this.#accepting;
    if (wasAccepting) {
      const ok = await confirmDialog({
        title: t('pause_confirm_title'),
        body: t('pause_confirm_body'),
        confirmText: t('dash_pause'),
        danger: true,
      });
      if (!ok) return;
    }
    try {
      const r = wasAccepting ? await pausePrinter() : await resumePrinter();
      if (this.#last) this.#update({ ...this.#last, accepting: r.accepting });
      showToast(t(wasAccepting ? 'toast_paused' : 'toast_resumed'));
    } catch (e) {
      showToast(errMessage(e), 'error');
    }
  }
}

customElements.define('dyj-page-dashboard', DyjPageDashboard);
