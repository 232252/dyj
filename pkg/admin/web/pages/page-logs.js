// 日志页：dyj-log-viewer（SSE）+ 局域网令牌提示。
import { t } from '../i18n/index.js';
import '../components/dyj-log-viewer.js';
import '../components/dyj-icon.js';

class DyjPageLogs extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-logs .hint { display: flex; align-items: center; gap: 6px;
    color: var(--dyj-text-dim); font-size: var(--dyj-fs-s); margin: 0 0 12px; }
  dyj-page-logs .hint dyj-icon { width: 13px; height: 13px; flex: none; }
</style>
<h1>${t('page_logs_title')}</h1>
<p class="hint"><dyj-icon name="info"></dyj-icon>${t('logs_token_hint')}</p>
<dyj-log-viewer></dyj-log-viewer>`;
  }
}

customElements.define('dyj-page-logs', DyjPageLogs);
