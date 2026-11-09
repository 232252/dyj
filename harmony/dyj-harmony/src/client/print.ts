/**
 * 打印。
 *
 * 打印走 IPP over HTTP：请求体 = IPP 帧 + 文档字节。帧与文档分开编码、分开
 * 过桥，原生侧再拼起来——大文件不必先整份复制进 JS 内存。
 *
 * 打印路径里带着配对令牌（/p/<令牌>/ipp/print/<打印机>），服务端据此认人；
 * 令牌失效时服务端回 401，界面据此引导重新配对。
 */

import {
  Operation,
  Status,
  allStrings,
  decodeMessage,
  encodeRequest,
  firstInt,
  integer,
  mimeType,
  nameAttr,
  uri,
} from '../ipp/codec';
import {postDocument} from '../native';

/** 打印失败的原因。 */
export type PrintErrorCode =
  | 'token_invalid'
  | 'paused'
  | 'format_unsupported'
  | 'too_large'
  | 'unreachable'
  | 'unknown';

/** 打印失败。code 决定界面怎么讲。 */
export class PrintError extends Error {
  readonly code: PrintErrorCode;

  constructor(code: PrintErrorCode) {
    super(code);
    this.name = 'PrintError';
    this.code = code;
  }
}

/** 一次打印的参数。 */
export interface PrintOptions {
  /** 作业名。 */
  jobName: string;
  /** 份数，1 表示不送 copies 属性。 */
  copies: number;
  /** 发起者名字。 */
  userName: string;
}

/** 一次打印的结果。 */
export interface PrintOutcome {
  ok: boolean;
  /** 作业号。 */
  jobId?: number;
  /** 页数（PDF 渲染成多页时大于 1）。 */
  pages?: number;
  /** 失败原因。 */
  code?: PrintErrorCode;
}

/** 打印路径拼成完整地址。baseUrl 形如 http://192.168.1.20:8080。 */
export function printUrl(baseUrl: string, uriPath: string): string {
  return `${baseUrl}${uriPath}`;
}

/** 打印路径对应的打印机标识（IPP 的 printer-uri 属性）。 */
export function printerUri(baseUrl: string, uriPath: string): string {
  const rest = baseUrl.replace(/^https?:\/\//, '');
  return `ipp://${rest}${uriPath}`;
}

/**
 * 送一份文档，返回作业号。
 *
 * 文档字节就是正文，不压缩——局域网上 gzip 省下的时间抵不上 CPU 代价，
 * 服务端也只在声明 compression=gzip 时才解压。
 */
export async function printDocument(
  url: string,
  printerUriValue: string,
  document: Uint8Array,
  format: string,
  options: PrintOptions,
  timeoutMs = 60000,
): Promise<number> {
  const frame = encodeRequest({
    operation: Operation.PrintJob,
    requestId: 1,
    operationAttributes: [
      uri('printer-uri', printerUriValue),
      nameAttr('job-name', options.jobName),
      mimeType('document-format', format),
      nameAttr('requesting-user-name', options.userName),
    ],
    jobAttributes: options.copies > 1 ? [integer('copies', options.copies)] : [],
  });

  let response;
  try {
    response = await postDocument(
      url,
      frame,
      document,
      'application/ipp',
      timeoutMs,
    );
  } catch {
    throw new PrintError('unreachable');
  }
  if (response.status === 401) {
    throw new PrintError('token_invalid');
  }
  if (response.status !== 200) {
    throw new PrintError('unknown');
  }

  const message = decodeMessage(response.body);
  if (message.code !== Status.OK) {
    throw new PrintError(statusToCode(message.code));
  }
  const jobId = firstInt(message.jobAttributes, 'job-id');
  if (jobId === undefined) {
    throw new PrintError('unknown');
  }
  return jobId;
}

/**
 * 问打印机支持哪些文档格式。
 *
 * 问不到就返回空数组——空数组表示「不限制」，端上按最保险的方式发（PDF 转图）。
 */
export async function supportedFormats(
  url: string,
  printerUriValue: string,
  timeoutMs = 8000,
): Promise<string[]> {
  const frame = encodeRequest({
    operation: Operation.GetPrinterAttributes,
    requestId: 1,
    operationAttributes: [uri('printer-uri', printerUriValue)],
  });
  let response;
  try {
    response = await postDocument(url, frame, new Uint8Array(0), 'application/ipp', timeoutMs);
  } catch {
    return [];
  }
  if (response.status !== 200) {
    return [];
  }
  try {
    const message = decodeMessage(response.body);
    return allStrings(message.printerAttributes, 'document-format-supported');
  } catch {
    return [];
  }
}

/** IPP 状态码 → 界面能讲的原因。 */
function statusToCode(status: number): PrintErrorCode {
  switch (status) {
    case Status.ClientErrorNotAuthenticated:
    case Status.ClientErrorNotAuthorized:
      return 'token_invalid';
    case Status.ClientErrorAttributesNotSupported:
    case Status.ClientErrorUnsupported:
      return 'format_unsupported';
    case Status.ClientErrorRequestEntityTooLarge:
      return 'too_large';
    case Status.ServerErrorNotAcceptingJobs:
      return 'paused';
    default:
      return 'unknown';
  }
}
