/**
 * dyj 鸿蒙端 —— 接收端（发现 → 配对 → 打印）
 *
 * 页面在 src/ 下，这里只负责挂上根组件。
 */

import React from 'react';

import {App as Root} from './src/app/App';

function App(): React.JSX.Element {
  return <Root />;
}

export default App;
