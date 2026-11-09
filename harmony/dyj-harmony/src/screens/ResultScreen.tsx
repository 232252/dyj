/**
 * 结果页：打出来没有？下一步做什么？
 *
 * 失败原因分三类：连接失效（要重新配对）、共享端暂时不收（去那边恢复）、其它
 * （重试）。分清楚了用户才知道该点什么。
 */

import React from 'react';
import {Pressable, Text, View} from 'react-native';

import type {PrintErrorCode, PrintOutcome} from '../client/print';
import {t} from '../i18n';
import {colors, common} from '../ui/theme';

interface Props {
  outcome: PrintOutcome;
  onRetry: () => void;
  onRepair: () => void;
  onDone: () => void;
}

export function ResultScreen(props: Props): React.JSX.Element {
  const {outcome} = props;
  const repair = outcome.ok === false && outcome.code === 'token_invalid';

  return (
    <View style={[common.screen, common.body]}>
      <Text style={common.title}>
        {outcome.ok ? t('resultSent', {id: outcome.jobId ?? 0}) : t('resultFailed')}
      </Text>
      {outcome.ok && outcome.pages !== undefined && outcome.pages > 1 && (
        <Text style={common.subtitle}>
          {t('resultPages', {count: outcome.pages})}
        </Text>
      )}
      {!outcome.ok && (
        <Text style={[common.subtitle, {color: colors.danger}]}>
          {describe(outcome.code)}
        </Text>
      )}

      <View style={common.card}>
        {repair ? (
          <Pressable style={common.primary} onPress={props.onRepair}>
            <Text style={common.primaryText}>{t('resultRepair')}</Text>
          </Pressable>
        ) : (
          !outcome.ok && (
            <Pressable style={common.primary} onPress={props.onRetry}>
              <Text style={common.primaryText}>{t('resultRetry')}</Text>
            </Pressable>
          )
        )}
        <Pressable style={common.secondary} onPress={props.onDone}>
          <Text style={common.secondaryText}>{t('commonBack')}</Text>
        </Pressable>
      </View>
    </View>
  );
}

/** 失败原因 → 一句话。 */
function describe(code: PrintErrorCode | undefined): string {
  switch (code) {
    case 'token_invalid':
      return t('resultTokenInvalid');
    case 'paused':
      return t('resultPaused');
    case 'format_unsupported':
      return t('resultFormatUnsupported');
    case 'too_large':
      return t('resultTooLarge');
    case 'unreachable':
      return t('pairErrorUnreachable');
    default:
      return t('resultUnknown');
  }
}
