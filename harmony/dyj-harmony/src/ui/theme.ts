/**
 * 共用配色与样式。
 *
 * 界面只有三个页面，用不着设计系统——一份共用的 StyleSheet 就够，改主题也只
 * 改这一处。
 */

import {StyleSheet} from 'react-native';

export const colors = {
  bg: '#f6f7f9',
  card: '#ffffff',
  line: '#e3e5e8',
  text: '#1b1c1e',
  dim: '#6b6c70',
  accent: '#2f6fed',
  accentText: '#ffffff',
  danger: '#c2410c',
  ok: '#1a7f37',
};

export const common = StyleSheet.create({
  screen: {
    flex: 1,
    backgroundColor: colors.bg,
  },
  body: {
    flex: 1,
    padding: 16,
  },
  title: {
    fontSize: 20,
    fontWeight: '600',
    color: colors.text,
  },
  subtitle: {
    marginTop: 4,
    fontSize: 14,
    color: colors.dim,
  },
  card: {
    marginTop: 12,
    padding: 14,
    borderRadius: 12,
    backgroundColor: colors.card,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.line,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  between: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  label: {
    fontSize: 15,
    color: colors.text,
  },
  dim: {
    fontSize: 13,
    color: colors.dim,
  },
  error: {
    marginTop: 10,
    fontSize: 14,
    color: colors.danger,
  },
  input: {
    marginTop: 8,
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.line,
    backgroundColor: colors.card,
    fontSize: 16,
    color: colors.text,
  },
  primary: {
    marginTop: 14,
    paddingVertical: 12,
    borderRadius: 10,
    backgroundColor: colors.accent,
    alignItems: 'center',
  },
  primaryText: {
    fontSize: 16,
    fontWeight: '600',
    color: colors.accentText,
  },
  primaryDisabled: {
    backgroundColor: '#b9c8e6',
  },
  secondary: {
    marginTop: 10,
    paddingVertical: 11,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.line,
    backgroundColor: colors.card,
    alignItems: 'center',
  },
  secondaryText: {
    fontSize: 15,
    color: colors.text,
  },
  link: {
    fontSize: 14,
    color: colors.accent,
  },
});
