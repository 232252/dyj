/**
 * 地址处理的小测试。
 *
 * 这里最容易出错的是「忘了补 http://」——配对请求会直接打到一个非法 URL 上，
 * 而且失败得很隐晦（连不上，而不是「格式不对」）。所以把规则钉住。
 */

import {baseUrl, manualAddresses} from './address';

describe('baseUrl', () => {
  it('裸地址补上 http://', () => {
    expect(baseUrl('192.168.1.20:6631')).toBe('http://192.168.1.20:6631');
  });

  it('带协议的原样用', () => {
    expect(baseUrl('http://192.168.1.20:6631')).toBe(
      'http://192.168.1.20:6631',
    );
  });
});

describe('manualAddresses', () => {
  it('只写主机名时补默认端口', () => {
    expect(manualAddresses('192.168.1.20')).toEqual(['192.168.1.20:6631']);
  });

  it('带了端口就原样用', () => {
    expect(manualAddresses('192.168.1.20:8080')).toEqual(['192.168.1.20:8080']);
  });

  it('带协议时去掉尾斜杠', () => {
    expect(manualAddresses('http://192.168.1.20:80/')).toEqual([
      'http://192.168.1.20:80',
    ]);
  });

  it('空输入没有候选', () => {
    expect(manualAddresses('   ')).toEqual([]);
  });
});
