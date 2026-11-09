/**
 * 简体中文文案。
 *
 * 规矩（ADR-0011 D4）：界面文案里不出现工程术语——不写地址、端口、令牌、
 * 打印协议、发现协议、后端、队列这些词，一律说「共享端 / 打印机 / 配对码 /
 * 地址」。禁术语的检查会扫这个文件（见 banned-words.test.ts）。
 */

import type {Strings} from './types';

export const zhCN: Strings = {
  appName: 'dyj 打印',

  discoverTitle: '共享端',
  discoverScan: '扫描',
  discoverScanning: '正在扫描…',
  discoverFound: '找到 {count} 个共享端',
  discoverNone: '没找到共享中的打印机',
  discoverScanFailed: '扫不到共享端，确认手机和共享端在同一个网络里',
  discoverAddByAddress: '按地址添加',
  discoverAddressHint: '共享端和手机要在同一个网络里',
  discoverAddressPlaceholder: '例如 192.168.1.20',
  discoverNotShared: '这是普通网络打印机，dyj 暂不支持它',
  discoverPairedBadge: '已连接',

  pairTitle: '输入配对码',
  pairHint: '请输入共享端屏幕上显示的 4 位数字',
  pairSubmit: '配对',
  pairWorking: '正在配对…',
  pairSuccess: '已连上 {name}',
  pairErrorBadPin: '配对码不对，请核对共享端屏幕上那 4 位数字',
  pairErrorLocked: '尝试次数太多，{minutes} 分钟后再试',
  pairErrorPinRequired: '配对码不能为空',
  pairErrorUnreachable: '连不上共享端，确认它在同一网络里并且已经打开了共享',
  pairErrorInternal: '共享端出了点问题，过一会儿再试',

  printTitle: '打印',
  printPickImage: '选一张图片',
  printPickText: '输入文字',
  printPickPdf: '选一个 PDF',
  printTextPlaceholder: '想打印的内容',
  printSend: '打印',
  printSending: '正在发送…',
  printPrinter: '打印机',
  printCopies: '份数',
  printPassthrough: '保持原样发送这个文件',
  printPassthroughUnavailable: '共享端不能直接收这个文件，已转成图片',
  printPickFirst: '先选一样要打印的内容',
  printTextTooLong: '文字太长了，先删掉一些',

  resultSent: '已发送到共享端（作业 #{id}）',
  resultPages: '共 {count} 页',
  resultFailed: '没打出来',
  resultRetry: '重试',
  resultRepair: '重新配对',
  resultTokenInvalid: '与共享端的连接已失效，需要重新配对',
  resultFormatUnsupported: '这台打印机不支持这个文件类型，换成图片试试',
  resultPaused: '共享端暂时不接收打印，去共享端上恢复一下',
  resultTooLarge: '文件太大了，换个小一点的试试',
  resultUnknown: '没打出来，过一会儿再试',

  commonCancel: '取消',
  commonBack: '返回',
  commonRemove: '删除',
};
