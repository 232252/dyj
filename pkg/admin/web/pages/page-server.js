// 服务端页（#/server，v0.4 research/14 §3.3）：同一组件两个状态。
// 挑选态 = 自动扫描本机打印机（默认全选、虚拟隐藏）→ 一键共享；
// 主页态 = 配对码大字 + 共享清单逐台开关 + 连上的电脑 + 最近打印 8 行。
// 向导完成后原地变主页，不存在"完成页跳转"；每台开关即时热生效。
import {
  getConfig, saveConfig, getLocalPrinters, getPairing, rotatePairing,
  getDevices, revokeDevice, getJobs, cancelJob, pausePrinter, resumePrinter,
  getOpen, openStart, openStop, getStatus,
  errMessage, esc,
} from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import { confirmDialog } from '../components/dyj-modal.js';
import { rowMeta, confirmAction } from '../ui.js';
import '../components/dyj-button.js';
import '../components/dyj-job-table.js';

// fmtRemain renders seconds as m:ss (the open-printing countdown).
const fmtRemain = (s) => {
  const n = Math.max(0, Math.floor(s || 0));
  return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, '0')}`;
};

class DyjPageServer extends HTMLElement {
  #cfg = null;
  #rows = null;          // null = 正在扫描
  #mode = 'pick';        // pick | home
  #showVirtual = false;
  #pollTimer = 0;        // 5s：配对码剩余 + 免认证打印倒计时
  #jobTimer = 0;
  #accepting = true;
  #onStatus = null;

  connectedCallback() {
    this.innerHTML = `
<style>
  /* .body/.sub/.list/.row 盒型与 .spin/.empty/.foot 在 theme.css 共享；
     这里只留本页差异（行距、徽章、配对码、设备行、导航）。 */
  dyj-page-server .row { gap: 12px; }
  dyj-page-server .row.bad { border-color: var(--dyj-danger, #c0392b); }
  dyj-page-server .badge { font-size: var(--dyj-fs-s); color: var(--dyj-primary);
    border: 1px solid var(--dyj-primary); border-radius: 99px; padding: 1px 8px; }
  dyj-page-server .linklike { margin-top: 10px; font-size: var(--dyj-fs-s); }
  dyj-page-server .linklike a { color: var(--dyj-primary); cursor: pointer; }
  dyj-page-server .pin { font-size: 46px; font-weight: 750; letter-spacing: .32em;
    padding: 8px 18px 6px 24px; font-variant-numeric: tabular-nums;
    color: var(--dyj-primary); background: var(--dyj-primary-weak);
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius-l, 16px);
    cursor: pointer; user-select: all; transition: box-shadow .15s; }
  dyj-page-server .pin:hover { box-shadow: var(--dyj-ring); }
  dyj-page-server .pinrow { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
  dyj-page-server .pinhint { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-server .tablet { margin-top: 14px; padding: 10px 14px;
    border: 1px dashed var(--dyj-border); border-radius: var(--dyj-radius);
    color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); line-height: 1.7; }
  dyj-page-server .tablet b { color: var(--dyj-text); font-weight: 600; }
  dyj-page-server .badge.virtual { font-size: var(--dyj-fs-s); border-radius: 99px; padding: 1px 8px; }
  dyj-page-server .openrow { display: flex; align-items: center; gap: 16px; flex-wrap: wrap;
    margin-top: 6px; }
  dyj-page-server .openrow .state { color: var(--dyj-text-dim); flex: 1; min-width: 200px; }
  dyj-page-server .openrow .state.on { color: var(--dyj-danger, #c0392b);
    font-variant-numeric: tabular-nums; }
  dyj-page-server h2 { font-size: var(--dyj-fs-l); margin: 20px 0 6px; }
  dyj-page-server .dev { display: flex; align-items: center; gap: 12px;
    padding: 8px 0; border-bottom: 1px solid var(--dyj-border); }
  dyj-page-server .dev .meta { flex: 1; }
  dyj-page-server .dev .sub2 { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-server .nav { display: flex; gap: 10px; margin-top: 18px; align-items: center; flex-wrap: wrap; }
  dyj-page-server .nav .grow { flex: 1; }
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
    clearInterval(this.#pollTimer);
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
    clearInterval(this.#pollTimer);
    clearInterval(this.#jobTimer);
    this.#pollTimer = 0;
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
        <section id="openSec" hidden>
          <h2>${t('server_open_title')}</h2>
          <div class="openrow">
            <div class="state" id="openState"></div>
            <div id="openBtns"></div>
          </div>
        </section>
        <div class="tablet" id="tabletHint" hidden></div>
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
    this.querySelector('#pin').addEventListener('click', () => this.#copyPin());
    this.#loadTabletHint();
    const pauseBtn = this.querySelector('#pauseBtn');
    pauseBtn.textContent = this.#accepting ? t('server_pause') : t('server_resume');
    this.querySelector('#pauseBtn').addEventListener('click', () => this.#togglePause());
    this.querySelector('#jobs').addEventListener('dyj-cancel', (ev) => this.#cancelJob(ev.detail.id));
    this.#renderPrinters();
    this.#refreshPin();
    this.#refreshOpen();
    let lpTick = 0;
    this.#pollTimer = setInterval(() => {
      this.#refreshPin();
      this.#refreshOpen();
      this.#loadTabletHint();
      // 每 30s 重拉本机打印机：新插/新装的打印机不用切页就出现。
      if (++lpTick % 6 === 0) this.#refreshLocal();
    }, 5000);
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
      cb.checked = !p.probe_err && !p.virtual; // 虚拟打印机不出纸：必须显式勾选，绝不默认选中
      cb.dataset.name = p.name;
      row.append(cb);
      row.append(rowMeta(p.name, [p.driver,
        p.probe_err ? t('server_probe_err') + '：' + p.probe_err : ''].filter(Boolean).join(' · ')));
      if (p.virtual) {
        const v = document.createElement('span');
        v.className = 'badge virtual';
        v.textContent = t('server_virtual_badge');
        v.title = t('server_virtual_hint');
        row.append(v);
      }
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
    // 防呆：选中的全是虚拟打印机（不出纸）时，先确认再共享。
    if (checked.every((p) => p.virtual)) {
      const ok = await confirmDialog({
        title: t('server_virtual_confirm_title'),
        body: t('server_virtual_confirm_body'),
        confirmText: t('server_virtual_confirm_ok'),
        danger: true,
      });
      if (!ok) return;
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
      row.append(rowMeta(p.name, p.driver || ''));
      if (p.virtual) {
        const v = document.createElement('span');
        v.className = 'badge virtual';
        v.textContent = t('server_virtual_badge');
        v.title = t('server_virtual_hint');
        row.append(v);
      }
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
    // 防呆：关掉最后一台 = 整个共享停止，必须先确认（此前会静默退回挑选态）。
    if (!want && cur.length === 1) {
      const ok = await confirmDialog({
        title: t('server_unshare_last_title'),
        body: t('server_unshare_last_body'),
        confirmText: t('server_unshare_last_ok'),
        danger: true,
      });
      if (!ok) { cb.checked = true; return; }
    }
    // 防呆：虚拟打印机不出纸，开启时明确告知（不阻断，用户可能就是要共享 PDF）。
    if (want && p.virtual) showToast(t('server_virtual_toast'), 'error');
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
      el.title = t('server_pin_copy_hint');
      this.querySelector('#pinHint').textContent = t('server_pin_hint', { n: remain_sec ?? 0 });
    } catch {
      if (sec) sec.hidden = true; // 配对不可用（旧服务）→ 不显示
    }
  }

  // 点配对码即复制（防呆：手抄 4 位数字容易错）。
  async #copyPin() {
    const pin = this.querySelector('#pin')?.textContent || '';
    if (!/^\d{4}$/.test(pin)) return;
    try {
      await navigator.clipboard.writeText(pin);
      showToast(t('btn_copied'));
    } catch {
      showToast(t('copy_failed'), 'error');
    }
  }

  // 平板/手机连接提示（research/17）：系统"按 IP 添加"没有地址栏以外的
  // 选项，把本机地址和开放打印的当前状态直接写出来，少一轮来回试错。
  async #loadTabletHint() {
    const box = this.querySelector('#tabletHint');
    if (!box) return;
    try {
      const st = await getStatus();
      const uri = (st.uris || []).find((u) => !u.includes('127.0.0.1'));
      const m = uri && uri.match(/ipp:\/\/([^:/]+)/);
      if (!m) { box.hidden = true; return; }
      const open = st.open_printing
        ? (st.open_print_always ? t('server_tablet_open_always') : t('server_tablet_open_on'))
        : t('server_tablet_open_off');
      box.innerHTML = `<b>${t('server_tablet_title')}</b><br>
        ${t('server_tablet_body', { ip: esc(m[1]) })}<br>${open}`;
      box.hidden = false;
    } catch { box.hidden = true; }
  }

  // ---- 免认证开放打印（ADR-0014）：三种状态渲染 + 两个入口 ----

  async #refreshOpen() {
    const sec = this.querySelector('#openSec');
    if (!sec) return;
    try {
      const st = await getOpen();
      sec.hidden = false;
      this.#renderOpen(st);
    } catch {
      sec.hidden = true; // 端点不可用（旧服务/无权限）→ 整节不出现
    }
  }

  // 静默重拉本机打印机列表；失败保留旧列表（枚举抖动不该清空向导）。
  async #refreshLocal() {
    try {
      const { printers } = await getLocalPrinters();
      this.#rows = printers || [];
      if (this.#mode === 'home') this.#renderPrinters();
    } catch { /* 保留旧列表 */ }
  }

  // 开放开关走 /api/open，不经过 #cfg；操作后重拉配置快照，避免后续
  // 逐台开关/保存把陈旧的 open_print 覆盖回去（服务端对缺键也保留现值，
  // 双保险）。
  async #syncCfg() {
    try {
      const { config } = await getConfig();
      this.#cfg = config || {};
    } catch { /* 快照刷新失败不阻塞开关操作 */ }
  }

  #renderOpen({ enabled, always, remain_sec }) {
    const state = this.querySelector('#openState');
    const btns = this.querySelector('#openBtns');
    if (!state || !btns) return;
    btns.replaceChildren();
    if (enabled) {
      state.classList.add('on');
      state.textContent = always
        ? t('server_open_always_on')
        : t('server_open_on', { t: fmtRemain(remain_sec) });
      btns.append(this.#openStopBtn());
      return;
    }
    state.classList.remove('on');
    state.textContent = t('server_open_hint');
    const b10 = document.createElement('dyj-button');
    b10.setAttribute('variant', 'primary');
    b10.textContent = t('server_open_start');
    b10.addEventListener('click', async () => {
      const ok = await confirmDialog({
        title: t('server_open_start_confirm_title'),
        body: t('server_open_start_confirm_body'),
        confirmText: t('server_open_start'),
      });
      if (!ok) return;
      try {
        await openStart({ duration_sec: 600 });
      } catch (e) {
        showToast(errMessage(e), 'error');
      }
      this.#refreshOpen();
      this.#syncCfg();
    });
    const bAlways = document.createElement('dyj-button');
    bAlways.setAttribute('variant', 'secondary');
    bAlways.textContent = t('server_open_always');
    bAlways.addEventListener('click', () => this.#openAlways());
    btns.append(b10, bAlways);
  }

  #openStopBtn() {
    const b = document.createElement('dyj-button');
    b.setAttribute('variant', 'secondary');
    b.textContent = t('server_open_stop');
    b.addEventListener('click', async () => {
      const ok = await confirmDialog({
        title: t('server_open_stop_confirm_title'),
        body: t('server_open_stop_confirm_body'),
        confirmText: t('server_open_stop'),
        danger: true,
      });
      if (!ok) return;
      try {
        await openStop();
        showToast(t('server_open_closed'));
      } catch (e) {
        showToast(errMessage(e), 'error');
      }
      this.#refreshOpen();
      this.#syncCfg();
    });
    return b;
  }

  // #openAlways：常开是持久的风险选择，必须先过一次红色确认框。
  async #openAlways() {
    const ok = await confirmDialog({
      title: t('server_open_risk_title'),
      body: t('server_open_risk_body'),
      confirmText: t('server_open_risk_confirm'),
      danger: true,
    });
    if (!ok) return;
    try {
      await openStart({ always: true });
    } catch (e) {
      showToast(errMessage(e), 'error');
    }
    this.#refreshOpen();
    this.#syncCfg();
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
        row.append(rowMeta(d.name || d.id.slice(0, 8), [
          d.source_ip,
          d.added_at ? new Date(d.added_at).toLocaleString() : '',
        ].filter(Boolean).join(' · ')));
        const btn = document.createElement('dyj-button');
        btn.setAttribute('variant', 'secondary');
        btn.textContent = t('devices_revoke');
        btn.addEventListener('click', () => confirmAction({
          title: t('devices_revoke_confirm'),
          body: t('devices_revoke_confirm_body'),
          confirmText: t('devices_revoke'),
          done: t('devices_revoked'),
        }, () => revokeDevice(d.id), () => this.#loadDevices()));
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

  #cancelJob(id) {
    return confirmAction({
      title: t('cancel_confirm_title', { id }),
      body: t('cancel_confirm_body'),
      confirmText: t('btn_cancel_job'),
      done: t('toast_canceled', { id }),
      fail: t('toast_cancel_failed') + '：',
    }, () => cancelJob(id), () => this.#refreshJobs());
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
