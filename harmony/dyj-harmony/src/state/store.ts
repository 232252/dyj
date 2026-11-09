/**
 * 本地持久化。
 *
 * 只存三样：设备身份、已配对的共享端、界面偏好。
 *
 * 配对令牌存在这里——打印路径里要用它（/p/<令牌>/…）。这份数据不要往外传，
 * 也不要打进日志（仓库约束：令牌与配对码不落日志）。
 */

import {getPref, removePref, setPref} from '../native';
import type {SharedPrinter} from '../client/pair';

const KEY_DEVICE_ID = 'device.id';
const KEY_DEVICE_NAME = 'device.name';
const KEY_HOSTS = 'paired.hosts';

/** 一个已配对的共享端。 */
export interface PairedHost {
  /** 地址（host:port），用来在发现列表里认出「已经连过」。 */
  address: string;
  /** 共享端名字。 */
  name: string;
  /** 配对令牌。 */
  token: string;
  /** 可打印的打印机。 */
  printers: SharedPrinter[];
}

/** 设备身份：服务端用它把令牌绑到这台设备上。 */
export async function deviceId(): Promise<string> {
  const existing = await getPref(KEY_DEVICE_ID);
  if (existing) {
    return existing;
  }
  const generated = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  await setPref(KEY_DEVICE_ID, generated);
  return generated;
}

/** 设备名字：显示在共享端的界面上。 */
export async function deviceName(): Promise<string> {
  const existing = await getPref(KEY_DEVICE_NAME);
  if (existing) {
    return existing;
  }
  const fallback = 'dyj 手机';
  await setPref(KEY_DEVICE_NAME, fallback);
  return fallback;
}

/** 改设备名字。 */
export async function setDeviceName(name: string): Promise<void> {
  await setPref(KEY_DEVICE_NAME, name.trim());
}

/** 已配对的共享端列表。 */
export async function pairedHosts(): Promise<PairedHost[]> {
  const raw = await getPref(KEY_HOSTS);
  if (!raw) {
    return [];
  }
  try {
    const parsed = JSON.parse(raw) as PairedHost[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

/** 记下一个已配对的共享端；同一地址覆盖旧记录。 */
export async function saveHost(host: PairedHost): Promise<void> {
  const hosts = await pairedHosts();
  const rest = hosts.filter(h => h.address !== host.address);
  await setPref(KEY_HOSTS, JSON.stringify([...rest, host]));
}

/** 忘掉一个共享端（令牌也一起丢）。 */
export async function forgetHost(address: string): Promise<void> {
  const hosts = await pairedHosts();
  await setPref(KEY_HOSTS, JSON.stringify(hosts.filter(h => h.address !== address)));
}

/** 找出某个地址对应的配对记录。 */
export async function findHost(address: string): Promise<PairedHost | undefined> {
  const hosts = await pairedHosts();
  return hosts.find(h => h.address === address);
}
