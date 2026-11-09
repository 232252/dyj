/**
 * 把一组 mDNS 报文整理成「共享端实例」列表。
 *
 * 为什么要去重：服务端在多网卡机器上会为同一个实例名发布多条 SRV 与 A 记录
 * （每个网卡一个主机名，见 pkg/discovery/mdns.go 的 hostFor），手机扫一次会
 * 收到好几条指向同一实例的记录。若不去重，列表里会出现几个同名共享端，
 * 用户看着像重复项。
 *
 * 去重键是**实例全名**（如 "dyj on desktop._ipp._tcp.local"），不是主机名。
 */

import {DnsMessage, DnsType, IPP_SERVICE_TYPE, normalizeName} from './parser';

/** 一个可达端点：主机名 + IPv4。 */
export interface MdnsEndpoint {
  host: string;
  address: string;
}

/** 一个扫描到的实例。 */
export interface MdnsInstance {
  /** 实例全名，小写。 */
  instance: string;
  /** 展示名：去掉服务类型后缀的实例标签。 */
  name: string;
  /** 服务端口（来自 SRV）。 */
  port: number;
  /** 可达端点，按发现顺序。 */
  endpoints: MdnsEndpoint[];
  /** TXT 键值。 */
  txt: Record<string, string>;
  /** 是否是 dyj 共享端（TXT 里带 dyj=1）。 */
  isDyj: boolean;
}

/**
 * 从报文里收集实例。
 *
 * 输入可以是收到的全部响应与通告；同一实例散落在多条报文里是常态，这里合并。
 */
export function collectInstances(messages: DnsMessage[]): MdnsInstance[] {
  // instance → 该实例的 SRV / TXT / A 线索
  const srvByInstance = new Map<
    string,
    Array<{host: string; port: number}>
  >();
  const txtByInstance = new Map<string, Record<string, string>>();
  const addressByHost = new Map<string, string>();
  const instances = new Set<string>();

  for (const msg of messages) {
    for (const rec of msg.answers.concat(msg.additionals)) {
      switch (rec.type) {
        case DnsType.PTR:
          // 只收 IPP 服务的 PTR；其它 _ipp._tcp 实例也是打印，但 dyj 只认自己
          // 通告的那些（TXT 里带 dyj=1），所以这里先全部收下，判定放在后面。
          if (rec.target) {
            instances.add(rec.target);
          }
          break;
        case DnsType.SRV:
          if (rec.target && rec.port) {
            const list = srvByInstance.get(rec.name) ?? [];
            list.push({host: rec.target, port: rec.port});
            srvByInstance.set(rec.name, list);
            instances.add(rec.name);
          }
          break;
        case DnsType.TXT:
          if (rec.txt) {
            const merged = txtByInstance.get(rec.name) ?? {};
            Object.assign(merged, rec.txt);
            txtByInstance.set(rec.name, merged);
            instances.add(rec.name);
          }
          break;
        case DnsType.A:
          if (rec.address) {
            addressByHost.set(rec.name, rec.address);
          }
          break;
        default:
          break;
      }
    }
  }

  const out: MdnsInstance[] = [];
  for (const instance of instances) {
    if (!instance.endsWith(IPP_SERVICE_TYPE)) {
      continue; // 只认 _ipp._tcp 服务
    }
    const srv = srvByInstance.get(instance) ?? [];
    if (srv.length === 0) {
      continue; // 只有 PTR，没有 SRV 就不知道端口与主机，不足以用
    }
    const txt = txtByInstance.get(instance) ?? {};
    const endpoints: MdnsEndpoint[] = [];
    for (const s of srv) {
      const address = addressByHost.get(s.host);
      if (address && !endpoints.some(e => e.host === s.host)) {
        endpoints.push({host: s.host, address});
      }
    }
    out.push({
      instance,
      name: displayName(instance),
      port: srv[0].port,
      endpoints,
      txt,
      isDyj: txt.dyj === '1',
    });
  }
  return out;
}

/**
 * 展示名：去掉服务类型后缀与结尾的点。
 *
 * "dyj on desktop._ipp._tcp.local" → "dyj on desktop"
 */
export function displayName(instance: string): string {
  const name = normalizeName(instance);
  const suffix = `.${IPP_SERVICE_TYPE}`;
  const trimmed = name.endsWith(suffix)
    ? name.slice(0, name.length - suffix.length)
    : name;
  return trimmed || name;
}
