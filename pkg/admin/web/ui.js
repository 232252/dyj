// 共享 DOM 助手：两个页面反复出现的行构建与「确认弹窗 → 动作 → toast」
// 模式收敛到这里，保持单一来源，视觉语言不再靠复制粘贴对齐。
import { showToast } from './components/dyj-toast.js';
import { confirmDialog } from './components/dyj-modal.js';
import { errMessage } from './api.js';

// rowMeta：列表行的「名称 + 副标」元信息块（.meta > .name + .sub2）。
// 文本一律走 textContent，调用方不需要（也不应该）再转义。
export function rowMeta(name, sub) {
  const meta = document.createElement('div');
  meta.className = 'meta';
  const title = document.createElement('div');
  title.className = 'name';
  title.textContent = name;
  const sub2 = document.createElement('div');
  sub2.className = 'sub2';
  sub2.textContent = sub;
  meta.append(title, sub2);
  return meta;
}

// confirmAction：确认弹窗（danger）→ 动作 → 成功/失败 toast → 无论成败
// 都回调 after()（刷新列表；失败时刷新只是还原视图，无副作用）。
// fail 为错误前缀（如「取消失败：」），空前缀直接显示 errMessage。
export async function confirmAction({ title, body, confirmText, done, fail = '' }, action, after) {
  if (!(await confirmDialog({ title, body, confirmText, danger: true }))) return;
  try {
    await action();
    showToast(done);
  } catch (e) {
    showToast(fail + errMessage(e), 'error');
  }
  after?.();
}
