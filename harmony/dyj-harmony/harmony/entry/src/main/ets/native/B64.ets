/**
 * base64 编解码。
 *
 * util.Base64Helper 的编解码是实例方法（不是静态方法），这里放一个共用实例，
 * 免得每次调用都新建。
 */

import { util } from '@kit.ArkTS';

export class B64 {
  private static readonly helper: util.Base64Helper = new util.Base64Helper();

  /** 字节数组转 base64 字符串。 */
  static encode(bytes: Uint8Array): string {
    return B64.helper.encodeToStringSync(bytes);
  }

  /** base64 字符串转字节数组。 */
  static decode(text: string): Uint8Array {
    return B64.helper.decodeSync(text);
  }
}
