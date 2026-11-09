/**
 * 应用自带的 TurboModule 包。
 *
 * 这里只登记模块，模块本体在同目录的各个 *Module.ets 里。名字必须与 JS 侧
 * 规格文件里的模块名一致——生成的 C++ 侧按名字找过来。
 */

import {
  RNOHPackage,
  UITurboModule,
  UITurboModuleContext,
} from '@rnoh/react-native-openharmony';
import { DyjFilesModule } from './DyjFilesModule';
import { DyjHttpModule } from './DyjHttpModule';
import { DyjImageModule } from './DyjImageModule';
import { DyjMdnsModule } from './DyjMdnsModule';
import { DyjPdfModule } from './DyjPdfModule';
import { DyjPrefsModule } from './DyjPrefsModule';

export class DyjPackage extends RNOHPackage {
  override getUITurboModuleFactoryByNameMap(): Map<
    string,
    (ctx: UITurboModuleContext) => UITurboModule | null
  > {
    return new Map<string, (ctx: UITurboModuleContext) => UITurboModule>()
      .set(DyjHttpModule.NAME, (ctx: UITurboModuleContext) => new DyjHttpModule(ctx))
      .set(DyjMdnsModule.NAME, (ctx: UITurboModuleContext) => new DyjMdnsModule(ctx))
      .set(DyjPrefsModule.NAME, (ctx: UITurboModuleContext) => new DyjPrefsModule(ctx))
      .set(DyjFilesModule.NAME, (ctx: UITurboModuleContext) => new DyjFilesModule(ctx))
      .set(DyjImageModule.NAME, (ctx: UITurboModuleContext) => new DyjImageModule(ctx))
      .set(DyjPdfModule.NAME, (ctx: UITurboModuleContext) => new DyjPdfModule(ctx));
  }

  override getDebugName(): string | undefined {
    return 'dyj';
  }
}
