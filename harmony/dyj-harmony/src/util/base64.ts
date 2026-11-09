/**
 * base64 编解码。
 *
 * Hermes 不保证提供 btoa/atob，而 ArkTS 侧与 JS 侧之间传二进制只能用文本，
 * 所以自己实现一份——纯函数，好测。
 *
 * 只做标准 base64（含 '+' '/' 与 '=' 补位），不做 URL-safe 变体：ArkTS 侧
 * util.Base64Helper 用的就是标准表。
 */

const ALPHABET =
  'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

const DECODE_TABLE: number[] = (() => {
  const table = new Array<number>(256).fill(-1);
  for (let i = 0; i < ALPHABET.length; i++) {
    table[ALPHABET.charCodeAt(i)] = i;
  }
  return table;
})();

/** 字节数组 → 标准 base64 文本。 */
export function base64Encode(bytes: Uint8Array): string {
  let out = '';
  let i = 0;
  for (; i + 2 < bytes.length; i += 3) {
    const n = (bytes[i] << 16) | (bytes[i + 1] << 8) | bytes[i + 2];
    out +=
      ALPHABET[(n >> 18) & 63] +
      ALPHABET[(n >> 12) & 63] +
      ALPHABET[(n >> 6) & 63] +
      ALPHABET[n & 63];
  }
  const rest = bytes.length - i;
  if (rest === 1) {
    const n = bytes[i] << 16;
    out += ALPHABET[(n >> 18) & 63] + ALPHABET[(n >> 12) & 63] + '==';
  } else if (rest === 2) {
    const n = (bytes[i] << 16) | (bytes[i + 1] << 8);
    out +=
      ALPHABET[(n >> 18) & 63] +
      ALPHABET[(n >> 12) & 63] +
      ALPHABET[(n >> 6) & 63] +
      '=';
  }
  return out;
}

/** 标准 base64 文本 → 字节数组。 */
export function base64Decode(text: string): Uint8Array {
  const clean = text.replace(/[\s=]/g, '');
  const out = new Uint8Array(Math.floor((clean.length * 3) / 4));
  let outLen = 0;
  let buffer = 0;
  let bits = 0;
  for (let i = 0; i < clean.length; i++) {
    const v = DECODE_TABLE[clean.charCodeAt(i)];
    if (v < 0) {
      throw new Error(`base64: invalid character at ${i}`);
    }
    buffer = (buffer << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[outLen++] = (buffer >> bits) & 0xff;
    }
  }
  return out.subarray(0, outLen);
}
