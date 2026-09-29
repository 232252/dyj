// 作业页：进行中/已完成/全部切换 + dyj-job-table + 30 秒自动刷新（页面
// 隐藏时跳过）；取消作业先 modal 确认再调 API，toast 反馈。
import { getJobs, cancelJob, errMessage } from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import { confirmDialog } from '../components/dyj-modal.js';
import '../components/dyj-job-table.js';
import '../components/dyj-empty-state.js';
import '../components/dyj-button.js';

const WHICH = [
  ['not-completed', 'which_not_completed'],
  ['completed', 'which_completed'],
  ['all', 'which_all'],
];

class DyjPageJobs extends HTMLElement {
  #which = 'not-completed';
  #timer = 0;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-jobs .seg { display: inline-flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; }
  dyj-page-jobs .head { display: flex; align-items: center; justify-content: space-between;
    gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
  dyj-page-jobs .left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
</style>
<h1>${t('page_jobs_title')}</h1>
<div class="head">
  <div class="left">
    <div class="seg" role="group" aria-label="${t('page_jobs_title')}">
      ${WHICH.map(([v, k]) => `<dyj-button data-which="${v}">${t(k)}</dyj-button>`).join('')}
    </div>
    <span class="dim">${t('jobs_auto_refresh')}</span>
  </div>
  <dyj-button id="refresh" variant="secondary">${t('btn_refresh')}</dyj-button>
</div>
<dyj-job-table id="table" hidden></dyj-job-table>
<dyj-empty-state id="empty" icon="files" text="${t('empty_jobs_title')}" hidden>
  <dyj-button slot="action" variant="secondary">${t('btn_refresh')}</dyj-button>
</dyj-empty-state>`;
    this.querySelectorAll('.seg dyj-button').forEach((b) => {
      b.addEventListener('click', () => {
        this.#which = b.dataset.which;
        this.#syncSeg();
        this.#load();
      });
    });
    this.querySelector('#refresh').addEventListener('click', () => this.#load());
    this.querySelector('#empty [slot=action]').addEventListener('click', () => this.#load());
    this.querySelector('#table').addEventListener('dyj-cancel', (ev) => this.#cancel(ev.detail.id));
    this.#syncSeg();
    this.#load();
    this.#timer = setInterval(() => { if (!document.hidden) this.#load(); }, 30000);
  }

  disconnectedCallback() { clearInterval(this.#timer); }

  #syncSeg() {
    this.querySelectorAll('.seg dyj-button').forEach((b) => {
      b.toggleAttribute('variant', false);
      b.setAttribute('variant', b.dataset.which === this.#which ? 'primary' : 'secondary');
    });
  }

  async #load() {
    try {
      const { jobs } = await getJobs(this.#which);
      const has = jobs.length > 0;
      const table = this.querySelector('#table');
      const empty = this.querySelector('#empty');
      table.hidden = !has;
      empty.hidden = has;
      if (has) table.jobs = jobs;
    } catch (e) {
      showToast(errMessage(e), 'error');
    }
  }

  async #cancel(id) {
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
      this.#load();
    } catch (e) {
      showToast(errMessage(e), 'error');
    }
  }
}

customElements.define('dyj-page-jobs', DyjPageJobs);
