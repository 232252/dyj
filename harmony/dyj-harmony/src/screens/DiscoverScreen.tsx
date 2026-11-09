/**
 * 发现页：找出局域网里的共享端。
 *
 * 两块内容：扫到的共享端（含已经连过的），和手工填地址。手工入口是兜底——
 * 有些网络环境不让组播包过，自动发现扫不到时还能用。
 */

import React from 'react';
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  Text,
  TextInput,
  View,
} from 'react-native';

import type {DiscoveredHost} from '../client/discover';
import {t} from '../i18n';
import type {PairedHost} from '../state/store';
import {colors, common} from '../ui/theme';

interface Props {
  hosts: DiscoveredHost[];
  paired: PairedHost[];
  scanning: boolean;
  /** 上一轮扫描没能完成（组播被挡、端口被占等）。 */
  scanFailed: boolean;
  onScan: () => void;
  onPick: (host: DiscoveredHost) => void;
  onOpenPaired: (host: PairedHost) => void;
  onManual: (address: string) => void;
}

export function DiscoverScreen(props: Props): React.JSX.Element {
  const [manual, setManual] = React.useState('');

  const pairedAddresses = new Set(props.paired.map(h => h.address));

  return (
    <View style={common.screen}>
      <View style={[common.body, {paddingBottom: 8}]}>
        <Text style={common.title}>{t('discoverTitle')}</Text>
        <Text style={common.subtitle}>{t('discoverAddressHint')}</Text>
        <Pressable
          style={common.primary}
          disabled={props.scanning}
          onPress={props.onScan}>
          {props.scanning ? (
            <View style={common.row}>
              <ActivityIndicator color={colors.accentText} />
              <Text style={[common.primaryText, {marginLeft: 8}]}>
                {t('discoverScanning')}
              </Text>
            </View>
          ) : (
            <Text style={common.primaryText}>{t('discoverScan')}</Text>
          )}
        </Pressable>
      </View>

      <ScrollView style={{flex: 1}} contentContainerStyle={{padding: 16, paddingTop: 0}}>
        {props.paired.length > 0 && (
          <>
            <Text style={common.label}>{t('discoverPairedBadge')}</Text>
            {props.paired.map(host => (
              <Pressable
                key={host.address}
                style={common.card}
                onPress={() => props.onOpenPaired(host)}>
                <View style={common.between}>
                  <Text style={common.label}>{host.name || host.address}</Text>
                  <Text style={common.link}>{t('discoverPairedBadge')}</Text>
                </View>
                <Text style={[common.dim, {marginTop: 4}]}>{host.address}</Text>
              </Pressable>
            ))}
          </>
        )}

        <Text style={[common.label, {marginTop: 16}]}>
          {props.hosts.length > 0
            ? t('discoverFound', {count: props.hosts.length})
            : props.scanFailed
              ? t('discoverScanFailed')
              : t('discoverNone')}
        </Text>
        {props.hosts.map(host => (
          <Pressable
            key={host.instance}
            style={common.card}
            onPress={() => props.onPick(host)}>
            <View style={common.between}>
              <Text style={common.label}>{host.name}</Text>
              {pairedAddresses.has(host.addresses[0]) && (
                <Text style={common.link}>{t('discoverPairedBadge')}</Text>
              )}
            </View>
            <Text style={[common.dim, {marginTop: 4}]}>
              {host.addresses[0]}
            </Text>
            {!host.isDyj && (
              <Text style={[common.dim, {marginTop: 4}]}>
                {t('discoverNotShared')}
              </Text>
            )}
          </Pressable>
        ))}

        <Text style={[common.label, {marginTop: 16}]}>
          {t('discoverAddByAddress')}
        </Text>
        <TextInput
          style={common.input}
          value={manual}
          onChangeText={setManual}
          placeholder={t('discoverAddressPlaceholder')}
          placeholderTextColor={colors.dim}
          autoCapitalize="none"
          keyboardType="numbers-and-punctuation"
        />
        <Pressable
          style={common.secondary}
          disabled={manual.trim().length === 0}
          onPress={() => {
            props.onManual(manual.trim());
            setManual('');
          }}>
          <Text style={common.secondaryText}>{t('discoverAddByAddress')}</Text>
        </Pressable>
      </ScrollView>
    </View>
  );
}
