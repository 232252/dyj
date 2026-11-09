/**
 * DyjHttp：二进制 HTTP POST。
 *
 * 为什么不放 TS 侧：RN 的 fetch 对裸 ArrayBuffer 请求体支持不确定，而 IPP
 * 必须逐字节精确。ArkTS 的 http.createHttp() 明确支持 extraData: ArrayBuffer。
 *
 * 约定：
 *  - 请求体是「头 + 文档」两段 base64，ArkTS 侧解码后拼接再发出——这样大块
 *    文档字节只需在桥上走一趟，不必先落到 JS 再回去。
 *  - 返回值是 JSON 串：{"status":200,"bodyBase64":"..."}。
 */

import type {TurboModule} from 'react-native';
import {TurboModuleRegistry} from 'react-native';

export interface Spec extends TurboModule {
  /** POST 一个 JSON 文本，返回 {"status":…,"bodyBase64":"…"}。 */
  postJson(url: string, json: string, timeoutMs: number): Promise<string>;
  /**
   * POST 一份二进制文档（IPP 报文），头与文档分开给。
   * 返回 {"status":…,"bodyBase64":"…"}。
   */
  postDocument(
    url: string,
    headerBase64: string,
    bodyBase64: string,
    contentType: string,
    timeoutMs: number,
  ): Promise<string>;
}

export default TurboModuleRegistry.getEnforcing<Spec>('DyjHttp');
