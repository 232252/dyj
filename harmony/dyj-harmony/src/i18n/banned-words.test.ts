/**
 * 禁术语检查。
 *
 * 规矩来自 ADR-0011 D4：用户可见界面不出现工程术语。检查对象是两份文案表，
 * 禁用词表与服务端那份（pkg/admin/i18n_banned_test.go）保持一致——服务端管
 * 网页，这一侧管手机，文案规范是同一套。
 */

import {enUS} from './en-US';
import {zhCN} from './zh-CN';

/** 禁用词：ASCII 按不区分大小写匹配，中文按原文匹配。 */
const BANNED = [
  'uri',
  'url',
  'ipp',
  'mdns',
  'tls',
  'token',
  'port',
  'backend',
  'driver',
  'queue',
  'hostname',
  'iface',
  '端口',
  '后端',
  '队列',
  '驱动',
  'mDNS',
  '主机名',
  '监听接口',
];

const TABLES: Array<[string, Record<string, string>]> = [
  ['zh-CN', zhCN as unknown as Record<string, string>],
  ['en-US', enUS as unknown as Record<string, string>],
];

describe('界面文案不出现工程术语', () => {
  it.each(TABLES)('%s 文案干净', (_name, table) => {
    const problems: string[] = [];
    for (const [key, value] of Object.entries(table)) {
      const lower = value.toLowerCase();
      for (const word of BANNED) {
        const hit = isAscii(word)
          ? lower.includes(word)
          : value.includes(word);
        if (hit) {
          problems.push(`${key}: 出现「${word}」——${value}`);
        }
      }
    }
    expect(problems).toEqual([]);
  });
});

function isAscii(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    if (s.charCodeAt(i) > 127) {
      return false;
    }
  }
  return true;
}
