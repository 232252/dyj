/**
 * DyjMdns：局域网发现。
 *
 * 这里只做「收包」——加入组播组、发查询、把窗口期内收到的原始 DNS 报文原样
 * 交回；解析与去重在 TS 侧（可单测）。
 *
 * 要问两轮：服务端对「有哪些打印服务」只回 PTR，对「这个服务在哪」才回
 * SRV/TXT/A。TS 侧拿到实例名后再带着名字问一次。
 */

import { UITurboModule, UITurboModuleContext } from '@rnoh/react-native-openharmony';
import { TM } from '../generated/ts';
import { socket } from '@kit.NetworkKit';
import { B64 } from './B64';

/** mDNS 组播组与端口（RFC 6762）。 */
const MDNS_GROUP: string = '224.0.0.251';
const MDNS_PORT: number = 5353;

/** 要浏览的服务类型。 */
const IPP_SERVICE: string = '_ipp._tcp.local';

/** DNS 记录类型。 */
const TYPE_PTR: number = 12;
const TYPE_ANY: number = 255;

interface MdnsMessageEvent {
  message: ArrayBuffer;
  remoteInfo: socket.SocketRemoteInfo;
}

export class DyjMdnsModule extends UITurboModule implements TM.DyjMdns.Spec {
  static readonly NAME: string = 'DyjMdns';

  constructor(ctx: UITurboModuleContext) {
    super(ctx);
  }

  async scan(timeoutMs: number, names: string[]): Promise<string> {
    const packets: string[] = [];
    const mdns = socket.constructMulticastSocketInstance();
    try {
      await mdns.bind({ address: '0.0.0.0', port: MDNS_PORT });
      await mdns.addMembership({ address: MDNS_GROUP });
      mdns.on('message', (event: MdnsMessageEvent) => {
        packets.push(B64.encode(new Uint8Array(event.message)));
      });

      await mdns.send({
        data: this.buildQuery(IPP_SERVICE, TYPE_PTR),
        address: { address: MDNS_GROUP, port: MDNS_PORT },
      });
      for (const name of names) {
        await mdns.send({
          data: this.buildQuery(name, TYPE_ANY),
          address: { address: MDNS_GROUP, port: MDNS_PORT },
        });
      }

      await this.sleep(timeoutMs);
    } finally {
      mdns.close();
    }
    return JSON.stringify(packets);
  }

  /**
   * 拼一条 DNS 查询报文。
   *
   * 头 12 字节 + 一条 question：名字用长度前缀的标签序列，QTYPE 决定问什么。
   */
  private buildQuery(name: string, qtype: number): ArrayBuffer {
    const bytes: number[] = [];
    // 头：ID=0、flags=0、QDCOUNT=1，其余计数为 0。
    bytes.push(0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0);
    const labels = name.toLowerCase().split('.');
    for (const label of labels) {
      if (label.length === 0) {
        continue;
      }
      bytes.push(label.length);
      for (let i = 0; i < label.length; i++) {
        bytes.push(label.charCodeAt(i) & 0xff);
      }
    }
    bytes.push(0);
    bytes.push((qtype >> 8) & 0xff, qtype & 0xff);
    bytes.push(0, 1); // QCLASS = IN
    return new Uint8Array(bytes).buffer;
  }

  private sleep(ms: number): Promise<void> {
    return new Promise<void>((resolve: () => void) => {
      setTimeout(() => resolve(), ms);
    });
  }
}
