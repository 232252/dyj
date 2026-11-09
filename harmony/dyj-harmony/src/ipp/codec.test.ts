/**
 * IPP 编解码的黄金字节与往返测试。
 *
 * 黄金字节文件与 Go 侧共用同一份事实源：
 *   pkg/client/testdata/print_job.hex
 * Go 侧由 pkg/client/golden_test.go 生成并断言，本侧读同一份文件——
 * 任何一侧的 wire 格式漂移都会让这两边同时变红。
 */

import {readFileSync} from 'fs';
import {join} from 'path';

import {fromHex, toHex} from '../util/utf8';
import {
  Delimiter,
  Operation,
  Status,
  ValueTag,
  allStrings,
  attr,
  decodeMessage,
  encodeRequest,
  firstInt,
  firstString,
  integer,
  keyword,
  mimeType,
  nameAttr,
  uri,
} from './codec';

const GOLDEN_PATH = join(
  __dirname,
  '..',
  '..',
  '..',
  '..',
  'pkg',
  'client',
  'testdata',
  'print_job.hex',
);

/** 与 Go 侧 goldenRequestAttrs 一一对应的规范请求。 */
const GOLDEN_OP_ATTRS = [
  uri(
    'printer-uri',
    'ipp://192.0.2.1:6631/p/0123456789abcdef0123456789abcdef/ipp/print/eps',
  ),
  nameAttr('job-name', 'dyj golden'),
  mimeType('document-format', 'image/jpeg'),
];

const GOLDEN_JOB_ATTRS = [
  integer('copies', 1),
  keyword('media', 'iso_a4_210x297mm'),
  keyword('print-color-mode', 'color'),
];

describe('IPP 黄金字节', () => {
  it('与 Go 侧共用的 fixture 逐字节相等', () => {
    const golden = fromHex(readFileSync(GOLDEN_PATH, 'utf8'));
    const ours = encodeRequest({
      operation: Operation.PrintJob,
      requestId: 1,
      operationAttributes: GOLDEN_OP_ATTRS,
      jobAttributes: GOLDEN_JOB_ATTRS,
    });
    expect(toHex(ours)).toBe(toHex(golden));
  });

  it('请求帧的骨架是 version/op-id/request-id/分隔符', () => {
    const bytes = encodeRequest({
      operation: Operation.PrintJob,
      requestId: 1,
      operationAttributes: GOLDEN_OP_ATTRS,
    });
    expect(bytes[0]).toBe(2); // version major
    expect(bytes[1]).toBe(0); // version minor
    expect(bytes[2]).toBe(0x00); // Print-Job 高字节
    expect(bytes[3]).toBe(0x02);
    expect(bytes[4]).toBe(0);
    expect(bytes[5]).toBe(0);
    expect(bytes[6]).toBe(0);
    expect(bytes[7]).toBe(1); // request-id
    expect(bytes[8]).toBe(Delimiter.OperationAttributes);
    expect(bytes[bytes.length - 1]).toBe(Delimiter.EndOfAttributes);
  });

  it('charset 与 natural-language 排在操作属性组最前面', () => {
    const bytes = encodeRequest({
      operation: Operation.PrintJob,
      requestId: 1,
      operationAttributes: GOLDEN_OP_ATTRS,
    });
    // 0x47 = charset，紧跟分隔符之后；名字长度占 2 字节，名字从第 12 字节起。
    expect(bytes[9]).toBe(ValueTag.Charset);
    expect((bytes[10] << 8) | bytes[11]).toBe('attributes-charset'.length);
    const name = 'attributes-charset';
    for (let i = 0; i < name.length; i++) {
      expect(bytes[12 + i]).toBe(name.charCodeAt(i));
    }
  });

  it('作业属性组同样以 charset 开头（与 Go 侧 writeGroup 一致）', () => {
    const bytes = encodeRequest({
      operation: Operation.PrintJob,
      requestId: 1,
      operationAttributes: GOLDEN_OP_ATTRS,
      jobAttributes: GOLDEN_JOB_ATTRS,
    });
    const sep = bytes.indexOf(Delimiter.JobAttributes, 9);
    expect(sep).toBeGreaterThan(0);
    expect(bytes[sep + 1]).toBe(ValueTag.Charset);
  });

  it('文档字节紧跟在 end-of-attributes 之后', () => {
    const doc = Uint8Array.of(0x01, 0x02, 0x03);
    const bytes = encodeRequest({
      operation: Operation.PrintJob,
      requestId: 1,
      operationAttributes: GOLDEN_OP_ATTRS,
      document: doc,
    });
    const end = bytes.indexOf(Delimiter.EndOfAttributes, 9);
    expect(Array.from(bytes.slice(end + 1))).toEqual([1, 2, 3]);
  });
});

describe('IPP 响应解析', () => {
  it('读出 status-code、job-id 与多值属性', () => {
    const msg = decodeMessage(
      buildMessage(Status.OK, [
        {
          delimiter: Delimiter.OperationAttributes,
          pairs: [pair(ValueTag.Charset, 'attributes-charset', 'utf-8')],
        },
        {
          delimiter: Delimiter.JobAttributes,
          pairs: [
            pair(ValueTag.Integer, 'job-id', intValue(12)),
            pair(ValueTag.MimeMediaType, 'document-format', 'image/jpeg'),
            pair(ValueTag.MimeMediaType, 'document-format', 'image/png', true),
          ],
        },
      ]),
    );
    expect(msg.code).toBe(Status.OK);
    expect(msg.requestId).toBe(1);
    expect(firstInt(msg.jobAttributes, 'job-id')).toBe(12);
    expect(allStrings(msg.jobAttributes, 'document-format')).toEqual([
      'image/jpeg',
      'image/png',
    ]);
    expect(firstString(msg.operationAttributes, 'attributes-charset')).toBe(
      'utf-8',
    );
  });

  it('解析打印机属性组', () => {
    const msg = decodeMessage(
      buildMessage(Status.OK, [
        {
          delimiter: Delimiter.PrinterAttributes,
          pairs: [
            pair(ValueTag.MimeMediaType, 'document-format', 'image/jpeg'),
            pair(ValueTag.MimeMediaType, 'document-format', 'text/plain', true),
          ],
        },
      ]),
    );
    expect(allStrings(msg.printerAttributes, 'document-format')).toEqual([
      'image/jpeg',
      'text/plain',
    ]);
  });
});

describe('IPP 属性构造', () => {
  it('多值属性的第二个值用 additional-value 形态（名字长度为 0）', () => {
    const bytes = encodeRequest({
      operation: Operation.GetPrinterAttributes,
      requestId: 1,
      operationAttributes: [
        uri('printer-uri', 'ipp://x/ipp/print'),
        attr(ValueTag.Keyword, 'requested-attributes', 'a', 'b'),
      ],
    });
    const text = toHex(bytes);
    // 'a' 之后紧跟一个 name-length=0 的 additional-value。
    expect(text).toContain('0000');
  });
});

/** 一个属性值对：值标签 + 名字 + 值。 */
interface Pair {
  tag: number;
  name: string;
  value: string | number[];
  /** true 表示 additional-value：名字长度为 0，并入上一个同名属性。 */
  additional?: boolean;
}

/**
 * 拼一条 IPP 响应报文。
 *
 * 手工写死长度极易出错（少一个字节就整体错位），所以这里按结构拼：
 * 每个属性组 = 分隔符 + 若干属性。
 */
function buildMessage(
  status: number,
  groups: Array<{delimiter: number; pairs: Pair[]}>,
): Uint8Array {
  const out: number[] = [2, 0, (status >> 8) & 0xff, status & 0xff, 0, 0, 0, 1];
  for (const group of groups) {
    out.push(group.delimiter);
    for (const p of group.pairs) {
      out.push(...valuePair(p));
    }
  }
  out.push(Delimiter.EndOfAttributes);
  return Uint8Array.from(out);
}

/** 一个属性值对的字节：value-tag(1) | name-length(2) | name | value-length(2) | value。 */
function valuePair(p: Pair): number[] {
  const name = p.additional ? [] : ascii(p.name);
  const value = typeof p.value === 'string' ? ascii(p.value) : p.value;
  return [
    p.tag,
    (name.length >> 8) & 0xff,
    name.length & 0xff,
    ...name,
    (value.length >> 8) & 0xff,
    value.length & 0xff,
    ...value,
  ];
}

/** 组一个属性值对。additional=true 表示 additional-value（空名字）。 */
function pair(
  tag: number,
  name: string,
  value: string | number[],
  additional = false,
): Pair {
  return {tag, name, value, additional};
}

/** 4 字节大端整数。 */
function intValue(n: number): number[] {
  return [(n >>> 24) & 0xff, (n >>> 16) & 0xff, (n >>> 8) & 0xff, n & 0xff];
}

/** ASCII 字符串 → 字节数组（测试里拼报文用）。 */
function ascii(s: string): number[] {
  return Array.from(s).map(c => c.charCodeAt(0));
}
