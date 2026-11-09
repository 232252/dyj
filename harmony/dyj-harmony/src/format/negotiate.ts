/**
 * 文档格式协商与降级。
 *
 * 服务端永远支持 image/pwg-raster、image/jpeg、image/png、text/plain；
 * application/pdf 只有装了 poppler 的服务端才有（见 README 与
 * pkg/ipp/template.go）。
 *
 * PDF 默认在端上渲染成 JPEG 逐页打印——这样任何服务端都能用，不依赖服务端
 * 能力；「保持原样直传」是用户主动打开的可选项。
 */

export type DocumentKind = 'image' | 'text' | 'pdf';

export const FORMAT_JPEG = 'image/jpeg';
export const FORMAT_PNG = 'image/png';
export const FORMAT_TEXT = 'text/plain';
export const FORMAT_PDF = 'application/pdf';

/** 一份文档要打印成什么样。 */
export interface FormatPlan {
  /** 能不能打印。false 时 format 为空串。 */
  ok: boolean;
  /** 实际发出的 document-format。 */
  format: string;
  /** 是否需要端上渲染（PDF → 逐页 JPEG）。 */
  renderOnDevice: boolean;
  /** 作业个数：渲染时等于页数，直传时为 1。 */
  pages: number;
  /** 是否因为服务端不支持而放弃了「保持原样」。 */
  passthroughUnavailable: boolean;
}

export interface FormatOptions {
  /** 用户是否勾了「保持原样直传」。 */
  pdfPassthrough?: boolean;
  /** PDF 页数（渲染时决定作业个数）。 */
  pdfPages?: number;
}

/**
 * 算出这份文档该怎么发。
 *
 * supported 是服务端 Get-Printer-Attributes 报回来的 document-format-supported；
 * 传空数组表示「没问过服务端」，此时按服务端永远支持的格式走。
 */
export function planFormat(
  kind: DocumentKind,
  supported: string[],
  options: FormatOptions = {},
): FormatPlan {
  const list = supported.map(s => s.trim().toLowerCase());
  const has = (f: string) => list.length === 0 || list.includes(f);

  if (kind === 'text') {
    return has(FORMAT_TEXT)
      ? {ok: true, format: FORMAT_TEXT, renderOnDevice: false, pages: 1, passthroughUnavailable: false}
      : {ok: false, format: '', renderOnDevice: false, pages: 0, passthroughUnavailable: false};
  }

  if (kind === 'image') {
    // 端上只产出 JPEG（PNG 也保留，因为服务端也支持）。
    if (has(FORMAT_JPEG)) {
      return {ok: true, format: FORMAT_JPEG, renderOnDevice: false, pages: 1, passthroughUnavailable: false};
    }
    if (has(FORMAT_PNG)) {
      return {ok: true, format: FORMAT_PNG, renderOnDevice: false, pages: 1, passthroughUnavailable: false};
    }
    return {ok: false, format: '', renderOnDevice: false, pages: 0, passthroughUnavailable: false};
  }

  // PDF
  const wantPassthrough = Boolean(options.pdfPassthrough);
  if (wantPassthrough && has(FORMAT_PDF)) {
    return {ok: true, format: FORMAT_PDF, renderOnDevice: false, pages: 1, passthroughUnavailable: false};
  }
  if (has(FORMAT_JPEG)) {
    return {
      ok: true,
      format: FORMAT_JPEG,
      renderOnDevice: true,
      pages: Math.max(1, options.pdfPages ?? 1),
      passthroughUnavailable: wantPassthrough,
    };
  }
  if (has(FORMAT_PNG)) {
    return {
      ok: true,
      format: FORMAT_PNG,
      renderOnDevice: true,
      pages: Math.max(1, options.pdfPages ?? 1),
      passthroughUnavailable: wantPassthrough,
    };
  }
  return {ok: false, format: '', renderOnDevice: false, pages: 0, passthroughUnavailable: wantPassthrough};
}

/**
 * 「保持原样直传」这个选项该不该给用户看。
 *
 * 只有服务端明确声明支持 application/pdf 时才有意义；否则不给看，免得用户
 * 勾了一个必然被降级的选项。
 */
export function canPassthroughPdf(supported: string[]): boolean {
  return supported.some(s => s.trim().toLowerCase() === FORMAT_PDF);
}
