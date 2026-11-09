/**
 * 配对页：输入共享端上的 4 位配对码。
 *
 * 配对码错三次以上服务端会锁 10 分钟，所以失败提示要说人话：码错了就提示核对，
 * 锁了就显示还要等多久。
 */

import React from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  Text,
  TextInput,
  View,
} from 'react-native';

import {PairError, pairAny} from '../client/pair';
import {t} from '../i18n';
import {deviceId, deviceName, saveHost, type PairedHost} from '../state/store';
import {colors, common} from '../ui/theme';

interface Props {
  /** 共享端名字（给用户看）。 */
  name: string;
  /** 可试的地址，按顺序挨个试。 */
  addresses: string[];
  onCancel: () => void;
  onPaired: (host: PairedHost) => void;
}

export function PairScreen(props: Props): React.JSX.Element {
  const [pin, setPin] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState('');
  const [waitSeconds, setWaitSeconds] = React.useState(0);

  // 锁定倒计时：每秒减一，减到 0 才让再试。
  React.useEffect(() => {
    if (waitSeconds <= 0) {
      return;
    }
    const timer = setTimeout(() => setWaitSeconds(s => s - 1), 1000);
    return () => clearTimeout(timer);
  }, [waitSeconds]);

  const submit = async () => {
    if (busy || pin.length !== 4 || waitSeconds > 0) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      const id = await deviceId();
      const name = await deviceName();
      const {result, address} = await pairAny(props.addresses, pin, id, name);
      const host: PairedHost = {
        address,
        name: result.serverName || props.name || address,
        token: result.token,
        printers: result.printers,
      };
      await saveHost(host);
      props.onPaired(host);
    } catch (e) {
      setError(describe(e));
      if (e instanceof PairError && e.code === 'locked') {
        setWaitSeconds(Math.max(1, e.retryAfterSec));
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <KeyboardAvoidingView
      style={common.screen}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <View style={common.body}>
        <Text style={common.title}>{t('pairTitle')}</Text>
        <Text style={common.subtitle}>{props.name}</Text>

        <View style={common.card}>
          <Text style={common.label}>{t('pairHint')}</Text>
          <TextInput
            style={[common.input, {fontSize: 24, letterSpacing: 8, textAlign: 'center'}]}
            value={pin}
            onChangeText={text => setPin(text.replace(/\D/g, '').slice(0, 4))}
            keyboardType="number-pad"
            maxLength={4}
            editable={!busy}
            placeholder="0000"
            placeholderTextColor={colors.dim}
          />
          {error.length > 0 && <Text style={common.error}>{error}</Text>}
          {waitSeconds > 0 && (
            <Text style={[common.dim, {marginTop: 6}]}>
              {t('pairErrorLocked', {minutes: Math.ceil(waitSeconds / 60)})}
            </Text>
          )}
          <Pressable
            style={[
              common.primary,
              (busy || pin.length !== 4 || waitSeconds > 0) &&
                common.primaryDisabled,
            ]}
            disabled={busy || pin.length !== 4 || waitSeconds > 0}
            onPress={submit}>
            {busy ? (
              <View style={common.row}>
                <ActivityIndicator color={colors.accentText} />
                <Text style={[common.primaryText, {marginLeft: 8}]}>
                  {t('pairWorking')}
                </Text>
              </View>
            ) : (
              <Text style={common.primaryText}>{t('pairSubmit')}</Text>
            )}
          </Pressable>
          <Pressable style={common.secondary} onPress={props.onCancel}>
            <Text style={common.secondaryText}>{t('commonBack')}</Text>
          </Pressable>
        </View>
      </View>
    </KeyboardAvoidingView>
  );
}

/** 把配对失败翻译成给用户看的一句话。 */
function describe(e: unknown): string {
  if (!(e instanceof PairError)) {
    return t('pairErrorInternal');
  }
  switch (e.code) {
    case 'bad_pin':
      return t('pairErrorBadPin');
    case 'locked':
      return t('pairErrorLocked', {
        minutes: Math.max(1, Math.ceil(e.retryAfterSec / 60)),
      });
    case 'pin_required':
      return t('pairErrorPinRequired');
    case 'unreachable':
      return t('pairErrorUnreachable');
    default:
      return t('pairErrorInternal');
  }
}
