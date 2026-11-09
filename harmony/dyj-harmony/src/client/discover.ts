/**
 * 发现共享端。
 *
 * 要问两轮：服务端对「有哪些打印服务」只回 PTR，对「这个服务在哪」才回
 * SRV/TXT/A（见 pkg/discovery/mdns.go）。所以先扫一轮拿到实例名，再带着实例名
 * 扫第二轮。
 *
 * 解析与去重都在纯函数里（src/mdns），这一层只负责「扫两轮」和「拼地址」。
 */

import {collectInstances, MdnsInstance} from '../mdns/dedupe';
import {parseDnsMessage} from '../mdns/parser';
import {scanMdns} from '../native';

export {baseUrl, manualAddresses} from './address';

/** 扫到的一个共享端。 */
export interface DiscoveredHost {
  /** 实例全名（小写）。 */
  instance: string;
  /** 展示名。 */
  name: string;
  /** 可试的地址（host:port），按发现顺序。 */
  addresses: string[];
  /** 是不是 dyj 共享端（TXT 里带 dyj=1）。 */
  isDyj: boolean;
}

/** 第一轮只找实例名，快一点；第二轮等回应，给足时间。 */
const FIRST_ROUND_MS = 1500;
const SECOND_ROUND_MS = 2500;

/** 扫一轮局域网，把发现的共享端整理成列表。 */
export async function discover(): Promise<DiscoveredHost[]> {
  const first = await scanMdns(FIRST_ROUND_MS);
  const names = collectInstances(first.map(parseDnsMessage)).map(i => i.instance);
  const second = names.length > 0 ? await scanMdns(SECOND_ROUND_MS, names) : [];
  const instances = collectInstances(first.concat(second).map(parseDnsMessage));
  return instances.filter(i => i.endpoints.length > 0).map(toHost);
}

function toHost(instance: MdnsInstance): DiscoveredHost {
  const addresses: string[] = [];
  for (const endpoint of instance.endpoints) {
    const address = `${endpoint.address}:${instance.port}`;
    if (!addresses.includes(address)) {
      addresses.push(address);
    }
  }
  return {
    instance: instance.instance,
    name: instance.name,
    addresses,
    isDyj: instance.isDyj,
  };
}
