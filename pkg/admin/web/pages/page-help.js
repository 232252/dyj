// 帮助页（#/help，v0.4 research/14 §3.5）：全产品唯一放技术信息的地方。
// 常见问题（人话）→ 运行自检（doctor）→ 导出诊断包 → 日志（折叠只读）→
// 关于（版本/角色/服务端口，只在此处可见）。其余界面永不出现工程术语。
import { getDiagnostics, exportDiag, getStatus, getConfig, errMessage, esc } from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import '../components/dyj-diagnostic.js';
import '../components/dyj-disclosure.js';
import '../components/dyj-log-viewer.js';
import '../components/dyj-button.js';

const ROLE_KEY = { share: 'role_share', use: 'role_use', both: 'role_both', '': 'role_none' };

class DyjPageHelp extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-help .card { background: var(--dyj-card); border: 1px solid var(--dyj-border);
    border-radius: var(--dyj-radius); box-shadow: var(--dyj-shadow); padding: 18px 24px; }
  dyj-page-help section { margin-bottom: 16px; }
  dyj-page-help h2 { font-size: var(--dyj-fs-l); margin: 0 0 8px; }
  dyj-page-help .dim { color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); margin: 4px 0 10px; }
  dyj-page-help .qa { margin: 8px 0; }
  dyj-page-help .qa b { display: block; }
  dyj-page-help .qa p { margin: 2px 0 0; color: var(--dyj-text-dim); }
  dyj-page-help .sum { margin: 8px 0 0; color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); }
</style>
<h1>${t('help_title')}</h1>

<section class="card">
  <h2>${t('help_faq_title')}</h2>
  <div class="qa"><b>${t('help_faq_scan_q')}</b><p>${t('help_faq_scan_a')}</p></div>
  <div class="qa"><b>${t('help_faq_pin_q')}</b><p>${t('help_faq_pin_a')}</p></div>
  <div class="qa"><b>${t('help_faq_print_q')}</b><p>${t('help_faq_print_a')}</p></div>
</section>

<section class="card">
  <h2>${t('help_check_title')}</h2>
  <div class="dim">${t('help_check_hint')}</div>
  <div id="checks"><div class="dim">${t('help_check_running')}</div></div>
  <div class="sum" id="sum"></div>
  <div class="nav" style="margin-top:10px">
    <dyj-button id="recheck" variant="secondary">${t('help_check_btn')}</dyj-button>
  </div>
</section>

<section class="card">
  <h2>${t('help_diag_title')}</h2>
  <div class="dim">${t('help_diag_hint')}</div>
  <dyj-button id="diagBtn" variant="secondary">${t('help_diag_btn')}</dyj-button>
</section>

<section>
  <dyj-disclosure summary="${t('help_logs_title')}">
    <dyj-log-viewer></dyj-log-viewer>
  </dyj-disclosure>
</section>

<section>
  <dyj-disclosure summary="${t('help_about_title')}">
    <div class="dim" id="about" style="margin:0"></div>
  </dyj-disclosure>
</section>`;
    this.querySelector('#recheck').addEventListener('click', () => this.#runChecks());
    this.querySelector('#diagBtn').addEventListener('click', () => this.#export());
    this.#runChecks();
    this.#loadAbout();
  }

  async #runChecks() {
    const box = this.querySelector('#checks');
    const sum = this.querySelector('#sum');
    if (!box) return;
    box.innerHTML = `<div class="dim">${t('help_check_running')}</div>`;
    if (sum) sum.textContent = '';
    try {
      const { checks } = await getDiagnostics();
      const list = checks || [];
      box.replaceChildren(...list.map((c) => {
        const d = document.createElement('dyj-diagnostic');
        d.check = c;
        return d;
      }));
      const bad = list.filter((c) => c.level === 'fail').length;
      const warn = list.filter((c) => c.level === 'warn').length;
      if (sum) sum.textContent = t('help_check_summary', { total: list.length, bad, warn });
    } catch (e) {
      box.innerHTML = `<div class="dim">${esc(errMessage(e))}</div>`;
    }
  }

  async #export() {
    const btn = this.querySelector('#diagBtn');
    btn.setAttribute('loading', '');
    try {
      const name = await exportDiag();
      showToast(t('help_diag_ok') + (name ? `（${name}）` : ''));
    } catch (e) {
      showToast(t('help_diag_fail') + '：' + errMessage(e), 'error');
    } finally {
      btn.removeAttribute('loading');
    }
  }

  async #loadAbout() {
    const el = this.querySelector('#about');
    if (!el) return;
    const parts = [];
    try {
      const s = await getStatus();
      if (s?.version) parts.push(t('about_version', { v: s.version }));
    } catch { /* 状态不可得就少一行 */ }
    try {
      const { config } = await getConfig();
      const role = config?.role || '';
      parts.push(t('about_role', { r: t(ROLE_KEY[role] || 'role_none') }));
      if (role === 'share' || role === 'both') {
        const n = (config?.printers || []).length;
        parts.push(t('about_shared_n', { n }));
      }
      if (config?.port) parts.push(t('about_port', { p: config.port }));
    } catch { /* 同上 */ }
    el.textContent = parts.join(' · ');
  }
}

customElements.define('dyj-page-help', DyjPageHelp);
