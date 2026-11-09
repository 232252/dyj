/**
 * 用户可见文案的结构。
 *
 * 界面文案一律走这里，不在页面里直接写中文——这样禁术语的检查能一次扫全。
 */

/** 一条文案里的占位符。 */
export interface TextParams {
  [key: string]: string | number;
}

export interface Strings {
  appName: string;

  // 发现
  discoverTitle: string;
  discoverScan: string;
  discoverScanning: string;
  discoverFound: string;
  discoverNone: string;
  discoverScanFailed: string;
  discoverAddByAddress: string;
  discoverAddressHint: string;
  discoverAddressPlaceholder: string;
  discoverNotShared: string;
  discoverPairedBadge: string;

  // 配对
  pairTitle: string;
  pairHint: string;
  pairSubmit: string;
  pairWorking: string;
  pairSuccess: string;
  pairErrorBadPin: string;
  pairErrorLocked: string;
  pairErrorPinRequired: string;
  pairErrorUnreachable: string;
  pairErrorInternal: string;

  // 打印
  printTitle: string;
  printPickImage: string;
  printPickText: string;
  printPickPdf: string;
  printTextPlaceholder: string;
  printSend: string;
  printSending: string;
  printPrinter: string;
  printCopies: string;
  printPassthrough: string;
  printPassthroughUnavailable: string;
  printPickFirst: string;
  printTextTooLong: string;

  // 结果
  resultSent: string;
  resultFailed: string;
  resultRetry: string;
  resultRepair: string;
  resultTokenInvalid: string;
  resultFormatUnsupported: string;
  resultPaused: string;
  resultTooLarge: string;
  resultUnknown: string;
  resultPages: string;

  // 通用
  commonCancel: string;
  commonBack: string;
  commonRemove: string;
}
