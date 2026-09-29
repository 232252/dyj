// Locale picker: zh-CN (default) or en, chosen from navigator.language.
// Everything else in the app imports only t() from this file.
import { strings as zhCN } from './zh-CN.js';
import { strings as en } from './en.js';

const tables = { 'zh-cn': zhCN, en };
const nav = (navigator.language || 'zh-CN').toLowerCase();
export const locale = nav.startsWith('zh') ? 'zh-cn' : 'en';

export function t(key, vars) {
  let s = tables[locale][key] ?? zhCN[key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) s = s.replaceAll('{' + k + '}', String(v));
  }
  return s;
}
