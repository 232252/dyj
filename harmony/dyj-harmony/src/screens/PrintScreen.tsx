/**
 * 打印页：选内容、选份数、送到共享端。
 *
 * 三种内容：
 *   图片 —— 端上转成 JPEG 再发（打印机只认这个，体积也小）。
 *   文字 —— 直接发，共享端用自己的排版引擎渲染。
 *   PDF  —— 默认在端上渲染成逐页图片再发；共享端明确支持时才给「保持原样」
 *           这个选项。
 */

import React from 'react';
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';

import {baseUrl} from '../client/address';
import {
  PrintError,
  printDocument,
  printUrl,
  printerUri,
  supportedFormats,
  type PrintErrorCode,
  type PrintOutcome,
} from '../client/print';
import {
  FORMAT_JPEG,
  FORMAT_PDF,
  FORMAT_TEXT,
  canPassthroughPdf,
  planFormat,
  type DocumentKind,
} from '../format/negotiate';
import {t} from '../i18n';
import {
  imageToJpeg,
  pdfPageCount,
  pickFile,
  readFileBytes,
  renderPdfPages,
  type PickedFile,
} from '../native';
import {deviceName, type PairedHost} from '../state/store';
import {utf8Encode} from '../util/utf8';
import {colors, common} from '../ui/theme';

/** 图片长边上限：再大就没必要了，打印机也吃不下。 */
const MAX_LONG_EDGE = 2000;
/** JPEG 质量。 */
const JPEG_QUALITY = 85;
/** 文字上限：再长共享端排版会卡住。 */
const MAX_TEXT_BYTES = 32 * 1024;

interface Props {
  host: PairedHost;
  onBack: () => void;
  onResult: (outcome: PrintOutcome) => void;
  /** 令牌失效——回到配对页重新配对。 */
  onRepair: () => void;
}

export function PrintScreen(props: Props): React.JSX.Element {
  const {host} = props;
  const [printer, setPrinter] = React.useState(host.printers[0]);
  const [kind, setKind] = React.useState<DocumentKind | null>(null);
  const [file, setFile] = React.useState<PickedFile | null>(null);
  const [text, setText] = React.useState('');
  const [copies, setCopies] = React.useState(1);
  const [passthrough, setPassthrough] = React.useState(false);
  const [supported, setSupported] = React.useState<string[]>([]);
  const [busy, setBusy] = React.useState(false);
  const [note, setNote] = React.useState('');

  const url = printer
    ? printUrl(baseUrl(host.address), printer.uriPath)
    : baseUrl(host.address);
  const puri = printer
    ? printerUri(baseUrl(host.address), printer.uriPath)
    : '';

  // 换打印机就重问一次它支持哪些格式。
  React.useEffect(() => {
    let cancelled = false;
    if (!printer) {
      return;
    }
    supportedFormats(url, puri).then(formats => {
      if (!cancelled) {
        setSupported(formats);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [url, puri, printer]);

  const choose = async (next: DocumentKind) => {
    setNote('');
    setKind(next);
    if (next === 'image') {
      const picked = await pickFile('image');
      setFile(picked.cancelled ? null : picked);
    } else if (next === 'pdf') {
      const picked = await pickFile('document');
      setFile(picked.cancelled ? null : picked);
    }
  };

  const run = async () => {
    if (busy || !printer) {
      return;
    }
    setBusy(true);
    setNote('');
    try {
      const options = {
        jobName: jobNameFor(kind, file),
        copies,
        userName: await deviceName(),
      };
      if (kind === 'text') {
        const bytes = utf8Encode(text);
        if (bytes.length > MAX_TEXT_BYTES) {
          setNote(t('printTextTooLong'));
          return;
        }
        const jobId = await printDocument(url, puri, bytes, FORMAT_TEXT, options);
        props.onResult({ok: true, jobId});
        return;
      }
      if (!file) {
        setNote(t('printPickFirst'));
        return;
      }
      if (kind === 'image') {
        const jpeg = await imageToJpeg(file.path, MAX_LONG_EDGE, JPEG_QUALITY);
        const jobId = await printDocument(url, puri, jpeg, FORMAT_JPEG, options);
        props.onResult({ok: true, jobId});
        return;
      }
      // PDF
      const pages = await pdfPageCount(file.path);
      const plan = planFormat('pdf', supported, {
        pdfPassthrough: passthrough,
        pdfPages: pages,
      });
      if (!plan.ok) {
        props.onResult({ok: false, code: 'format_unsupported'});
        return;
      }
      if (plan.passthroughUnavailable) {
        setNote(t('printPassthroughUnavailable'));
      }
      if (!plan.renderOnDevice) {
        const bytes = await readFileBytes(file.path);
        const jobId = await printDocument(url, puri, bytes, FORMAT_PDF, options);
        props.onResult({ok: true, jobId, pages: 1});
        return;
      }
      const rendered = await renderPdfPages(file.path, MAX_LONG_EDGE, JPEG_QUALITY);
      let lastJob = 0;
      for (const page of rendered) {
        lastJob = await printDocument(url, puri, page, FORMAT_JPEG, options);
      }
      props.onResult({ok: true, jobId: lastJob, pages: rendered.length});
    } catch (e) {
      if (e instanceof PrintError && e.code === 'token_invalid') {
        props.onRepair();
        return;
      }
      props.onResult({ok: false, code: codeOf(e)});
    } finally {
      setBusy(false);
    }
  };

  const ready =
    printer !== undefined &&
    (kind === 'text' ? text.trim().length > 0 : kind !== null && file !== null);

  return (
    <View style={common.screen}>
      <View style={[common.body, {paddingBottom: 8}]}>
        <Text style={common.title}>{t('printTitle')}</Text>
        <Text style={common.subtitle}>{host.name || host.address}</Text>
      </View>

      <ScrollView style={{flex: 1}} contentContainerStyle={{padding: 16, paddingTop: 0}}>
        {host.printers.length === 0 && (
          <View style={common.card}>
            <Text style={common.label}>{t('discoverNone')}</Text>
          </View>
        )}

        {host.printers.length > 1 && (
          <View style={common.card}>
            <Text style={common.label}>{t('printPrinter')}</Text>
            {host.printers.map(p => (
              <Pressable
                key={p.uriPath}
                style={{marginTop: 8}}
                onPress={() => setPrinter(p)}>
                <View style={common.between}>
                  <Text
                    style={[
                      common.label,
                      p.uriPath === printer?.uriPath && {fontWeight: '600'},
                    ]}>
                    {p.name || p.key}
                  </Text>
                  {p.uriPath === printer?.uriPath && (
                    <Text style={common.link}>✓</Text>
                  )}
                </View>
              </Pressable>
            ))}
          </View>
        )}

        <View style={common.card}>
          <PickButton
            label={t('printPickImage')}
            active={kind === 'image'}
            onPress={() => choose('image')}
          />
          {kind === 'image' && file && (
            <Text style={[common.dim, {marginTop: 6}]}>{file.name}</Text>
          )}
          <PickButton
            label={t('printPickText')}
            active={kind === 'text'}
            onPress={() => choose('text')}
          />
          {kind === 'text' && (
            <TextInput
              style={[common.input, {minHeight: 96, textAlignVertical: 'top'}]}
              value={text}
              onChangeText={setText}
              multiline
              placeholder={t('printTextPlaceholder')}
              placeholderTextColor={colors.dim}
            />
          )}
          <PickButton
            label={t('printPickPdf')}
            active={kind === 'pdf'}
            onPress={() => choose('pdf')}
          />
          {kind === 'pdf' && file && (
            <Text style={[common.dim, {marginTop: 6}]}>{file.name}</Text>
          )}
          {kind === 'pdf' && canPassthroughPdf(supported) && (
            <View style={[common.between, {marginTop: 12}]}>
              <Text style={common.label}>{t('printPassthrough')}</Text>
              <Switch value={passthrough} onValueChange={setPassthrough} />
            </View>
          )}
        </View>

        <View style={common.card}>
          <View style={common.between}>
            <Text style={common.label}>{t('printCopies')}</Text>
            <View style={common.row}>
              <Pressable
                style={common.secondary}
                onPress={() => setCopies(c => Math.max(1, c - 1))}>
                <Text style={common.secondaryText}>−</Text>
              </Pressable>
              <Text style={[common.label, {marginHorizontal: 16}]}>{copies}</Text>
              <Pressable
                style={common.secondary}
                onPress={() => setCopies(c => Math.min(99, c + 1))}>
                <Text style={common.secondaryText}>+</Text>
              </Pressable>
            </View>
          </View>
        </View>

        {note.length > 0 && <Text style={common.error}>{note}</Text>}

        <Pressable
          style={[common.primary, (!ready || busy) && common.primaryDisabled]}
          disabled={!ready || busy}
          onPress={run}>
          {busy ? (
            <View style={common.row}>
              <ActivityIndicator color={colors.accentText} />
              <Text style={[common.primaryText, {marginLeft: 8}]}>
                {t('printSending')}
              </Text>
            </View>
          ) : (
            <Text style={common.primaryText}>{t('printSend')}</Text>
          )}
        </Pressable>
        <Pressable style={common.secondary} onPress={props.onBack}>
          <Text style={common.secondaryText}>{t('commonBack')}</Text>
        </Pressable>
      </ScrollView>
    </View>
  );
}

/** 一个内容选择按钮。 */
function PickButton(props: {
  label: string;
  active: boolean;
  onPress: () => void;
}): React.JSX.Element {
  return (
    <Pressable style={{marginTop: 10}} onPress={props.onPress}>
      <View style={common.between}>
        <Text style={[common.label, props.active && {fontWeight: '600'}]}>
          {props.label}
        </Text>
        {props.active && <Text style={common.link}>✓</Text>}
      </View>
    </Pressable>
  );
}

/** 作业名：有文件名就用文件名。 */
function jobNameFor(kind: DocumentKind | null, file: PickedFile | null): string {
  if (file && file.name) {
    return file.name;
  }
  return kind === 'text' ? 'dyj text' : 'dyj';
}

function codeOf(e: unknown): PrintErrorCode {
  return e instanceof PrintError ? e.code : 'unknown';
}
