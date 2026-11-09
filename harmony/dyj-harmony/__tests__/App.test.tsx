/**
 * 应用冒烟测试：能不能渲染出发现页。
 *
 * 原生模块在这里是替身——单测环境里没有 ArkTS 侧的实现，扫不到东西也只是
 * 空列表，不该把渲染搞崩。
 */

import React from 'react';
import ReactTestRenderer from 'react-test-renderer';

jest.mock('../src/native', () => ({
  scanMdns: jest.fn(async () => []),
  postJson: jest.fn(async () => ({status: 200, body: new Uint8Array(0)})),
  postDocument: jest.fn(async () => ({status: 200, body: new Uint8Array(0)})),
  getPref: jest.fn(async () => ''),
  setPref: jest.fn(async () => {}),
  removePref: jest.fn(async () => {}),
  pickFile: jest.fn(async () => ({
    cancelled: true,
    path: '',
    name: '',
    mime: '',
    size: 0,
  })),
  readFileBytes: jest.fn(async () => new Uint8Array(0)),
  imageToJpeg: jest.fn(async () => new Uint8Array(0)),
  renderPdfPages: jest.fn(async () => []),
  pdfPageCount: jest.fn(async () => 0),
}));

import App from '../App';

test('renders correctly', async () => {
  await ReactTestRenderer.act(() => {
    ReactTestRenderer.create(<App />);
  });
});
