/**
 * 原生模块的 TS 侧门面。
 *
 * 原生模块一律返回字符串（JSON 串或 base64），这里把它们还原成类型化的值。
 * 这样 ArkTS 侧不必处理复杂对象，codegen 也不必支持对象类型。
 */

import {base64Decode, base64Encode} from '../util/base64';

import DyjFiles from '../specs/NativeDyjFiles';
import DyjHttp from '../specs/NativeDyjHttp';
import DyjImage from '../specs/NativeDyjImage';
import DyjMdns from '../specs/NativeDyjMdns';
import DyjPdf from '../specs/NativeDyjPdf';
import DyjPrefs from '../specs/NativeDyjPrefs';

/** HTTP 响应。 */
export interface HttpResponse {
  status: number;
  body: Uint8Array;
}

/** 选文件的结果。 */
export interface PickedFile {
  cancelled: boolean;
  /** 沙箱里的路径（取消时为空）。 */
  path: string;
  name: string;
  mime: string;
  size: number;
}

interface RawHttpResponse {
  status: number;
  bodyBase64: string;
}

/** POST 一个 JSON 文本。 */
export async function postJson(
  url: string,
  json: string,
  timeoutMs: number,
): Promise<HttpResponse> {
  const raw = await DyjHttp.postJson(url, json, timeoutMs);
  return parseHttpResponse(raw);
}

/**
 * POST 一份二进制文档。
 *
 * 头与文档分开传，ArkTS 侧拼接后一次发出——大文档只在桥上走一趟。
 */
export async function postDocument(
  url: string,
  header: Uint8Array,
  body: Uint8Array,
  contentType: string,
  timeoutMs: number,
): Promise<HttpResponse> {
  const raw = await DyjHttp.postDocument(
    url,
    base64Encode(header),
    base64Encode(body),
    contentType,
    timeoutMs,
  );
  return parseHttpResponse(raw);
}

function parseHttpResponse(raw: string): HttpResponse {
  const parsed = JSON.parse(raw) as RawHttpResponse;
  return {
    status: parsed.status,
    body: parsed.bodyBase64 ? base64Decode(parsed.bodyBase64) : new Uint8Array(0),
  };
}

/**
 * 扫描局域网内的打印共享端，返回收到的原始 DNS 报文。
 *
 * 服务端对「有哪些打印服务」和「这个服务在哪」给的是不同的记录集，所以要问
 * 两轮：先问服务类型拿到实例名，再带着实例名问端口与主机。
 */
export async function scanMdns(
  timeoutMs: number,
  names: string[] = [],
): Promise<Uint8Array[]> {
  const raw = await DyjMdns.scan(timeoutMs, names);
  const list = JSON.parse(raw) as string[];
  return list.map(base64Decode);
}

/** 读一个本地键；没有则返回空串。 */
export async function getPref(key: string): Promise<string> {
  return DyjPrefs.get(key);
}

/** 写一个本地键。 */
export async function setPref(key: string, value: string): Promise<void> {
  await DyjPrefs.set(key, value);
}

/** 删一个本地键。 */
export async function removePref(key: string): Promise<void> {
  await DyjPrefs.remove(key);
}

/** 打开系统选择器挑一个文件（kind: 'image' | 'document'）。 */
export async function pickFile(kind: 'image' | 'document'): Promise<PickedFile> {
  const raw = await DyjFiles.pick(kind);
  return JSON.parse(raw) as PickedFile;
}

/** 读一个文件的全部字节。 */
export async function readFileBytes(path: string): Promise<Uint8Array> {
  return base64Decode(await DyjFiles.readBase64(path));
}

/** 把一张图片转成 JPEG（返回字节）。 */
export async function imageToJpeg(
  path: string,
  maxLongEdge: number,
  quality: number,
): Promise<Uint8Array> {
  return base64Decode(await DyjImage.toJpeg(path, maxLongEdge, quality));
}

/** 渲染 PDF 的每一页，返回每页的 JPEG 字节。 */
export async function renderPdfPages(
  path: string,
  maxLongEdge: number,
  quality: number,
): Promise<Uint8Array[]> {
  const raw = await DyjPdf.render(path, maxLongEdge, quality);
  const parsed = JSON.parse(raw) as {pages: string[]};
  return parsed.pages.map(base64Decode);
}

/** 取 PDF 页数。 */
export async function pdfPageCount(path: string): Promise<number> {
  return DyjPdf.pageCount(path);
}
