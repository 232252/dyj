/**
 * DyjFiles：选文件与读文件。
 *
 * 选中的文件会被复制进应用沙箱，返回沙箱里的路径——之后的图片转码与 PDF 渲染
 * 都在 ArkTS 侧直接按路径处理，文档字节不必在桥上往返。
 */

import { UITurboModule, UITurboModuleContext } from '@rnoh/react-native-openharmony';
import { TM } from '../generated/ts';
import { fileIo, picker } from '@kit.CoreFileKit';
import { B64 } from './B64';

/** 图片可选的后缀。 */
const IMAGE_SUFFIXES: string[] = ['.jpg', '.jpeg', '.png', '.webp', '.bmp', '.heic', '.gif'];
/** 文档可选的后缀。 */
const DOCUMENT_SUFFIXES: string[] = ['.pdf', '.txt', '.md'];

export class DyjFilesModule extends UITurboModule implements TM.DyjFiles.Spec {
  static readonly NAME: string = 'DyjFiles';

  constructor(ctx: UITurboModuleContext) {
    super(ctx);
  }

  async pick(kind: string): Promise<string> {
    const options: picker.DocumentSelectOptions = {
      maxSelectNumber: 1,
      fileSuffixFilters: kind === 'image' ? IMAGE_SUFFIXES : DOCUMENT_SUFFIXES,
    };
    const documentPicker = new picker.DocumentViewPicker(this.ctx.uiAbilityContext);
    const uris = await documentPicker.select(options);
    if (uris.length === 0) {
      return '{"cancelled":true}';
    }
    const uri = uris[0];
    // 选择器给的是 file:// 形式的 URI：文件名从 URI 末段取，拷贝直接吃 URI。
    const name = this.basename(uri);
    const target = `${this.ctx.uiAbilityContext.filesDir}/picked_${Date.now()}_${name}`;
    fileIo.copyFileSync(uri, target);
    const stat = fileIo.statSync(target);
    const mime = this.mimeOf(name);
    return (
      `{"cancelled":false,"path":"${target}","name":"${name}",` +
      `"mime":"${mime}","size":${stat.size}}`
    );
  }

  async readBase64(path: string): Promise<string> {
    // fs 没有 readFileSync：得自己开文件、按长度读一块、再关掉。
    const file = fileIo.openSync(path, fileIo.OpenMode.READ_ONLY);
    try {
      const stat = fileIo.statSync(path);
      const buffer = new ArrayBuffer(stat.size);
      fileIo.readSync(file.fd, buffer);
      return B64.encode(new Uint8Array(buffer));
    } finally {
      fileIo.closeSync(file);
    }
  }

  /** 取路径末段作为文件名（URI 里的百分号编码也一并解掉）。 */
  private basename(path: string): string {
    const parts = path.split('/');
    const last = parts.length > 0 ? parts[parts.length - 1] : 'file';
    try {
      return decodeURIComponent(last);
    } catch {
      return last;
    }
  }

  /** 按后缀猜类型——只用得上这几种，猜不准也不影响打印。 */
  private mimeOf(name: string): string {
    const lower = name.toLowerCase();
    if (lower.endsWith('.pdf')) {
      return 'application/pdf';
    }
    if (lower.endsWith('.png')) {
      return 'image/png';
    }
    if (lower.endsWith('.webp')) {
      return 'image/webp';
    }
    if (lower.endsWith('.txt') || lower.endsWith('.md')) {
      return 'text/plain';
    }
    return 'image/jpeg';
  }
}
