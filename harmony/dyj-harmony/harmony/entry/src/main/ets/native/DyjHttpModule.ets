/**
 * DyjHttp：二进制 HTTP POST。
 *
 * 放在 ArkTS 侧是因为 RN 的 fetch 对裸 ArrayBuffer 请求体支持不确定，而 IPP
 * 报文必须逐字节精确。ArkTS 的 http.createHttp() 明确支持 extraData。
 *
 * 请求体分两段给（头 + 文档），这里解码后拼接再发出——大块文档字节只需要在
 * 桥上走一趟，不必先落到 JS 再回去。
 */

import { UITurboModule, UITurboModuleContext } from '@rnoh/react-native-openharmony';
import { TM } from '../generated/ts';
import { http } from '@kit.NetworkKit';
import { B64 } from './B64';

export class DyjHttpModule extends UITurboModule implements TM.DyjHttp.Spec {
  static readonly NAME: string = 'DyjHttp';

  constructor(ctx: UITurboModuleContext) {
    super(ctx);
  }

  async postJson(url: string, json: string, timeoutMs: number): Promise<string> {
    const response = await this.send(url, json, 'application/json; charset=utf-8', timeoutMs);
    return response;
  }

  async postDocument(
    url: string,
    headerBase64: string,
    bodyBase64: string,
    contentType: string,
    timeoutMs: number,
  ): Promise<string> {
    const body = this.concatBase64(headerBase64, bodyBase64);
    const response = await this.send(url, body, contentType, timeoutMs);
    return response;
  }

  /**
   * 发一次 POST。
   *
   * 返回 {"status":…,"bodyBase64":"…"}。状态码原样带回，交给 TS 侧判断
   * （401 表示令牌失效、403 表示被锁）。
   */
  private async send(
    url: string,
    body: string | ArrayBuffer,
    contentType: string,
    timeoutMs: number,
  ): Promise<string> {
    const request = http.createHttp();
    try {
      const header: Record<string, string> = {
        'Content-Type': contentType,
      };
      const response = await request.request(url, {
        method: http.RequestMethod.POST,
        header: header,
        extraData: body,
        expectDataType: http.HttpDataType.ARRAY_BUFFER,
        connectTimeout: timeoutMs,
        readTimeout: timeoutMs,
      });
      const raw = response.result;
      let bytes: Uint8Array = new Uint8Array(0);
      if (typeof raw !== 'string') {
        bytes = new Uint8Array(raw as ArrayBuffer);
      }
      const encoded = B64.encode(bytes);
      return `{"status":${response.responseCode},"bodyBase64":"${encoded}"}`;
    } finally {
      request.destroy();
    }
  }

  /** 把两段 base64 解码后拼成一段字节。 */
  private concatBase64(headerBase64: string, bodyBase64: string): ArrayBuffer {
    const head: Uint8Array =
      headerBase64.length > 0 ? B64.decode(headerBase64) : new Uint8Array(0);
    const body: Uint8Array =
      bodyBase64.length > 0 ? B64.decode(bodyBase64) : new Uint8Array(0);
    const out = new Uint8Array(head.length + body.length);
    out.set(head, 0);
    out.set(body, head.length);
    return out.buffer;
  }
}
