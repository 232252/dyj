/**
 * DyjImage：图片解码与 JPEG 打包。
 *
 * 端上把任意图片（jpg/png/webp/heic 等）解码成像素图，按 EXIF 方向摆正，
 * 必要时降采样，再打包成 JPEG。打印机只吃 image/jpeg（服务端也支持 png，
 * 但 JPEG 体积小得多，局域网传输更稳）。
 */

import type {TurboModule} from 'react-native';
import {TurboModuleRegistry} from 'react-native';

export interface Spec extends TurboModule {
  /**
   * 把一张图片转成 JPEG。
   *
   * @param path 沙箱里的图片路径。
   * @param maxLongEdge 长边上限（像素），超过则等比降采样。
   * @param quality JPEG 质量（1-100）。
   * @returns base64 的 JPEG 字节。
   */
  toJpeg(path: string, maxLongEdge: number, quality: number): Promise<string>;
}

export default TurboModuleRegistry.getEnforcing<Spec>('DyjImage');
