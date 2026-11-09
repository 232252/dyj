/**
 * IPP 编解码（RFC 8010 报文格式 / RFC 8011 操作语义）。
 *
 * 字节布局与本仓库 Go 侧严格一致（pkg/attributes/writer.go、pkg/client/client.go）：
 *
 *   属性     value-tag(1) | name-length(2 BE) | name | value-length(2 BE) | value
 *   多值     第 2 个及以后的值 name-length = 0（additional-value，RFC 8010 §3.1.5）
 *   整数     4 字节 BE（RFC 8010 §3.8）
 *   布尔     1 字节，0x01 真
 *   消息     version(2) | operation-id(2 BE) | request-id(4 BE) | 分隔符+属性 … | 0x03
 *
 * 注意 value-length 在 name 之后——把它塞进固定 5 字节头会让接收方从第一个属性
 * 就错位（Go 侧 writeValuePair 的注释记录了这个坑）。
 *
 * 两条不可动摇的对齐约定：
 *  1. 每个属性组都以 attributes-charset + attributes-natural-language 开头
 *     （Go 侧 writeGroup 就是这么做的，RFC 8011 §4.1.4 要求 operation 组如此）。
 *  2. 请求帧的字节由 pkg/client/testdata/print_job.hex 锁定，两侧共用同一份
 *     事实源，任何一侧漂移都会让测试变红。
 */

import {utf8Decode, utf8Encode} from '../util/utf8';

/** 分隔符标签（RFC 8010 §3.5.1 Table 2）。 */
export const Delimiter = {
  OperationAttributes: 0x01,
  JobAttributes: 0x02,
  EndOfAttributes: 0x03,
  PrinterAttributes: 0x04,
  UnsupportedAttributes: 0x05,
} as const;

/** 值标签（RFC 8010 §3.5.2 Tables 4–6）。 */
export const ValueTag = {
  Unsupported: 0x10,
  Integer: 0x21,
  Boolean: 0x22,
  Enum: 0x23,
  Text: 0x41,
  Name: 0x42,
  Keyword: 0x44,
  URI: 0x45,
  URIScheme: 0x46,
  Charset: 0x47,
  NaturalLanguage: 0x48,
  MimeMediaType: 0x49,
} as const;

/** 操作码（RFC 8011）。 */
export const Operation = {
  PrintJob: 0x0002,
  ValidateJob: 0x0004,
  CancelJob: 0x0008,
  GetJobs: 0x000a,
  GetPrinterAttributes: 0x000b,
} as const;

/** 状态码（RFC 8011 Appendix B）。 */
export const Status = {
  OK: 0x0000,
  OKIgnoredOrSubstitutedAttributes: 0x0001,
  ClientErrorBadRequest: 0x0400,
  ClientErrorForbidden: 0x0401,
  ClientErrorNotAuthenticated: 0x0402,
  ClientErrorNotAuthorized: 0x0403,
  ClientErrorNotPossible: 0x0404,
  ClientErrorRequestEntityTooLarge: 0x0405,
  ClientErrorNotFound: 0x0406,
  ClientErrorAttributesNotSupported: 0x040b,
  ClientErrorUnsupported: 0x040c,
  ClientErrorDocumentAccess: 0x0412,
  ServerErrorInternalError: 0x0500,
  ServerErrorOperationNotSupported: 0x0501,
  ServerErrorServiceUnavailable: 0x0502,
  ServerErrorVersionNotSupported: 0x0503,
  ServerErrorNotAcceptingJobs: 0x0506,
} as const;

export type AttrValue = string | number | boolean;
export type AttrValueList = AttrValue[];

export interface Attr {
  tag: number;
  name: string;
  values: AttrValueList;
}

export interface RequestSpec {
  operation: number;
  requestId: number;
  /** 操作属性；charset 与 natural-language 由本模块自动补在最前面。 */
  operationAttributes: Attr[];
  /** 作业属性；charset 与 natural-language 由本模块自动补在最前面。 */
  jobAttributes?: Attr[];
  /** 文档字节：紧跟在 end-of-attributes 之后。 */
  document?: Uint8Array;
}

export interface IppMessage {
  versionMajor: number;
  versionMinor: number;
  /** 响应里是 status-code，请求里是 operation-id。 */
  code: number;
  requestId: number;
  operationAttributes: Record<string, AttrValueList>;
  jobAttributes: Record<string, AttrValueList>;
  printerAttributes: Record<string, AttrValueList>;
  unsupportedAttributes: Record<string, AttrValueList>;
}

/** 构造一个属性。整数用 0x21、布尔用 0x22，其余按调用方给的标签。 */
export function attr(tag: number, name: string, ...values: AttrValue[]): Attr {
  return {tag, name, values};
}

/** 关键字属性（0x44）。 */
export function keyword(name: string, ...values: string[]): Attr {
  return attr(ValueTag.Keyword, name, ...values);
}

/** 名称属性（0x42）。 */
export function nameAttr(name: string, value: string): Attr {
  return attr(ValueTag.Name, name, value);
}

/** URI 属性（0x45）。 */
export function uri(name: string, value: string): Attr {
  return attr(ValueTag.URI, name, value);
}

/** MIME 类型属性（0x49）。 */
export function mimeType(name: string, value: string): Attr {
  return attr(ValueTag.MimeMediaType, name, value);
}

/** 整数属性（0x21）。 */
export function integer(name: string, value: number): Attr {
  return attr(ValueTag.Integer, name, value);
}

/** 布尔属性（0x22）。 */
export function bool(name: string, value: boolean): Attr {
  return attr(ValueTag.Boolean, name, value);
}

/**
 * 每个属性组的开头两项。
 *
 * RFC 8011 §4.1.4 要求 operation-attributes 组以 attributes-charset 打头；
 * Go 侧 writeGroup 对 job-attributes 组也照做，这里保持一致——否则两端
 * 编出的字节不同，黄金字节测试必然变红。
 */
function leadingAttributes(): Attr[] {
  return [
    attr(ValueTag.Charset, 'attributes-charset', 'utf-8'),
    attr(ValueTag.NaturalLanguage, 'attributes-natural-language', 'en-us'),
  ];
}

function encodeValue(tag: number, value: AttrValue): Uint8Array {
  if (typeof value === 'string') {
    return utf8Encode(value);
  }
  if (typeof value === 'boolean') {
    return Uint8Array.of(value ? 0x01 : 0x00);
  }
  const out = new Uint8Array(4);
  const n = value | 0;
  out[0] = (n >>> 24) & 0xff;
  out[1] = (n >>> 16) & 0xff;
  out[2] = (n >>> 8) & 0xff;
  out[3] = n & 0xff;
  return out;
}

/** 编码一个属性（含多值的 additional-value 形态）。 */
export function encodeAttribute(a: Attr): Uint8Array {
  if (!a.name) {
    throw new Error('ipp: attribute has empty name');
  }
  if (a.values.length === 0) {
    throw new Error(`ipp: attribute ${a.name} has no values`);
  }
  const chunks: Uint8Array[] = [];
  a.values.forEach((v, i) => {
    const name = i === 0 ? a.name : ''; // additional-value 不带名字
    const nameBytes = utf8Encode(name);
    const valueBytes = encodeValue(a.tag, v);
    const head = new Uint8Array(3 + nameBytes.length + 2);
    head[0] = a.tag;
    head[1] = (nameBytes.length >>> 8) & 0xff;
    head[2] = nameBytes.length & 0xff;
    head.set(nameBytes, 3);
    const vl = 3 + nameBytes.length;
    head[vl] = (valueBytes.length >>> 8) & 0xff;
    head[vl + 1] = valueBytes.length & 0xff;
    chunks.push(head, valueBytes);
  });
  return concat(chunks);
}

/** 编码一组属性（不带分隔符，也不补 charset/natural-language）。 */
export function encodeAttributes(attrs: Attr[]): Uint8Array {
  return concat(attrs.map(encodeAttribute));
}

/** 编码完整请求：头 + 属性组 + end-of-attributes + 文档。 */
export function encodeRequest(spec: RequestSpec): Uint8Array {
  const head = new Uint8Array(8);
  head[0] = 2; // version major
  head[1] = 0; // version minor
  head[2] = (spec.operation >>> 8) & 0xff;
  head[3] = spec.operation & 0xff;
  const id = spec.requestId >>> 0;
  head[4] = (id >>> 24) & 0xff;
  head[5] = (id >>> 16) & 0xff;
  head[6] = (id >>> 8) & 0xff;
  head[7] = id & 0xff;

  const chunks: Uint8Array[] = [head];
  chunks.push(Uint8Array.of(Delimiter.OperationAttributes));
  chunks.push(encodeAttributes(leadingAttributes().concat(spec.operationAttributes)));
  if (spec.jobAttributes && spec.jobAttributes.length > 0) {
    chunks.push(Uint8Array.of(Delimiter.JobAttributes));
    chunks.push(encodeAttributes(leadingAttributes().concat(spec.jobAttributes)));
  }
  chunks.push(Uint8Array.of(Delimiter.EndOfAttributes));
  if (spec.document && spec.document.length > 0) {
    chunks.push(spec.document);
  }
  return concat(chunks);
}

function concat(parts: Uint8Array[]): Uint8Array {
  let total = 0;
  for (const p of parts) {
    total += p.length;
  }
  const out = new Uint8Array(total);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}

interface Cursor {
  bytes: Uint8Array;
  pos: number;
}

function readUint16(c: Cursor): number {
  if (c.pos + 2 > c.bytes.length) {
    throw new Error('ipp: truncated uint16');
  }
  const v = (c.bytes[c.pos] << 8) | c.bytes[c.pos + 1];
  c.pos += 2;
  return v;
}

function readUint32(c: Cursor): number {
  if (c.pos + 4 > c.bytes.length) {
    throw new Error('ipp: truncated uint32');
  }
  const v =
    c.bytes[c.pos] * 0x1000000 +
    ((c.bytes[c.pos + 1] << 16) | (c.bytes[c.pos + 2] << 8) | c.bytes[c.pos + 3]);
  c.pos += 4;
  return v;
}

function decodeValue(tag: number, raw: Uint8Array): AttrValue {
  if (tag === ValueTag.Integer || tag === ValueTag.Enum) {
    if (raw.length !== 4) {
      throw new Error(`ipp: ${tag.toString(16)} value must be 4 octets`);
    }
    return (
      raw[0] * 0x1000000 +
      ((raw[1] << 16) | (raw[2] << 8) | raw[3]) |
      0
    );
  }
  if (tag === ValueTag.Boolean) {
    return raw.length > 0 && raw[0] !== 0;
  }
  return utf8Decode(raw);
}

/**
 * 解析一条 IPP 消息（响应）。
 *
 * 与 Go 侧 attributes.DecodeMessageStream 同语义：additional-value（空名字）
 * 并入上一个同名属性；每个值自带 value-tag，所以按 tag 解码即可。
 *
 * 未识别的值标签按字符型解码——服务端不会回给我们别的类型，遇到也不会致命。
 */
export function decodeMessage(bytes: Uint8Array): IppMessage {
  const c: Cursor = {bytes, pos: 0};
  const versionMajor = bytes[0];
  const versionMinor = bytes[1];
  c.pos = 2;
  const code = readUint16(c);
  const requestId = readUint32(c);

  const groups: Record<string, Record<string, AttrValueList>> = {
    operation: {},
    job: {},
    printer: {},
    unsupported: {},
  };

  let current: Record<string, AttrValueList> | null = null;
  let lastName = '';

  while (c.pos < bytes.length) {
    const tag = bytes[c.pos];
    if (tag === Delimiter.EndOfAttributes) {
      c.pos++;
      break;
    }
    if (
      tag === Delimiter.OperationAttributes ||
      tag === Delimiter.JobAttributes ||
      tag === Delimiter.PrinterAttributes ||
      tag === Delimiter.UnsupportedAttributes
    ) {
      c.pos++;
      current =
        tag === Delimiter.OperationAttributes
          ? groups.operation
          : tag === Delimiter.JobAttributes
            ? groups.job
            : tag === Delimiter.PrinterAttributes
              ? groups.printer
              : groups.unsupported;
      lastName = '';
      continue;
    }
    // 值标签：value-tag(1) | name-length(2) | name | value-length(2) | value
    c.pos++;
    const nameLen = readUint16(c);
    let name = lastName;
    if (nameLen > 0) {
      const nameBytes = bytes.subarray(c.pos, c.pos + nameLen);
      c.pos += nameLen;
      name = utf8Decode(nameBytes);
    }
    const valueLen = readUint16(c);
    const raw = bytes.subarray(c.pos, c.pos + valueLen);
    c.pos += valueLen;
    const value = decodeValue(tag, raw);
    if (current) {
      const bucket = current[name] || (current[name] = []);
      bucket.push(value);
    }
    lastName = name;
  }

  return {
    versionMajor,
    versionMinor,
    code,
    requestId,
    operationAttributes: groups.operation,
    jobAttributes: groups.job,
    printerAttributes: groups.printer,
    unsupportedAttributes: groups.unsupported,
  };
}

/** 取属性第一个字符串值（没有则空串）。 */
export function firstString(
  group: Record<string, AttrValueList>,
  name: string,
): string {
  const v = group[name];
  if (!v || v.length === 0) {
    return '';
  }
  const first = v[0];
  return typeof first === 'string' ? first : String(first);
}

/** 取属性第一个整数值（没有则 undefined）。 */
export function firstInt(
  group: Record<string, AttrValueList>,
  name: string,
): number | undefined {
  const v = group[name];
  if (!v || v.length === 0) {
    return undefined;
  }
  const first = v[0];
  return typeof first === 'number' ? first : Number(first);
}

/** 取属性全部字符串值。 */
export function allStrings(
  group: Record<string, AttrValueList>,
  name: string,
): string[] {
  const v = group[name];
  if (!v) {
    return [];
  }
  return v.map(x => (typeof x === 'string' ? x : String(x)));
}
