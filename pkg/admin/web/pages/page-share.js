// 共享向导（#/share）：自动扫描本机打印机 → 勾选（默认全选非虚拟）→
// 一键共享（保存配置即刻生效）→ 完成页展示 4 位配对码。
import { getConfig, saveConfig, getLocalPrinters, getPairing, rotatePairing, errMessage } from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import '../components/dyj-stepper.js';
import '../components/dyj-button.js';
import '../components/dyj-card.js';
import '../components/dyj-icon.js';

class DyjPageShare extends HTMLElement {
  #cur = 0;
  #rows = [];        // all local printers from the API
  #showVirtual = false;
  #cfg = null;
  #pinTimer = 0;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-share .body { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow); padding: 24px; }
  dyj-page-share .list { display: flex; flex-direction: column; gap: 8px; margin-top: 12px; }
  dyj-page-share .row { display: flex; align-items: center; gap: 12px; padding: 12px 14px;
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius); }
  dyj-page-share .row.bad { border-color: var(--dyj-danger, #c0392b); }
  dyj-page-share .row .meta { flex: 1; min-width: 0; }
  dyj-page-share .row .name { font-weight: 600; }
  dyj-page-share .row .sub { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-share .badge { font-size: var(--dyj-fs-s); color: var(--dyj-primary);
    border: 1px solid var(--dyj-primary); border-radius: 99px; padding: 1px 8px; }
  dyj-page-share .toggle { margin-top: 10px; font-size: var(--dyj-fs-s); }
  dyj-page-share .toggle a { color: var(--dyj-primary); cursor: pointer; }
  dyj-page-share .pin { font-size: 44px; font-weight: 700; letter-spacing: .35em;
    color: var(--dyj-text); padding: 6px 0 2px; font-variant-numeric: tabular-nums; }
  dyj-page-share .pin-hint { color: var(--dyj-text-dim); max-width: 40em; }
  dyj-page-share .nav { display: flex; gap: 10px; margin-top: 18px; }
  dyj-page-share .empty { color: var(--dyj-text-dim); }
  dyj-page-share .spin { color: var(--dyj-text-dim); }
</style>
<h1>${t('share_title')}</h1>
<dyj-stepper id="stepper"></dyj-stepper>
<div class="body" id="body"></div>
<div class="nav" id="nav"></div>`;
    const steps = [t('share_step_pick'), t('share_step_share'), t('share_step_done')];
    const st = this.querySelector('#stepper');
    st.steps = steps;
    st.addEventListener('dyj-goto', (ev) => this.#show(ev.detail.index));
    getConfig().then(({ config }) => { this.#cfg = config || {}; }).catch(() => { this.#cfg = {}; });
    this.#show(0);
    this.#load();
  }

  disconnectedCallback() { clearInterval(this.#pinTimer); }

  #show(i) {
    this.#cur = i;
    this.querySelector('#stepper').step = i;
    const body = this.querySelector('#body');
    const nav = this.querySelector('#nav');
    nav.replaceChildren();
    if (i === 0) this.#renderPick(body, nav);
    else if (i === 1) this.#renderSaving(body);
    else this.#renderDone(body, nav);
  }

  async #load() {
    if (this.#cur !== 0) return;
    const body = this.querySelector('#body');
    body.innerHTML = `<div class="spin">${t('share_scanning')}</div>`;
    try {
      const { printers } = await getLocalPrinters();
      this.#rows = printers || [];
      // Re-render through #show so the nav is rebuilt exactly once.
      if (this.#cur === 0) this.#show(0);
    } catch (e) {
      body.innerHTML = `<div class="empty">${errMessage(e)}</div>`;
    }
  }

  #visibleRows() {
    return this.#rows.filter((p) => this.#showVirtual || !p.virtual);
  }

  #renderPick(body, nav) {
    if (!this.#rows) return; // still loading
    const rows = this.#visibleRows();
    if (!rows.length) {
      body.innerHTML = `
        <div class="empty">${t('share_none_found')}</div>
        <div class="empty">${t('share_none_found_hint')}</div>`;
      const btn = document.createElement('dyj-button');
      btn.textContent = t('share_refresh');
      btn.addEventListener('click', () => this.#load());
      nav.append(btn);
      return;
    }
    body.innerHTML = `
      <p class="spin">${t('share_pick_hint')}</p>
      <div class="list" id="list"></div>
      <div class="toggle"><a id="tog"></a></div>`;
    const list = this.querySelector('#list');
    list.replaceChildren(...rows.map((p, idx) => {
      const row = document.createElement('label');
      row.className = 'row' + (p.probe_err ? ' bad' : '');
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.checked = !p.probe_err && idx >= 0 && rows.slice(0, idx).every(() => true) && !p.virtual;
      // default: every probeable non-virtual printer is checked
      cb.checked = !p.probe_err;
      cb.dataset.name = p.name;
      row.append(cb);
      const meta = document.createElement('div');
      meta.className = 'meta';
      meta.innerHTML = `<div class="name"></div><div class="sub"></div>`;
      meta.querySelector('.name').textContent = p.name;
      const sub = [p.driver, p.probe_err ? t('share_probe_err') + '：' + p.probe_err : ''].filter(Boolean).join(' · ');
      meta.querySelector('.sub').textContent = sub;
      row.append(meta);
      if (p.default) {
        const b = document.createElement('span');
        b.className = 'badge';
        b.textContent = t('share_default');
        row.append(b);
      }
      return row;
    }));
    const tog = this.querySelector('#tog');
    tog.textContent = this.#showVirtual ? t('share_hide_virtual') : t('share_show_virtual');
    tog.addEventListener('click', () => {
      this.#showVirtual = !this.#showVirtual;
      this.#renderPick(this.querySelector('#body'), this.querySelector('#nav'));
    });
    const btn = document.createElement('dyj-button');
    btn.setAttribute('variant', 'primary');
    btn.textContent = t('share_btn_share');
    btn.addEventListener('click', () => this.#save());
    nav.append(btn);
  }

  async #save() {
    const checked = [...this.querySelectorAll('#list input[type=checkbox]')]
      .filter((cb) => cb.checked)
      .map((cb) => this.#rows.find((p) => p.name === cb.dataset.name))
      .filter(Boolean);
    if (!checked.length) {
      showToast(t('share_pick_hint'), 'error');
      return;
    }
    const prev = this.#cfg || {};
    const next = {
      ...prev,
      role: prev.role === 'use' || prev.role === 'both' ? 'both' : 'share',
      printers: checked.map((p) => ({ name: p.name, queue: p.name })),
    };
    this.#show(1);
    try {
      await saveConfig(next);
      this.#show(2);
      showToast(t('share_saved'));
    } catch (e) {
      showToast(errMessage(e), 'error');
      this.#show(0);
    }
  }

  #renderSaving(body) {
    body.innerHTML = `<div class="spin">${t('share_saving')}</div>`;
  }

  async #renderDone(body, nav) {
    body.innerHTML = `
      <h2>${t('share_pin_title')}</h2>
      <div class="pin" id="pin">····</div>
      <div class="pin-hint" id="pinHint"></div>
      <div style="margin-top:10px"><dyj-button id="rotate" variant="secondary">${t('share_pin_rotate')}</dyj-button></div>`;
    this.querySelector('#rotate').addEventListener('click', async () => {
      await rotatePairing().catch(() => {});
      this.#refreshPin();
    });
    const addBtn = document.createElement('dyj-button');
    addBtn.textContent = t('share_go_add');
    addBtn.addEventListener('click', () => { location.hash = '#/add'; });
    const stBtn = document.createElement('dyj-button');
    stBtn.setAttribute('variant', 'primary');
    stBtn.textContent = t('share_go_status');
    stBtn.addEventListener('click', () => { location.hash = '#/status'; });
    nav.append(addBtn, stBtn);
    clearInterval(this.#pinTimer);
    this.#pinTimer = setInterval(() => this.#refreshPin(), 5000);
    this.#refreshPin();
  }

  async #refreshPin() {
    try {
      const { pin, remain_sec } = await getPairing();
      const el = this.querySelector('#pin');
      if (!el) return;
      el.textContent = pin || '····';
      this.querySelector('#pinHint').textContent = t('share_pin_hint').replace('{n}', remain_sec ?? 0);
    } catch { /* loopback-only; ignore */ }
  }
}

customElements.define('dyj-page-share', DyjPageShare);
