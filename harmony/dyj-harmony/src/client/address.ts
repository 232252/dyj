/**
 * 地址处理。
 *
 * 共享端的地址在三处出现：mDNS 扫出来的、手工填的、配对记录里存着的。
 * 统一在这里转成基址，免得某处忘了补 `http://`。
 */

/**
 * 地址 → HTTP 基址。
 *
 * 服务端把管理页、配对、打印都挂在同一个端口上，所以基址就是 `http://<地址>`。
 * 已经带协议的原样用。
 */
export function baseUrl(address: string): string {
  return address.includes('://') ? address : `http://${address}`;
}

/**
 * 把手工填的地址整理成候选列表。
 *
 * 允许只填主机名（端口用默认 6631），也允许带端口或带协议。
 */
export function manualAddresses(input: string, defaultPort = 6631): string[] {
  const text = input.trim();
  if (!text) {
    return [];
  }
  if (text.includes('://')) {
    return [text.replace(/\/+$/, '')];
  }
  if (/:(\d+)$/.test(text)) {
    return [text];
  }
  return [`${text}:${defaultPort}`];
}
