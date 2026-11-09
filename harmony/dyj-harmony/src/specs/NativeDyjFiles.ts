/**
 * DyjFiles：选文件与读文件。
 *
 * 选中的文件会被复制进应用沙箱，返回沙箱里的路径——之后的图片转码与 PDF 渲染
 * 都在 ArkTS 侧直接按路径处理，文档字节不必在桥上往返。
 */

import type {TurboModule} from 'react-native';
import {TurboModuleRegistry} from 'react-native';

export interface Spec extends TurboModule {
  /**
   * 打开系统选择器挑一个文件并复制进沙箱。
   *
   * @param kind 'image' | 'document'，决定可选的文件类型。
   * @returns JSON 串：
   *   取消 {"cancelled":true}
   *   成功 {"cancelled":false,"path":"…","name":"…","mime":"…","size":123}
   */
  pick(kind: string): Promise<string>;
  /** 读一个文件的全部字节，返回 base64。 */
  readBase64(path: string): Promise<string>;
}

export default TurboModuleRegistry.getEnforcing<Spec>('DyjFiles');
