// 接收端向导（#/add）：扫描局域网 → 选中 → （dyj 共享输入 4 位码配对）→
// 自动安装系统打印队列 → 测试页。同时管理"本机已添加"列表。
import {
  scanServices, pairWith, installQueue, removeQueue, testQueue, getAddedPrinters,
  getConfig, saveConfig, errMessage,
} from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import { confirmDialog } from '../components/dyj-modal.js';
import '../components/dyj-stepper.js';
import '../components/dyj-button.js';
import '../components/dyj-textfield.js';
import '../components/dyj-icon.js';

class DyjPageAdd extends HTMLElement {
  #cur = 0;
  #services = null;   // null = scanning
  #picked = null;     // chosen scanEntry
  #paired = null;     // pair response (printers with ipp_url)
  #installed = [];    // {queue_name, ok, err}
  #scanTimer = 0;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-add .body { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow); padding: 24px; }
  dyj-page-add .list { display: flex; flex-direction: column; gap: 8px; margin-top: 12px; }
  dyj-page-add .row { display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 12px 14px; border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius);
    background: none; color: inherit; text-align: left; cursor: pointer; font: inherit; }
  dyj-page-add .row:hover { border-color: var(--dyj-primary); }
  dyj-page-add .row[disabled] { opacity: .45; cursor: default; }
  dyj-page-add .row .meta { flex: 1; }
  dyj-page-add .row .name { font-weight: 600; }
  dyj-page-add .row .sub { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
  dyj-page-add .tag { font-size: var(--dyj-fs-s); border-radius: 99px; padding: 1px 8px;
    border: 1px solid var(--dyj-border); color: var(--dyj-text-dim); }
  dyj-page-add .tag.dyj { border-color: var(--dyj-primary); color: var(--dyj-primary); }
  dyj-page-add .spin { color: var(--dyj-text-dim); }
  dyj-page-add .empty { color: var(--dyj-text-dim); }
  dyj-page-add .pinbox { display: flex; gap: 12px; align-items: flex-end; margin-top: 14px; }
  dyj-page-add .pin { font-size: 40px; font-weight: 700; letter-spacing: .3em; margin: 8px 0; }
  dyj-page-add .install-ok { color: var(--dyj-text); }
  dyj-page-add .install-err { color: var(--dyj-danger, #c0392b); }
  dyj-page-add .hint { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); max-width: 42em; }
</style>
<h1>${t('add_title')}</h1>
<dyj-stepper id="stepper"></dyj-stepper>
<div class="body" id="body"></div>
<div style="display:flex; gap:10px; margin-top:18px" id="nav"></div>`;
    const steps = [t('add_step_scan'), t('add_step_pair'), t('add_step_install'), t('add_step_done')];
    const st = this.querySelector('#stepper');
    st.steps = steps;
    st.addEventListener('dyj-goto', (ev) => { if (ev.detail.index < this.#cur) this.#show(ev.detail.index); });
    this.#show(0);
    this.#scan();
  }

  disconnectedCallback() { clearTimeout(this.#scanTimer); }

  #show(i) {
    this.#cur = i;
    this.querySelector('#stepper').step = i;
    const body = this.querySelector('#body');
    const nav = this.querySelector('#nav');
    nav.replaceChildren();
    if (i === 0) this.#renderScan(body, nav);
    else if (i === 1) this.#renderPair(body, nav);
    else if (i === 2) this.#renderInstalling(body);
    else this.#renderDone(body, nav);
  }

  async #scan() {
    try {
      const { services } = await scanServices();
      this.#services = services || [];
      // Re-render through #show so the nav is rebuilt exactly once (a
      // retry timer may have raced the first fetch).
      if (this.#cur === 0) this.#show(0);
    } catch (e) {
      this.#services = [];
      if (this.#cur === 0) {
        this.querySelector('#body').innerHTML = `<div class="empty">${errMessage(e)}</div>`;
      }
    }
  }

  #renderScan(body, nav) {
    const added = document.createElement('div');
    if (this.#services === null) {
      body.innerHTML = `<div class="spin">${t('add_scanning')}</div>`;
      clearTimeout(this.#scanTimer);
      this.#scanTimer = setTimeout(() => this.#scan(), 3000);
      return;
    }
    const rows = this.#services;
    if (!rows.length) {
      body.innerHTML = `
        <div class="empty">${t('add_none_found')}</div>
        <div class="empty">${t('add_none_hint')}</div>`;
      const btn = document.createElement('dyj-button');
      btn.textContent = t('add_rescan');
      btn.addEventListener('click', () => { this.#services = null; this.#show(0); this.#scan(); });
      nav.append(btn);
    } else {
      body.innerHTML = `<p class="spin">${t('add_pick_hint')}</p><div class="list" id="list"></div>`;
      const list = this.querySelector('#list');
      list.replaceChildren(...rows.map((s) => {
        const row = document.createElement('button');
        row.type = 'button';
        row.className = 'row';
        row.disabled = !s.reachable;
        const meta = document.createElement('div');
        meta.className = 'meta';
        meta.innerHTML = `<div class="name"></div><div class="sub"></div>`;
        meta.querySelector('.name').textContent = s.name || s.instance;
        meta.querySelector('.sub').textContent = `${s.host}:${s.port}${s.reachable ? '' : ' · ' + t('add_unreachable')}`;
        row.append(meta);
        if (s.dyj) {
          const tag = document.createElement('span');
          tag.className = 'tag dyj';
          tag.textContent = t('add_dyj_badge');
          row.append(tag);
        }
        const tag2 = document.createElement('span');
        tag2.className = 'tag';
        tag2.textContent = s.need_pin ? t('add_need_pin') : t('add_no_pin');
        row.append(tag2);
        row.addEventListener('click', () => { this.#picked = s; this.#show(1); });
        return row;
      }));
      const btn = document.createElement('dyj-button');
      btn.textContent = t('add_rescan');
      btn.addEventListener('click', () => { this.#services = null; this.#show(0); this.#scan(); });
      nav.append(btn);
    }
    // 已添加管理（每一步 0 都展示）
    added.style.marginTop = '22px';
    added.innerHTML = `<h2></h2><div class="list"></div>`;
    added.querySelector('h2').textContent = t('add_added_title');
    body.append(added);
    const box = added.querySelector('.list');
    getAddedPrinters().then(({ printers }) => {
      if (!printers?.length) {
        box.innerHTML = `<div class="empty">${t('add_added_empty')}</div>`;
        return;
      }
      box.replaceChildren(...printers.map((p) => {
        const row = document.createElement('div');
        row.className = 'row';
        row.style.cursor = 'default';
        const meta = document.createElement('div');
        meta.className = 'meta';
        meta.innerHTML = `<div class="name"></div><div class="sub"></div>`;
        meta.querySelector('.name').textContent = p.queue_name;
        meta.querySelector('.sub').textContent = [p.server_name, `${p.server_host}:${p.server_port}`].filter(Boolean).join(' · ');
        row.append(meta);
        const rm = document.createElement('dyj-button');
        rm.setAttribute('variant', 'secondary');
        rm.textContent = t('add_remove');
        rm.addEventListener('click', async () => {
          const ok = await confirmDialog({
            title: t('add_remove_confirm'),
            body: t('add_remove_confirm_body').replace('{name}', p.queue_name),
            confirmText: t('devices_revoke'),
            danger: true,
          });
          if (!ok) return;
          try {
            await removeQueue({ queue_name: p.queue_name });
            showToast(t('add_removed').replace('{name}', p.queue_name));
            this.#show(0);
          } catch (e) { showToast(errMessage(e), 'error'); }
        });
        row.append(rm);
        return row;
      }));
    }).catch(() => { added.hidden = true; });
  }

  #renderPair(body, nav) {
    const s = this.#picked;
    if (!s) { this.#show(0); return; }
    if (!s.need_pin) {
      // open IPP printer: install straight against its advertised path
      this.#paired = {
        server_name: s.name || s.instance,
        printers: [{ name: s.name || s.instance, ipp_url: `ipp://${s.host}:${s.port}${s.path || '/ipp/print'}` }],
      };
      this.#show(2);
      return;
    }
    body.innerHTML = `
      <h2></h2>
      <div class="sub" style="color:var(--dyj-text-dim)"></div>
      <div class="pinbox">
        <dyj-textfield id="pin" label="${t('add_pin_label')}" placeholder="${t('add_pin_placeholder')}"></dyj-textfield>
        <dyj-button id="pair" variant="primary">${t('add_pin_pair')}</dyj-button>
      </div>
      <p class="hint" id="msg"></p>`;
    body.querySelector('h2').textContent = s.name || s.instance;
    body.querySelector('.sub').textContent = `${s.host}:${s.port}`;
    const go = async () => {
      const pinEl = this.querySelector('#pin');
      const pin = (pinEl?.value || pinEl?.shadowRoot?.querySelector('input')?.value || '').trim();
      const msg = this.querySelector('#msg');
      if (!/^\\d{4}$/.test(pin)) { msg.textContent = t('add_pin_placeholder'); return; }
      msg.textContent = t('add_pairing');
      try {
        // mark the role as receiving (both if this machine also shares)
        const { config } = await getConfig().catch(() => ({ config: {} }));
        const prev = config || {};
        const next = { ...prev, role: prev.role === 'share' || prev.role === 'both' ? 'both' : 'use' };
        await saveConfig(next).catch(() => {});
        const r = await pairWith({ host: s.host, port: s.port, pin, device_name: undefined });
        this.#paired = r;
        this.#show(2);
      } catch (e) {
        if (e?.status === 403 && e?.data?.retry_after_sec !== undefined) {
          msg.textContent = t('add_pair_locked').replace('{n}', e.data.retry_after_sec);
        } else if (e?.status === 401) {
          msg.textContent = t('add_pair_bad');
        } else {
          msg.textContent = errMessage(e);
        }
      }
    };
    this.querySelector('#pair').addEventListener('click', go);
  }

  async #renderInstalling(body) {
    body.innerHTML = `<div class="spin">${t('add_installing')}</div><p class="hint">${t('add_may_prompt')}</p>`;
    const s = this.#picked;
    const r = this.#paired;
    const results = [];
    for (const p of r.printers || []) {
      try {
        const out = await installQueue({
          name: p.name,
          ipp_url: p.ipp_url,
          server_name: r.server_name || (s?.name || ''),
          server_host: s?.host || '',
          server_port: s?.port || 0,
          uri_path: p.uri_path || '',
        });
        results.push({ queue_name: out.queue_name, ok: true });
      } catch (e) {
        results.push({ queue_name: p.name, ok: false, err: errMessage(e) });
      }
    }
    this.#installed = results;
    this.#show(3);
  }

  #renderDone(body, nav) {
    const ok = this.#installed.filter((r) => r.ok);
    const bad = this.#installed.filter((r) => !r.ok);
    body.innerHTML = '<div id="rows"></div>';
    const rows = this.querySelector('#rows');
    rows.replaceChildren(...this.#installed.map((r) => {
      const div = document.createElement('div');
      div.className = r.ok ? 'install-ok' : 'install-err';
      div.textContent = r.ok
        ? t('add_install_ok').replace('{name}', r.queue_name)
        : `${t('add_install_fail')}「${r.queue_name}」：${r.err}`;
      return div;
    }));
    const hint = document.createElement('p');
    hint.className = 'hint';
    hint.textContent = t('add_test_hint');
    rows.append(hint);
    for (const r of ok) {
      const btn = document.createElement('dyj-button');
      btn.setAttribute('variant', 'secondary');
      btn.textContent = `${t('add_test_page')}：${r.queue_name}`;
      btn.addEventListener('click', async (ev) => {
        const b = ev.currentTarget;
        b.textContent = t('add_testing');
        try {
          await testQueue({ queue_name: r.queue_name });
          showToast(t('add_test_ok'));
        } catch (e) { showToast(errMessage(e), 'error'); }
        b.textContent = `${t('add_test_page')}：${r.queue_name}`;
      });
      nav.append(btn);
    }
    const fin = document.createElement('dyj-button');
    fin.setAttribute('variant', 'primary');
    fin.textContent = t('add_finish');
    fin.addEventListener('click', () => { location.hash = '#/status'; });
    nav.append(fin);
    if (!ok.length && bad.length) {
      const back = document.createElement('dyj-button');
      back.textContent = t('add_rescan');
      back.addEventListener('click', () => { this.#services = null; this.#show(0); this.#scan(); });
      nav.prepend(back);
    }
  }
}

customElements.define('dyj-page-add', DyjPageAdd);
