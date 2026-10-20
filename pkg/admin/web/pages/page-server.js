// 服务端页（#/server，v0.4 research/14 §3.3）：同一组件两个状态。
// 挑选态 = 自动扫描本机打印机（默认全选、虚拟隐藏）→ 一键共享；
// 主页态 = 配对码大字 + 共享清单逐台开关 + 连上的电脑 + 最近打印 8 行。
// 向导完成后原地变主页，不存在"完成页跳转"；每台开关即时热生效。
import {
  getConfig, saveConfig, getLocalPrinters, getPairing, rotatePairing,
  getDevices, revokeDevice, getJobs, cancelJob, pausePrinter, resumePrinter,
  errMessage, esc,
} from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import { confirmDialog } from '../components/dyj-modal.js';
import '../components/dyj-button.js';
import '../components/dyj-job-table.js';

class DyjPageServer extends HTMLElement {
  #cfg = null;
  #rows = null;          // null = 正在扫描
  #mode = 'pick';        // pick | home
  #showVirtual = false;
  #pinTimer = 0;
  #jobTimer = 0;
  #accepting = true;
  #onStatus = null;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-server .body { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow); padding: 24px; }
  dyj-page-server .sub { color: var(--dyj-text-dim); margin: 0 0 10px; }
  dyj-page-server .list { display: flex; flex-direction: column; gap: 8px; margin-top: 10px; }
  dyj-page-server .row { display: flex; align-items: center; gap: 12px; padding: 12px 14px;
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius); }
  dyj-page-server .row.bad { border-color: var(--dyj-danger, #c0392b); }
  dyj-page-server .row .meta { flex: 1; min-width: 0; }
  dyj-page-server .row .name { font-weight: 600; }
  dyj-page-server .row .sub2 { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-server .badge { font-size: var(--dyj-fs-s); color: var(--dyj-primary);
    border: 1px solid var(--dyj-primary); border-radius: 99px; padding: 1px 8px; }
  dyj-page-server .linklike { margin-top: 10px; font-size: var(--dyj-fs-s); }
  dyj-page-server .linklike a { color: var(--dyj-primary); cursor: pointer; }
  dyj-page-server .pin { font-size: 44px; font-weight: 700; letter-spacing: .35em;
    padding: 4px 0 2px; font-variant-numeric: tabular-nums; }
  dyj-page-server .pinrow { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
  dyj-page-server .pinhint { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-server h2 { font-size: var(--dyj-fs-l); margin: 20px 0 6px; }
  dyj-page-server .dev { display: flex; align-items: center; gap: 12px;
    padding: 8px 0; border-bottom: 1px solid var(--dyj-border); }
  dyj-page-server .dev .meta { flex: 1; }
  dyj-page-server .dev .sub2 { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-server .nav { display: flex; gap: 10px; margin-top: 18px; align-items: center; flex-wrap: wrap; }
  dyj-page-server .nav .grow { flex: 1; }
  dyj-page-server .foot { margin-top: 18px; display: flex; gap: 18px; font-size: var(--dyj-fs-s); }
  dyj-page-server .foot a { color: var(--dyj-text-dim); }
  dyj-page-server .foot a:hover { color: var(--dyj-primary); }
  dyj-page-server .spin { color: var(--dyj-text-dim); }
  dyj-page-server .empty { color: var(--dyj-text-dim); }
</style>
<div id="root"></div>`;
    this.#onStatus = (ev) => {
      this.#accepting = !!ev.detail?.accepting;
      const btn = this.querySelector('#pauseBtn');
      if (btn) btn.textContent = this.#accepting ? t('server_pause') : t('server_resume');
    };
    window.addEventListener('dyj-status', this.#onStatus);
    this.#load();
  }

  disconnectedCallback() {
    window.removeEventListener('dyj-status', this.#onStatus);
    clearInterval(this.#pinTimer);
    clearInterval(this.#jobTimer);
  }

  // ---- data ----

  async #load() {
    const root = this.querySelector('#root');
    root.innerHTML = `<div class="spin">${t('server_scanning')}</div>`;
    const [cfgRes, lpRes] = await Promise.allSettled([getConfig(), getLocalPrinters()]);
    this.#cfg = cfgRes.status === 'fulfilled' ? (cfgRes.value.config || {}) : {};
    this.#rows = lpRes.status === 'fulfilled' ? (lpRes.value.printers || []) : null;
    this.#mode = (this.#cfg.printers || []).length > 0 ? 'home' : 'pick';
    if (lpRes.status === 'rejected') {
      root.innerHTML = `<div class="body"><div class="empty">${esc(errMessage(lpRes.reason))}</div></div>
        <div class="nav"><dyj-button id="rescan">${t('client_rescan')}</dyj-button></div>`;
      this.querySelector('#rescan').addEventListener('click', () => this.#load());
      return;
    }
    this.#render();
  }

  #visibleRows() {
    return (this.#rows || []).filter((p) => this.#showVirtual || !p.virtual);
  }

  #sharedNames() {
    return new Set((this.#cfg.printers || []).map((p) => p.name));
  }

  // ---- render ----

  #render() {
    clearInterval(this.#pinTimer);
    clearInterval(this.#jobTimer);
    this.#pinTimer = 0;
    this.#jobTimer = 0;
    const root = this.querySelector('#root');
    if (this.#mode === 'pick') {
      root.innerHTML = `
        <h1>${t('server_title')}</h1>
        <div class="body" id="body"></div>
        <div class="nav" id="nav"></div>
        <div class="foot">
          <a href="#/client">${t('server_goto_client')}</a>
          <a href="#/help">${t('goto_help')}</a>
        </div>`;
      this.#renderPick(this.querySelector('#body'), this.querySelector('#nav'));
      return;
    }
    const n = (this.#cfg.printers || []).length;
    root.innerHTML = `
      <h1>${t('server_title')} <span class="dim">· ${t('server_share_count', { n })}</span></h1>
      <div class="body">
        <section id="pinSec" hidden>
          <div class="pinrow">
            <div class="pin" id="pin">····</div>
            <div>
              <dyj-button id="rotate" variant="secondary">${t('server_pin_rotate')}</dyj-button>
              <div class="pinhint" id="pinHint" style="margin-top:6px"></div>
            </div>
          </div>
        </section>
        <section>
          <h2>${t('server_printers_title')}</h2>
          <div id="printersBox"></div>
        </section>
        <section id="devSec" hidden>
          <h2>${t('server_devices_title')}</h2>
          <div id="devList"></div>
        </section>
        <section id="jobsSec" hidden>
          <h2>${t('server_recent_title')}</h2>
          <dyj-job-table id="jobs" hidden></dyj-job-table>
        </section>
      </div>
      <div class="nav">
        <dyj-button id="pauseBtn" variant="secondary"></dyj-button>
        <span class="grow"></span>
      </div>
      <div class="foot">
        <a href="#/client">${t('server_goto_client')}</a>
        <a href="#/help">${t('goto_help')}</a>
      </div>`;
    this.querySelector('#rotate').addEventListener('click', async () => {
      await rotatePairing().catch(() => {});
      this.#refreshPin();
    });
    const pauseBtn = this.querySelector('#pauseBtn');
    pauseBtn.textContent = this.#accepting ? t('server_pause') : t('server_resume');
    this.querySelector('#pauseBtn').addEventListener('click', () => this.#togglePause());
    this.querySelector('#jobs').addEventListener('dyj-cancel', (ev) => this.#cancelJob(ev.detail.id));
    this.#renderPrinters();
    this.#refreshPin();
    this.#pinTimer = setInterval(() => this.#refreshPin(), 5000);
    this.#loadDevices();
    this.#refreshJobs();
    this.#jobTimer = setInterval(() => {
      if (!document.hidden) this.#refreshJobs();
    }, 10000);
  }

  // ---- pick state ----

  #renderPick(body, nav) {
    if (this.#rows === null) {
      body.innerHTML = `<div class="spin">${t('server_scanning')}</div>`;
      return;
    }
    const rows = this.#visibleRows();
    if (!rows.length) {
      body.innerHTML = `
        <div class="empty">${t('server_none_found')}</div>
        <div class="empty">${t('server_none_found_hint')}</div>`;
      const btn = document.createElement('dyj-button');
      btn.textContent = t('client_rescan');
      btn.addEventListener('click', () => this.#load());
      nav.replaceChildren(btn);
      return;
    }
    body.innerHTML = `
      <p class="sub">${t('server_pick_hint')}</p>
      <div class="list" id="list"></div>
      <div class="linklike"><a id="tog"></a></div>`;
    const list = body.querySelector('#list');
    list.replaceChildren(...rows.map((p) => {
      const row = document.createElement('label');
      row.className = 'row' + (p.probe_err ? ' bad' : '');
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.checked = !p.probe_err; // 默认全选打得开的打印机（虚拟默认隐藏）
      cb.dataset.name = p.name;
      row.append(cb);
      const meta = document.createElement('div');
      meta.className = 'meta';
      meta.innerHTML = `<div class="name"></div><div class="sub2"></div>`;
      meta.querySelector('.name').textContent = p.name;
      meta.querySelector('.sub2').textContent = [p.driver,
        p.probe_err ? t('server_probe_err') + '：' + p.probe_err : ''].filter(Boolean).join(' · ');
      row.append(meta);
      if (p.default) {
        const b = document.createElement('span');
        b.className = 'badge';
        b.textContent = t('server_default');
        row.append(b);
      }
      return row;
    }));
    const tog = body.querySelector('#tog');
    tog.textContent = this.#showVirtual ? t('server_hide_virtual') : t('server_show_virtual');
    tog.addEventListener('click', () => {
      this.#showVirtual = !this.#showVirtual;
      this.#renderPick(body, nav);
    });
    const btn = document.createElement('dyj-button');
    btn.setAttribute('variant', 'primary');
    btn.textContent = t('server_btn_share');
    const updateLabel = () => {
      const n = [...body.querySelectorAll('#list input:checked')].length;
      btn.textContent = t('server_btn_share_n', { n });
    };
    list.addEventListener('change', updateLabel);
    updateLabel();
    btn.addEventListener('click', () => this.#save());
    nav.replaceChildren(btn);
  }

  async #save() {
    const checked = [...this.querySelectorAll('#list input[type=checkbox]:checked')]
      .map((cb) => (this.#rows || []).find((p) => p.name === cb.dataset.name))
      .filter(Boolean);
    if (!checked.length) {
      showToast(t('server_pick_hint'), 'error');
      return;
    }
    const prev = this.#cfg || {};
    const next = {
      ...prev,
      role: prev.role === 'use' ? 'both' : 'share',
      printers: checked.map((p) => ({ name: p.name, queue: p.name })),
    };
    this.querySelector('#nav').replaceChildren();
    const body = this.querySelector('#body');
    body.innerHTML = `<div class="spin">${t('server_saving')}</div>`;
    try {
      await saveConfig(next);
      this.#cfg = next;
      this.#mode = 'home';
      showToast(t('server_saved'));
      this.#render();
    } catch (e) {
      showToast(errMessage(e), 'error');
      this.#renderPick(body, this.querySelector('#nav'));
    }
  }

  // ---- home state ----

  #renderPrinters() {
    const box = this.querySelector('#printersBox');
    if (!box) return;
    const shared = this.#sharedNames();
    const rows = this.#visibleRows();
    if (!rows.length) {
      box.innerHTML = `<div class="empty">${t('server_none_found')}</div>`;
      return;
    }
    const list = document.createElement('div');
    list.className = 'list';
    list.replaceChildren(...rows.map((p) => {
      const row = document.createElement('div');
      row.className = 'row';
      const meta = document.createElement('div');
      meta.className = 'meta';
      meta.innerHTML = `<div class="name"></div><div class="sub2"></div>`;
      meta.querySelector('.name').textContent = p.name;
      meta.querySelector('.sub2').textContent = p.driver || '';
      row.append(meta);
      if (p.default) {
        const b = document.createElement('span');
        b.className = 'badge';
        b.textContent = t('server_default');
        row.append(b);
      }
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.checked = shared.has(p.name);
      cb.setAttribute('aria-label', p.name);
      cb.style.accentColor = 'var(--dyj-primary)';
      cb.addEventListener('change', () => this.#togglePrinter(p, cb));
      row.append(cb);
      return row;
    }));
    const tog = document.createElement('div');
    tog.className = 'linklike';
    tog.innerHTML = `<a></a>`;
    tog.querySelector('a').textContent = this.#showVirtual
      ? t('server_hide_virtual') : t('server_show_virtual');
    tog.querySelector('a').addEventListener('click', () => {
      this.#showVirtual = !this.#showVirtual;
      this.#renderPrinters();
    });
    box.replaceChildren(list, tog);
  }

  // 逐台开关：改 printers[] → PUT 配置（进程内热生效，research/14 §3.3）。
  async #togglePrinter(p, cb) {
    const want = cb.checked;
    const prev = this.#cfg || {};
    const cur = prev.printers || [];
    const next = want
      ? [...cur.filter((x) => x.name !== p.name), { name: p.name, queue: p.name }]
      : cur.filter((x) => x.name !== p.name);
    const body = { ...prev, role: next.length ? (prev.role === 'use' ? 'both' : 'share') : prev.role, printers: next };
    cb.disabled = true;
    try {
      await saveConfig(body);
      this.#cfg = body;
      if (!next.length) {
        this.#mode = 'pick';
        this.#render();
        return;
      }
      const n = next.length;
      const h1 = this.querySelector('h1 .dim');
      if (h1) h1.textContent = '· ' + t('server_share_count', { n });
    } catch (e) {
      cb.checked = !want; // 回弹
      showToast(errMessage(e), 'error');
    } finally {
      cb.disabled = false;
    }
  }

  async #refreshPin() {
    const sec = this.querySelector('#pinSec');
    if (!sec) return;
    try {
      const { pin, remain_sec } = await getPairing();
      sec.hidden = false;
      const el = this.querySelector('#pin');
      if (!el) return;
      el.textContent = pin || '····';
      this.querySelector('#pinHint').textContent = t('server_pin_hint', { n: remain_sec ?? 0 });
    } catch {
      if (sec) sec.hidden = true; // 配对不可用（旧服务）→ 不显示
    }
  }

  async #loadDevices() {
    const sec = this.querySelector('#devSec');
    const box = this.querySelector('#devList');
    if (!sec || !box) return;
    try {
      const { devices } = await getDevices();
      sec.hidden = !(devices || []).length;
      box.replaceChildren(...(devices || []).map((d) => {
        const row = document.createElement('div');
        row.className = 'dev';
        const meta = document.createElement('div');
        meta.className = 'meta';
        meta.innerHTML = `<div></div><div class="sub2"></div>`;
        meta.firstChild.textContent = d.name || d.id.slice(0, 8);
        meta.querySelector('.sub2').textContent = [
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
    } catch { sec.hidden = true; }
  }

  async #refreshJobs() {
    const sec = this.querySelector('#jobsSec');
    const tbl = this.querySelector('#jobs');
    if (!tbl) return;
    try {
      const { jobs } = await getJobs('all', 8);
      sec.hidden = !(jobs || []).length; // 没有记录就整节不出现（research/14 §3.3 极简）
      tbl.hidden = !(jobs || []).length;
      tbl.jobs = jobs || [];
    } catch { sec.hidden = true; }
  }

  async #cancelJob(id) {
    const ok = await confirmDialog({
      title: t('cancel_confirm_title', { id }),
      body: t('cancel_confirm_body'),
      confirmText: t('btn_cancel_job'),
      danger: true,
    });
    if (!ok) return;
    try {
      await cancelJob(id);
      showToast(t('toast_canceled', { id }));
    } catch (e) {
      showToast(t('toast_cancel_failed') + '：' + errMessage(e), 'error');
    }
    this.#refreshJobs();
  }

  async #togglePause() {
    const wasAccepting = this.#accepting;
    if (wasAccepting) {
      const ok = await confirmDialog({
        title: t('pause_confirm_title'),
        body: t('pause_confirm_body'),
        confirmText: t('server_pause'),
        danger: true,
      });
      if (!ok) return;
    }
    try {
      await (wasAccepting ? pausePrinter() : resumePrinter());
      this.#accepting = !wasAccepting;
      const btn = this.querySelector('#pauseBtn');
      if (btn) btn.textContent = this.#accepting ? t('server_pause') : t('server_resume');
      showToast(t(this.#accepting ? 'toast_resumed' : 'toast_paused'));
    } catch (e) {
      showToast(errMessage(e), 'error');
    }
  }
}

customElements.define('dyj-page-server', DyjPageServer);
