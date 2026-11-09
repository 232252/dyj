import { hapTasks } from '@ohos/hvigor-ohos-plugin';
import { createRNOHModulePlugin } from '@rnoh/hvigor-plugin';

export default {
  system: hapTasks,
  plugins: [
    createRNOHModulePlugin({
      codegen: {
        // 生成的 ArkTS 规格放在应用自己的 ets/generated 下。默认会写进
        // oh_modules/@rnoh/react-native-openharmony/generated ——那是依赖包的
        // 目录（ohpm 还会用软链指到共享 store），写进去有风险。
        //
        // 这里刻意不写结尾的 generated：codegen 是在路径后追加 'generated'，
        // 而它判断「是否已以 generated 结尾」用的正斜杠正则，在 Windows 上
        // 匹配不到反斜杠路径，会再追加一次变成 generated/generated。
        etsOutputPath: './entry/src/main/ets',
      },
    }),
  ],
};
