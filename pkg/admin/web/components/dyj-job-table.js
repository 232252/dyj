// dyj-job-table：作业表。状态点+文字、相对时间（服务端无创建时间，记录本页
// 首见时间）、行内“取消”仅 pending/held/processing 显示；空列表显示空态行。
// 取消点击发 dyj-cancel {id}，由页面确认后调 API。
import { t } from '../i18n/index.js';

const seen = new Map(); // job id → 本页首见时刻
const ACTIVE = new Set(['pending', 'held', 'processing']);
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function ageText(ms) {
  const s = Math.max(0, Math.floor((Date.now() - ms) / 1000));
  if (s < 60) return t('age_now');
  const m = Math.floor(s / 60);
  if (m < 60) return t('age_min', { n: m });
  return t('age_hour', { n: Math.floor(m / 60) });
}

class DyjJobTable extends HTMLElement {
  #jobs = [];

  constructor() { super(); this.attachShadow({ mode: 'open' }); }

  set jobs(list) {
    this.#jobs = Array.isArray(list) ? list : [];
    const now = Date.now();
    for (const j of this.#jobs) if (!seen.has(j.id)) seen.set(j.id, now);
    for (const id of [...seen.keys()]) {
      if (!this.#jobs.some((j) => j.id === id)) seen.delete(id);
    }
    this.#render();
  }

  get jobs() { return this.#jobs.slice(); }

  #render() {
    const th = ['#', 'col_name', 'col_user', 'col_state', 'col_size', 'col_copies', 'col_age', '']
      .map((k, i) => `<th scope="col">${k === '#' ? '#' : t(k)}</th>`).join('');
    const rows = this.#jobs.length ? this.#jobs.map((j) => {
      const stateKey = 'state_' + (j.state_name || 'pending');
      const reasons = (j.reasons || []).join(', ');
      return `<tr>
        <td>${j.id}</td>
        <td class="nm" title="${esc(j.name)}">${esc(j.name)}</td>
        <td>${esc(j.user || '—')}</td>
        <td><span class="st st-${esc(j.state_name)}" title="${esc(reasons)}"><span class="dot"></span>${t(stateKey)}</span></td>
        <td>${t('size_kb', { n: j.size_kb ?? 0 })}</td>
        <td>${j.copies ?? 1}</td>
        <td>${ageText(seen.get(j.id) ?? Date.now())}</td>
        <td>${ACTIVE.has(j.state_name) ? `<button type="button" class="cancel" data-id="${j.id}">${t('btn_cancel_job')}</button>` : ''}</td>
      </tr>`;
    }).join('') : `<tr><td colspan="8" class="empty">${t('empty_jobs_body')}</td></tr>`;

    this.shadowRoot.innerHTML = `
<style>
  :host { display: block; overflow-x: auto; background: var(--dyj-card);
    border: 1px solid var(--dyj-border); border-radius: var(--dyj-radius); }
  :host([hidden]) { display: none; }
  table { width: 100%; border-collapse: collapse; font-size: var(--dyj-fs-m); }
  th { text-align: left; font-size: var(--dyj-fs-s); color: var(--dyj-text-dim);
    font-weight: 600; padding: 8px 10px; white-space: nowrap;
    border-bottom: 1px solid var(--dyj-border); }
  td { padding: 10px; border-bottom: 1px solid var(--dyj-border); vertical-align: middle; }
  tr:last-child td { border-bottom: none; }
  .nm { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .st { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--dyj-text-dim); flex: none; }
  .st-completed .dot { background: var(--dyj-success); }
  .st-processing .dot { background: var(--dyj-primary); }
  .st-pending .dot, .st-held .dot { background: var(--dyj-warning); }
  .st-canceled .dot, .st-aborted .dot { background: var(--dyj-danger); }
  .cancel { font: inherit; font-size: var(--dyj-fs-s); color: var(--dyj-danger);
    background: none; border: 1px solid var(--dyj-border); border-radius: 6px;
    padding: 4px 10px; cursor: pointer; transition: border-color .15s; }
  .cancel:hover { border-color: var(--dyj-danger); }
  .cancel:focus-visible { outline: 2px solid var(--dyj-danger); outline-offset: 1px; }
  .empty { text-align: center; color: var(--dyj-text-dim); padding: 28px 0; }
</style>
<table><thead><tr>${th}</tr></thead><tbody>${rows}</tbody></table>`;

    this.shadowRoot.querySelectorAll('.cancel').forEach((b) => {
      b.addEventListener('click', () => {
        this.dispatchEvent(new CustomEvent('dyj-cancel', { detail: { id: Number(b.dataset.id) }, bubbles: true }));
      });
    });
  }
}

customElements.define('dyj-job-table', DyjJobTable);
