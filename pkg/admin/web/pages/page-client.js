// 客户端页（#/client，v0.4 research/14 §3.4）：自动扫描局域网 → 点一台 →
// （dyj 共享时弹 4 位配对码）→ 自动装成系统队列 → 原页完成，"本机已安装"
// 列表常驻下方。列表副标只说人话（dyj 共享 / 普通网络打印机），不暴露
// host:port；已安装每行带实时状态（就绪/服务端不在线/已移除/配对被撤销）。
import {
  scanServices, pairWith, installQueue, removeQueue, testQueue,
  getAddedPrinters, getConfig, saveConfig, errMessage,
} from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import { confirmDialog } from '../components/dyj-modal.js';
import '../components/dyj-button.js';
import '../components/dyj-pin-input.js';

class DyjPageClient extends HTMLElement {
  #services = null;          // null = 扫描中
  #scanTimer = 0;
  #flow = null;              // null | {stage, picked, paired, results}
  #addedTimer = 0;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-client .body { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow); padding: 24px; }
  dyj-page-client .sub { color: var(--dyj-text-dim); margin: 0 0 10px; }
  dyj-page-client .list { display: flex; flex-direction: column; gap: 8px; margin-top: 10px; }
  dyj-page-client .row { display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 12px 14px; border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius);
    background: none; color: inherit; text-align: left; cursor: pointer; font: inherit; }
  dyj-page-client .row:hover { border-color: var(--dyj-primary); }
  dyj-page-client .row[disabled] { opacity: .45; cursor: default; }
  dyj-page-client .row.static { cursor: default; }
  dyj-page-client .row.static:hover { border-color: var(--dyj-border); }
  dyj-page-client .row .meta { flex: 1; }
  dyj-page-client .row .name { font-weight: 600; }
  dyj-page-client .row .sub2 { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-client .tag { font-size: var(--dyj-fs-s); border-radius: 99px; padding: 1px 8px;
    border: 1px solid var(--dyj-border); color: var(--dyj-text-dim); white-space: nowrap; }
  dyj-page-client .tag.dyj { border-color: var(--dyj-primary); color: var(--dyj-primary); }
  dyj-page-client .tag.ok { border-color: var(--dyj-success); color: var(--dyj-success); }
  dyj-page-client .tag.warn { border-color: var(--dyj-warning); color: var(--dyj-warning); }
  dyj-page-client .tag.bad { border-color: var(--dyj-danger); color: var(--dyj-danger); }
  dyj-page-client h2 { font-size: var(--dyj-fs-l); margin: 22px 0 6px; }
  dyj-page-client .spin { color: var(--dyj-text-dim); }
  dyj-page-client .empty { color: var(--dyj-text-dim); }
  dyj-page-client .okline { margin: 6px 0; }
  dyj-page-client .badline { margin: 6px 0; color: var(--dyj-danger); }
  dyj-page-client .nav { display: flex; gap: 10px; margin-top: 16px; flex-wrap: wrap; }
  dyj-page-client .foot { margin-top: 18px; display: flex; gap: 18px; font-size: var(--dyj-fs-s); }
  dyj-page-client .foot a { color: var(--dyj-text-dim); }
  dyj-page-client .foot a:hover { color: var(--dyj-primary); }
</style>
<div id="scanSec">
  <h1>${t('client_title')}</h1>
  <div class="body" id="scanBody"></div>
</div>
<div id="flowSec" hidden>
  <h1 id="flowTitle"></h1>
  <div class="body" id="flowBody"></div>
</div>
<div>
  <h2>${t('client_added_title')}</h2>
  <div class="body" id="addedBox"><div class="empty">${t('client_added_empty')}</div></div>
</div>
<div class="foot">
  <a href="#/server">${t('client_goto_server')}</a>
  <a href="#/help">${t('goto_help')}</a>
</div>`;
    this.#scan();
    this.#loadAdded();
    this.#addedTimer = setInterval(() => { if (!document.hidden) this.#loadAdded(); }, 15000);
  }

  disconnectedCallback() {
    clearTimeout(this.#scanTimer);
    clearInterval(this.#addedTimer);
  }

  #showFlow(on) {
    this.querySelector('#scanSec').hidden = on;
    this.querySelector('#flowSec').hidden = !on;
  }

  // ---- scan ----

  async #scan() {
    if (this.#flow) return;
    const body = this.querySelector('#scanBody');
    body.innerHTML = `<div class="spin">${t('client_scanning')}</div>`;
    try {
      const { services } = await scanServices();
      this.#services = services || [];
      if (!this.#flow) this.#renderScan();
    } catch (e) {
      this.#services = [];
      if (!this.#flow) {
        body.innerHTML = `<div class="empty">${errMessage(e)}</div>`;
      }
    }
  }

  #renderScan() {
    const body = this.querySelector('#scanBody');
    const rows = this.#services || [];
    if (!rows.length) {
      body.innerHTML = `
        <div class="empty">${t('client_none_found')}</div>
        <div class="empty">${t('client_none_hint')}</div>
        <div class="nav"><dyj-button id="rescan">${t('client_rescan')}</dyj-button></div>`;
    } else {
      body.innerHTML = `
        <p class="sub">${t('client_pick_hint')}</p>
        <div class="list" id="list"></div>
        <div class="nav"><dyj-button id="rescan">${t('client_rescan')}</dyj-button></div>`;
      const list = body.querySelector('#list');
      list.replaceChildren(...rows.map((s) => {
        const row = document.createElement('button');
        row.type = 'button';
        row.className = 'row';
        row.disabled = !s.reachable;
        const meta = document.createElement('div');
        meta.className = 'meta';
        meta.innerHTML = `<div class="name"></div><div class="sub2"></div>`;
        meta.querySelector('.name').textContent = s.name || s.instance;
        meta.querySelector('.sub2').textContent = s.reachable ? '' : t('client_unreachable');
        row.append(meta);
        const tag = document.createElement('span');
        tag.className = 'tag' + (s.dyj ? ' dyj' : '');
        tag.textContent = s.dyj
          ? (s.need_pin ? t('client_badge_dyj_pin') : t('client_badge_dyj_open'))
          : t('client_badge_plain');
        row.append(tag);
        row.addEventListener('click', () => this.#begin(s));
        return row;
      }));
    }
    body.querySelector('#rescan').addEventListener('click', () => {
      this.#services = null;
      this.#scan();
    });
  }

  // ---- flow: pair (PIN dialog) → install → done ----

  async #begin(s) {
    this.#flow = { stage: 'pick', picked: s, paired: null, results: [] };
    this.#showFlow(true);
    this.querySelector('#flowTitle').textContent = t('client_flow_title', { name: s.name || s.instance });
    const body = this.querySelector('#flowBody');
    if (s.need_pin) {
      body.innerHTML = `
        <p class="sub">${t('client_pair_hint')}</p>
        <dyj-pin-input id="pin" autofocus></dyj-pin-input>
        <p class="sub" id="msg" style="margin-top:12px"></p>
        <div class="nav">
          <dyj-button id="connect" variant="primary">${t('client_pair_connect')}</dyj-button>
          <dyj-button id="cancel">${t('modal_cancel')}</dyj-button>
        </div>`;
      const pinEl = body.querySelector('#pin');
      const msg = body.querySelector('#msg');
      const connect = async () => {
        const pin = pinEl.value;
        if (pin.length !== 4) {
          msg.textContent = t('client_pair_hint');
          pinEl.focusFirst();
          return;
        }
        msg.textContent = t('client_pairing');
        body.querySelector('#connect').setAttribute('disabled', '');
        try {
          await this.#markRole();
          const r = await pairWith({ host: s.host, port: s.port, pin, device_name: undefined });
          this.#flow.paired = r;
          await this.#install();
        } catch (e) {
          body.querySelector('#connect').removeAttribute('disabled');
          if (e?.status === 403 && e?.data?.retry_after_sec !== undefined) {
            msg.textContent = t('client_pair_locked', { n: e.data.retry_after_sec });
          } else if (e?.status === 401) {
            msg.textContent = t('client_pair_bad');
            pinEl.markBad();
            pinEl.clear();
          } else {
            msg.textContent = errMessage(e);
          }
        }
      };
      body.querySelector('#connect').addEventListener('click', connect);
      pinEl.addEventListener('dyj-complete', connect);
      body.querySelector('#cancel').addEventListener('click', () => this.#abort());
      pinEl.focusFirst();
      return;
    }
    // 免配对（dyj 开放共享 / 普通网络打印机）：直接安装。
    this.#flow.paired = {
      server_name: s.name || s.instance,
      printers: [{ name: s.name || s.instance, ipp_url: `ipp://${s.host}:${s.port}${s.path || '/ipp/print'}` }],
    };
    await this.#install();
  }

  #abort() {
    this.#flow = null;
    this.#showFlow(false);
    this.#renderScan();
  }

  // 配对/安装落定角色：下次打开直接落在本页（research/14 §3.1）。
  async #markRole() {
    try {
      const { config } = await getConfig();
      const prev = config || {};
      if (prev.role === 'share' || prev.role === 'both') return; // 已是服务端，保 both 语义
      await saveConfig({ ...prev, role: 'use' });
    } catch { /* 角色标记失败不阻塞添加 */ }
  }

  async #install() {
    const body = this.querySelector('#flowBody');
    const name = this.#flow.picked?.name || this.#flow.paired?.server_name || '';
    body.innerHTML = `<div class="spin">${t('client_installing', { name })}</div>
      <p class="sub">${t('client_may_prompt')}</p>`;
    const results = [];
    for (const p of this.#flow.paired.printers || []) {
      try {
        const out = await installQueue({
          name: p.name,
          ipp_url: p.ipp_url,
          server_name: this.#flow.paired.server_name || (this.#flow.picked?.name || ''),
          server_host: this.#flow.picked?.host || '',
          server_port: this.#flow.picked?.port || 0,
          uri_path: p.uri_path || '',
        });
        results.push({ queue_name: out.queue_name, ok: true });
      } catch (e) {
        results.push({ queue_name: p.name, ok: false, err: errMessage(e) });
      }
    }
    this.#flow.results = results;
    this.#renderDone();
    this.#loadAdded();
  }

  #renderDone() {
    const body = this.querySelector('#flowBody');
    const ok = this.#flow.results.filter((r) => r.ok);
    const bad = this.#flow.results.filter((r) => !r.ok);
    body.innerHTML = `
      ${ok.map((r) => `<p class="okline">✓ ${t('client_install_ok', { name: r.queue_name })}</p>`).join('')}
      ${bad.map((r) => `<p class="badline">✗ ${t('client_install_fail')}「${r.queue_name}」：${r.err}</p>`).join('')}
      <div class="nav">
        ${ok.map((r) => `<dyj-button data-test="${r.queue_name}" variant="secondary">${t('client_test_page')}</dyj-button>`).join('')}
        <dyj-button id="done" variant="primary">${t('client_done')}</dyj-button>
      </div>`;
    body.querySelectorAll('[data-test]').forEach((btn) => {
      btn.addEventListener('click', async () => {
        btn.textContent = t('client_testing');
        try {
          await testQueue({ queue_name: btn.dataset.test });
          showToast(t('client_test_ok'));
        } catch (e) { showToast(errMessage(e), 'error'); }
        btn.textContent = t('client_test_page');
      });
    });
    body.querySelector('#done').addEventListener('click', () => this.#abort());
  }

  // ---- added list (本机已安装，带实时状态) ----

  async #loadAdded() {
    const box = this.querySelector('#addedBox');
    if (!box) return;
    try {
      const { printers } = await getAddedPrinters();
      const list = printers || [];
      if (!list.length) {
        box.innerHTML = `<div class="empty">${t('client_added_empty')}</div>`;
        return;
      }
      box.innerHTML = `<div class="list" id="alist"></div>`;
      const alist = box.querySelector('#alist');
      alist.replaceChildren(...list.map((p) => {
        const row = document.createElement('div');
        row.className = 'row static';
        const meta = document.createElement('div');
        meta.className = 'meta';
        meta.innerHTML = `<div class="name"></div><div class="sub2"></div>`;
        meta.querySelector('.name').textContent = p.queue_name;
        meta.querySelector('.sub2').textContent = p.server_name || '';
        row.append(meta);
        const tag = document.createElement('span');
        const key = { ok: 'client_status_ok', offline: 'client_status_offline',
          missing: 'client_status_missing', revoked: 'client_status_revoked' }[p.status]
          || 'client_status_unknown';
        const tone = { ok: 'ok', offline: 'warn', missing: 'warn', revoked: 'bad' }[p.status] || 'warn';
        tag.className = 'tag ' + tone;
        tag.textContent = t(key);
        row.append(tag);
        const rm = document.createElement('dyj-button');
        rm.setAttribute('variant', 'secondary');
        rm.textContent = t('client_remove');
        rm.addEventListener('click', async () => {
          const yes = await confirmDialog({
            title: t('client_remove_confirm'),
            body: t('client_remove_confirm_body', { name: p.queue_name }),
            confirmText: t('client_remove'),
            danger: true,
          });
          if (!yes) return;
          try {
            await removeQueue({ queue_name: p.queue_name });
            showToast(t('client_removed', { name: p.queue_name }));
            this.#loadAdded();
          } catch (e) { showToast(errMessage(e), 'error'); }
        });
        row.append(rm);
        return row;
      }));
    } catch { box.innerHTML = `<div class="empty">${t('client_added_empty')}</div>`; }
  }
}

customElements.define('dyj-page-client', DyjPageClient);
