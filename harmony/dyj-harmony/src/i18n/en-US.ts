/**
 * 英文文案（预留；v1 默认中文）。
 *
 * 同样不许出现工程术语——禁术语的检查会扫这个文件。
 */

import type {Strings} from './types';

export const enUS: Strings = {
  appName: 'dyj Print',

  discoverTitle: 'Shared devices',
  discoverScan: 'Scan',
  discoverScanning: 'Scanning…',
  discoverFound: 'Found {count} shared devices',
  discoverNone: 'No shared printer found',
  discoverScanFailed: 'Could not scan. Make sure this phone and the shared device are on the same network',
  discoverAddByAddress: 'Add by address',
  discoverAddressHint: 'The shared device and this phone must be on the same network',
  discoverAddressPlaceholder: 'For example 192.168.1.20',
  discoverNotShared: 'This is a regular network printer; dyj cannot use it',
  discoverPairedBadge: 'Connected',

  pairTitle: 'Enter the pairing code',
  pairHint: 'Type the 4 digits shown on the shared device',
  pairSubmit: 'Pair',
  pairWorking: 'Pairing…',
  pairSuccess: 'Connected to {name}',
  pairErrorBadPin: 'That code is not right. Check the 4 digits on the shared device',
  pairErrorLocked: 'Too many attempts. Try again in {minutes} minutes',
  pairErrorPinRequired: 'The pairing code cannot be empty',
  pairErrorUnreachable: 'Cannot reach the shared device. Make sure it is on the same network and sharing is on',
  pairErrorInternal: 'The shared device hit a problem. Try again later',

  printTitle: 'Print',
  printPickImage: 'Choose a picture',
  printPickText: 'Type some text',
  printPickPdf: 'Choose a PDF',
  printTextPlaceholder: 'What to print',
  printSend: 'Print',
  printSending: 'Sending…',
  printPrinter: 'Printer',
  printCopies: 'Copies',
  printPassthrough: 'Send the file as it is',
  printPassthroughUnavailable: 'The shared device cannot take this file directly; it was turned into a picture',
  printPickFirst: 'Pick something to print first',
  printTextTooLong: 'That text is too long. Shorten it first',

  resultSent: 'Sent to the shared device (job #{id})',
  resultPages: '{count} pages in total',
  resultFailed: 'Could not print',
  resultRetry: 'Try again',
  resultRepair: 'Pair again',
  resultTokenInvalid: 'The connection to the shared device expired. Pair again',
  resultFormatUnsupported: 'This printer does not accept that kind of file. Try a picture instead',
  resultPaused: 'The shared device is not accepting jobs right now. Resume it there',
  resultTooLarge: 'That file is too large. Try a smaller one',
  resultUnknown: 'Could not print. Try again later',

  commonCancel: 'Cancel',
  commonBack: 'Back',
  commonRemove: 'Remove',
};
