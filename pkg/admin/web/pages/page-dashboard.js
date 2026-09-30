// 首页：大状态徽章 + 打印机名 + URI 复制 + 暂停/恢复 + 指标卡片 + 各系统
// 添加打印机指引（折叠）。数据来自 window "dyj-status" 广播 + 自身首取。
import { getStatus, pausePrinter, resumePrinter, getPairing, rotatePairing, getDevices, revokeDevice, errMessage } from '../api.js';
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
  #pinTimer = 0;
  #devsLoaded = false;

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
  dyj-page-dashboard .pin { font-size: 38px; font-weight: 700; letter-spacing: .3em;
    font-variant-numeric: tabular-nums; }
  dyj-page-dashboard .pinrow { display: flex; align-items: center; gap: 16px; }
  dyj-page-dashboard .dev { display: flex; align-items: center; gap: 12px;
    padding: 8px 0; border-bottom: 1px solid var(--dyj-border); }
  dyj-page-dashboard .dev .meta { flex: 1; }
  dyj-page-dashboard .dev .sub { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
</style>
<h1>${t('nav_status')}</h1>
<section class="hero">
  <dyj-status-badge id="badge" state="unknown" size="large"></dyj-status-badge>
  <div class="pname" id="pname"></div>
  <div class="dim" id="pinfo"></div>
  <div class="dim" id="reasons" hidden></div>
  <div class="acts"><dyj-button id="toggleBtn" variant="secondary"></dyj-button></div>
</section>
<section id="pairSec" hidden>
  <h2>${t('pin_title')}</h2>
  <div class="pinrow"><div class="pin" id="pin">····</div>
    <dyj-button id="rotatePin" variant="secondary">${t('share_pin_rotate')}</dyj-button></div>
  <div class="dim" id="pinHint" style="margin-top:6px"></div>
</section>
<section>
  <h2>${t('uris_title')}</h2>
  <div class="uris" id="uris"></div>
</section>
<section id="devSec" hidden>
  <h2>${t('devices_title')}</h2>
  <div id="devList"></div>
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
    this.querySelector('#rotatePin').addEventListener('click', async () => {
      await rotatePairing().catch(() => {});
      this.#refreshPin();
    });
    this.#onStatus = (ev) => this.#update(ev.detail);
    window.addEventListener('dyj-status', this.#onStatus);
    getStatus().then((s) => this.#update(s)).catch((e) => {
      this.querySelector('#pinfo').textContent = errMessage(e);
    });
  }

  disconnectedCallback() {
    if (this.#onStatus) window.removeEventListener('dyj-status', this.#onStatus);
    clearInterval(this.#pinTimer);
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
    this.#updatePairing(s);
  }

  // ---- v0.3: pairing code card + paired-device management ----
  #updatePairing(s) {
    const on = !!s.pairing_enabled && (s.printers || []).length > 0;
    this.querySelector('#pairSec').hidden = !on;
    this.querySelector('#devSec').hidden = !on;
    if (!on) {
      clearInterval(this.#pinTimer);
      this.#pinTimer = 0;
      this.#devsLoaded = false;
      return;
    }
    if (!this.#pinTimer) {
      this.#pinTimer = setInterval(() => this.#refreshPin(), 5000);
      this.#refreshPin();
    }
    if (!this.#devsLoaded) {
      this.#devsLoaded = true;
      this.#loadDevices();
    }
  }

  async #refreshPin() {
    try {
      const { pin, remain_sec } = await getPairing();
      const el = this.querySelector('#pin');
      if (!el) return;
      el.textContent = pin || '····';
      this.querySelector('#pinHint').textContent = t('share_pin_hint').replace('{n}', remain_sec ?? 0);
    } catch { /* token/loopback gated; ignore */ }
  }

  async #loadDevices() {
    const box = this.querySelector('#devList');
    try {
      const { devices } = await getDevices();
      if (!devices?.length) {
        box.innerHTML = `<div class="dim">${t('devices_empty')}</div>`;
        return;
      }
      box.replaceChildren(...devices.map((d) => {
        const row = document.createElement('div');
        row.className = 'dev';
        const meta = document.createElement('div');
        meta.className = 'meta';
        meta.innerHTML = `<div></div><div class="sub"></div>`;
        meta.firstChild.textContent = d.name || d.id.slice(0, 8);
        meta.querySelector('.sub').textContent = [
          d.source_ip,
          d.added_at ? new Date(d.added_at).toLocaleString() : '',
        ].filter(Boolean).join(' · ');
        row.append(meta);
        const btn = document.createElement('dyj-button');
        btn.setAttribute('variant', 'secondary');
        btn.textContent = t('devices_revoke');
        btn.addEventListener('click', async () => {
          const ok = await confirmDialog({
            title: t('devices_revoke_confirm'),
            body: t('devices_revoke_confirm_body'),
            confirmText: t('devices_revoke'),
            danger: true,
          });
          if (!ok) return;
          try {
            await revokeDevice(d.id);
            showToast(t('devices_revoked'));
            this.#loadDevices();
          } catch (e) { showToast(errMessage(e), 'error'); }
        });
        row.append(btn);
        return row;
      }));
    } catch { box.innerHTML = ''; }
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
