// 网络设置页：端口 / TLS 端口 / mDNS 开关 / 主机名；“监听接口”收进 dyj-
// disclosure 高级。改端口或关 mDNS 前先 dyj-modal 确认，再 PUT config。
import { getConfig, saveConfig, errMessage } from '../api.js';
import { t } from '../i18n/index.js';
import { showToast } from '../components/dyj-toast.js';
import { confirmDialog } from '../components/dyj-modal.js';
import '../components/dyj-card.js';
import '../components/dyj-textfield.js';
import '../components/dyj-toggle.js';
import '../components/dyj-disclosure.js';
import '../components/dyj-button.js';

class DyjPageNetwork extends HTMLElement {
  #cfg = null;
  #origNoMdns = false;

  connectedCallback() {
    this.innerHTML = `
<style>
  dyj-page-network .form { display: flex; flex-direction: column; gap: 16px; max-width: 520px; }
  dyj-page-network .mdns-row { display: flex; flex-direction: column; gap: 4px; }
  dyj-page-network .save-row { display: flex; align-items: center; gap: 12px; }
  dyj-page-network dyj-button { align-self: flex-start; }
</style>
<h1>${t('page_network_title')}</h1>
<dyj-card>
  <div class="form">
    <dyj-textfield id="port" type="number" min="1" max="65535" label="${t('field_port')}"></dyj-textfield>
    <p class="dim">${t('field_port_desc')}</p>
    <dyj-textfield id="tls" type="number" min="0" max="65535" label="${t('field_tls')}"></dyj-textfield>
    <p class="dim">${t('field_tls_desc')}</p>
    <div class="mdns-row">
      <dyj-toggle id="mdns" label="${t('field_mdns')}">${t('field_mdns')}</dyj-toggle>
      <p class="dim">${t('field_mdns_desc')}</p>
    </div>
    <dyj-textfield id="hostname" label="${t('field_hostname')}"></dyj-textfield>
    <p class="dim">${t('field_hostname_desc')}</p>
    <dyj-disclosure summary="${t('adv_title')}">
      <dyj-textfield id="ifaces" label="${t('field_ifaces')}"></dyj-textfield>
      <p class="dim">${t('field_ifaces_desc')}</p>
    </dyj-disclosure>
    <div class="save-row">
      <dyj-button id="save" variant="primary">${t('btn_save')}</dyj-button>
      <span class="dim">${t('save_restart_hint')}</span>
    </div>
  </div>
</dyj-card>`;
    this.querySelector('#save').addEventListener('click', () => this.#save());
    this.#load();
  }

  async #load() {
    try {
      const { config } = await getConfig();
      this.#cfg = config || {};
      this.querySelector('#port').value = this.#cfg.port ?? '';
      this.querySelector('#tls').value = this.#cfg.tls_port ?? 0;
      this.#origNoMdns = !!this.#cfg.no_mdns;
      this.querySelector('#mdns').checked = !this.#cfg.no_mdns;
      this.querySelector('#hostname').value = this.#cfg.hostname || '';
      this.querySelector('#ifaces').value = this.#cfg.ifaces || '';
    } catch (e) {
      showToast(errMessage(e), 'error');
    }
  }

  async #save() {
    const portEl = this.querySelector('#port');
    const tlsEl = this.querySelector('#tls');
    const port = parseInt(portEl.value, 10);
    const tls = parseInt(tlsEl.value, 10);
    portEl.error = Number.isInteger(port) && port >= 1 && port <= 65535 ? '' : t('err_port');
    tlsEl.error = Number.isInteger(tls) && tls >= 0 && tls <= 65535 ? '' : t('err_tls');
    if (portEl.error || tlsEl.error) return;

    const mdnsOn = this.querySelector('#mdns').checked;
    if (port !== this.#cfg.port) {
      if (!(await confirmDialog({
        title: t('port_confirm_title'), body: t('port_confirm_body'),
        confirmText: t('btn_save'), danger: true,
      }))) return;
    }
    if (!this.#origNoMdns && !mdnsOn) {
      if (!(await confirmDialog({
        title: t('mdns_confirm_title'), body: t('mdns_confirm_body'),
        confirmText: t('btn_save'), danger: true,
      }))) return;
    }

    const btn = this.querySelector('#save');
    btn.setAttribute('loading', '');
    const body = {
      ...this.#cfg,
      port,
      tls_port: tls,
      no_mdns: !mdnsOn,
      hostname: this.querySelector('#hostname').value.trim(),
      ifaces: this.querySelector('#ifaces').value.trim(),
    };
    try {
      await saveConfig(body);
      this.#cfg = body;
      this.#origNoMdns = !mdnsOn;
      localStorage.setItem('dyj_seen_wizard', '1');
      showToast(t('toast_saved'));
    } catch (e) {
      showToast(errMessage(e), 'error');
    } finally {
      btn.removeAttribute('loading');
    }
  }
}

customElements.define('dyj-page-network', DyjPageNetwork);
