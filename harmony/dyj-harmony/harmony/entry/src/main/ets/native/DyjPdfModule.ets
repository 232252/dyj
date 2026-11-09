/**
 * DyjPdf：PDF 逐页渲染成图片。
 *
 * 默认路径是把 PDF 在端上渲染成逐页 JPEG 再打印——任何服务端都能用，不依赖
 * 服务端是否装了 poppler。每页一个作业。
 *
 * 逐页渲染、逐页释放：一次把整份 PDF 转成图会把内存吃光（官方文档明确要求
 * 用完就 release PixelMap）。
 */

import { UITurboModule, UITurboModuleContext } from '@rnoh/react-native-openharmony';
import { TM } from '../generated/ts';
import { pdfService } from '@kit.PDFKit';
import { image } from '@kit.ImageKit';
import { B64 } from './B64';

export class DyjPdfModule extends UITurboModule implements TM.DyjPdf.Spec {
  static readonly NAME: string = 'DyjPdf';

  constructor(ctx: UITurboModuleContext) {
    super(ctx);
  }

  async render(path: string, maxLongEdge: number, quality: number): Promise<string> {
    const document = new pdfService.PdfDocument();
    const pages: string[] = [];
    try {
      const parsed = document.loadDocument(path);
      if (parsed !== pdfService.ParseResult.PARSE_SUCCESS) {
        throw new Error(`pdf: cannot open ${path} (${parsed})`);
      }
      const count = document.getPageCount();
      const packer = image.createImagePacker();
      try {
        for (let i = 0; i < count; i++) {
          const page = document.getPage(i);
          const pixelMap = page.getPagePixelMap();
          try {
            const packing: image.PackingOption = {
              format: 'image/jpeg',
              quality: quality,
            };
            const data = await packer.packing(pixelMap, packing);
            pages.push(B64.encode(new Uint8Array(data)));
          } finally {
            await pixelMap.release();
          }
        }
      } finally {
        await packer.release();
      }
    } finally {
      document.releaseDocument();
    }
    return `{"pages":[${pages.map(p => `"${p}"`).join(',')}]}`;
  }

  async pageCount(path: string): Promise<number> {
    const document = new pdfService.PdfDocument();
    try {
      const parsed = document.loadDocument(path);
      if (parsed !== pdfService.ParseResult.PARSE_SUCCESS) {
        throw new Error(`pdf: cannot open ${path} (${parsed})`);
      }
      return document.getPageCount();
    } finally {
      document.releaseDocument();
    }
  }
}
