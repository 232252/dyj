/**
 * 配对。
 *
 * 服务端的 /pair 是免鉴权的——「知道配对码」本身就是认证，所以这里没有会话、
 * 没有密码。成功后拿到的令牌放在打印路径里（/p/<令牌>/ipp/print/<打印机>），
 * 令牌不单独走请求头。
 *
 * 失败原因只有几种，界面按 PairError.code 给人话（文案见 src/i18n）。
 */

import {postJson} from '../native';
import {utf8Decode} from '../util/utf8';
import {baseUrl} from './address';

/** 一台可打印的共享打印机。 */
export interface SharedPrinter {
  key: string;
  name: string;
  /** 打印路径，形如 /p/<令牌>/ipp/print/<打印机>。 */
  uriPath: string;
}

/** 配对成功的结果。 */
export interface PairResult {
  token: string;
  serverName: string;
  printers: SharedPrinter[];
}

/** 配对失败的原因。 */
export type PairErrorCode =
  | 'pin_required'
  | 'bad_pin'
  | 'locked'
  | 'unreachable'
  | 'internal';

/** 配对失败。retryAfterSec 只在 locked 时有意义。 */
export class PairError extends Error {
  readonly code: PairErrorCode;
  readonly retryAfterSec: number;

  constructor(code: PairErrorCode, retryAfterSec = 0) {
    super(code);
    this.name = 'PairError';
    this.code = code;
    this.retryAfterSec = retryAfterSec;
  }
}

interface PairResponse {
  token?: string;
  server_name?: string;
  printers?: Array<{key?: string; name?: string; uri_path?: string}>;
  error?: string;
  retry_after_sec?: number;
}

/**
 * 用配对码换一个令牌。
 *
 * baseUrl 形如 http://192.168.1.20:8080（不带尾斜杠）。
 */
export async function pair(
  baseUrl: string,
  pin: string,
  deviceId: string,
  deviceName: string,
  timeoutMs = 8000,
): Promise<PairResult> {
  const body = JSON.stringify({
    pin,
    device_id: deviceId,
    device_name: deviceName,
  });
  let response;
  try {
    response = await postJson(`${baseUrl}/pair`, body, timeoutMs);
  } catch {
    // 原生层把连接失败、超时都抛出来了——对用户都是「连不上」。
    throw new PairError('unreachable');
  }
  if (response.status === 200) {
    const parsed = parsePairResponse(response.body);
    return {
      token: parsed.token ?? '',
      serverName: parsed.server_name ?? '',
      printers: (parsed.printers ?? []).map(p => ({
        key: p.key ?? '',
        name: p.name ?? '',
        uriPath: p.uri_path ?? '',
      })),
    };
  }
  if (response.status === 400) {
    throw new PairError('pin_required');
  }
  if (response.status === 401) {
    throw new PairError('bad_pin');
  }
  if (response.status === 403) {
    const parsed = safeParse(response.body);
    throw new PairError('locked', parsed?.retry_after_sec ?? 0);
  }
  throw new PairError('internal');
}

/**
 * 依次尝试几个地址，直到有一个配对成功。
 *
 * 共享端在多网卡机器上会通告多个地址，手机未必跟它们都在同一网段——挨个试
 * 比让用户自己挑一个更省事。
 */
export async function pairAny(
  addresses: string[],
  pin: string,
  deviceId: string,
  deviceName: string,
  timeoutMs = 8000,
): Promise<{result: PairResult; address: string}> {
  let last: PairError | undefined;
  for (const address of addresses) {
    try {
      const result = await pair(
        baseUrl(address),
        pin,
        deviceId,
        deviceName,
        timeoutMs,
      );
      return {result, address};
    } catch (e) {
      if (e instanceof PairError && e.code !== 'unreachable') {
        throw e; // 码错了、锁了这类，换地址也没用
      }
      last = e instanceof PairError ? e : new PairError('unreachable');
    }
  }
  throw last ?? new PairError('unreachable');
}

function parsePairResponse(bytes: Uint8Array): PairResponse {
  const text = utf8Decode(bytes).trim();
  return text ? (JSON.parse(text) as PairResponse) : {};
}

function safeParse(bytes: Uint8Array): PairResponse | undefined {
  try {
    return parsePairResponse(bytes);
  } catch {
    return undefined;
  }
}
