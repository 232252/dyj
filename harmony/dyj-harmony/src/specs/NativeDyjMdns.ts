/**
 * DyjMdns：局域网发现。
 *
 * ArkTS 侧只做「收包」——加入组播组、发一条 PTR 查询、把窗口期内收到的原始
 * DNS 报文原样交回；解析与去重在 TS 侧（可单测）。
 *
 * 返回值是 JSON 串：一个 base64 报文数组。
 */

import type {TurboModule} from 'react-native';
import {TurboModuleRegistry} from 'react-native';

export interface Spec extends TurboModule {
  /**
   * 扫描局域网内的打印共享端。
   *
   * @param timeoutMs 收包窗口（毫秒）。
   * @param names 要额外追问的实例名。传空数组时只问「有哪些打印服务」；
   *   拿到实例名后再带着名字问一次，才能取到端口与主机——服务端对这两种
   *   查询给的是不同的记录集。
   * @returns JSON 串，形如 ["<base64 报文>", …]。
   */
  scan(timeoutMs: number, names: string[]): Promise<string>;
}

export default TurboModuleRegistry.getEnforcing<Spec>('DyjMdns');
