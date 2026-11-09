/**
 * Jest 配置。
 *
 * 只测本工程的 TypeScript 代码：harmony/ 下是 ArkTS 工程（自带测试）与构建
 * 产物，不归 jest 管。
 */
module.exports = {
  preset: 'react-native',
  roots: ['<rootDir>/src', '<rootDir>/__tests__'],
};
