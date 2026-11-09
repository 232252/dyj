/**
 * 最小 DNS / DNS-SD 报文解析。
 *
 * 只解析发现共享端用得到的部分：PTR（服务类型 → 实例）、SRV（实例 → 端口与
 * 主机）、TXT（实例 → 键值）、A（主机 → IPv4）。够用即可，不做完整 DNS。
 *
 * 名字压缩（RFC 1035 §4.1.4）必须支持：服务端用 miekg/dns 打包，开启压缩后
 * 响应里到处是指针，不处理就会把整个报文读歪。
 *
 * 报文由 ArkTS 侧收包后原样传来，这里只做纯函数解析——便于单测。
 */

import {utf8Decode} from '../util/utf8';

/** 记录类型。 */
export const DnsType = {
  A: 1,
  PTR: 12,
  TXT: 16,
  AAAA: 28,
  SRV: 33,
  ANY: 255,
} as const;

/**
 * DNS-SD 服务类型：IPP 打印。
 *
 * 不带结尾的点——报文里的名字统一经 normalizeName 处理（转小写、去尾点），
 * 比较时两边形态必须一致。
 */
export const IPP_SERVICE_TYPE = '_ipp._tcp.local';

export interface DnsQuestion {
  name: string;
  type: number;
  cls: number;
}

export interface DnsRecord {
  /** 属主名，小写、无尾点。 */
  name: string;
  type: number;
  cls: number;
  ttl: number;
  /** PTR：实例名；SRV：目标主机名。 */
  target?: string;
  /** SRV：端口。 */
  port?: number;
  /** TXT：键值对。 */
  txt?: Record<string, string>;
  /** A：IPv4 文本。 */
  address?: string;
}

export interface DnsMessage {
  id: number;
  isResponse: boolean;
  questions: DnsQuestion[];
  answers: DnsRecord[];
  authorities: DnsRecord[];
  additionals: DnsRecord[];
}

/** 名字规范化：小写、去掉尾点。 */
export function normalizeName(name: string): string {
  return name.toLowerCase().replace(/\.$/, '');
}

/**
 * 解析一条 DNS 报文。
 *
 * 遇到截断或非法数据抛错——调用方（收包侧）据此丢弃这一包，而不是把半截数据
 * 当成有效结果。
 */
export function parseDnsMessage(msg: Uint8Array): DnsMessage {
  if (msg.length < 12) {
    throw new Error('dns: message shorter than header');
  }
  const flags = (msg[2] << 8) | msg[3];
  const qdCount = (msg[4] << 8) | msg[5];
  const anCount = (msg[6] << 8) | msg[7];
  const nsCount = (msg[8] << 8) | msg[9];
  const arCount = (msg[10] << 8) | msg[11];

  let pos = 12;
  const questions: DnsQuestion[] = [];
  for (let i = 0; i < qdCount; i++) {
    const name = readName(msg, pos);
    pos = name.next;
    if (pos + 4 > msg.length) {
      throw new Error('dns: truncated question');
    }
    questions.push({
      name: normalizeName(name.name),
      type: (msg[pos] << 8) | msg[pos + 1],
      cls: (msg[pos + 2] << 8) | msg[pos + 3],
    });
    pos += 4;
  }

  const readSection = (count: number): DnsRecord[] => {
    const out: DnsRecord[] = [];
    for (let i = 0; i < count; i++) {
      const owner = readName(msg, pos);
      pos = owner.next;
      if (pos + 10 > msg.length) {
        throw new Error('dns: truncated resource record');
      }
      const type = (msg[pos] << 8) | msg[pos + 1];
      const cls = (msg[pos + 2] << 8) | msg[pos + 3];
      const ttl =
        msg[pos + 4] * 0x1000000 +
        ((msg[pos + 5] << 16) | (msg[pos + 6] << 8) | msg[pos + 7]);
      const rdLength = (msg[pos + 8] << 8) | msg[pos + 9];
      pos += 10;
      if (pos + rdLength > msg.length) {
        throw new Error('dns: truncated rdata');
      }
      const rdata = msg.subarray(pos, pos + rdLength);
      pos += rdLength;
      out.push(buildRecord(owner.name, type, cls, ttl, rdata, msg));
    }
    return out;
  };

  return {
    id: (msg[0] << 8) | msg[1],
    isResponse: (flags & 0x8000) !== 0,
    questions,
    answers: readSection(anCount),
    authorities: readSection(nsCount),
    additionals: readSection(arCount),
  };
}

function buildRecord(
  owner: string,
  type: number,
  cls: number,
  ttl: number,
  rdata: Uint8Array,
  whole: Uint8Array,
): DnsRecord {
  const rec: DnsRecord = {name: normalizeName(owner), type, cls, ttl};
  if (type === DnsType.PTR) {
    rec.target = normalizeName(readName(whole, rdataStart(whole, rdata)).name);
  } else if (type === DnsType.SRV) {
    // priority(2) | weight(2) | port(2) | target(name)
    if (rdata.length < 7) {
      throw new Error('dns: short SRV rdata');
    }
    rec.port = (rdata[4] << 8) | rdata[5];
    const target = readName(whole, rdataStart(whole, rdata) + 6);
    rec.target = normalizeName(target.name);
  } else if (type === DnsType.TXT) {
    rec.txt = parseTxt(rdata);
  } else if (type === DnsType.A) {
    if (rdata.length !== 4) {
      throw new Error('dns: A record must be 4 octets');
    }
    rec.address = `${rdata[0]}.${rdata[1]}.${rdata[2]}.${rdata[3]}`;
  }
  return rec;
}

/**
 * rdata 在整条报文里的起始偏移。
 *
 * 压缩指针指向报文开头，所以必须相对整条报文定位；这里用「rdata 之前最后一个
 * 已知绝对位置」推不出来，改为扫描——rdata 紧跟在名字之后，所以从记录起点
 * 反推即可。buildRecord 的调用方已经把 rdata 切成 subarray，subarray 的
 * byteOffset 保留了原报文里的位置。
 */
function rdataStart(whole: Uint8Array, rdata: Uint8Array): number {
  return rdata.byteOffset;
}

/** TXT rdata：若干个「长度 + 文本」的串。 */
function parseTxt(rdata: Uint8Array): Record<string, string> {
  const out: Record<string, string> = {};
  let pos = 0;
  while (pos < rdata.length) {
    const len = rdata[pos];
    pos++;
    if (pos + len > rdata.length) {
      break;
    }
    const text = utf8Decode(rdata.subarray(pos, pos + len));
    pos += len;
    const eq = text.indexOf('=');
    if (eq > 0) {
      out[text.slice(0, eq)] = text.slice(eq + 1);
    } else if (text.length > 0) {
      out[text] = '';
    }
  }
  return out;
}

/**
 * 读一个（可能被压缩的）域名。
 *
 * 返回压缩后的名字与「下一个字节位置」——指针只前进不回头，所以调用方拿到的是
 * 记录里真正的下一个字段位置。
 */
function readName(msg: Uint8Array, offset: number): {name: string; next: number} {
  const labels: string[] = [];
  let pos = offset;
  let next = -1;
  let hops = 0;
  while (pos < msg.length) {
    if (hops++ > 128) {
      throw new Error('dns: name compression loop');
    }
    const len = msg[pos];
    if (len === 0) {
      pos++;
      break;
    }
    if ((len & 0xc0) === 0xc0) {
      if (pos + 1 >= msg.length) {
        throw new Error('dns: truncated compression pointer');
      }
      const ptr = ((len & 0x3f) << 8) | msg[pos + 1];
      if (next < 0) {
        next = pos + 2;
      }
      pos = ptr;
      continue;
    }
    if (pos + 1 + len > msg.length) {
      throw new Error('dns: truncated label');
    }
    labels.push(utf8Decode(msg.subarray(pos + 1, pos + 1 + len)));
    pos += 1 + len;
  }
  return {name: labels.join('.'), next: next >= 0 ? next : pos};
}
