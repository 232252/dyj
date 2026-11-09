/**
 * DyjPrefs：本地键值存储。
 *
 * 存设备身份与已配对的共享端记录（含配对令牌）。令牌只落在这份本地存储里，
 * 不写日志、不进崩溃上报。
 */

import type {TurboModule} from 'react-native';
import {TurboModuleRegistry} from 'react-native';

export interface Spec extends TurboModule {
  /** 读一个键；没有则返回空串。 */
  get(key: string): Promise<string>;
  /** 写一个键并落盘。 */
  set(key: string, value: string): Promise<void>;
  /** 删一个键。 */
  remove(key: string): Promise<void>;
}

export default TurboModuleRegistry.getEnforcing<Spec>('DyjPrefs');
