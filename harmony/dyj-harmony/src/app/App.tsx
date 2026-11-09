/**
 * 应用根组件：发现 → 配对 → 打印 → 结果。
 *
 * 页面之间是一台状态机：每一步只关心「下一步去哪」。四个页面，不引导航库。
 */

import React from 'react';
import {StatusBar, View} from 'react-native';

import {discover, type DiscoveredHost} from '../client/discover';
import {manualAddresses} from '../client/address';
import type {PrintOutcome} from '../client/print';
import {DiscoverScreen} from '../screens/DiscoverScreen';
import {PairScreen} from '../screens/PairScreen';
import {PrintScreen} from '../screens/PrintScreen';
import {ResultScreen} from '../screens/ResultScreen';
import {pairedHosts, type PairedHost} from '../state/store';
import {colors} from '../ui/theme';

/** 当前在哪一步。 */
type Step =
  | {name: 'discover'}
  | {name: 'pair'; host: DiscoveredHost}
  | {name: 'print'; host: PairedHost}
  | {name: 'result'; outcome: PrintOutcome; host: PairedHost};

export function App(): React.JSX.Element {
  const [step, setStep] = React.useState<Step>({name: 'discover'});
  const [hosts, setHosts] = React.useState<DiscoveredHost[]>([]);
  const [paired, setPaired] = React.useState<PairedHost[]>([]);
  const [scanning, setScanning] = React.useState(false);
  const [scanFailed, setScanFailed] = React.useState(false);

  const refreshPaired = React.useCallback(async () => {
    setPaired(await pairedHosts());
  }, []);

  React.useEffect(() => {
    refreshPaired();
  }, [refreshPaired]);

  const scan = async () => {
    if (scanning) {
      return;
    }
    setScanning(true);
    setScanFailed(false);
    try {
      setHosts(await discover());
    } catch {
      // 扫不到就扫不到——用户还能手工填地址，不必弹错误；只记一笔，
      // 让界面说明白是「扫失败」而不是「没有共享端」。
      setHosts([]);
      setScanFailed(true);
    } finally {
      setScanning(false);
    }
  };

  const openManual = (address: string) => {
    // 手工填的地址可能只写了主机名——补上默认端口再走。
    const addresses = manualAddresses(address);
    if (addresses.length === 0) {
      return;
    }
    setStep({
      name: 'pair',
      host: {
        instance: addresses[0],
        name: addresses[0],
        addresses,
        isDyj: true,
      },
    });
  };

  return (
    <View style={{flex: 1, backgroundColor: colors.bg}}>
      <StatusBar barStyle="dark-content" backgroundColor={colors.bg} />
      {step.name === 'discover' && (
        <DiscoverScreen
          hosts={hosts}
          paired={paired}
          scanning={scanning}
          scanFailed={scanFailed}
          onScan={scan}
          onPick={host => setStep({name: 'pair', host})}
          onOpenPaired={host => setStep({name: 'print', host})}
          onManual={openManual}
        />
      )}
      {step.name === 'pair' && (
        <PairScreen
          name={step.host.name}
          addresses={step.host.addresses}
          onCancel={() => setStep({name: 'discover'})}
          onPaired={async host => {
            await refreshPaired();
            setStep({name: 'print', host});
          }}
        />
      )}
      {step.name === 'print' && (
        <PrintScreen
          host={step.host}
          onBack={() => setStep({name: 'discover'})}
          onResult={outcome => setStep({name: 'result', outcome, host: step.host})}
          onRepair={() => {
            setStep({
              name: 'pair',
              host: {
                instance: step.host.address,
                name: step.host.name,
                addresses: [step.host.address],
                isDyj: true,
              },
            });
          }}
        />
      )}
      {step.name === 'result' && (
        <ResultScreen
          outcome={step.outcome}
          onRetry={() => setStep({name: 'print', host: step.host})}
          onRepair={() =>
            setStep({
              name: 'pair',
              host: {
                instance: step.host.address,
                name: step.host.name,
                addresses: [step.host.address],
                isDyj: true,
              },
            })
          }
          onDone={async () => {
            await refreshPaired();
            setStep({name: 'discover'});
          }}
        />
      )}
    </View>
  );
}
