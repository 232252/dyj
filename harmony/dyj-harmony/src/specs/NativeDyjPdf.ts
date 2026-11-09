/**
 * DyjPdf：PDF 逐页渲染成图片。
 *
 * 默认路径是把 PDF 在端上渲染成逐页 JPEG 再打印——任何服务端都能用，不依赖
 * 服务端是否装了 poppler。每页一个作业。
 */

import type {TurboModule} from 'react-native';
import {TurboModuleRegistry} from 'react-native';

export interface Spec extends TurboModule {
  /**
   * 渲染 PDF 的每一页。
   *
   * @param path 沙箱里的 PDF 路径。
   * @param maxLongEdge 长边上限（像素），超过则等比降采样。
   * @param quality JPEG 质量（1-100）。
   * @returns JSON 串 {"pages":["<base64 JPEG>", …]}
   */
  render(path: string, maxLongEdge: number, quality: number): Promise<string>;
  /** 取 PDF 的页数。 */
  pageCount(path: string): Promise<number>;
}

export default TurboModuleRegistry.getEnforcing<Spec>('DyjPdf');
