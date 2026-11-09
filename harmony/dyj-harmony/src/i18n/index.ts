/**
 * 文案入口。
 *
 * 界面上一律用 t('key') 取文案，不在页面里直接写中文——这样禁术语的检查能一次
 * 扫全（见 banned-words.test.ts），新加文案漏检查就会红。
 */

import { enUS } from './en-US';
import { zhCN } from './zh-CN';
import type {Strings, TextParams} from './types';

/** 支持的语言。 */
const LOCALES: Record<string, Strings> = {
  'zh-CN': zhCN,
  'en-US': enUS,
};

/** 当前语言，默认简体中文。 */
let current: Strings = zhCN;

/** 切换语言；不认识的语言回落简体中文。 */
export function setLocale(locale: string): void {
  current = LOCALES[locale] ?? zhCN;
}

/** 当前文案表。 */
export function strings(): Strings {
  return current;
}

/**
 * 取一条文案，并把 {占位符} 换成参数。
 *
 * 缺参数时保留 {name} 原样——文案里占位符名写错，一眼能看出来。
 */
export function t(key: keyof Strings, params?: TextParams): string {
  const raw = current[key];
  if (!params) {
    return raw;
  }
  return raw.replace(/\{(\w+)\}/g, (whole: string, name: string) => {
    const value = params[name];
    return value === undefined ? whole : String(value);
  });
}
