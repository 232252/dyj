/**
 * mDNS 报文解析与实例去重的测试。
 *
 * 报文在测试里手工拼出来（带名字压缩指针），因为真实报文只在局域网里抓得到，
 * 录进仓库会随服务端实现漂移。拼出来的字节严格按 RFC 1035 来，服务端用
 * miekg/dns 打包的格式与之一致。
 */

import {collectInstances, displayName} from './dedupe';
import {DnsType, IPP_SERVICE_TYPE, parseDnsMessage} from './parser';

/** 按 RFC 1035 写名字：长度 + 标签，已写过的后缀用压缩指针。 */
class NameWriter {
  bytes: number[] = [];
  private offsets = new Map<string, number>();

  write(name: string): void {
    const lower = name.toLowerCase();
    const known = this.offsets.get(lower);
    if (known !== undefined) {
      this.bytes.push(0xc0 | (known >> 8), known & 0xff);
      return;
    }
    const labels = lower.split('.').filter(Boolean);
    for (let i = 0; i < labels.length; i++) {
      const suffix = labels.slice(i).join('.');
      if (!this.offsets.has(suffix)) {
        this.offsets.set(suffix, this.bytes.length);
      }
    }
    for (const label of labels) {
      this.bytes.push(label.length, ...ascii(label));
    }
    this.bytes.push(0);
  }
}

interface RecordSpec {
  name: string;
  type: number;
  /** rdata 的原始字节（已按需含压缩指针，由 build 提供）。 */
  rdata: (w: NameWriter) => number[];
}

/** 拼一条 DNS 响应（全部放 Answer 段）。 */
function buildResponse(records: RecordSpec[]): Uint8Array {
  const w = new NameWriter();
  const header = [0x00, 0x00, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00];
  const body: number[] = [];
  // 头 + 记录区
  const headerAndBody = [...header];
  const start = headerAndBody.length;
  void start;
  for (const rec of records) {
    const before = w.bytes.length;
    w.write(rec.name);
    const nameBytes = w.bytes.slice(before);
    headerAndBody.push(...nameBytes);
    const rdata = rec.rdata(w);
    headerAndBody.push((rec.type >> 8) & 0xff, rec.type & 0xff);
    headerAndBody.push(0x00, 0x01); // class IN
    headerAndBody.push(0x00, 0x00, 0x00, 0x78); // ttl 120
    headerAndBody.push((rdata.length >> 8) & 0xff, rdata.length & 0xff);
    headerAndBody.push(...rdata);
  }
  // 回填记录数
  headerAndBody[6] = (records.length >> 8) & 0xff;
  headerAndBody[7] = records.length & 0xff;
  return Uint8Array.from(headerAndBody);
}

function ptrRecord(name: string, target: string): RecordSpec {
  return {
    name,
    type: DnsType.PTR,
    rdata: w => {
      const before = w.bytes.length;
      w.write(target);
      return w.bytes.slice(before);
    },
  };
}

function srvRecord(name: string, port: number, target: string): RecordSpec {
  return {
    name,
    type: DnsType.SRV,
    rdata: w => {
      const before = w.bytes.length;
      w.write(target);
      return [0x00, 0x00, 0x00, 0x00, (port >> 8) & 0xff, port & 0xff, ...w.bytes.slice(before)];
    },
  };
}

function txtRecord(name: string, txt: string[]): RecordSpec {
  return {
    name,
    type: DnsType.TXT,
    rdata: () => {
      const out: number[] = [];
      for (const t of txt) {
        const b = ascii(t);
        out.push(b.length, ...b);
      }
      return out;
    },
  };
}

function aRecord(name: string, ip: string): RecordSpec {
  return {
    name,
    type: DnsType.A,
    rdata: () => ip.split('.').map(x => parseInt(x, 10)),
  };
}

const INSTANCE = 'dyj on desktop._ipp._tcp.local.';

describe('DNS 报文解析', () => {
  it('读出 PTR / SRV / TXT / A 四类记录', () => {
    const msg = parseDnsMessage(
      buildResponse([
        ptrRecord(IPP_SERVICE_TYPE, INSTANCE),
        srvRecord(INSTANCE, 6631, 'desktop.local.'),
        txtRecord(INSTANCE, ['dyj=1', 'pair=1', 'dyjver=3']),
        aRecord('desktop.local.', '192.168.1.20'),
      ]),
    );

    expect(msg.isResponse).toBe(true);
    expect(msg.answers).toHaveLength(4);

    const ptr = msg.answers.find(r => r.type === DnsType.PTR);
    expect(ptr?.target).toBe('dyj on desktop._ipp._tcp.local');

    const srv = msg.answers.find(r => r.type === DnsType.SRV);
    expect(srv?.port).toBe(6631);
    expect(srv?.target).toBe('desktop.local');

    const txt = msg.answers.find(r => r.type === DnsType.TXT);
    expect(txt?.txt).toEqual({dyj: '1', pair: '1', dyjver: '3'});

    const a = msg.answers.find(r => r.type === DnsType.A);
    expect(a?.address).toBe('192.168.1.20');
  });

  it('名字压缩指针不破坏后续记录', () => {
    // 三条记录共用了同一个实例名，压缩后只有第一条带完整名字。
    const msg = parseDnsMessage(
      buildResponse([
        ptrRecord(IPP_SERVICE_TYPE, INSTANCE),
        srvRecord(INSTANCE, 6631, 'desktop.local.'),
        aRecord('desktop.local.', '10.0.0.9'),
      ]),
    );
    expect(msg.answers).toHaveLength(3);
    expect(msg.answers[1].port).toBe(6631);
    expect(msg.answers[2].address).toBe('10.0.0.9');
  });

  it('拒绝截断的报文', () => {
    expect(() => parseDnsMessage(Uint8Array.of(1, 2, 3))).toThrow();
  });
});

describe('实例去重', () => {
  it('同一实例的多网卡 SRV 合并成一条，保留多个可达地址', () => {
    const msg = parseDnsMessage(
      buildResponse([
        ptrRecord(IPP_SERVICE_TYPE, INSTANCE),
        srvRecord(INSTANCE, 6631, 'desktop.local.'),
        srvRecord(INSTANCE, 6631, 'desktop-eth.local.'),
        aRecord('desktop.local.', '192.168.1.20'),
        aRecord('desktop-eth.local.', '10.0.0.9'),
        txtRecord(INSTANCE, ['dyj=1', 'pair=1']),
      ]),
    );
    const instances = collectInstances([msg]);
    expect(instances).toHaveLength(1);
    expect(instances[0].port).toBe(6631);
    expect(instances[0].isDyj).toBe(true);
    expect(instances[0].endpoints.map(e => e.address).sort()).toEqual([
      '10.0.0.9',
      '192.168.1.20',
    ]);
  });

  it('多条报文里的同一实例也只出一条', () => {
    const first = parseDnsMessage(
      buildResponse([ptrRecord(IPP_SERVICE_TYPE, INSTANCE)]),
    );
    const second = parseDnsMessage(
      buildResponse([
        srvRecord(INSTANCE, 6631, 'desktop.local.'),
        aRecord('desktop.local.', '192.168.1.20'),
      ]),
    );
    expect(collectInstances([first, second])).toHaveLength(1);
  });

  it('没有 SRV 的实例不出现（不知道端口就没法用）', () => {
    const msg = parseDnsMessage(
      buildResponse([ptrRecord(IPP_SERVICE_TYPE, INSTANCE)]),
    );
    expect(collectInstances([msg])).toHaveLength(0);
  });

  it('非 dyj 的打印实例会被标成非共享端', () => {
    const msg = parseDnsMessage(
      buildResponse([
        ptrRecord(IPP_SERVICE_TYPE, '普通打印机._ipp._tcp.local.'),
        srvRecord('普通打印机._ipp._tcp.local.', 631, 'printer.local.'),
        aRecord('printer.local.', '192.168.1.30'),
      ]),
    );
    const instances = collectInstances([msg]);
    expect(instances).toHaveLength(1);
    expect(instances[0].isDyj).toBe(false);
  });

  it('展示名去掉服务类型后缀', () => {
    expect(displayName(INSTANCE)).toBe('dyj on desktop');
  });
});

function ascii(s: string): number[] {
  return Array.from(s).map(c => c.charCodeAt(0));
}
