/**
 * DyjImage：图片解码与 JPEG 打包。
 *
 * 打印机只吃 image/jpeg（服务端也支持 png，但 JPEG 体积小得多，局域网传输更
 * 稳）。长边超过上限时先等比降采样再打包——手机照片动辄几千万像素，不降下来
 * 一张就顶得上整份文档。
 *
 * EXIF 方向由 ImageKit 的解码器处理（它按 EXIF 摆正像素图），这里不再额外
 * 旋转，避免转两次。
 */

import { UITurboModule, UITurboModuleContext } from '@rnoh/react-native-openharmony';
import { TM } from '../generated/ts';
import { image } from '@kit.ImageKit';
import { B64 } from './B64';

export class DyjImageModule extends UITurboModule implements TM.DyjImage.Spec {
  static readonly NAME: string = 'DyjImage';

  constructor(ctx: UITurboModuleContext) {
    super(ctx);
  }

  async toJpeg(path: string, maxLongEdge: number, quality: number): Promise<string> {
    const source = image.createImageSource(path);
    try {
      const info = await source.getImageInfo();
      const options: image.DecodingOptions = { editable: false };
      const desired = this.desiredSize(info.size.width, info.size.height, maxLongEdge);
      if (desired !== undefined) {
        options.desiredSize = desired;
      }
      const pixelMap = await source.createPixelMap(options);
      try {
        const packer = image.createImagePacker();
        try {
          const packing: image.PackingOption = {
            format: 'image/jpeg',
            quality: quality,
          };
          const data = await packer.packing(pixelMap, packing);
          return B64.encode(new Uint8Array(data));
        } finally {
          await packer.release();
        }
      } finally {
        await pixelMap.release();
      }
    } finally {
      await source.release();
    }
  }

  /**
   * 算出降采样后的尺寸；不超限时返回 undefined（保持原尺寸）。
   */
  private desiredSize(width: number, height: number, maxLongEdge: number): image.Size | undefined {
    if (maxLongEdge <= 0) {
      return undefined;
    }
    const longest = Math.max(width, height);
    if (longest <= maxLongEdge) {
      return undefined;
    }
    const ratio = maxLongEdge / longest;
    return {
      width: Math.max(1, Math.round(width * ratio)),
      height: Math.max(1, Math.round(height * ratio)),
    };
  }
}
