/**
 * DyjPrefs：本地键值存储。
 *
 * 存设备身份与已配对的共享端记录（含配对令牌）。令牌只落在这份本地存储里，
 * 不写日志、不进崩溃上报。
 */

import { UITurboModule, UITurboModuleContext } from '@rnoh/react-native-openharmony';
import { TM } from '../generated/ts';
import { preferences } from '@kit.ArkData';

const STORE_NAME: string = 'dyj';

export class DyjPrefsModule extends UITurboModule implements TM.DyjPrefs.Spec {
  static readonly NAME: string = 'DyjPrefs';

  private store: preferences.Preferences | null = null;

  constructor(ctx: UITurboModuleContext) {
    super(ctx);
  }

  async get(key: string): Promise<string> {
    const store = await this.ensureStore();
    const value = await store.get(key, '');
    return typeof value === 'string' ? value : '';
  }

  async set(key: string, value: string): Promise<void> {
    const store = await this.ensureStore();
    await store.put(key, value);
    await store.flush();
  }

  async remove(key: string): Promise<void> {
    const store = await this.ensureStore();
    await store.delete(key);
    await store.flush();
  }

  private async ensureStore(): Promise<preferences.Preferences> {
    if (this.store === null) {
      const options: preferences.Options = { name: STORE_NAME };
      this.store = await preferences.getPreferences(this.ctx.uiAbilityContext, options);
    }
    return this.store;
  }
}
