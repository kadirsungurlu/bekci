<script lang="ts">
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    MASK,
    type DownWhen,
    type LocationSetup,
    type MonitorInput,
    type MonitorType,
    type MonitorView,
    type NotificationChannel,
    type Probe,
    type Tag,
  } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { navigate, router } from '../lib/router.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import CopyButton from '../components/CopyButton.svelte';
  import { collator, fmtInterval, lower } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import { NOTIFY_LABELS } from '../lib/notifyTypes';
  import { changedDestinations, destinationPhrase, guardUnsaved, markInvalid, snapshot } from '../lib/forms';
  import {
    CATEGORY_LABELS,
    CATEGORY_ORDER,
    GROUP_MODES,
    type GroupMode,
    HTTP_EXTRA_FIELDS,
    JSON_OPS,
    MONITOR_TYPES,
    hasTimeout,
    hasUpsideDown,
    isRemoteCapable,
    typeDef,
  } from '../lib/monitorTypes';
  import Icon from '../components/Icon.svelte';
  import MonitorPicker from '../components/MonitorPicker.svelte';
  import TagChip from '../components/TagChip.svelte';
  import TagDialog from '../components/TagDialog.svelte';
  import ConfigFields, { fieldConfig, fieldDefaults, fieldError, fieldValues } from '../components/ConfigFields.svelte';
  import { t, tParts, type TKey } from '../lib/i18n';

  let { id }: { id?: number } = $props();
  // svelte-ignore state_referenced_locally
  const isEdit = id !== undefined;
  // Listedeki "Kopyala": kopya durdurulmuş oluşturulur, bu formda kaydedilince başlar.
  const fromClone = isEdit && /[?&]kopya=1\b/.test(router.path);

  // Tip seçici: kategorilere ayrılmış, aranabilir kartlar. Düzenlemede (ve dar
  // ekranda seçim yapıldıktan sonra) yalnızca seçili tip gösterilir.
  let typeQuery = $state('');
  // svelte-ignore state_referenced_locally
  let pickerOpen = $state(!isEdit);
  const typeGroups = $derived.by(() => {
    const q = lower(typeQuery.trim());
    const match = (d: (typeof MONITOR_TYPES)[number]) =>
      !q || lower(`${d.label} ${d.badge} ${d.key} ${d.desc} ${d.keywords ?? ''} ${CATEGORY_LABELS[d.category]}`).includes(q);
    return CATEGORY_ORDER.map((c) => ({ key: c, label: CATEGORY_LABELS[c], types: MONITOR_TYPES.filter((d) => d.category === c && match(d)) })).filter(
      (g) => g.types.length,
    );
  });

  const PRESETS = [30, 60, 120, 300, 600, 900, 1800, 3600, 86400];
  const METHODS = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'];
  const RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'SOA', 'SRV', 'CAA', 'PTR'];

  let loading = $state(isEdit);
  let loadError = $state('');
  let saving = $state(false);
  let error = $state('');
  let errorEl: HTMLDivElement | undefined = $state();
  /** İlk hatadan sonraki diğer hatalar (hepsi birlikte listelenir). */
  let moreErrors = $state<string[]>([]);
  let showAdvanced = $state(false);

  // Genel
  let type = $state<MonitorType>('http');
  let name = $state('');
  let description = $state('');
  let intervalPreset = $state('60');
  let customInterval = $state<number | null>(60);
  // Push: beklenen aralığın üstüne eklenen tolerans (sn); 0 = yok.
  let pushGrace = $state<number | null>(0);
  let retryInterval = $state<number | null>(null);
  // Yeni monitör varsayılanları: 1 tekrar deneme (tek anlık hata alarm
  // üretmesin) ve aralığın yarısını geçmeyen zaman aşımı (kullanıcı elle
  // değiştirmediği sürece aralıkla birlikte güncellenir).
  let maxRetries = $state<number | null>(1);
  let timeout = $state<number | null>(30);
  let timeoutAuto = $state(true);
  let resendEvery = $state<number | null>(0);
  // Yavaş yanıt eşiği (ms; 0 = kapalı) ve ortalama penceresi (kontrol sayısı).
  let slowMs = $state<number | null>(0);
  let slowChecks = $state<number | null>(3);
  let upsideDown = $state(false);

  // Bildirimler
  let channels = $state.raw<NotificationChannel[]>([]);
  let notifIds = $state<number[]>([]);
  let channelsLoaded = $state(false);
  let channelsOk = false;

  // HTTP
  let url = $state('');
  let method = $state('GET');
  let headers = $state('');
  let body = $state('');
  let basicUser = $state('');
  let basicPass = $state('');
  let acceptedCodes = $state('200-399');
  let maxRedirects = $state<number | null>(10);
  let ignoreTls = $state(false);
  let certExpiry = $state(true);
  let contentMode = $state<'none' | 'keyword' | 'json'>('none');
  let keyword = $state('');
  let keywordInvert = $state(false);
  let keywordCase = $state(false);
  let jsonPath = $state('');
  let jsonOp = $state('==');
  let jsonExpected = $state('');

  // TCP / Ping / DNS
  let host = $state('');
  let port = $state<number | null>(null);
  let pingCount = $state<number | null>(3);
  let dnsServer = $state('1.1.1.1');
  let dnsPort = $state<number | null>(53);
  let recordType = $state('A');
  let dnsExpected = $state('');

  // Grup
  let groupIds = $state<number[]>([]);
  let groupMode = $state<GroupMode>('any_down');
  let groupPercent = $state<number | null>(50);

  // Kayıt defterinde alanlarıyla tanımlı (özel bölümü olmayan) tipler
  let extra = $state<Record<string, string>>({});
  const def = $derived(typeDef(type));
  const genericFields = $derived(def?.fields ?? []);
  const hasAdvancedFields = $derived(genericFields.some((f) => f.advanced));

  // HTTP ekleri: proxy, mTLS, OAuth2
  let httpExtra = $state<Record<string, string>>(fieldDefaults(HTTP_EXTRA_FIELDS));

  // Düzenlenen monitörün kayıtlı tipi ve ayarı: tip değiştirilip geri dönülürse değerler (maskeli şifreler dahil) geri gelir.
  let origType: MonitorType | null = null;
  let origConfig: Record<string, unknown> = {};

  // Etiketler
  let allTags = $state.raw<Tag[]>([]);
  let tagsLoaded = $state(false);
  let mtags = $state<{ id: number; value: string }[]>([]);
  let origTags: { id: number; value: string }[] = [];
  let tagPick = $state('');
  let tagDialogOpen = $state(false);
  let tagDialogKey = $state(0);
  const tagById = $derived(new Map(allTags.map((t) => [t.id, t])));
  const addableTags = $derived(allTags.filter((t) => !mtags.some((m) => m.id === t.id)).sort((a, b) => collator.compare(a.name, b.name)));

  // Konumlar (kontrol noktaları)
  let probes = $state.raw<Probe[]>([]);
  let probesLoaded = $state(false);
  let locLocal = $state(true);
  let locProbeIds = $state<number[]>([]);
  let locDownWhen = $state<DownWhen>('any');
  let locNotify = $state(false);
  let origLoc: LocationSetup = { include_local: true, probe_ids: [], down_when: 'any', notify_partial: false };
  const remoteOk = $derived(isRemoteCapable(type));
  const DOWN_WHEN: { v: DownWhen; l: TKey }[] = [
    { v: 'any', l: 'monitors.form.dwAny' },
    { v: 'majority', l: 'monitors.form.dwMajority' },
    { v: 'all', l: 'monitors.form.dwAll' },
  ];

  const interval = $derived(intervalPreset === 'custom' ? (customInterval ?? 0) : Number(intervalPreset));
  /** Zaman aşımı kontrol aralığını aşamaz (tekrar deneme aralığından uzun olabilir: kontroller sırayla). */
  const timeoutLimit = $derived(interval || 0);
  const timeoutTooLong = $derived(hasTimeout(type) && timeout !== null && timeoutLimit >= 20 && timeout > timeoutLimit);
  $effect(() => {
    if (timeoutAuto && hasTimeout(type) && interval >= 20) timeout = Math.min(30, Math.max(1, Math.floor(interval / 2)));
  });

  const str = (c: Record<string, unknown>, k: string, d = '') => (typeof c[k] === 'string' ? (c[k] as string) : d);
  const num = (c: Record<string, unknown>, k: string, d: number) => (typeof c[k] === 'number' ? (c[k] as number) : d);
  const bool = (c: Record<string, unknown>, k: string, d: boolean) => (typeof c[k] === 'boolean' ? (c[k] as boolean) : d);

  // Push adresi (yalnızca düzenlemede; kaydedildikten sonra görünür).
  let pushToken = $state('');
  const pushUrl = $derived(pushToken ? `${location.origin}/api/push/${pushToken}` : '');
  let regenerating = $state(false);
  async function regeneratePush() {
    if (!isEdit) return;
    const ok = await confirmDialog({
      title: t('monitors.form.pushRegenerate'),
      message: t('monitors.form.pushRegenerateMsg'),
      confirmText: t('monitors.form.pushRegenerate'),
      danger: true,
    });
    if (!ok) return;
    regenerating = true;
    try {
      const res = await api.regeneratePushToken(id!);
      pushToken = res.push_token ?? '';
      live.upsert(res);
      toast.success(t('monitors.form.pushRegenerated'));
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      regenerating = false;
    }
  }

  function fill(m: MonitorView) {
    pushToken = m.push_token ?? '';
    type = m.type;
    name = m.name;
    description = m.description;
    if (PRESETS.includes(m.interval)) intervalPreset = String(m.interval);
    else {
      intervalPreset = 'custom';
      customInterval = m.interval;
    }
    retryInterval = m.retry_interval === m.interval ? null : m.retry_interval;
    maxRetries = m.max_retries;
    timeout = m.timeout;
    timeoutAuto = false;
    resendEvery = m.resend_every;
    slowMs = m.slow_ms ?? 0;
    slowChecks = m.slow_checks || 3;
    upsideDown = m.upside_down;
    notifIds = [...m.notification_ids];
    mtags = (m.tags ?? []).map((t) => ({ id: t.id, value: t.value }));
    origTags = mtags.map((t) => ({ ...t }));
    if (m.locations) {
      origLoc = {
        include_local: m.locations.include_local,
        probe_ids: [...(m.locations.probe_ids ?? [])],
        down_when: m.locations.down_when || 'any',
        notify_partial: !!m.locations.notify_partial,
      };
      locLocal = origLoc.include_local;
      locProbeIds = [...origLoc.probe_ids];
      locDownWhen = origLoc.down_when;
      locNotify = !!origLoc.notify_partial;
    }

    const c = m.config ?? {};
    origType = m.type;
    origConfig = c;
    pushGrace = num(c, 'grace_sec', 0);
    switch (m.type) {
      case 'http': {
        url = str(c, 'url');
        method = str(c, 'method', 'GET') || 'GET';
        headers = str(c, 'headers');
        body = str(c, 'body');
        basicUser = str(c, 'basic_user');
        basicPass = str(c, 'basic_pass');
        const codes = Array.isArray(c.accepted_codes) ? (c.accepted_codes as string[]) : ['200-399'];
        acceptedCodes = codes.join(', ');
        maxRedirects = num(c, 'max_redirects', 10);
        ignoreTls = bool(c, 'ignore_tls', false);
        certExpiry = bool(c, 'cert_expiry', true);
        keyword = str(c, 'keyword');
        keywordInvert = bool(c, 'keyword_invert', false);
        keywordCase = bool(c, 'keyword_case', false);
        jsonPath = str(c, 'json_path');
        jsonOp = str(c, 'json_op', '==') || '==';
        jsonExpected = str(c, 'json_expected');
        contentMode = jsonPath ? 'json' : keyword ? 'keyword' : 'none';
        httpExtra = fieldValues(HTTP_EXTRA_FIELDS, c);
        break;
      }
      case 'tcp':
        host = str(c, 'host');
        port = num(c, 'port', 0) || null;
        break;
      case 'ping':
        host = str(c, 'host');
        pingCount = num(c, 'count', 3);
        break;
      case 'dns':
        host = str(c, 'host');
        dnsServer = str(c, 'server', '1.1.1.1');
        dnsPort = num(c, 'port', 53);
        recordType = str(c, 'record_type', 'A') || 'A';
        dnsExpected = str(c, 'expected');
        break;
      case 'group':
        groupIds = Array.isArray(c.monitor_ids) ? (c.monitor_ids as number[]).slice() : [];
        groupMode = c.mode === 'all_down' || c.mode === 'percent_down' ? c.mode : 'any_down';
        groupPercent = typeof c.percent === 'number' ? c.percent : 50;
        break;
      default: {
        const f = typeDef(m.type)?.fields;
        if (f) extra = fieldValues(f, c);
      }
    }
  }

  function pickType(nt: MonitorType) {
    const pristine = !isEdit && !isDirty();
    if (nt !== type) {
      clearedSecrets = [];
      const prevHost = ['tcp', 'ping', 'dns'].includes(type) ? host.trim() : '';
      type = nt;
      const f = typeDef(nt)?.fields;
      if (f) {
        if (nt === origType) extra = fieldValues(f, origConfig);
        else {
          extra = fieldDefaults(f);
          // Önceki tipte girilen adres/sunucu yeni tipe taşınır.
          if (prevHost && f.some((x) => x.key === 'host')) extra.host = prevHost;
        }
      }
    }
    // Yeni monitörde yalnızca tip seçmek "kaydedilmemiş değişiklik" sayılmasın.
    if (pristine) tick().then(resetBaseline);
    // Dar ekranda uzun listeyi kapatıp forma geç.
    if (!isEdit && matchMedia('(max-width: 640px)').matches) {
      pickerOpen = false;
      tick().then(() => document.getElementById('name')?.focus());
    }
  }

  /** Ad boşsa girilen adres/sunucudan bir ad önerir. */
  function suggestName(v: string) {
    if (name.trim()) return;
    try {
      const u = new URL(v);
      if (u.hostname) {
        name = u.hostname;
        return;
      }
    } catch {
      /* adres değil */
    }
    name = v.length > 60 ? v.slice(0, 60) : v;
  }

  let unsubProbe: (() => void) | undefined;
  onDestroy(() => unsubProbe?.());

  onMount(async () => {
    api
      .tags()
      .then((list) => (allTags = list))
      .catch(() => (allTags = []))
      .finally(() => (tagsLoaded = true));
    api
      .probes()
      .then((list) => (probes = list))
      .catch(() => (probes = []))
      .finally(() => (probesLoaded = true));
    unsubProbe = live.onProbe((ev) => {
      probes = probes.map((p) => (p.id === ev.probe_id ? { ...p, online: ev.online, last_seen_at: ev.last_seen_at } : p));
    });
    const chP = api
      .notifications()
      .then((list) => {
        channels = list;
        channelsOk = true;
        if (!isEdit) notifIds = list.filter((n) => n.is_default).map((n) => n.id);
      })
      .catch(() => {
        channels = [];
      })
      .finally(() => (channelsLoaded = true));
    if (isEdit) {
      try {
        const d = await api.monitor(id!);
        fill(d.monitor);
      } catch (e) {
        loadError = e instanceof ApiError && e.status === 404 ? t('monitors.form.notFound') : errorMessage(e);
      } finally {
        loading = false;
      }
    }
    await chP;
    await tick();
    resetBaseline();
  });

  onMount(() => guardUnsaved(isDirty));

  // Yardımcı davranışlar ------------------------------------------------------------
  function urlBlur() {
    const u = url.trim();
    if (u && !/^[a-z][a-z0-9+.-]*:\/\//i.test(u)) url = 'https://' + u;
    if (!name.trim()) {
      try {
        name = new URL(url).hostname;
      } catch {
        /* geçersiz adres */
      }
    }
  }
  function hostBlur() {
    if (!name.trim() && host.trim()) name = host.trim();
  }

  /** Kabul edilen durum kodu: 100-599 arası kod, küçükten büyüğe aralık veya 2xx gibi sınıf. */
  function validCode(c: string): boolean {
    if (/^[1-5]xx$/i.test(c)) return true;
    const m = c.match(/^(\d{3})(?:-(\d{3}))?$/);
    if (!m) return false;
    const lo = Number(m[1]);
    const hi = m[2] ? Number(m[2]) : lo;
    return lo >= 100 && hi <= 599 && lo <= hi;
  }

  function parseCodes(): string[] {
    return acceptedCodes
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
  }

  const inRange = (v: number | null, lo: number, hi: number) =>
    v !== null && Number.isInteger(v) && v >= lo && v <= hi;

  /** Hata metni, hatanın gelişmiş ayarlarda olup olmadığı ve hatalı alanın kimliği. */
  type Invalid = { msg: string; advanced?: boolean; field?: string };

  /**
   * Formdaki tüm hatalar (ilk hata ilk sırada): kullanıcı hataları tek tek
   * düzeltmek zorunda kalmasın. Her alan grubu kendi ilk hatasını verir.
   */
  function validateAll(): Invalid[] {
    const checks: (() => Invalid | null)[] = [
      () =>
        !name.trim()
          ? { msg: t('monitors.form.v.nameRequired'), field: 'name' }
          : name.trim().length > 100
            ? { msg: t('monitors.form.v.nameTooLong'), field: 'name' }
            : null,
      () =>
        !inRange(interval, 20, 86400)
          ? { msg: t('monitors.form.v.intervalRange'), field: intervalPreset === 'custom' ? 'cint' : 'int' }
          : null,
      () => (type === 'push' && !inRange(pushGrace ?? 0, 0, 86400) ? { msg: t('monitors.form.v.pushGraceRange'), field: 'pgrace' } : null),
      validateType,
      () => (showLocations && !locLocal && locProbeIds.length === 0 ? { msg: t('monitors.form.v.locEmpty') } : null),
      () =>
        retryInterval !== null && !inRange(retryInterval, 20, 86400)
          ? { msg: t('monitors.form.v.retryIntervalRange'), advanced: true, field: 'ri' }
          : null,
      () => (!inRange(maxRetries, 0, 20) ? { msg: t('monitors.form.v.retriesRange'), advanced: true, field: 'mr' } : null),
      () =>
        hasTimeout(type) && !inRange(timeout, 1, 300)
          ? { msg: t('monitors.form.v.timeoutRange'), advanced: true, field: 'to' }
          : timeoutTooLong
            ? { msg: t('monitors.form.v.timeoutInterval'), advanced: true, field: 'to' }
            : null,
      () => (!inRange(resendEvery, 0, 10000) ? { msg: t('monitors.form.v.resendRange'), advanced: true, field: 're' } : null),
      () => (!inRange(slowMs ?? 0, 0, 600000) ? { msg: t('monitors.form.v.slowMsRange'), advanced: true, field: 'sm' } : null),
      () => ((slowMs ?? 0) > 0 && !inRange(slowChecks ?? 0, 1, 100) ? { msg: t('monitors.form.v.slowChecksRange'), advanced: true, field: 'sc' } : null),
      () =>
        description.trim().length > 500 ? { msg: t('monitors.form.v.descTooLong'), advanced: true, field: 'desc' } : null,
    ];
    return checks.map((c) => c()).filter((v): v is Invalid => v !== null);
  }

  /** Türe özgü alanların ilk hatası. */
  function validateType(): Invalid | null {
    switch (type) {
      case 'http': {
        if (!/^https?:\/\/[^\s/]+/i.test(url.trim())) return { msg: t('monitors.form.v.urlInvalid'), field: 'url' };
        const codes = parseCodes();
        const bad = codes.find((c) => !validCode(c));
        if (bad)
          return {
            msg: t('monitors.form.v.badCode', { code: bad }),
            advanced: true,
            field: 'codes',
          };
        if (!inRange(maxRedirects, 0, 30)) return { msg: t('monitors.form.v.redirectsRange'), advanced: true, field: 'rd' };
        if (contentMode === 'keyword' && !keyword) return { msg: t('monitors.form.v.keywordRequired'), advanced: true, field: 'kw' };
        if (contentMode === 'json' && !jsonPath.trim()) return { msg: t('monitors.form.v.jsonPathRequired'), advanced: true, field: 'jp' };
        if (method === 'HEAD' && contentMode !== 'none')
          return { msg: t('monitors.form.v.headNoBody'), advanced: true, field: 'method' };
        const xe = fieldError(HTTP_EXTRA_FIELDS, httpExtra);
        if (xe) return { msg: xe.msg, advanced: true, field: `hx-${xe.key}` };
        const x = httpExtra;
        if (x.proxy_url.trim() && x.proxy_pass && !x.proxy_user.trim())
          return { msg: t('monitors.form.v.proxyUserRequired'), advanced: true, field: 'hx-proxy_user' };
        // Sertifika silindiyse kayıtlı (maskeli) anahtar da kaldırılacak: dolu sayılmaz.
        const keySet = !!x.tls_key.trim() && !(x.tls_key === MASK && !x.tls_cert.trim());
        if (!!x.tls_cert.trim() !== keySet)
          return {
            msg: t('monitors.form.v.certKeyPair'),
            advanced: true,
            field: x.tls_cert.trim() ? 'hx-tls_key' : 'hx-tls_cert',
          };
        if (x.oauth_token_url.trim()) {
          if (!x.oauth_client_id.trim() || !x.oauth_client_secret)
            return {
              msg: t('monitors.form.v.oauthRequired'),
              advanced: true,
              field: x.oauth_client_id.trim() ? 'hx-oauth_client_secret' : 'hx-oauth_client_id',
            };
          if (basicUser || basicPass) return { msg: t('monitors.form.v.oauthBasic'), advanced: true, field: 'bu' };
        }
        break;
      }
      case 'tcp':
        if (!host.trim()) return { msg: t('monitors.form.v.hostRequired'), field: 'host' };
        if (!inRange(port, 1, 65535)) return { msg: t('monitorTypes.f.portRange'), field: 'port' };
        break;
      case 'ping':
        if (!host.trim()) return { msg: t('monitors.form.v.hostRequired'), field: 'host' };
        if (!inRange(pingCount, 1, 10)) return { msg: t('monitors.form.v.pingCount'), field: 'count' };
        break;
      case 'dns':
        if (!host.trim()) return { msg: t('monitors.form.v.domainRequired'), field: 'host' };
        if (!inRange(dnsPort, 1, 65535)) return { msg: t('monitors.form.v.dnsPortRange'), field: 'dport' };
        break;
      case 'group':
        if (groupIds.length === 0) return { msg: t('monitors.form.v.groupEmpty') };
        if (isEdit && groupIds.includes(id!)) return { msg: t('monitors.form.v.groupSelf') };
        if (groupMode === 'percent_down' && !inRange(groupPercent, 0, 99)) return { msg: t('monitors.form.v.groupPercent'), field: 'grp-pct' };
        break;
      default:
        if (genericFields.length) {
          const fe = fieldError(genericFields, extra);
          if (fe) return { ...fe, field: `${fe.advanced ? 'mta' : 'mt'}-${fe.key}` };
        }
    }
    return null;
  }

  function buildConfig(): Record<string, unknown> {
    switch (type) {
      case 'http': {
        const x = fieldConfig(HTTP_EXTRA_FIELDS, httpExtra) as Record<string, string>;
        // Sertifika kaldırıldıysa kayıtlı (maskeli) anahtar da kaldırılır.
        if (!x.tls_cert.trim() && x.tls_key === MASK) x.tls_key = '';
        return {
          url: url.trim(),
          method,
          headers,
          body,
          basic_user: basicUser,
          basic_pass: basicPass,
          accepted_codes: parseCodes().length ? parseCodes() : ['200-399'],
          max_redirects: maxRedirects ?? 10,
          ignore_tls: ignoreTls,
          cert_expiry: certExpiry,
          keyword: contentMode === 'keyword' ? keyword : '',
          keyword_invert: contentMode === 'keyword' ? keywordInvert : false,
          keyword_case: contentMode === 'keyword' ? keywordCase : false,
          json_path: contentMode === 'json' ? jsonPath.trim() : '',
          json_op: contentMode === 'json' ? jsonOp : '',
          json_expected: contentMode === 'json' && jsonOp !== 'exists' ? jsonExpected : '',
          ...x,
        };
      }
      case 'tcp':
        return { host: host.trim(), port: port ?? 0 };
      case 'ping':
        return { host: host.trim(), count: pingCount ?? 3 };
      case 'dns':
        return {
          host: host.trim(),
          server: dnsServer.trim() || '1.1.1.1',
          port: dnsPort ?? 53,
          record_type: recordType,
          expected: dnsExpected.trim(),
        };
      case 'group':
        return groupMode === 'percent_down'
          ? { monitor_ids: groupIds, mode: groupMode, percent: groupPercent ?? 50 }
          : { monitor_ids: groupIds, mode: groupMode };
      case 'push':
        return pushGrace ? { grace_sec: pushGrace } : {};
      default:
        return genericFields.length ? fieldConfig(genericFields, extra) : {};
    }
  }

  async function showError(msg: string, advanced = false, field?: string, more: string[] = []) {
    error = msg;
    moreErrors = more;
    if (advanced) showAdvanced = true;
    await tick();
    markInvalid(field, 'mf-error');
    errorEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    moreErrors = [];
    markInvalid(null, 'mf-error');
    const errs = validateAll();
    if (errs.length) {
      const v = errs[0];
      showError(
        v.msg,
        errs.some((x) => x.advanced),
        v.field,
        errs.slice(1).map((x) => x.msg),
      );
      return;
    }
    const input: MonitorInput = {
      name: name.trim(),
      type,
      description: description.trim(),
      interval,
      retry_interval: retryInterval ?? interval,
      max_retries: maxRetries ?? 0,
      timeout: timeout ?? 30,
      resend_every: resendEvery ?? 0,
      upside_down: hasUpsideDown(type) ? upsideDown : false,
      slow_ms: slowMs ?? 0,
      slow_checks: slowChecks ?? 3,
      config: buildConfig(),
      // Kanal listesi alınamadıysa null: yeni monitörde varsayılanlar, düzenlemede mevcut bağlantılar korunur.
      notification_ids: channelsOk ? notifIds.filter((nid) => channels.some((c) => c.id === nid)) : null,
    };
    saving = true;
    let res: MonitorView;
    try {
      res = isEdit ? await api.updateMonitor(id!, input) : await api.createMonitor(input);
    } catch (err) {
      showError(errorMessage(err));
      saving = false;
      return;
    }
    // Etiketler ve konumlar monitör kaydedildikten sonra ayrı isteklerle yazılır.
    const problems: string[] = [];
    if (tagsLoaded && tagsChanged()) {
      try {
        const tags = await api.setMonitorTags(
          res.id,
          mtags.map((t) => ({ tag_id: t.id, value: t.value.trim() })),
        );
        res = { ...res, tags };
      } catch (err) {
        problems.push(t('monitors.form.tagsFailed', { err: errorMessage(err) }));
      }
    }
    const loc = locationTarget();
    if (loc) {
      try {
        const l = await api.setMonitorLocations(res.id, loc);
        res = { ...res, locations: { include_local: l.include_local, probe_ids: l.probe_ids, down_when: l.down_when, notify_partial: l.notify_partial } };
      } catch (err) {
        problems.push(t('monitors.form.locFailed', { err: errorMessage(err) }));
      }
    }
    if (fromClone && !res.active) {
      try {
        res = await api.resumeMonitor(res.id);
      } catch (err) {
        problems.push(t('monitors.form.resumeFailed', { err: errorMessage(err) }));
      }
    }
    live.upsert(res);
    saving = false;
    saved = true;
    if (problems.length) {
      toast.error(t('monitors.form.savedBut', { problems: problems.join(' ') }));
      // Yeni monitör artık var: tekrar göndermek kopya oluşturmasın diye düzenleme sayfasına geç.
      navigate(`/monitors/${res.id}/edit`, true);
      return;
    }
    toast.success(isEdit ? t('monitors.form.changesSaved') : t('monitors.form.added', { name: res.name }));
    navigate(`/monitors/${res.id}`);
  }

  // Kaydedilmemiş değişiklikler --------------------------------------------------------
  let baseline = '';
  let saved = false;
  function formState() {
    return {
      name: name.trim(),
      type,
      description: description.trim(),
      interval,
      retryInterval,
      maxRetries,
      timeout,
      resendEvery,
      upsideDown,
      cfg: buildConfig(),
      notif: [...notifIds].sort((a, b) => a - b),
      tags: tagKey(mtags),
      loc: [locLocal, [...locProbeIds].sort((a, b) => a - b), locDownWhen, locNotify],
    };
  }
  const isDirty = () => !saved && !!baseline && !loading && !loadError && snapshot(formState()) !== baseline;
  const resetBaseline = () => (baseline = snapshot(formState()));

  // Hedef değişince kayıtlı gizli alanlar ----------------------------------------------
  // Sunucu, adres/sunucu/port değiştiğinde maskeli (kayıtlı) gizli değeri yeni hedefe
  // taşımaz. Bu durumda maskeli alanlar boşaltılır, gelişmiş ayarlar açılır ve ne
  // yapılması gerektiği yazılır; hedef eski hâline dönerse kayıtlı değerler geri gelir.
  const HTTP_SECRETS: Record<string, TKey> = {
    basic_pass: 'monitors.form.basicPass',
    headers: 'monitors.form.secret.headers',
    proxy_pass: 'monitorTypes.f.http.proxyPass',
    tls_key: 'monitors.form.secret.tlsKey',
    oauth_client_secret: 'monitors.form.secret.clientSecret',
  };
  const secretKeys = $derived(
    type === 'http' ? Object.keys(HTTP_SECRETS) : genericFields.filter((f) => f.kind === 'secret' || f.secret).map((f) => f.key),
  );
  function getSecret(k: string): string {
    if (type === 'http') return k === 'basic_pass' ? basicPass : k === 'headers' ? headers : (httpExtra[k] ?? '');
    return extra[k] ?? '';
  }
  function setSecret(k: string, v: string) {
    if (type === 'http') {
      if (k === 'basic_pass') basicPass = v;
      else if (k === 'headers') headers = v;
      else httpExtra[k] = v;
    } else extra[k] = v;
  }
  const secretLabel = (k: string) =>
    (type === 'http' ? (HTTP_SECRETS[k] ? t(HTTP_SECRETS[k]) : undefined) : genericFields.find((f) => f.key === k)?.label) ?? k;

  const destChanged = $derived.by(() => {
    if (!isEdit || loading || origType !== type) return [] as string[];
    return changedDestinations(buildConfig(), origConfig);
  });
  let clearedSecrets = $state<string[]>([]);

  $effect(() => {
    const changed = destChanged.length > 0;
    untrack(() => {
      if (changed) {
        const keys = secretKeys.filter((k) => getSecret(k) === MASK);
        if (!keys.length) return;
        for (const k of keys) setSecret(k, '');
        clearedSecrets = [...new Set([...clearedSecrets, ...keys])];
        const advancedKeys = type === 'http' ? keys : keys.filter((k) => genericFields.find((f) => f.key === k)?.advanced);
        if (advancedKeys.length) showAdvanced = true;
      } else if (clearedSecrets.length) {
        for (const k of clearedSecrets) if (getSecret(k) === '') setSecret(k, MASK);
        clearedSecrets = [];
      }
    });
  });
  const rebindMsg = $derived(
    clearedSecrets.length && destChanged.length
      ? t(clearedSecrets.length > 1 ? 'monitors.form.rebindMany' : 'monitors.form.rebindOne', {
          dest: destinationPhrase(destChanged),
          secrets: clearedSecrets.map(secretLabel).join(', '),
          adv: type === 'http' ? t('monitors.form.rebindAdv') : '',
        })
      : '',
  );

  // Etiketler --------------------------------------------------------------------------
  const tagKey = (l: { id: number; value: string }[]) =>
    l
      .map((t) => `${t.id}=${t.value.trim()}`)
      .sort()
      .join('|');
  const tagsChanged = () => tagKey(mtags) !== tagKey(origTags);

  function addTag(idStr: string) {
    const tid = Number(idStr);
    tagPick = '';
    if (!tid || mtags.some((t) => t.id === tid)) return;
    mtags = [...mtags, { id: tid, value: '' }];
    tick().then(() => document.getElementById(`tv-${tid}`)?.focus());
  }

  function tagCreated(tag: Tag) {
    allTags = [...allTags, tag];
    addTag(String(tag.id));
  }

  // Konumlar ----------------------------------------------------------------------------
  const showLocations = $derived(remoteOk && probesLoaded && probes.length > 0);

  /** Kaydedilecek konum ayarı; değişiklik yoksa null (gereksiz istek ve işlem kaydı olmasın). */
  function locationTarget(): LocationSetup | null {
    if (!probesLoaded) return null;
    const target: LocationSetup =
      remoteOk && probes.length > 0
        ? {
            include_local: locLocal,
            probe_ids: locProbeIds.filter((pid) => probes.some((p) => p.id === pid)).sort((a, b) => a - b),
            down_when: locDownWhen,
            notify_partial: locNotify,
          }
        : { include_local: true, probe_ids: [], down_when: 'any', notify_partial: false };
    const same =
      target.include_local === origLoc.include_local &&
      target.down_when === origLoc.down_when &&
      !!target.notify_partial === !!origLoc.notify_partial &&
      target.probe_ids.join(',') === [...origLoc.probe_ids].sort((a, b) => a - b).join(',');
    return same ? null : target;
  }
  const locCount = $derived((locLocal ? 1 : 0) + locProbeIds.length);

  const cancelHref = $derived(isEdit ? `#/monitors/${id}` : '#/');
</script>

<a class="back" href={cancelHref}><Icon name="chevron-left" size={16} /> {isEdit ? t('monitors.form.backToMonitor') : t('nav.monitors')}</a>
<div class="page-head">
  <h1>{isEdit ? t('monitors.form.titleEdit') : t('nav.titles.newMonitor')}<span class="dot">.</span></h1>
</div>

{#if loading}
  <div class="skeleton" style="height:420px;max-width:820px"></div>
{:else if loadError}
  <div class="card empty">
    <h3>{t('monitors.form.loadFailed')}</h3>
    <p>{loadError}</p>
    <a class="btn primary" href="#/">{t('nav.backToMonitors')}</a>
  </div>
{:else}
  <form class="form" onsubmit={submit} novalidate>
    {#if fromClone && !live.byId(id!)?.active}
      <div class="alert info small">
        {t('monitors.form.cloneNote')}
      </div>
    {/if}
    <section class="card">
      <div class="tp-head">
        <h2 class="card-title">{t('monitors.form.type')}</h2>
        {#if pickerOpen}
          <div class="tsearch">
            <span class="s-ic"><Icon name="search" size={15} /></span>
            <input
              class="input"
              type="search"
              placeholder={t('monitors.form.typeSearchPh')}
              bind:value={typeQuery}
              aria-label={t('monitors.form.typeSearchAria')}
            />
          </div>
        {/if}
      </div>
      {#if !pickerOpen && def}
        <div class="tcur">
          <span class="ticon on"><Icon name={def.icon} size={20} /></span>
          <span class="tcur-t">
            <span class="tlabel">{def.label}</span>
            <span class="tdesc">{def.desc}</span>
          </span>
          <button type="button" class="btn sm" onclick={() => (pickerOpen = true)}>{t('monitors.form.change')}</button>
        </div>
      {:else}
        {#if isEdit}
          <p class="help tp-warn">{t('monitors.form.typeChangeWarn')}</p>
        {/if}
        {#each typeGroups as g (g.key)}
          <h3 class="tgroup">{g.label}</h3>
          <div class="types" role="radiogroup" aria-label={g.label}>
            {#each g.types as tp (tp.key)}
              <button
                type="button"
                role="radio"
                aria-checked={type === tp.key}
                class="type"
                class:active={type === tp.key}
                onclick={() => pickType(tp.key)}
              >
                <span class="ticon"><Icon name={tp.icon} size={20} /></span>
                <span class="tlabel">{tp.label}</span>
                <span class="tdesc">{tp.desc}</span>
              </button>
            {/each}
          </div>
        {:else}
          <p class="muted small nomargin">
            {t('monitors.form.noTypeMatch', { q: typeQuery })}
            <button type="button" class="linkbtn" onclick={() => (typeQuery = '')}>{t('monitors.form.clearSearch')}</button>
          </p>
        {/each}
        {#if isEdit}
          <button type="button" class="linkbtn tp-close" onclick={() => (pickerOpen = false)}>{t('monitors.form.closeList')}</button>
        {/if}
      {/if}
    </section>

    <section class="card stack">
      <div class="field">
        <label for="name">{t('common.name')}</label>
        <input id="name" class="input" bind:value={name} maxlength="100" placeholder={t('monitors.form.namePh')} />
      </div>

      {#if type === 'http'}
        <div class="field">
          <label for="url">{t('monitors.form.url')}</label>
          <input
            id="url"
            class="input"
            type="url"
            inputmode="url"
            autocapitalize="none"
            spellcheck="false"
            bind:value={url}
            onblur={urlBlur}
            placeholder={t('monitors.form.urlPh')}
          />
        </div>
      {:else if type === 'tcp'}
        <div class="grid-host">
          <div class="field">
            <label for="host">{t('monitors.form.host')}</label>
            <input
              id="host"
              class="input"
              autocapitalize="none"
              spellcheck="false"
              bind:value={host}
              onblur={hostBlur}
              placeholder={t('monitors.form.hostPh')}
            />
          </div>
          <div class="field">
            <label for="port">{t('monitorTypes.f.port')}</label>
            <input id="port" class="input" type="number" min="1" max="65535" bind:value={port} placeholder="443" />
          </div>
        </div>
      {:else if type === 'ping'}
        <div class="grid-host">
          <div class="field">
            <label for="host">{t('monitors.form.host')}</label>
            <input
              id="host"
              class="input"
              autocapitalize="none"
              spellcheck="false"
              bind:value={host}
              onblur={hostBlur}
              placeholder={t('monitors.form.hostPh')}
            />
          </div>
          <div class="field">
            <label for="count">{t('monitors.form.packets')}</label>
            <input id="count" class="input" type="number" min="1" max="10" bind:value={pingCount} />
          </div>
        </div>
      {:else if type === 'dns'}
        <div class="field">
          <label for="host">{t('monitors.form.domain')}</label>
          <input
            id="host"
            class="input"
            autocapitalize="none"
            spellcheck="false"
            bind:value={host}
            onblur={hostBlur}
            placeholder={t('monitors.form.domainPh')}
          />
        </div>
        <div class="grid-3">
          <div class="field">
            <label for="rt">{t('monitors.form.recordType')}</label>
            <select id="rt" class="input" bind:value={recordType}>
              {#each RECORD_TYPES as r (r)}<option value={r}>{r}</option>{/each}
            </select>
          </div>
          <div class="field">
            <label for="dsrv">{t('monitors.form.dnsServer')}</label>
            <input id="dsrv" class="input" autocapitalize="none" spellcheck="false" bind:value={dnsServer} placeholder="1.1.1.1" />
          </div>
          <div class="field">
            <label for="dport">{t('monitorTypes.f.port')}</label>
            <input id="dport" class="input" type="number" min="1" max="65535" bind:value={dnsPort} />
          </div>
        </div>
        <div class="field">
          <label for="dexp">{t('monitorTypes.f.expectedValue')} <span class="muted">{t('monitors.optionalParen')}</span></label>
          <input id="dexp" class="input" bind:value={dnsExpected} placeholder={t('monitors.form.dnsExpectedPh')} />
          <span class="help">{t('monitors.form.dnsExpectedHelp')}</span>
        </div>
      {:else if type === 'push'}
        {#if isEdit && pushUrl}
          <div class="field">
            <span class="label">{t('monitors.form.pushAddress')}</span>
            <div class="pushbox">
              <code>{pushUrl}</code>
              <CopyButton text={pushUrl} />
              <button type="button" class="btn sm" onclick={regeneratePush} disabled={regenerating} title={t('monitors.form.pushRegenerateTitle')}>
                {#if regenerating}<span class="spinner"></span>{:else}<Icon name="refresh" size={14} />{/if}
                {t('monitors.form.pushRegenerate')}
              </button>
            </div>
            <span class="help">{t('monitors.form.pushAddressHelp')}</span>
          </div>
        {:else}
          <div class="alert info">
            {#each tParts('monitors.form.pushInfo') as p, i (i)}{#if p.slot === 'push'}<b>{t('monitors.form.pushUrl')}</b>{:else}{p.text}{/if}{/each}
          </div>
        {/if}
      {:else if type === 'group'}
        <div class="field">
          <span class="label" id="grp-l">{t('monitors.detail.children')}</span>
          <MonitorPicker bind:selected={groupIds} exclude={isEdit ? [id!] : []} label={t('monitors.detail.children')} id="grp" />
        </div>
        <div class="field">
          <span class="label" id="grp-mode">{t('monitors.form.mode')}</span>
          <div class="seg" role="radiogroup" aria-labelledby="grp-mode">
            {#each GROUP_MODES as gm (gm.v)}
              <button type="button" role="radio" aria-checked={groupMode === gm.v} class:active={groupMode === gm.v} onclick={() => (groupMode = gm.v)}>
                {gm.l}
              </button>
            {/each}
          </div>
          <span class="help">{t('monitors.childrenHelp')}</span>
        </div>
        {#if groupMode === 'percent_down'}
          <div class="field pct">
            <label for="grp-pct">{t('monitorTypes.groupModes.percentLabel')}</label>
            <input id="grp-pct" class="input" type="number" min="0" max="99" bind:value={groupPercent} />
            <span class="help">{t('monitorTypes.groupModes.percentHelp')}</span>
          </div>
        {/if}
      {:else if genericFields.length}
        {#if def?.about}<p class="about text-2 small">{def.about}</p>{/if}
        {#if def?.note}<div class="alert warning">{def.note}</div>{/if}
        <ConfigFields fields={genericFields} bind:values={extra} idPrefix="mt" part="basic" onsuggest={suggestName} />
      {/if}

      {#if rebindMsg}
        <div class="alert warning small" role="status">{rebindMsg}</div>
      {/if}

      <div class="grid-int">
        <div class="field">
          <label for="int">{type === 'push' ? t('monitors.form.intervalPush') : t('monitors.form.interval')}</label>
          <select id="int" class="input" bind:value={intervalPreset}>
            {#each PRESETS as p (p)}<option value={String(p)}>{fmtInterval(p)}</option>{/each}
            <option value="custom">{t('monitors.form.custom')}</option>
          </select>
        </div>
        {#if intervalPreset === 'custom'}
          <div class="field">
            <label for="cint">{t('monitors.form.seconds')}</label>
            <input id="cint" class="input" type="number" min="20" max="86400" bind:value={customInterval} />
          </div>
        {/if}
      </div>
      <span class="help int-help">
        {type === 'push' ? t('monitors.form.intervalPushHelp') : t('monitors.form.intervalHelp')}
      </span>
      {#if type === 'push'}
        <div class="field grace">
          <label for="pgrace">{t('monitors.form.pushGrace')}</label>
          <input id="pgrace" class="input" type="number" min="0" max="86400" step="1" bind:value={pushGrace} />
          <span class="help">{t('monitors.form.pushGraceHelp')}</span>
        </div>
      {/if}
    </section>

    <section class="card">
      <h2 class="card-title">{t('nav.notifications')}</h2>
      {#if !channelsLoaded}
        <div class="skeleton" style="height:40px"></div>
      {:else if channels.length === 0}
        <p class="muted small nomargin">
          {#each tParts('monitors.form.noChannels') as p, i (i)}{#if p.slot === 'link'}<a href="#/notifications">{t('nav.notifications')}</a
            >{:else}{p.text}{/if}{/each}
        </p>
      {:else}
        <p class="help nomargin sp">{t('monitors.form.notifyHelp')}</p>
        <div class="channels">
          {#each channels as ch (ch.id)}
            <label class="check ch">
              <input type="checkbox" value={ch.id} bind:group={notifIds} />
              <span>
                {ch.name}
                <small
                  >{NOTIFY_LABELS[ch.type] ?? ch.type}{ch.active ? '' : ` · ${t('monitors.inactive')}`}{ch.is_default
                    ? ` · ${t('monitors.form.isDefault')}`
                    : ''}</small
                >
              </span>
            </label>
          {/each}
        </div>
      {/if}
    </section>

    <section class="card">
      <h2 class="card-title"><Icon name="tag" size={17} /> {t('monitors.tags')}</h2>
      {#if !tagsLoaded}
        <div class="skeleton" style="height:40px"></div>
      {:else}
        {#if mtags.length}
          <ul class="mtags">
            {#each mtags as mt (mt.id)}
              {@const tg = tagById.get(mt.id)}
              <li>
                <span class="mt-chip"><TagChip name={tg?.name ?? `#${mt.id}`} color={tg?.color ?? ''} /></span>
                <input
                  id="tv-{mt.id}"
                  class="input mt-val"
                  maxlength="100"
                  bind:value={mt.value}
                  placeholder={t('monitors.form.tagValuePh')}
                  aria-label={t('monitors.form.tagValueAria', { name: tg?.name ?? t('monitors.tagFallback') })}
                />
                <button
                  type="button"
                  class="btn ghost icon sm"
                  aria-label={t('monitors.removeTag', { name: tg?.name ?? '' })}
                  onclick={() => (mtags = mtags.filter((x) => x.id !== mt.id))}><Icon name="x" size={15} /></button
                >
              </li>
            {/each}
          </ul>
          <p class="help sp">{t('monitors.form.tagValueHelp')}</p>
        {/if}
        <div class="tag-add">
          {#if addableTags.length}
            <select class="input" bind:value={tagPick} onchange={() => addTag(tagPick)} aria-label={t('monitors.addTag')}>
              <option value="">{t('monitors.addTagOption')}</option>
              {#each addableTags as tg (tg.id)}<option value={String(tg.id)}>{tg.name}</option>{/each}
            </select>
          {:else if allTags.length === 0}
            <span class="muted small">{t('monitors.noTags')}</span>
          {/if}
          <button
            type="button"
            class="btn sm"
            onclick={() => {
              tagDialogKey++;
              tagDialogOpen = true;
            }}><Icon name="plus" size={14} /> {t('monitors.newTag')}</button
          >
        </div>
      {/if}
    </section>

    {#if remoteOk}
      <section class="card">
        <h2 class="card-title"><Icon name="map-pin" size={17} /> {t('monitors.form.locations')}</h2>
        {#if !probesLoaded}
          <div class="skeleton" style="height:40px"></div>
        {:else if probes.length === 0}
          <p class="muted small nomargin">
            {t('monitors.form.localOnly')}
            {#if session.isAdmin}
              {#each tParts('monitors.form.addProbeAdmin') as p, i (i)}{#if p.slot === 'link'}<a href="#/settings/probes"
                    >{t('monitors.form.probesPath')}</a
                  >{:else}{p.text}{/if}{/each}
            {:else}
              {#each tParts('monitors.form.addProbeUser') as p, i (i)}{#if p.slot === 'path'}<b>{t('monitors.form.probesPath')}</b
                  >{:else}{p.text}{/if}{/each}
            {/if}
          </p>
        {:else}
          <p class="help nomargin sp">{t('monitors.form.locHelp')}</p>
          <div class="locs">
            <label class="check loc">
              <input type="checkbox" bind:checked={locLocal} />
              <span class="loc-t">
                <span class="odot up" aria-hidden="true"></span>
                <span>{t('monitors.form.mainServer')}<small>{t('monitors.form.mainServerDesc')}</small></span>
              </span>
            </label>
            {#each probes as p (p.id)}
              <label class="check loc" class:off={!p.active}>
                <input type="checkbox" value={p.id} bind:group={locProbeIds} />
                <span class="loc-t">
                  <span class="odot {!p.active ? 'paused' : p.online ? 'up' : 'down'}" aria-hidden="true"></span>
                  <span
                    >{p.name}<small
                      >{!p.active ? t('monitors.form.probeDisabled') : p.online ? t('monitors.form.online') : t('monitors.form.offline')}</small
                    ></span
                  >
                </span>
              </label>
            {/each}
          </div>
          <div class="field dw">
            <label for="dw">{t('monitors.form.downRule')}</label>
            <select id="dw" class="input" bind:value={locDownWhen} disabled={locCount < 2}>
              {#each DOWN_WHEN as d (d.v)}<option value={d.v}>{t(d.l)}</option>{/each}
            </select>
            <span class="help">{t('monitors.form.downRuleHelp')}</span>
          </div>
          <label class="check">
            <input type="checkbox" bind:checked={locNotify} disabled={locCount < 2} />
            <span>{t('monitors.form.notifyPartial')}<small>{t('monitors.form.notifyPartialHelp')}</small></span>
          </label>
        {/if}
      </section>
    {/if}

    <section class="card adv">
      <button type="button" class="adv-toggle" aria-expanded={showAdvanced} onclick={() => (showAdvanced = !showAdvanced)}>
        <span>{t('monitors.form.advanced')}</span>
        <span class="chev" class:open={showAdvanced}><Icon name="chevron-down" /></span>
      </button>

      {#if showAdvanced}
        <div class="adv-body stack">
          <div class="grid-2">
            <!-- Grup kendisi ağa çıkmaz: tekrar deneme ve zaman aşımı alt monitörlerde uygulanır, burada gizlenir. -->
            {#if type !== 'group'}
              <div class="field">
                <label for="mr">{t('monitors.form.retries')}</label>
                <input id="mr" class="input" type="number" min="0" max="20" bind:value={maxRetries} />
                <span class="help">{t('monitors.form.retriesHelp')}</span>
              </div>
              <div class="field">
                <label for="ri">{t('monitors.form.retryInterval')}</label>
                <input
                  id="ri"
                  class="input"
                  type="number"
                  min="20"
                  max="86400"
                  bind:value={retryInterval}
                  placeholder={t('monitors.form.retryIntervalPh', { n: interval || 60 })}
                />
                <span class="help">{t('monitors.form.retryIntervalHelp')}</span>
              </div>
            {/if}
            {#if hasTimeout(type)}
              <div class="field">
                <label for="to">{t('monitors.form.timeout')}</label>
                <input id="to" class="input" type="number" min="1" max="300" bind:value={timeout} oninput={() => (timeoutAuto = false)} />
                {#if timeoutTooLong}
                  <span class="help tp-warn">{t('monitors.form.timeoutTooLong', { n: timeoutLimit })}</span>
                {:else}
                  <span class="help">{t('monitors.form.timeoutHelp')}</span>
                {/if}
              </div>
            {/if}
            <div class="field">
              <label for="re">{t('monitors.form.resend')}</label>
              <input id="re" class="input" type="number" min="0" max="10000" bind:value={resendEvery} />
              <span class="help">{t('monitors.form.resendHelp')}</span>
            </div>
          </div>

          {#if type !== 'group'}
            <div class="grid-2">
              <div class="field">
                <label for="sm">{t('monitors.form.slowMs')}</label>
                <input id="sm" class="input" type="number" min="0" max="600000" step="100" bind:value={slowMs} placeholder="0" />
                <span class="help">{t('monitors.form.slowMsHelp')}</span>
              </div>
              {#if (slowMs ?? 0) > 0}
                <div class="field">
                  <label for="sc">{t('monitors.form.slowChecks')}</label>
                  <input id="sc" class="input" type="number" min="1" max="100" bind:value={slowChecks} />
                  <span class="help">{t('monitors.form.slowChecksHelp')}</span>
                </div>
              {/if}
            </div>
          {/if}

          {#if hasUpsideDown(type)}
            <label class="check">
              <input type="checkbox" bind:checked={upsideDown} />
              <span>{t('monitors.form.upsideDown')}<small>{t('monitors.form.upsideDownHelp')}</small></span>
            </label>
          {/if}

          {#if hasAdvancedFields}
            <div class="divider"></div>
            <h3>{t('monitors.form.typeSettings', { type: def?.label ?? '' })}</h3>
            <ConfigFields fields={genericFields} bind:values={extra} idPrefix="mta" part="advanced" onsuggest={suggestName} />
          {/if}

          {#if type === 'http'}
            <div class="divider"></div>
            <h3>{t('monitors.form.httpRequest')}</h3>
            <div class="grid-2">
              <div class="field">
                <label for="method">{t('monitors.form.method')}</label>
                <select id="method" class="input" bind:value={method}>
                  {#each METHODS as m (m)}<option value={m}>{m}</option>{/each}
                </select>
              </div>
              <div class="field">
                <label for="codes">{t('monitors.form.codes')}</label>
                <input id="codes" class="input" bind:value={acceptedCodes} placeholder="200-399" />
                <span class="help">{t('monitors.form.codesHelp')}</span>
              </div>
            </div>
            <div class="field">
              <label for="hdr">{t('monitors.form.headers')}</label>
              {#if headers === MASK}
                <!-- Başlıklar gizli alan: sunucu maskeli döndürür; değiştirilmezse kayıtlı değer korunur. -->
                <div class="kept-row">
                  <span class="help">{t('monitors.form.headersKept')}</span>
                  <button type="button" id="hdr" class="btn sm" onclick={() => (headers = '')}>{t('monitors.form.change')}</button>
                </div>
              {:else}
                <textarea id="hdr" class="input" rows="3" bind:value={headers} placeholder={t('monitors.form.headersPh')}></textarea>
                <span class="help"
                  >{#each tParts('monitors.form.headersHelp') as p, i (i)}{#if p.slot === 'code'}<code>{t('monitors.form.headersHelpCode')}</code
                      >{:else}{p.text}{/if}{/each}</span
                >
              {/if}
            </div>
            <div class="field">
              <label for="body">{t('monitors.form.body')}</label>
              <textarea id="body" class="input" rows="3" bind:value={body} placeholder={t('monitors.form.bodyPh')}></textarea>
              <span class="help">{t('monitors.form.bodyHelp')}</span>
            </div>
            <div class="grid-2">
              <div class="field">
                <label for="bu">{t('monitors.form.basicUser')}</label>
                <input id="bu" class="input" autocomplete="off" bind:value={basicUser} />
              </div>
              <div class="field">
                <label for="bp">{t('monitors.form.basicPass')}</label>
                <input id="bp" class="input" type="password" autocomplete="new-password" bind:value={basicPass} />
                {#if basicPass === MASK}<span class="help">{t('monitors.form.passKept')}</span>{/if}
              </div>
              <div class="field">
                <label for="rd">{t('monitors.form.maxRedirects')}</label>
                <input id="rd" class="input" type="number" min="0" max="30" bind:value={maxRedirects} />
                <span class="help">{t('monitors.form.maxRedirectsHelp')}</span>
              </div>
            </div>
            <label class="check">
              <input type="checkbox" bind:checked={ignoreTls} />
              <span>{t('monitors.form.ignoreTls')}<small>{t('monitorTypes.f.ignoreTlsHelp')}</small></span>
            </label>
            <label class="check">
              <input type="checkbox" bind:checked={certExpiry} />
              <span>{t('monitors.form.certExpiry')}<small>{t('monitors.form.certExpiryHelp')}</small></span>
            </label>

            <div class="divider"></div>
            <h3>{t('monitors.form.content')}</h3>
            <div class="seg" role="radiogroup" aria-label={t('monitors.form.content')}>
              <button
                type="button"
                role="radio"
                aria-checked={contentMode === 'none'}
                class:active={contentMode === 'none'}
                onclick={() => (contentMode = 'none')}>{t('common.none')}</button
              >
              <button
                type="button"
                role="radio"
                aria-checked={contentMode === 'keyword'}
                class:active={contentMode === 'keyword'}
                onclick={() => (contentMode = 'keyword')}>{t('monitors.form.keyword')}</button
              >
              <button type="button" role="radio" aria-checked={contentMode === 'json'} class:active={contentMode === 'json'} onclick={() => (contentMode = 'json')}>JSON</button>
            </div>
            {#if contentMode === 'keyword'}
              <div class="field">
                <label for="kw">{t('monitors.form.keywordLabel')}</label>
                <input id="kw" class="input" bind:value={keyword} placeholder={t('monitors.form.keywordPh')} />
                <span class="help">{t('monitors.form.keywordHelp')}</span>
              </div>
              <label class="check">
                <input type="checkbox" bind:checked={keywordInvert} />
                <span>{t('monitors.form.invert')}<small>{t('monitors.form.invertHelp')}</small></span>
              </label>
              <label class="check">
                <input type="checkbox" bind:checked={keywordCase} />
                <span>{t('monitors.form.caseSensitive')}</span>
              </label>
            {:else if contentMode === 'json'}
              <div class="grid-3">
                <div class="field">
                  <label for="jp">{t('monitorTypes.f.mqtt.jsonPath')}</label>
                  <input id="jp" class="input mono" autocapitalize="none" spellcheck="false" bind:value={jsonPath} placeholder="data.status" />
                </div>
                <div class="field">
                  <label for="jo">{t('monitorTypes.f.snmp.condition')}</label>
                  <select id="jo" class="input" bind:value={jsonOp}>
                    {#each JSON_OPS as o (o.v)}<option value={o.v}>{o.l}</option>{/each}
                  </select>
                </div>
                {#if jsonOp !== 'exists'}
                  <div class="field">
                    <label for="je">{t('monitorTypes.f.expectedValue')}</label>
                    <input id="je" class="input" bind:value={jsonExpected} placeholder="ok" />
                  </div>
                {/if}
              </div>
              <span class="help">
                {#each tParts('monitors.form.jsonHelp') as p, i (i)}{#if p.slot === 'a'}<code>status</code>{:else if p.slot === 'b'}<code
                      >data.status</code
                    >{:else if p.slot === 'c'}<code>items.0.name</code>{:else if p.slot === 'd'}<code>items.#</code>{:else}{p.text}{/if}{/each}
              </span>
            {/if}

            <div class="divider"></div>
            <h3>{t('monitors.form.connAuth')}</h3>
            <ConfigFields fields={HTTP_EXTRA_FIELDS} bind:values={httpExtra} idPrefix="hx" />
          {/if}

          <div class="divider"></div>
          <div class="field">
            <label for="desc">{t('monitors.form.description')} <span class="muted">{t('monitors.optionalParen')}</span></label>
            <textarea
              id="desc"
              class="input plain"
              rows="2"
              maxlength="500"
              bind:value={description}
              placeholder={t('monitors.form.descPh')}
            ></textarea>
          </div>
        </div>
      {/if}
    </section>

    {#if error}
      <div class="alert error" role="alert" id="mf-error" bind:this={errorEl}>
        {#if moreErrors.length}
          <ul class="errs">
            <li>{error}</li>
            {#each moreErrors as m, i (i)}<li>{m}</li>{/each}
          </ul>
        {:else}
          {error}
        {/if}
      </div>
    {/if}

    <div class="actions">
      <a class="btn" href={cancelHref}>{t('common.cancel')}</a>
      <button class="btn primary" type="submit" disabled={saving}>
        {#if saving}<span class="spinner"></span>{/if}
        {isEdit ? t('common.save') : t('monitors.form.submitNew')}
      </button>
    </div>
  </form>

  {#key tagDialogKey}
    {#if tagDialogKey > 0}
      <TagDialog bind:open={tagDialogOpen} onsaved={tagCreated} />
    {/if}
  {/key}
{/if}

<style>
  .pushbox {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 8px);
    background: var(--card-2, var(--bg-2));
  }
  .pushbox code {
    flex: 1 1 240px;
    min-width: 0;
    word-break: break-all;
    font-size: 0.85rem;
  }

  .errs {
    margin: 0;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--text-2);
    font-size: 0.88rem;
    margin-bottom: 14px;
  }
  .form {
    max-width: 820px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .types {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
    gap: 8px;
  }
  .tp-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 14px;
  }
  .tp-head .card-title {
    margin: 0;
  }
  .tsearch {
    position: relative;
    flex: 0 1 260px;
    min-width: 0;
  }
  .tsearch .input {
    height: 36px;
    padding-left: 34px;
  }
  .s-ic {
    position: absolute;
    left: 11px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    color: var(--muted);
    pointer-events: none;
  }
  .tp-warn {
    margin: -6px 0 10px;
  }
  .tp-close {
    margin-top: 12px;
    font-size: 0.88rem;
  }
  .tcur {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 10px 10px 12px;
    border-radius: 10px;
    border: 1px solid var(--accent);
    background: var(--accent-soft);
  }
  .tcur-t {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .tcur .ticon {
    margin: 0;
    flex-shrink: 0;
  }
  .ticon.on {
    background: var(--accent);
    color: var(--accent-contrast);
  }
  .tgroup {
    font-size: 0.78rem;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin: 16px 0 8px;
  }
  .tp-head + .tgroup,
  .tp-warn + .tgroup {
    margin-top: 0;
  }
  .about {
    margin: -4px 0 0;
  }
  /* Yatay kart: 20 tip tek bakışta sığsın diye simge solda, ad ve açıklama sağda. */
  .type {
    display: grid;
    grid-template-columns: 34px minmax(0, 1fr);
    grid-template-areas:
      'icon label'
      'icon desc';
    column-gap: 12px;
    row-gap: 1px;
    align-items: center;
    padding: 10px 12px;
    border-radius: 10px;
    border: 1px solid var(--border-strong);
    background: var(--input);
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
    transition:
      border-color 0.15s,
      background 0.15s;
  }
  @media (hover: hover) {
    .type:hover {
      border-color: var(--border-hover);
    }
  }
  .type.active {
    border-color: var(--accent);
    background: var(--accent-soft);
    box-shadow: 0 0 0 1px var(--accent) inset;
  }
  .ticon {
    display: inline-flex;
    width: 34px;
    height: 34px;
    border-radius: 9px;
    align-items: center;
    justify-content: center;
    background: var(--card-2);
    color: var(--text-2);
    grid-area: icon;
  }
  /* Seçili kartın açıklaması vurgu zemininde okunur kalsın (≥ 4,5:1). */
  .type.active .tdesc,
  .tcur .tdesc {
    color: var(--text-2);
  }
  .type.active .ticon {
    background: var(--accent);
    color: var(--accent-contrast);
  }
  .tlabel {
    grid-area: label;
    font-weight: 700;
    font-size: 0.93rem;
    line-height: 1.3;
  }
  .tdesc {
    grid-area: desc;
    font-size: 0.76rem;
    color: var(--muted);
    line-height: 1.35;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .card-title :global(svg) {
    vertical-align: -3px;
    margin-right: 4px;
    color: var(--accent-text);
  }
  .mtags {
    list-style: none;
    margin: 0 0 6px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .mtags li {
    display: grid;
    grid-template-columns: minmax(0, 200px) minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
  }
  .mt-chip {
    display: flex;
    min-width: 0;
  }
  .mt-val {
    height: 34px;
  }
  .tag-add {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .tag-add select {
    width: auto;
    min-width: 200px;
    height: 34px;
    flex: 0 1 260px;
  }
  .locs {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 10px;
  }
  .loc {
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
  }
  .loc.off .loc-t {
    opacity: 0.65;
  }
  .loc-t {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    min-width: 0;
  }
  .odot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    margin-top: 6px;
    flex-shrink: 0;
    background: var(--paused);
  }
  .odot.up {
    background: var(--up);
    box-shadow: 0 0 0 3px var(--up-ring);
  }
  .odot.down {
    background: var(--down);
  }
  .dw {
    margin-top: 14px;
    max-width: 420px;
  }
  .grid-host {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 140px;
    gap: 16px;
  }
  .grid-int {
    display: grid;
    grid-template-columns: 220px 160px;
    gap: 16px;
  }
  .grace {
    margin-top: 12px;
    max-width: 260px;
  }
  .int-help {
    margin-top: -8px;
  }
  .nomargin {
    margin: 0;
  }
  .sp {
    margin-bottom: 12px;
  }
  .channels {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px 16px;
  }
  .ch {
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
  }
  .adv {
    padding: 0;
  }
  .adv-toggle {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: none;
    border: none;
    color: var(--text);
    font: inherit;
    font-weight: 700;
    font-size: 1.02rem;
    padding: 18px 20px;
    cursor: pointer;
  }
  .chev {
    display: inline-flex;
    color: var(--muted);
    transition: transform 0.2s;
  }
  .chev.open {
    transform: rotate(180deg);
  }
  .adv-body {
    padding: 0 20px 20px;
  }
  .divider {
    height: 1px;
    background: var(--border);
    margin: 4px 0;
  }
  h3 {
    font-size: 0.95rem;
    margin-bottom: -4px;
  }
  textarea.plain {
    font-family: var(--font);
    font-size: 0.92rem;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }
  .help code {
    color: var(--text-2);
  }

  @media (max-width: 900px) and (min-width: 641px) {
    .types {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (max-width: 640px) {
    /* Dar ekranda tek sütun, yatay kart: 5 tip için 2 sütunlu ızgarada
       tek kalan kart yerine düzenli bir liste. */
    .types {
      grid-template-columns: minmax(0, 1fr);
      gap: 8px;
    }
    .type {
      padding: 9px 12px;
    }
    .grid-host,
    .grid-int,
    .channels,
    .locs {
      grid-template-columns: minmax(0, 1fr);
    }
    .types {
      gap: 6px;
    }
    .tdesc {
      display: block;
      white-space: nowrap;
    }
    .tsearch {
      flex: 1 1 100%;
    }
    .mtags li {
      grid-template-columns: minmax(0, 1fr) auto;
    }
    .mt-chip {
      grid-column: 1 / -1;
    }
    .mt-val {
      grid-column: 1;
    }
    .tag-add select {
      flex: 1 1 100%;
      min-width: 0;
    }
    .adv-toggle {
      padding: 16px;
    }
    .adv-body {
      padding: 0 16px 16px;
    }
    .actions > :global(*) {
      flex: 1;
    }
  }
  .kept-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 10px;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
  }
</style>
