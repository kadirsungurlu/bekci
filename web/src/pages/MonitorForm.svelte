<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
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
  import { navigate } from '../lib/router.svelte';
  import { toast } from '../lib/ui.svelte';
  import { collator, fmtInterval, lower } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import { NOTIFY_LABELS } from '../lib/notifyTypes';
  import {
    CATEGORY_LABELS,
    CATEGORY_ORDER,
    GROUP_MODES,
    HTTP_EXTRA_FIELDS,
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

  let { id }: { id?: number } = $props();
  // svelte-ignore state_referenced_locally
  const isEdit = id !== undefined;

  // Tip seçici: kategorilere ayrılmış, aranabilir kartlar. Düzenlemede (ve dar
  // ekranda seçim yapıldıktan sonra) yalnızca seçili tip gösterilir.
  let typeQuery = $state('');
  // svelte-ignore state_referenced_locally
  let pickerOpen = $state(!isEdit);
  const typeGroups = $derived.by(() => {
    const q = lower(typeQuery.trim());
    const match = (t: (typeof MONITOR_TYPES)[number]) =>
      !q || lower(`${t.label} ${t.badge} ${t.key} ${t.desc} ${t.keywords ?? ''} ${CATEGORY_LABELS[t.category]}`).includes(q);
    return CATEGORY_ORDER.map((c) => ({ key: c, label: CATEGORY_LABELS[c], types: MONITOR_TYPES.filter((t) => t.category === c && match(t)) })).filter(
      (g) => g.types.length,
    );
  });

  const PRESETS = [30, 60, 120, 300, 600, 900, 1800, 3600, 86400];
  const METHODS = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'];
  const RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'SOA', 'SRV', 'CAA', 'PTR'];
  const JSON_OPS: { v: string; l: string }[] = [
    { v: '==', l: 'eşittir (==)' },
    { v: '!=', l: 'eşit değildir (!=)' },
    { v: 'contains', l: 'içerir' },
    { v: '>', l: 'büyüktür (>)' },
    { v: '>=', l: 'büyük veya eşit (>=)' },
    { v: '<', l: 'küçüktür (<)' },
    { v: '<=', l: 'küçük veya eşit (<=)' },
    { v: 'exists', l: 'mevcut (değer fark etmez)' },
  ];

  let loading = $state(isEdit);
  let loadError = $state('');
  let saving = $state(false);
  let error = $state('');
  let errorEl: HTMLDivElement | undefined = $state();
  let showAdvanced = $state(false);

  // Genel
  let type = $state<MonitorType>('http');
  let name = $state('');
  let description = $state('');
  let intervalPreset = $state('60');
  let customInterval = $state<number | null>(60);
  let retryInterval = $state<number | null>(null);
  let maxRetries = $state<number | null>(0);
  let timeout = $state<number | null>(30);
  let resendEvery = $state<number | null>(0);
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
  let groupMode = $state<'any_down' | 'all_down'>('any_down');

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
  let origLoc: LocationSetup = { include_local: true, probe_ids: [], down_when: 'any' };
  const remoteOk = $derived(isRemoteCapable(type));
  const DOWN_WHEN: { v: DownWhen; l: string }[] = [
    { v: 'any', l: 'Herhangi bir konum çalışmıyorsa' },
    { v: 'majority', l: 'Konumların çoğunluğu çalışmıyorsa' },
    { v: 'all', l: 'Tüm konumlar çalışmıyorsa' },
  ];

  const interval = $derived(intervalPreset === 'custom' ? (customInterval ?? 0) : Number(intervalPreset));

  const str = (c: Record<string, unknown>, k: string, d = '') => (typeof c[k] === 'string' ? (c[k] as string) : d);
  const num = (c: Record<string, unknown>, k: string, d: number) => (typeof c[k] === 'number' ? (c[k] as number) : d);
  const bool = (c: Record<string, unknown>, k: string, d: boolean) => (typeof c[k] === 'boolean' ? (c[k] as boolean) : d);

  function fill(m: MonitorView) {
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
    resendEvery = m.resend_every;
    upsideDown = m.upside_down;
    notifIds = [...m.notification_ids];
    mtags = (m.tags ?? []).map((t) => ({ id: t.id, value: t.value }));
    origTags = mtags.map((t) => ({ ...t }));
    if (m.locations) {
      origLoc = { include_local: m.locations.include_local, probe_ids: [...(m.locations.probe_ids ?? [])], down_when: m.locations.down_when || 'any' };
      locLocal = origLoc.include_local;
      locProbeIds = [...origLoc.probe_ids];
      locDownWhen = origLoc.down_when;
    }

    const c = m.config ?? {};
    origType = m.type;
    origConfig = c;
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
        groupMode = c.mode === 'all_down' ? 'all_down' : 'any_down';
        break;
      default: {
        const f = typeDef(m.type)?.fields;
        if (f) extra = fieldValues(f, c);
      }
    }
  }

  function pickType(t: MonitorType) {
    if (t !== type) {
      const prevUrl = type === 'http' ? url.trim() : '';
      const prevHost = ['tcp', 'ping', 'dns'].includes(type) ? host.trim() : '';
      type = t;
      const f = typeDef(t)?.fields;
      if (f) {
        if (t === origType) extra = fieldValues(f, origConfig);
        else {
          extra = fieldDefaults(f);
          // Önceki tipte girilen adres/sunucu yeni tipe taşınır.
          if (prevUrl && t === 'browser') extra.url = prevUrl;
          if (prevHost && f.some((x) => x.key === 'host')) extra.host = prevHost;
        }
      }
    }
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
        loadError = e instanceof ApiError && e.status === 404 ? 'Monitör bulunamadı.' : errorMessage(e);
      } finally {
        loading = false;
      }
    }
    await chP;
  });

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

  const CODE_RE = /^(\d{3}|\d{3}-\d{3}|[1-5]xx)$/i;

  function parseCodes(): string[] {
    return acceptedCodes
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
  }

  const inRange = (v: number | null, lo: number, hi: number) =>
    v !== null && Number.isInteger(v) && v >= lo && v <= hi;

  /** Hata metni ve hatanın gelişmiş ayarlarda olup olmadığı. */
  function validate(): { msg: string; advanced?: boolean } | null {
    if (!name.trim()) return { msg: 'Monitör adı gerekli.' };
    if (name.trim().length > 100) return { msg: 'Ad en fazla 100 karakter olabilir.' };
    if (!inRange(interval, 20, 86400)) return { msg: 'Kontrol aralığı 20 saniye ile 24 saat (86400 sn) arasında olmalı.' };
    switch (type) {
      case 'http': {
        if (!/^https?:\/\/[^\s/]+/i.test(url.trim())) return { msg: 'Geçerli bir http:// veya https:// adresi girin.' };
        const codes = parseCodes();
        const bad = codes.find((c) => !CODE_RE.test(c));
        if (bad) return { msg: `Geçersiz durum kodu: “${bad}”. Örnek biçimler: 200, 200-299, 2xx`, advanced: true };
        if (!inRange(maxRedirects, 0, 30)) return { msg: 'Yönlendirme sayısı 0-30 arasında olmalı.', advanced: true };
        if (contentMode === 'keyword' && !keyword) return { msg: 'Aranacak kelimeyi girin.', advanced: true };
        if (contentMode === 'json' && !jsonPath.trim()) return { msg: 'JSON yolunu girin (ör. data.status).', advanced: true };
        if (method === 'HEAD' && contentMode !== 'none')
          return { msg: 'HEAD isteği gövde döndürmez; kelime/JSON kontrolü için GET kullanın.', advanced: true };
        const xe = fieldError(HTTP_EXTRA_FIELDS, httpExtra);
        if (xe) return { msg: xe.msg, advanced: true };
        const x = httpExtra;
        if (x.proxy_url.trim() && x.proxy_pass && !x.proxy_user.trim())
          return { msg: 'Proxy şifresi için kullanıcı adı da gerekli.', advanced: true };
        if (!!x.tls_cert.trim() !== !!x.tls_key.trim())
          return { msg: 'İstemci sertifikası ve özel anahtarı birlikte girilmeli.', advanced: true };
        if (x.oauth_token_url.trim()) {
          if (!x.oauth_client_id.trim() || !x.oauth_client_secret)
            return { msg: 'OAuth2 için istemci kimliği (Client ID) ve istemci sırrı (Client Secret) gerekli.', advanced: true };
          if (basicUser || basicPass) return { msg: 'Basic auth ile OAuth2 birlikte kullanılamaz; birini boşaltın.', advanced: true };
        }
        break;
      }
      case 'tcp':
        if (!host.trim()) return { msg: 'Sunucu adresi gerekli.' };
        if (!inRange(port, 1, 65535)) return { msg: 'Port 1-65535 arasında olmalı.' };
        break;
      case 'ping':
        if (!host.trim()) return { msg: 'Sunucu adresi gerekli.' };
        if (!inRange(pingCount, 1, 10)) return { msg: 'Ping sayısı 1-10 arasında olmalı.' };
        break;
      case 'dns':
        if (!host.trim()) return { msg: 'Sorgulanacak alan adı gerekli.' };
        if (!inRange(dnsPort, 1, 65535)) return { msg: 'DNS sunucu portu 1-65535 arasında olmalı.' };
        break;
      case 'group':
        if (groupIds.length === 0) return { msg: 'En az bir alt monitör seçin.' };
        if (isEdit && groupIds.includes(id!)) return { msg: 'Grup kendisini alt monitör olarak içeremez.' };
        break;
      default:
        if (genericFields.length) {
          const fe = fieldError(genericFields, extra);
          if (fe) return fe;
        }
    }
    if (showLocations && !locLocal && locProbeIds.length === 0) return { msg: 'Konumlar: en az bir konum seçin.' };
    if (retryInterval !== null && !inRange(retryInterval, 20, 86400))
      return { msg: 'Tekrar deneme aralığı 20 saniye ile 24 saat arasında olmalı.', advanced: true };
    if (!inRange(maxRetries, 0, 20)) return { msg: 'Tekrar deneme sayısı 0-20 arasında olmalı.', advanced: true };
    if (hasTimeout(type) && !inRange(timeout, 1, 300)) return { msg: 'Zaman aşımı 1-300 saniye arasında olmalı.', advanced: true };
    if (!inRange(resendEvery, 0, 10000)) return { msg: 'Hatırlatma sıklığı 0-10000 arasında olmalı.', advanced: true };
    if (description.trim().length > 500) return { msg: 'Açıklama en fazla 500 karakter olabilir.', advanced: true };
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
        return { monitor_ids: groupIds, mode: groupMode };
      default:
        return genericFields.length ? fieldConfig(genericFields, extra) : {};
    }
  }

  async function showError(msg: string, advanced = false) {
    error = msg;
    if (advanced) showAdvanced = true;
    await tick();
    errorEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const v = validate();
    if (v) {
      showError(v.msg, v.advanced);
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
        problems.push(`Etiketler kaydedilemedi: ${errorMessage(err)}`);
      }
    }
    const loc = locationTarget();
    if (loc) {
      try {
        const l = await api.setMonitorLocations(res.id, loc);
        res = { ...res, locations: { include_local: l.include_local, probe_ids: l.probe_ids, down_when: l.down_when } };
      } catch (err) {
        problems.push(`Konumlar kaydedilemedi: ${errorMessage(err)}`);
      }
    }
    live.upsert(res);
    saving = false;
    if (problems.length) {
      toast.error(`Monitör kaydedildi, ancak: ${problems.join(' ')}`);
      // Yeni monitör artık var: tekrar göndermek kopya oluşturmasın diye düzenleme sayfasına geç.
      navigate(`/monitors/${res.id}/edit`, true);
      return;
    }
    toast.success(isEdit ? 'Değişiklikler kaydedildi' : `“${res.name}” eklendi`);
    navigate(`/monitors/${res.id}`);
  }

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

  function tagCreated(t: Tag) {
    allTags = [...allTags, t];
    addTag(String(t.id));
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
          }
        : { include_local: true, probe_ids: [], down_when: 'any' };
    const same =
      target.include_local === origLoc.include_local &&
      target.down_when === origLoc.down_when &&
      target.probe_ids.join(',') === [...origLoc.probe_ids].sort((a, b) => a - b).join(',');
    return same ? null : target;
  }
  const locCount = $derived((locLocal ? 1 : 0) + locProbeIds.length);

  const cancelHref = $derived(isEdit ? `#/monitors/${id}` : '#/');
</script>

<a class="back" href={cancelHref}><Icon name="chevron-left" size={16} /> {isEdit ? 'Monitöre dön' : 'Monitörler'}</a>
<div class="page-head">
  <h1>{isEdit ? 'Monitörü düzenle' : 'Yeni monitör'}<span class="dot">.</span></h1>
</div>

{#if loading}
  <div class="skeleton" style="height:420px;max-width:820px"></div>
{:else if loadError}
  <div class="card empty">
    <h3>Monitör yüklenemedi</h3>
    <p>{loadError}</p>
    <a class="btn primary" href="#/">Monitörlere dön</a>
  </div>
{:else}
  <form class="form" onsubmit={submit} novalidate>
    <section class="card">
      <div class="tp-head">
        <h2 class="card-title">Monitör tipi</h2>
        {#if pickerOpen}
          <div class="tsearch">
            <span class="s-ic"><Icon name="search" size={15} /></span>
            <input class="input" type="search" placeholder="Tip ara (ör. redis, ssl)" bind:value={typeQuery} aria-label="Monitör tipi ara" />
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
          <button type="button" class="btn sm" onclick={() => (pickerOpen = true)}>Değiştir</button>
        </div>
      {:else}
        {#if isEdit}
          <p class="help tp-warn">Tipi değiştirmek monitörün hedefini değiştirir; açık olay kapatılır.</p>
        {/if}
        {#each typeGroups as g (g.key)}
          <h3 class="tgroup">{g.label}</h3>
          <div class="types" role="radiogroup" aria-label={g.label}>
            {#each g.types as t (t.key)}
              <button
                type="button"
                role="radio"
                aria-checked={type === t.key}
                class="type"
                class:active={type === t.key}
                onclick={() => pickType(t.key)}
              >
                <span class="ticon"><Icon name={t.icon} size={20} /></span>
                <span class="tlabel">{t.label}</span>
                <span class="tdesc">{t.desc}</span>
              </button>
            {/each}
          </div>
        {:else}
          <p class="muted small nomargin">“{typeQuery}” ile eşleşen tip yok. <button type="button" class="linkbtn" onclick={() => (typeQuery = '')}>Aramayı temizle</button></p>
        {/each}
        {#if isEdit}
          <button type="button" class="linkbtn tp-close" onclick={() => (pickerOpen = false)}>Listeyi kapat</button>
        {/if}
      {/if}
    </section>

    <section class="card stack">
      <div class="field">
        <label for="name">Ad</label>
        <input id="name" class="input" bind:value={name} maxlength="100" placeholder="Ör. Şirket web sitesi" />
      </div>

      {#if type === 'http'}
        <div class="field">
          <label for="url">Adres (URL)</label>
          <input
            id="url"
            class="input"
            type="url"
            inputmode="url"
            autocapitalize="none"
            spellcheck="false"
            bind:value={url}
            onblur={urlBlur}
            placeholder="https://ornek.com"
          />
        </div>
      {:else if type === 'tcp'}
        <div class="grid-host">
          <div class="field">
            <label for="host">Sunucu</label>
            <input id="host" class="input" autocapitalize="none" spellcheck="false" bind:value={host} onblur={hostBlur} placeholder="ornek.com veya 192.168.1.10" />
          </div>
          <div class="field">
            <label for="port">Port</label>
            <input id="port" class="input" type="number" min="1" max="65535" bind:value={port} placeholder="443" />
          </div>
        </div>
      {:else if type === 'ping'}
        <div class="grid-host">
          <div class="field">
            <label for="host">Sunucu</label>
            <input id="host" class="input" autocapitalize="none" spellcheck="false" bind:value={host} onblur={hostBlur} placeholder="ornek.com veya 192.168.1.10" />
          </div>
          <div class="field">
            <label for="count">Paket sayısı</label>
            <input id="count" class="input" type="number" min="1" max="10" bind:value={pingCount} />
          </div>
        </div>
      {:else if type === 'dns'}
        <div class="field">
          <label for="host">Alan adı</label>
          <input id="host" class="input" autocapitalize="none" spellcheck="false" bind:value={host} onblur={hostBlur} placeholder="ornek.com" />
        </div>
        <div class="grid-3">
          <div class="field">
            <label for="rt">Kayıt tipi</label>
            <select id="rt" class="input" bind:value={recordType}>
              {#each RECORD_TYPES as r (r)}<option value={r}>{r}</option>{/each}
            </select>
          </div>
          <div class="field">
            <label for="dsrv">DNS sunucusu</label>
            <input id="dsrv" class="input" autocapitalize="none" spellcheck="false" bind:value={dnsServer} placeholder="1.1.1.1" />
          </div>
          <div class="field">
            <label for="dport">Port</label>
            <input id="dport" class="input" type="number" min="1" max="65535" bind:value={dnsPort} />
          </div>
        </div>
        <div class="field">
          <label for="dexp">Beklenen değer <span class="muted">(isteğe bağlı)</span></label>
          <input id="dexp" class="input" bind:value={dnsExpected} placeholder="Ör. 93.184.216.34" />
          <span class="help">Doluysa cevaplardan en az biri bu metni içermeli; aksi halde çalışmıyor sayılır.</span>
        </div>
      {:else if type === 'push'}
        <div class="alert info">
          Kaydettikten sonra bu monitöre özel bir <b>push adresi</b> oluşturulur. Zamanlanmış işiniz her çalıştığında bu
          adrese istek gönderir; belirlediğiniz süre içinde istek gelmezse size haber veririz.
        </div>
      {:else if type === 'group'}
        <div class="field">
          <span class="label" id="grp-l">Alt monitörler</span>
          <MonitorPicker bind:selected={groupIds} exclude={isEdit ? [id!] : []} label="Alt monitörler" id="grp" />
        </div>
        <div class="field">
          <span class="label" id="grp-mode">Mod</span>
          <div class="seg" role="radiogroup" aria-labelledby="grp-mode">
            {#each GROUP_MODES as gm (gm.v)}
              <button type="button" role="radio" aria-checked={groupMode === gm.v} class:active={groupMode === gm.v} onclick={() => (groupMode = gm.v)}>
                {gm.l}
              </button>
            {/each}
          </div>
          <span class="help">Durdurulmuş ve bakımdaki alt monitörler hesaba katılmaz.</span>
        </div>
      {:else if genericFields.length}
        {#if def?.about}<p class="about text-2 small">{def.about}</p>{/if}
        {#if def?.note}<div class="alert warning">{def.note}</div>{/if}
        <ConfigFields fields={genericFields} bind:values={extra} idPrefix="mt" part="basic" onsuggest={suggestName} />
      {/if}

      <div class="grid-int">
        <div class="field">
          <label for="int">{type === 'push' ? 'Beklenen push aralığı' : 'Kontrol aralığı'}</label>
          <select id="int" class="input" bind:value={intervalPreset}>
            {#each PRESETS as p (p)}<option value={String(p)}>{fmtInterval(p)}</option>{/each}
            <option value="custom">Özel</option>
          </select>
        </div>
        {#if intervalPreset === 'custom'}
          <div class="field">
            <label for="cint">Saniye</label>
            <input id="cint" class="input" type="number" min="20" max="86400" bind:value={customInterval} />
          </div>
        {/if}
      </div>
      <span class="help int-help">
        {#if type === 'push'}
          Bu süre içinde push isteği gelmezse monitör çalışmıyor sayılır.
        {:else}
          Hedef bu sıklıkla kontrol edilir. En az 20 saniye, en fazla 24 saat.
        {/if}
      </span>
    </section>

    <section class="card">
      <h2 class="card-title">Bildirimler</h2>
      {#if !channelsLoaded}
        <div class="skeleton" style="height:40px"></div>
      {:else if channels.length === 0}
        <p class="muted small nomargin">
          Henüz bildirim kanalı yok. <a href="#/notifications">Bildirimler</a> sayfasından WhatsApp, Telegram, e-posta gibi
          bir kanal ekleyebilirsiniz.
        </p>
      {:else}
        <p class="help nomargin sp">Bu monitör çalışmadığında ve düzeldiğinde seçili kanallara bildirim gönderilir.</p>
        <div class="channels">
          {#each channels as ch (ch.id)}
            <label class="check ch">
              <input type="checkbox" value={ch.id} bind:group={notifIds} />
              <span>
                {ch.name}
                <small>{NOTIFY_LABELS[ch.type] ?? ch.type}{ch.active ? '' : ' · pasif'}{ch.is_default ? ' · varsayılan' : ''}</small>
              </span>
            </label>
          {/each}
        </div>
      {/if}
    </section>

    <section class="card">
      <h2 class="card-title"><Icon name="tag" size={17} /> Etiketler</h2>
      {#if !tagsLoaded}
        <div class="skeleton" style="height:40px"></div>
      {:else}
        {#if mtags.length}
          <ul class="mtags">
            {#each mtags as mt (mt.id)}
              {@const t = tagById.get(mt.id)}
              <li>
                <span class="mt-chip"><TagChip name={t?.name ?? `#${mt.id}`} color={t?.color ?? ''} /></span>
                <input
                  id="tv-{mt.id}"
                  class="input mt-val"
                  maxlength="100"
                  bind:value={mt.value}
                  placeholder="ör. canlı"
                  aria-label="{t?.name ?? 'Etiket'} değeri (isteğe bağlı)"
                />
                <button
                  type="button"
                  class="btn ghost icon sm"
                  aria-label="“{t?.name ?? ''}” etiketini kaldır"
                  onclick={() => (mtags = mtags.filter((x) => x.id !== mt.id))}><Icon name="x" size={15} /></button
                >
              </li>
            {/each}
          </ul>
          <p class="help sp">Değer isteğe bağlıdır; listede “ad: değer” olarak görünür.</p>
        {/if}
        <div class="tag-add">
          {#if addableTags.length}
            <select class="input" bind:value={tagPick} onchange={() => addTag(tagPick)} aria-label="Etiket ekle">
              <option value="">Etiket ekle…</option>
              {#each addableTags as t (t.id)}<option value={String(t.id)}>{t.name}</option>{/each}
            </select>
          {:else if allTags.length === 0}
            <span class="muted small">Henüz etiket yok.</span>
          {/if}
          <button
            type="button"
            class="btn sm"
            onclick={() => {
              tagDialogKey++;
              tagDialogOpen = true;
            }}><Icon name="plus" size={14} /> Yeni etiket</button
          >
        </div>
      {/if}
    </section>

    {#if remoteOk}
      <section class="card">
        <h2 class="card-title"><Icon name="map-pin" size={17} /> Konumlar</h2>
        {#if !probesLoaded}
          <div class="skeleton" style="height:40px"></div>
        {:else if probes.length === 0}
          <p class="muted small nomargin">
            Bu monitör şu an yalnızca bu sunucudan kontrol ediliyor.
            {#if session.isAdmin}
              Farklı şehir veya ağlardan da kontrol etmek için <a href="#/settings/probes">Ayarlar → Kontrol noktaları</a> bölümünden bir kontrol
              noktası ekleyin.
            {:else}
              Farklı konumlardan kontrol için yöneticinizin <b>Ayarlar → Kontrol noktaları</b> bölümünden kontrol noktası eklemesi gerekir.
            {/if}
          </p>
        {:else}
          <p class="help nomargin sp">Monitör seçili her konumdan ayrı ayrı kontrol edilir.</p>
          <div class="locs">
            <label class="check loc">
              <input type="checkbox" bind:checked={locLocal} />
              <span class="loc-t">
                <span class="odot up" aria-hidden="true"></span>
                <span>Ana sunucu<small>Bu uygulamanın çalıştığı sunucu</small></span>
              </span>
            </label>
            {#each probes as p (p.id)}
              <label class="check loc" class:off={!p.active}>
                <input type="checkbox" value={p.id} bind:group={locProbeIds} />
                <span class="loc-t">
                  <span class="odot {!p.active ? 'paused' : p.online ? 'up' : 'down'}" aria-hidden="true"></span>
                  <span>{p.name}<small>{!p.active ? 'Devre dışı' : p.online ? 'Çevrimiçi' : 'Çevrimdışı'}</small></span>
                </span>
              </label>
            {/each}
          </div>
          <div class="field dw">
            <label for="dw">Kesinti kuralı</label>
            <select id="dw" class="input" bind:value={locDownWhen} disabled={locCount < 2}>
              {#each DOWN_WHEN as d (d.v)}<option value={d.v}>{d.l}</option>{/each}
            </select>
            <span class="help">
              Bir konum, tekrar deneme hakkını kullandıktan sonra da başarısızsa çalışmıyor sayılır. 3 kontrol aralığı boyunca sonuç
              göndermeyen konum hesaba katılmaz.
            </span>
          </div>
        {/if}
      </section>
    {/if}

    <section class="card adv">
      <button type="button" class="adv-toggle" aria-expanded={showAdvanced} onclick={() => (showAdvanced = !showAdvanced)}>
        <span>Gelişmiş ayarlar</span>
        <span class="chev" class:open={showAdvanced}><Icon name="chevron-down" /></span>
      </button>

      {#if showAdvanced}
        <div class="adv-body stack">
          <div class="grid-2">
            <div class="field">
              <label for="mr">Tekrar deneme sayısı</label>
              <input id="mr" class="input" type="number" min="0" max="20" bind:value={maxRetries} />
              <span class="help">Çalışmıyor saymadan önce kaç kez daha denensin. 0 = ilk hatada bildir.</span>
            </div>
            <div class="field">
              <label for="ri">Tekrar deneme aralığı (sn)</label>
              <input id="ri" class="input" type="number" min="20" max="86400" bind:value={retryInterval} placeholder="Kontrol aralığıyla aynı ({interval || 60})" />
              <span class="help">Hata sonrası tekrar denemeler arasındaki süre.</span>
            </div>
            {#if hasTimeout(type)}
              <div class="field">
                <label for="to">Zaman aşımı (sn)</label>
                <input id="to" class="input" type="number" min="1" max="300" bind:value={timeout} />
                <span class="help">Bu sürede yanıt gelmezse kontrol başarısız sayılır.</span>
              </div>
            {/if}
            <div class="field">
              <label for="re">Hatırlatma sıklığı</label>
              <input id="re" class="input" type="number" min="0" max="10000" bind:value={resendEvery} />
              <span class="help">Kesinti sürerken her N başarısız kontrolde bir tekrar bildir. 0 = kapalı.</span>
            </div>
          </div>

          {#if hasUpsideDown(type)}
            <label class="check">
              <input type="checkbox" bind:checked={upsideDown} />
              <span>Ters mod<small>Hedef erişilebilir olduğunda “çalışmıyor”, erişilemediğinde “çalışıyor” sayılır.</small></span>
            </label>
          {/if}

          {#if hasAdvancedFields}
            <div class="divider"></div>
            <h3>{def?.label} ayarları</h3>
            <ConfigFields fields={genericFields} bind:values={extra} idPrefix="mta" part="advanced" onsuggest={suggestName} />
          {/if}

          {#if type === 'http'}
            <div class="divider"></div>
            <h3>HTTP isteği</h3>
            <div class="grid-2">
              <div class="field">
                <label for="method">Metot</label>
                <select id="method" class="input" bind:value={method}>
                  {#each METHODS as m (m)}<option value={m}>{m}</option>{/each}
                </select>
              </div>
              <div class="field">
                <label for="codes">Kabul edilen durum kodları</label>
                <input id="codes" class="input" bind:value={acceptedCodes} placeholder="200-399" />
                <span class="help">Virgülle ayırın. Ör. 200, 201-204, 3xx</span>
              </div>
            </div>
            <div class="field">
              <label for="hdr">Başlıklar (headers)</label>
              {#if headers === MASK}
                <!-- Başlıklar gizli alan: sunucu maskeli döndürür; değiştirilmezse kayıtlı değer korunur. -->
                <div class="kept-row">
                  <span class="help">Kayıtlı başlıklar korunuyor (gizli).</span>
                  <button type="button" id="hdr" class="btn sm" onclick={() => (headers = '')}>Değiştir</button>
                </div>
              {:else}
                <textarea id="hdr" class="input" rows="3" bind:value={headers} placeholder={'Authorization: Bearer abc123\nX-Ozel-Baslik: değer'}></textarea>
                <span class="help">Her satıra bir başlık: <code>Ad: değer</code>. Kayıttan sonra gizli tutulur.</span>
              {/if}
            </div>
            <div class="field">
              <label for="body">İstek gövdesi (body)</label>
              <textarea id="body" class="input" rows="3" bind:value={body} placeholder={'{"ornek": true}'}></textarea>
              <span class="help">Genellikle POST/PUT için. Geçerli JSON ise Content-Type otomatik application/json olur.</span>
            </div>
            <div class="grid-2">
              <div class="field">
                <label for="bu">Basic auth kullanıcı adı</label>
                <input id="bu" class="input" autocomplete="off" bind:value={basicUser} />
              </div>
              <div class="field">
                <label for="bp">Basic auth şifresi</label>
                <input id="bp" class="input" type="password" autocomplete="new-password" bind:value={basicPass} />
                {#if basicPass === MASK}<span class="help">Kayıtlı şifre korunur; değiştirmek için yenisini yazın.</span>{/if}
              </div>
              <div class="field">
                <label for="rd">En fazla yönlendirme</label>
                <input id="rd" class="input" type="number" min="0" max="30" bind:value={maxRedirects} />
                <span class="help">0 = yönlendirmeleri takip etme.</span>
              </div>
            </div>
            <label class="check">
              <input type="checkbox" bind:checked={ignoreTls} />
              <span>TLS/SSL hatalarını yok say<small>Kendinden imzalı veya süresi dolmuş sertifikada da bağlan.</small></span>
            </label>
            <label class="check">
              <input type="checkbox" bind:checked={certExpiry} />
              <span>SSL bitiş uyarısı<small>Sertifikanın süresi dolmak üzereyken bildirim gönder.</small></span>
            </label>

            <div class="divider"></div>
            <h3>İçerik kontrolü</h3>
            <div class="seg" role="radiogroup" aria-label="İçerik kontrolü">
              <button type="button" role="radio" aria-checked={contentMode === 'none'} class:active={contentMode === 'none'} onclick={() => (contentMode = 'none')}>Yok</button>
              <button type="button" role="radio" aria-checked={contentMode === 'keyword'} class:active={contentMode === 'keyword'} onclick={() => (contentMode = 'keyword')}>Kelime</button>
              <button type="button" role="radio" aria-checked={contentMode === 'json'} class:active={contentMode === 'json'} onclick={() => (contentMode = 'json')}>JSON</button>
            </div>
            {#if contentMode === 'keyword'}
              <div class="field">
                <label for="kw">Aranacak kelime</label>
                <input id="kw" class="input" bind:value={keyword} placeholder="Ör. Hoş geldiniz" />
                <span class="help">Sayfa gövdesinde bu metin yoksa monitör çalışmıyor sayılır.</span>
              </div>
              <label class="check">
                <input type="checkbox" bind:checked={keywordInvert} />
                <span>Tersine çevir<small>Kelime sayfada VARSA çalışmıyor say (ör. “Hata”, “Bakımdayız”).</small></span>
              </label>
              <label class="check">
                <input type="checkbox" bind:checked={keywordCase} />
                <span>Büyük/küçük harfe duyarlı</span>
              </label>
            {:else if contentMode === 'json'}
              <div class="grid-3">
                <div class="field">
                  <label for="jp">JSON yolu</label>
                  <input id="jp" class="input mono" autocapitalize="none" spellcheck="false" bind:value={jsonPath} placeholder="data.status" />
                </div>
                <div class="field">
                  <label for="jo">Koşul</label>
                  <select id="jo" class="input" bind:value={jsonOp}>
                    {#each JSON_OPS as o (o.v)}<option value={o.v}>{o.l}</option>{/each}
                  </select>
                </div>
                {#if jsonOp !== 'exists'}
                  <div class="field">
                    <label for="je">Beklenen değer</label>
                    <input id="je" class="input" bind:value={jsonExpected} placeholder="ok" />
                  </div>
                {/if}
              </div>
              <span class="help">
                Yol örnekleri: <code>status</code>, <code>data.status</code>, <code>items.0.name</code>, <code>items.#</code> (dizi uzunluğu).
                Koşul sağlanmazsa monitör çalışmıyor sayılır.
              </span>
            {/if}

            <div class="divider"></div>
            <h3>Bağlantı ve kimlik doğrulama</h3>
            <ConfigFields fields={HTTP_EXTRA_FIELDS} bind:values={httpExtra} idPrefix="hx" />
          {/if}

          <div class="divider"></div>
          <div class="field">
            <label for="desc">Açıklama <span class="muted">(isteğe bağlı)</span></label>
            <textarea id="desc" class="input plain" rows="2" maxlength="500" bind:value={description} placeholder="Bu monitörle ilgili notlar"></textarea>
          </div>
        </div>
      {/if}
    </section>

    {#if error}
      <div class="alert error" role="alert" bind:this={errorEl}>{error}</div>
    {/if}

    <div class="actions">
      <a class="btn" href={cancelHref}>Vazgeç</a>
      <button class="btn primary" type="submit" disabled={saving}>
        {#if saving}<span class="spinner"></span>{/if}
        {isEdit ? 'Kaydet' : 'Monitörü ekle'}
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
