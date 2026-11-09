/**
 * 最小 UTF-8 编解码。
 *
 * 不依赖 TextEncoder/TextDecoder：Hermes 不保证提供它们，而 IPP 的属性值、
 * mDNS 的 TXT 记录都是 UTF-8 文本，必须在字节与字符串之间精确往返。
 */

/** UTF-8 编码：字符串 → 字节。 */
export function utf8Encode(s: string): Uint8Array {
  const out: number[] = [];
  for (let i = 0; i < s.length; i++) {
    let c = s.codePointAt(i) as number;
    if (c > 0xffff) {
      // 代理对：占位符占两码元，跳过后一个码元。
      i++;
    }
    if (c < 0x80) {
      out.push(c);
    } else if (c < 0x800) {
      out.push(0xc0 | (c >> 6), 0x80 | (c & 0x3f));
    } else if (c < 0x10000) {
      out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
    } else {
      out.push(
        0xf0 | (c >> 18),
        0x80 | ((c >> 12) & 0x3f),
        0x80 | ((c >> 6) & 0x3f),
        0x80 | (c & 0x3f),
      );
    }
  }
  return Uint8Array.from(out);
}

/** UTF-8 解码：字节 → 字符串。非法序列按替换字符处理，不抛异常。 */
export function utf8Decode(bytes: Uint8Array): string {
  let out = '';
  let i = 0;
  while (i < bytes.length) {
    const b0 = bytes[i];
    let cp: number;
    let size: number;
    if (b0 < 0x80) {
      cp = b0;
      size = 1;
    } else if ((b0 & 0xe0) === 0xc0) {
      cp = b0 & 0x1f;
      size = 2;
    } else if ((b0 & 0xf0) === 0xe0) {
      cp = b0 & 0x0f;
      size = 3;
    } else if ((b0 & 0xf8) === 0xf0) {
      cp = b0 & 0x07;
      size = 4;
    } else {
      out += '�';
      i++;
      continue;
    }
    if (i + size > bytes.length) {
      out += '�';
      break;
    }
    let ok = true;
    for (let k = 1; k < size; k++) {
      const b = bytes[i + k];
      if ((b & 0xc0) !== 0x80) {
        ok = false;
        break;
      }
      cp = (cp << 6) | (b & 0x3f);
    }
    if (!ok) {
      out += '�';
      i++;
      continue;
    }
    i += size;
    if (cp > 0xffff) {
      cp -= 0x10000;
      out += String.fromCharCode(0xd800 + (cp >> 10), 0xdc00 + (cp & 0x3ff));
    } else {
      out += String.fromCharCode(cp);
    }
  }
  return out;
}

/** 字节数组 → 小写十六进制串（黄金字节文件的格式，无分隔符）。 */
export function toHex(bytes: Uint8Array): string {
  let s = '';
  for (let i = 0; i < bytes.length; i++) {
    s += bytes[i].toString(16).padStart(2, '0');
  }
  return s;
}

/**
 * 十六进制串 → 字节数组。
 *
 * 黄金字节文件（pkg/client/testdata/print_job.hex）与 Go 侧共用同一份
 * 事实源，两端都按「小写十六进制、无分隔符」读写，这里负责把它读回来。
 */
export function fromHex(text: string): Uint8Array {
  const clean = text.replace(/[\s]/g, '').toLowerCase();
  if (clean.length % 2 !== 0) {
    throw new Error(`utf8: hex string has odd length ${clean.length}`);
  }
  const out = new Uint8Array(clean.length / 2);
  for (let i = 0; i < out.length; i++) {
    const byte = parseInt(clean.substr(i * 2, 2), 16);
    if (Number.isNaN(byte)) {
      throw new Error(`utf8: invalid hex at offset ${i * 2}`);
    }
    out[i] = byte;
  }
  return out;
}
