/**
 * 格式协商与降级的测试。
 */

import {
  FORMAT_JPEG,
  FORMAT_PDF,
  FORMAT_PNG,
  FORMAT_TEXT,
  canPassthroughPdf,
  planFormat,
} from './negotiate';

/** 服务端永远支持的四种格式。 */
const ALWAYS = ['image/pwg-raster', 'image/jpeg', 'image/png', 'text/plain'];

describe('格式协商', () => {
  it('图片走 JPEG', () => {
    const plan = planFormat('image', ALWAYS);
    expect(plan.ok).toBe(true);
    expect(plan.format).toBe(FORMAT_JPEG);
    expect(plan.renderOnDevice).toBe(false);
    expect(plan.pages).toBe(1);
  });

  it('文本走 text/plain', () => {
    const plan = planFormat('text', ALWAYS);
    expect(plan.format).toBe(FORMAT_TEXT);
    expect(plan.renderOnDevice).toBe(false);
  });

  it('PDF 默认端上渲染成 JPEG，逐页一个作业', () => {
    const plan = planFormat('pdf', ALWAYS, {pdfPages: 3});
    expect(plan.format).toBe(FORMAT_JPEG);
    expect(plan.renderOnDevice).toBe(true);
    expect(plan.pages).toBe(3);
    expect(plan.passthroughUnavailable).toBe(false);
  });

  it('服务端支持 PDF 且用户选了保持原样 → 直传一个作业', () => {
    const plan = planFormat('pdf', [...ALWAYS, FORMAT_PDF], {
      pdfPassthrough: true,
      pdfPages: 3,
    });
    expect(plan.format).toBe(FORMAT_PDF);
    expect(plan.renderOnDevice).toBe(false);
    expect(plan.pages).toBe(1);
  });

  it('服务端不支持 PDF 但用户勾了保持原样 → 退回渲染并标记', () => {
    const plan = planFormat('pdf', ALWAYS, {pdfPassthrough: true, pdfPages: 2});
    expect(plan.format).toBe(FORMAT_JPEG);
    expect(plan.renderOnDevice).toBe(true);
    expect(plan.pages).toBe(2);
    expect(plan.passthroughUnavailable).toBe(true);
  });

  it('没问过服务端能力时按永远支持的格式走', () => {
    const plan = planFormat('pdf', []);
    expect(plan.ok).toBe(true);
    expect(plan.format).toBe(FORMAT_JPEG);
  });

  it('服务端既不收 JPEG 也不收 PNG 时图片打不了', () => {
    const plan = planFormat('image', ['text/plain']);
    expect(plan.ok).toBe(false);
    expect(plan.format).toBe('');
  });

  it('服务端只收 PNG 时图片降级到 PNG', () => {
    const plan = planFormat('image', ['image/png', 'text/plain']);
    expect(plan.ok).toBe(true);
    expect(plan.format).toBe(FORMAT_PNG);
  });

  it('只有服务端声明支持 PDF 时才给「保持原样」这个选项', () => {
    expect(canPassthroughPdf(ALWAYS)).toBe(false);
    expect(canPassthroughPdf([...ALWAYS, FORMAT_PDF])).toBe(true);
    expect(canPassthroughPdf([...ALWAYS, 'APPLICATION/PDF'])).toBe(true);
  });
});
