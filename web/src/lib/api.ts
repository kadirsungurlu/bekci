// Go sunucusunun REST API'si için tipler ve küçük bir fetch sarmalayıcısı.

import { i18n, t, type Locale } from './i18n';

export const MASK = '••••••';

export const STATUS_DOWN = 0;
export const STATUS_UP = 1;
export const STATUS_PENDING = 2;
export const STATUS_MAINTENANCE = 3;

/** Bilinen tipler; yeni tip eklenirken monitorTypes.ts'teki kayıt defterine de eklenir. */
export type MonitorType =
  | 'http'
  | 'tcp'
  | 'ping'
  | 'dns'
  | 'tlscert'
  | 'smtp'
  | 'websocket'
  | 'grpc'
  | 'mqtt'
  | 'snmp'
  | 'mysql'
  | 'postgres'
  | 'mssql'
  | 'redis'
  | 'mongodb'
  | 'docker'
  | 'push'
  | 'group';

export type Role = 'admin' | 'editor' | 'viewer';

export interface User {
  id: number;
  username: string;
  display_name: string;
  role: Role;
  must_change_password: boolean;
  all_monitors: boolean;
  two_factor_enabled: boolean;
  /** Sunucular ekranı açık mı (kısıtlı izleyicide kendisine sunucu atanmışsa). Eski sunucuda gelmez. */
  servers?: boolean;
  /** Arayüz dili tercihi; "" = tarayıcı dili. Eski sunucuda gelmez. */
  lang?: '' | Locale;
}

/** Kullanıcılar sayfasındaki kayıt (yalnızca yönetici). */
export interface UserRecord extends User {
  disabled: boolean;
  monitor_ids: number[] | null;
  server_ids?: number[] | null;
  last_login_at: number;
  created_at: number;
}

export interface UserInput {
  display_name: string;
  role: Role;
  disabled: boolean;
  all_monitors: boolean;
  monitor_ids: number[];
  server_ids: number[];
}

export interface UserCreateInput extends UserInput {
  username: string;
  password: string;
}

export interface AuditEntry {
  id: number;
  time: number;
  user_id: number;
  username: string;
  action: string;
  target_type: string;
  target_id: number;
  target_name: string;
  detail: string;
  ip: string;
}

export type LoginResult = { user: User; two_factor_required?: undefined } | { two_factor_required: true; challenge: string };

export interface TwoFactorStatus {
  enabled: boolean;
  recovery_codes_left: number;
}

export interface TwoFactorSetup {
  secret: string;
  otpauth_url: string;
  qr_png?: string;
}

export type ApiKeyStatus = 'active' | 'expired' | 'revoked';

export interface ApiKey {
  id: number;
  user_id: number;
  username: string;
  name: string;
  prefix: string;
  role: Role;
  created_at: number;
  last_used_at: number;
  expires_at: number;
  revoked_at: number;
  status: ApiKeyStatus;
}

export interface AuthState {
  setup_needed: boolean;
  user: User | null;
  version: string;
}

export interface Bucket {
  t: number;
  up: number;
  down: number;
  ping: number;
  ping_min: number;
  ping_max: number;
}

export interface MonitorView {
  id: number;
  name: string;
  type: MonitorType;
  description: string;
  active: boolean;
  interval: number;
  retry_interval: number;
  max_retries: number;
  timeout: number;
  resend_every: number;
  upside_down: boolean;
  config: Record<string, unknown>;
  push_token?: string;
  /** Yavaş yanıt eşiği (ms; 0 = kapalı) ve ortalama penceresi (kontrol sayısı). Eski sunucuda yok. */
  slow_ms?: number;
  slow_checks?: number;
  /** Şu an yavaş (son kontrollerin ortalaması eşiği aşıyor). */
  slow?: boolean;
  status: number;
  last_check_at: number;
  last_change_at: number;
  last_ping_ms: number;
  last_message: string;
  cert_expires_at: number;
  cert_issuer: string;
  created_at: number;
  updated_at: number;
  target: string;
  notification_ids: number[];
  uptime_24h: number | null;
  bars: Bucket[];
  in_maintenance?: boolean;
  tags?: MonitorTag[];
  locations?: LocationSetup;
  /** Süren olayın kimliği ("Olayı gör"); yoksa null. Eski sunucularda alan yok. */
  open_incident_id?: number | null;
  /**
   * Süren konum kesintisinin kimliği: bazı kontrol konumları çalışmıyor, monitör
   * genel olarak çalışıyor (durum ve uptime etkilenmez). Yoksa null.
   */
  open_partial_incident_id?: number | null;
  /** Süren yavaş yanıt olayının kimliği; yoksa null. Eski sunucuda alan yok. */
  open_degraded_incident_id?: number | null;
  /**
   * Son kontrollerin yanıt süreleri (ms, eskiden yeniye; en çok 30). PING_DOWN:
   * başarısız kontrol, PING_NONE: ölçüm yok. Eski sunucularda alan yok.
   */
  pings?: number[];
  uptime_7d?: number | null;
  uptime_30d?: number | null;
  /** Çok konumlu, çalışan monitörde konumların canlı durumu. */
  loc_states?: LocationDot[];
}

/** Yanıt süresi listesindeki özel değerler (MonitorView.pings). */
export const PING_NONE = -1;
export const PING_DOWN = -2;

/** Listedeki konum noktası. status: up | down | retrying | unknown (| waiting …). */
export interface LocationDot {
  probe_id: number;
  name: string;
  status: string;
  ping_ms: number;
}

/** Monitör listesindeki toplu işlemler (POST /api/monitors/bulk). */
export type BulkAction =
  | { action: 'pause' | 'resume' | 'delete' }
  | { action: 'add_tag'; tag_id: number; value: string }
  | { action: 'remove_tag'; tag_id: number }
  | { action: 'add_notification' | 'remove_notification'; notification_id: number };

export interface BulkResult {
  /** Gerçekten değişen monitör sayısı (zaten durdurulmuş olanlar vb. sayılmaz). */
  changed: number;
  monitors: MonitorView[];
  deleted: number[];
}

// Etiketler ------------------------------------------------------------------------

export interface Tag {
  id: number;
  name: string;
  color: string;
  created_at: number;
  updated_at: number;
  monitor_count: number;
}

/** Monitöre bağlı etiket (id etiketin kimliğidir). */
export interface MonitorTag {
  id: number;
  name: string;
  color: string;
  value: string;
}

// Kontrol noktaları ------------------------------------------------------------------

export interface Probe {
  id: number;
  name: string;
  active: boolean;
  online: boolean;
  last_seen_at: number;
  // Yalnızca yönetici görünümünde:
  token_prefix?: string;
  created_at?: number;
  last_ip?: string;
  version?: string;
  monitor_count?: number;
  /** Sunucu metrikleri toplanıyor mu (yönetici görünümü; eski sunucularda yok). */
  metrics?: boolean;
  /** Ajan yalnızca ilk bağlandığı IP'den veri gönderebilir mi (yönetici görünümü). */
  ip_lock?: boolean;
  /** Sabitlenmiş IP; boşsa ajan henüz bağlanmadı (yönetici görünümü). */
  locked_ip?: string;
  /** Çevrimdışı kalınca / tekrar çevrimiçi olunca bildirim (yönetici görünümü; eski sunucuda yok). */
  notify_offline?: boolean;
  /** Çevrimdışı bildiriminin gideceği kanallar (yönetici görünümü). */
  notification_ids?: number[];
}

export interface ProbeSetup {
  probe: Probe;
  token: string;
  server_url: string;
  docker_command: string;
  /** Sunucu ajanı (host metrikleri) için Docker komutu; eski sunucularda yok. */
  docker_agent?: string;
  /** Doğrudan kurulum (systemd) betiği; eski sunucularda yok. */
  systemd?: string;
  /** Windows hizmeti kurulumu (Yönetici PowerShell); eski sunucularda yok. */
  windows?: string;
}

export type DownWhen = 'any' | 'majority' | 'all';

export interface LocationSetup {
  include_local: boolean;
  probe_ids: number[];
  down_when: DownWhen;
  /** Konum kesintisinde (monitör çalışırken bir konum düştüğünde) de bildirim. */
  notify_partial?: boolean;
}

/** waiting: konum yeni eklendi, ilk sonucu henüz gelmedi (süresi dolunca unknown). */
export type LocationState = 'up' | 'down' | 'retrying' | 'waiting' | 'unknown';

export interface LocationStatus {
  probe_id: number;
  name: string;
  status: LocationState;
  last_check_at: number;
  ping_ms: number;
  message: string;
}

export interface MonitorLocations extends LocationSetup {
  supported: boolean;
  locations: LocationStatus[];
}

export interface ProbeEvent {
  probe_id: number;
  name: string;
  online: boolean;
  last_seen_at: number;
}

// Yedekleme ve içe aktarma ---------------------------------------------------------------

export interface ImportCounts {
  monitors: number;
  notifications: number;
  tags: number;
  status_pages: number;
}

export type ImportKind = 'monitor' | 'notification' | 'tag' | 'status_page' | 'settings';
export type ImportResultKind = 'created' | 'existing' | 'skipped';

export interface ImportItem {
  kind: ImportKind;
  name: string;
  type?: string;
  result: ImportResultKind;
  id?: number;
  messages?: string[];
}

export interface ImportSummary {
  source: 'uptime-kadir' | 'uptime-kuma' | 'uptimerobot';
  mode: 'merge' | 'replace';
  dry_run: boolean;
  created: ImportCounts;
  existing: ImportCounts;
  skipped: ImportCounts;
  deleted?: ImportCounts;
  settings_applied: boolean;
  warnings: string[] | null;
  items: ImportItem[] | null;
}

export interface MonitorInput {
  name: string;
  type: MonitorType;
  description: string;
  interval: number;
  retry_interval: number;
  max_retries: number;
  timeout: number;
  resend_every: number;
  upside_down: boolean;
  config: Record<string, unknown>;
  notification_ids: number[] | null;
  /** Yavaş yanıt eşiği (ms; 0 = kapalı) ve penceresi (kontrol sayısı; 0 = varsayılan 3). */
  slow_ms?: number;
  slow_checks?: number;
}

export type UptimeKey = '24h' | '7d' | '30d' | '90d';

export interface MonitorDetail {
  monitor: MonitorView;
  uptime: Record<UptimeKey, number | null>;
  avg_ping_24h: number;
  open_incident_since: number;
  /** Açık olayın kimliği (0: yok; eski sunucuda alan gelmez). */
  open_incident_id?: number;
}

export interface Summary {
  total: number;
  up: number;
  down: number;
  pending: number;
  paused: number;
  maintenance?: number;
  uptime_24h: number | null;
  incidents_24h: number;
}

export interface RawPoint {
  t: number;
  s: number;
  p: number;
  m?: string;
}

export type SeriesRange = '24h' | '7d' | '30d' | '90d';

export type Series =
  | { range: SeriesRange; kind: 'raw'; points: RawPoint[] }
  | { range: SeriesRange; kind: 'hourly' | 'daily'; points: Bucket[] };

/**
 * Olay türü: monitor (kesinti), partial (kısmi kesinti: bazı konumlar çalışmıyor,
 * monitör çalışıyor; bildirim yok), server_offline / server_alert (sunucu takibi).
 * Eski sunucular türü göndermez (monitor sayılır).
 */
export type IncidentKind = 'monitor' | 'partial' | 'server_offline' | 'server_alert' | 'probe_offline' | 'degraded';
/** Olay listesi süzgeci (sunucu: sunucu ve kontrol noktası türleri birlikte). */
export type IncidentFilterKind = '' | 'monitor' | 'server' | 'partial' | 'degraded';

export interface Incident {
  id: number;
  kind?: IncidentKind;
  monitor_id: number;
  monitor_name: string;
  /** Yalnızca sunucu olaylarında. */
  server_id?: number;
  server_name?: string;
  started_at: number;
  resolved_at: number;
  cause: string;
  /** Türe özgü veri: kısmi → { locations: string[] }; sunucu → ServerIncidentData. */
  data?: Record<string, unknown> | null;
}

/** Sunucu olayının verisi (store.ServerIncidentData). */
export interface ServerIncidentData {
  metric: string;
  mount?: string;
  threshold?: number;
  minutes: number;
  value: number;
  peak: number;
  last: number;
  last_seen?: number;
}

/** Ajan olayı: sunucu (server_offline/server_alert) veya kontrol noktası (probe_offline); server_id dolu. */
export const isServerIncident = (k: IncidentKind | undefined) => k === 'server_offline' || k === 'server_alert' || k === 'probe_offline';
/** Kontrol noktası çevrimdışı olayı (ayrıntı ve bağlantılar kontrol noktaları sayfasına gider). */
export const isProbeIncident = (k: IncidentKind | undefined) => k === 'probe_offline';

/** Olay ayrıntıları (docs/PLAN.md §13). */
export type IncidentEventKind =
  | 'retry'
  | 'down'
  | 'change'
  | 'location'
  | 'reminder'
  | 'maint_start'
  | 'maint_end'
  | 'notify'
  | 'edited'
  | 'paused'
  | 'up'
  | 'limit'
  | 'escalated'
  | 'from_partial'
  | 'resumed';

export interface IncidentEvent {
  id: number;
  time: number;
  kind: IncidentEventKind;
  location: string;
  message: string;
  data: Record<string, unknown> | null;
}

export interface HttpHeader {
  name: string;
  value: string;
}

/** HTTP dışı tipin bağlantı denemesi (tek IP). */
export interface ConnAttempt {
  ip: string;
  /** ok | refused | timeout | reset | unreachable | dns_error | tls_error | other */
  result: string;
  elapsed_ms: number;
  error?: string;
}

/** HTTP dışı tipin başarısızlık tanısı (sunucu: check.Diag). Kodlar arayüzde çevrilir. */
export interface ConnDiag {
  target?: string;
  port?: number;
  resolved?: string[];
  resolve_ms?: number;
  resolve_error?: string;
  attempts?: ConnAttempt[];
  timeout_ms?: number;
  elapsed_ms?: number;
  phase?: string;
  error_class?: string;
  raw_error?: string;
  banner?: string;
  ping?: {
    target: string;
    sent: number;
    received: number;
    loss_pct: number;
    min_ms?: number;
    avg_ms?: number;
    max_ms?: number;
    /** unavailable (ICMP izni yok) | failed */
    error?: string;
    raw_error?: string;
  };
  dns?: {
    server: string;
    type: string;
    query: string;
    transport?: string;
    rcode?: string;
    answers?: string[];
    elapsed_ms?: number;
  };
}

/** Başarısız kontrolün ayrıntısı: HTTP isteği ve yanıtı ya da (diğer tiplerde) bağlantı tanısı (maskeli). */
export interface CheckDetail {
  /** Monitör tipi; eski HTTP kayıtlarında yok. */
  kind?: string;
  diag?: ConnDiag;
  method?: string;
  url?: string;
  request_headers?: HttpHeader[];
  status?: number;
  status_text?: string;
  proto?: string;
  final_url?: string;
  response_headers?: HttpHeader[];
  content_type?: string;
  body?: string;
  body_size?: number;
  body_truncated?: boolean;
  body_binary?: boolean;
  error?: string;
}

export interface IncidentCapture {
  time: number;
  location: string;
  detail: CheckDetail;
}

export interface IncidentLocation {
  probe_id: number;
  name: string;
  status: LocationState;
  message: string;
}

export interface IncidentDetail {
  incident: Incident;
  /** Sunucu olayında boş (id 0). */
  /** type/target olay anındaki değerler; changed: sonradan değiştirildi (güncel değerler current_*). */
  monitor: {
    id: number;
    name: string;
    type: string;
    target: string;
    active: boolean;
    status: number;
    changed?: boolean;
    current_type?: string;
    current_target?: string;
    /** Monitörde "konum kesintisinde de bildirim gönder" açık. */
    notify_partial?: boolean;
    /** Monitör şu an bakım penceresinde (yalnızca süren olayda). */
    in_maintenance?: boolean;
  };
  /** Yalnızca sunucu olayında. */
  server?: { id: number; name: string; hostname: string; active: boolean };
  location: string;
  locations: IncidentLocation[];
  events: IncidentEvent[];
  capture: IncidentCapture | null;
  /** Çok konumlu olayda çalışmayan her konumun kaydı (ilki = capture); eski sunucuda yok. */
  captures?: IncidentCapture[];
  details: boolean;
}

export type NotificationType =
  | 'whatsapp'
  | 'telegram'
  | 'email'
  | 'discord'
  | 'slack'
  | 'webhook'
  | 'ntfy'
  | 'gotify'
  | 'pushover'
  | 'teams'
  | 'googlechat'
  | 'mattermost'
  | 'rocketchat'
  | 'matrix'
  | 'signal'
  | 'pagerduty'
  | 'opsgenie'
  | 'homeassistant'
  | 'netgsm'
  | 'twilio'
  | 'pushbullet'
  | 'bark'
  | 'line'
  | 'apprise';

/** Bildirim türleri (kanal süzgeci için; test hariç). */
export type NotifyKind =
  | 'down'
  | 'up'
  | 'reminder'
  | 'cert'
  | 'location_down'
  | 'location_up'
  | 'slow'
  | 'slow_resolved'
  | 'server_alert'
  | 'server_resolved'
  | 'probe_offline'
  | 'probe_online';

/** Sessiz saatler: start/end "SS:DD" (tz diliminde; end <= start gece yarısını aşar). */
export interface QuietHours {
  start: string;
  end: string;
  /** IANA saat dilimi; boş: sunucunun yerel saati. */
  tz: string;
  /** critical: yalnızca 🔴 ve onların 🟢'si geçer; none: her şey pencere bitimine ertelenir. */
  mode: 'critical' | 'none';
}

/** Kanal kuralları (eski sunucuda gelmez → kural yok). */
export interface NotificationRules {
  /** Alınacak olay türleri; boş = hepsi. */
  events?: NotifyKind[] | null;
  quiet_hours?: QuietHours | null;
  /** Gecikme (dk): sorun bu kadar sürmezse 🔴 (ve 🟢) gitmez. 0 = kapalı. */
  delay_min?: number;
  /** Eskalasyon (dk): bu kadar süredir açık her olay bağlı olmasa da bu kanala gider. 0 = kapalı. */
  escalate_min?: number;
  /** Kanalın bildirim dili; "" = ayarlardaki. */
  lang?: '' | Locale;
}

export interface NotificationChannel extends NotificationRules {
  id: number;
  name: string;
  type: NotificationType;
  config: Record<string, unknown>;
  is_default: boolean;
  active: boolean;
  created_at: number;
  updated_at: number;
}

export interface NotificationInput extends NotificationRules {
  name: string;
  type: NotificationType;
  config: Record<string, unknown>;
  is_default: boolean;
  active: boolean;
  apply_existing: boolean;
}

export interface AppSettings {
  retention_raw_days: number;
  retention_hourly_days: number;
  cert_days: number[];
  backup_keep: number;
  /** Bildirim metinlerinin dili (eski sunucuda gelmez → tr). */
  notify_lang?: Locale;
  /** Kontrol isteklerinin User-Agent'ı; boş: varsayılan. */
  check_user_agent?: string;
  /** Yalnızca okunur: varsayılan User-Agent (GET yanıtında). */
  default_user_agent?: string;
  /** Saklama süreleri (gün; 0 = süresiz). Eski sunucuda gelmez → 365 / 90 / 365. */
  retention_incident_days?: number;
  retention_capture_days?: number;
  retention_audit_days?: number;
}

export interface BeatEvent {
  monitor_id: number;
  status: number;
  time: number;
  ping: number;
  message: string;
  last_change_at: number;
  cert_expires_at: number;
  /** Yavaş yanıt durumu (eski sunucuda gelmez). */
  slow?: boolean;
}

// Durum sayfaları ------------------------------------------------------------------

export interface PageMonitorRef {
  id: number;
  name: string;
}

export interface PageSection {
  title: string;
  monitors: PageMonitorRef[];
}

export interface StatusPage {
  id: number;
  slug: string;
  title: string;
  description: string;
  footer: string;
  sections: PageSection[];
  custom_domain: string;
  has_password: boolean;
  show_targets: boolean;
  bar_range: BarRange;
  /** Son 14 günün olayları herkese açık sayfada görünsün mü (eski sunucuda gelmez: görünür). */
  show_incidents?: boolean;
  /** Gruplar ziyaretçi tarafından açılıp kapanabilir mi (eski sunucuda gelmez: hayır). */
  collapsible?: boolean;
  /** Herkese açık sayfanın dili (eski sunucuda gelmez: tr). */
  lang?: Locale;
  /** Yerleşim, genişlik ve bölüm sırası (eski sunucuda gelmez: varsayılan). */
  layout?: PageLayout;
  /** Olay bölümünün penceresi (gün; eski sunucuda gelmez: 14). */
  incident_days?: number;
  /** Uptime pencereleri (boş: çubuk kapsamına göre tek pencere). */
  uptime_windows?: UptimeWindow[];
  published: boolean;
  has_logo: boolean;
  created_at: number;
  updated_at: number;
}

/** Liste ekranındaki sayfa özeti (GET /api/status-pages; eski sunucuda gelmez). */
export interface PageSummary {
  status: OverallStatus;
  monitors: number;
  down: number;
  /** Sayfadaki monitörlerin yüzdelerinin ortalaması; veri yoksa null. */
  uptime_24h: number | null;
  uptime_30d: number | null;
  sections: { title: string; statuses: PublicMonitorStatus[] }[];
  /** Son 90 günün en yeni olayı (sayfadaki görünen adla). */
  last_incident: { monitor: string; started_at: number; resolved_at: number } | null;
  ongoing: number;
}

export interface StatusPageListItem extends StatusPage {
  summary?: PageSummary;
}

export interface PageInput {
  slug: string;
  title: string;
  description: string;
  footer: string;
  sections: PageSection[];
  custom_domain: string;
  show_targets: boolean;
  bar_range: BarRange;
  show_incidents: boolean;
  collapsible: boolean;
  /** Gönderilmezse değişmez (yeni sayfada tr). */
  lang?: Locale;
  /** Gönderilmezse değişmez. Olaylar bölümünün görünürlüğü show_incidents ile aynıdır. */
  layout?: PageLayout;
  published: boolean;
  /** Gönderilmezse değişmez, "" kaldırır, dolu değer yeni şifredir. */
  password?: string;
  /** Gönderilmezse değişmez. */
  incident_days?: number;
  uptime_windows?: UptimeWindow[];
}

export type Severity = 'info' | 'warning' | 'danger' | 'success';

export interface Announcement {
  id: number;
  page_id: number;
  title: string;
  body: string;
  severity: Severity;
  starts_at: number;
  ends_at: number;
  created_at: number;
  updated_at: number;
}

export interface AnnouncementInput {
  title: string;
  body: string;
  severity: Severity;
  starts_at: number;
  ends_at: number;
  /** Yalnızca eklemede: kopyası eklenecek diğer sayfalar / tüm sayfalar. */
  page_ids?: number[];
  all_pages?: boolean;
}

/**
 * Durum sayfası yerleşimi: liste (çubuklu), ızgara (geniş ekranda iki sütun),
 * sık liste (çubuksuz), tek satır (durum ışığı, ad, çubuklar ve uptime aynı satırda).
 */
export type PageStyle = 'list' | 'grid' | 'compact' | 'rows';
export type PageWidth = 'narrow' | 'wide';
/** Herkese açık sayfanın sıralanabilir bölümleri. */
export type PageBlockId = 'overall' | 'announcements' | 'groups' | 'incidents';

export interface PageLayout {
  style: PageStyle;
  width: PageWidth;
  /** Dört bölüm de sırasıyla. */
  blocks: { id: PageBlockId; visible: boolean }[];
  /** Monitörlerin uptime yüzdesi gösterilsin mi (eski sunucu/sayfa: gösterilir). */
  show_uptime: boolean;
}

/** Düzenleyicideki canlı önizleme verisi (POST /api/status-pages/preview-data). */
export interface PagePreviewData {
  range: BarRange;
  uptime_window: '24h' | '90d';
  updated_at: number;
  /** Monitör kimliği → herkese açık veri (ad: monitörün kendi adı). */
  monitors: Record<string, PublicMonitor>;
  incidents: { monitor_id: number; started_at: number; resolved_at: number }[];
  announcements: PublicAnnouncement[];
}

/** Durum sayfası çubukları: son kontroller (her çubuk bir kontrol), son 24 saat (saatlik), son 90 gün (günlük). */
export type BarRange = 'recent' | '24h' | '90d';

export type PublicMonitorStatus = 'up' | 'down' | 'pending' | 'paused' | 'maintenance';
export type OverallStatus = 'up' | 'partial' | 'down' | 'unknown';

export interface PublicBar {
  t: number;
  up: number;
  down: number;
}

export interface PublicMonitor {
  name: string;
  status: PublicMonitorStatus;
  /** Sayfanın uptime_window'u için (recent/24h: son 24 saat, 90d: 90 gün). */
  uptime?: number | null;
  uptime_90d: number | null;
  bars: PublicBar[];
  target?: string;
  /** Sayfada birden fazla uptime penceresi seçiliyse pencere → yüzde (veri yoksa null). */
  uptimes?: Record<string, number | null>;
}

/** Durum sayfasındaki uptime pencereleri (sabit sıra). */
export type UptimeWindow = '24h' | '7d' | '30d' | '90d';
export const UPTIME_WINDOWS: UptimeWindow[] = ['24h', '7d', '30d', '90d'];
/** Olay bölümünün penceresi (gün). */
export const INCIDENT_DAY_OPTIONS = [7, 14, 30, 90] as const;

export interface PublicAnnouncement {
  id: number;
  title: string;
  body: string;
  severity: Severity;
  starts_at: number;
  ends_at: number;
}

export interface PublicPage {
  slug: string;
  title: string;
  description: string;
  footer: string;
  has_logo: boolean;
  logo_url: string | null;
  updated_at: number;
  /** Eski sunucularda yok → 90d kabul edilir. */
  range?: BarRange;
  uptime_window?: '24h' | '90d';
  status: OverallStatus;
  sections: { title: string; monitors: PublicMonitor[] }[];
  announcements: PublicAnnouncement[];
  incidents: { monitor: string; started_at: number; resolved_at: number }[];
  /** false ise olay bölümü gösterilmez (eski sunucuda gelmez: gösterilir). */
  show_incidents?: boolean;
  /** true ise gruplar açılıp kapanabilir. */
  collapsible?: boolean;
  /** Sayfanın dili; sayfa bu dilde gösterilir (eski sunucuda gelmez: tr). */
  lang?: Locale;
  /** Sayfa şifreli (ziyaretçi açmış): RSS bağlantısı gösterilmez. Eski sunucuda gelmez. */
  has_password?: boolean;
  /** Yerleşim, genişlik ve bölüm sırası (eski sunucuda gelmez: varsayılan). */
  layout?: PageLayout;
  /** Olay bölümünün penceresi (gün; eski sunucuda gelmez: 14). */
  incident_days?: number;
  /** Monitör satırlarında gösterilen uptime pencereleri (boş: tek pencere, uptime_window). */
  uptime_windows?: UptimeWindow[];
}

/** Şifreli sayfanın 401 yanıtı. */
export interface PublicLocked {
  password_required: true;
  title: string;
  has_logo: boolean;
  logo_url: string | null;
  lang?: Locale;
}

// Bakım pencereleri -------------------------------------------------------------------

export type MaintStrategy = 'manual' | 'once' | 'recurring_weekly' | 'recurring_daily' | 'cron';
export type MaintStatus = 'active' | 'scheduled' | 'ended' | 'inactive';

export interface MaintenanceInput {
  title: string;
  description: string;
  active: boolean;
  strategy: MaintStrategy;
  timezone: string;
  start: string;
  end: string;
  weekdays: number[];
  start_time: string;
  end_time: string;
  date_from: string;
  date_to: string;
  cron: string;
  duration_minutes: number;
  all_monitors: boolean;
  monitor_ids: number[];
}

export interface Maintenance extends MaintenanceInput {
  id: number;
  created_at: number;
  updated_at: number;
  /** Süren tekrar "şimdi bitir" ile bitirildiyse o an (unix); 0: yok. */
  ended_at?: number;
  status: MaintStatus;
  next_start: number;
  next_end: number;
}

// Sunucu takibi ---------------------------------------------------------------------
// Alanlar internal/metrics/types.go ile aynıdır (docs/PLAN.md §12.9). Zamanlar unix saniye.

export interface HostInfo {
  hostname: string;
  os: string;
  platform: string;
  kernel: string;
  arch: string;
  cpu_model: string;
  cores: number;
  threads: number;
  mem_total: number;
  boot_time: number;
  docker: boolean;
}

export interface DiskInfo {
  mount: string;
  device: string;
  fs: string;
  total: number;
  used: number;
}

export interface TempInfo {
  name: string;
  c: number;
}

export interface ContainerInfo {
  id: string;
  name: string;
  cpu: number;
  mem: number;
  mem_limit?: number;
  net_rx_bps: number;
  net_tx_bps: number;
}

export interface ServerStats {
  cpu: number;
  cpu_max?: number;
  load1: number;
  load5: number;
  load15: number;
  mem_total: number;
  mem_used: number;
  mem_cache: number;
  swap_total: number;
  swap_used: number;
  disk_read_bps: number;
  disk_write_bps: number;
  net_rx_bps: number;
  net_tx_bps: number;
  uptime: number;
  disks?: DiskInfo[] | null;
  temps?: TempInfo[] | null;
  containers?: ContainerInfo[] | null;
}

/** online | offline | unavailable (ajan toplayamıyor) | waiting (hiç örnek yok) | disabled */
export type ServerState = 'online' | 'offline' | 'unavailable' | 'waiting' | 'disabled';

export type ServerMetric = 'cpu' | 'mem' | 'swap' | 'disk' | 'load' | 'temp' | 'net' | 'offline';

export interface ServerView {
  id: number;
  name: string;
  active: boolean;
  metrics: boolean;
  state: ServerState;
  note: string;
  interval: number;
  last_seen_at: number;
  metrics_at: number;
  version: string;
  host: HostInfo | null;
  /** Liste biçiminde containers ve temps boş gelir. */
  latest: ServerStats | null;
  container_count: number;
  temp_max: number | null;
  firing: ServerMetric[] | null;
  /** Ajan yalnızca ilk bağlandığı IP'den veri gönderebilir mi. */
  ip_lock: boolean;
  /** Sabitlenmiş IP; boşsa ajan henüz bağlanmadı. */
  locked_ip: string;
  /** Ajanın son bağlandığı IP (yalnızca yöneticiye). */
  ip?: string;
  /** Liste biçiminde son bir saatin dakikalık CPU değerleri (eskiden yeniye). */
  cpu_hist?: number[];
}

export interface AlertRule {
  id: number;
  metric: ServerMetric;
  /** Disk: bölüm ("" = en dolu bölüm); diğer metriklerde boş. */
  mount?: string;
  threshold: number;
  minutes: number;
  active: boolean;
  firing: boolean;
  fired_at: number;
}

export type AlertRuleInput = Pick<AlertRule, 'metric' | 'mount' | 'threshold' | 'minutes' | 'active'>;

export interface ServerDetail extends ServerView {
  alerts: AlertRule[] | null;
  notification_ids: number[] | null;
}

export type StatsRange = '1h' | '24h' | '7d' | '30d';

export interface StatsPoint {
  t: number;
  cpu: number;
  cpu_max?: number;
  load1: number;
  load5: number;
  load15: number;
  mem_used: number;
  mem_cache: number;
  mem_total: number;
  swap_used: number;
  swap_total: number;
  disk_read_bps: number;
  disk_write_bps: number;
  net_rx_bps: number;
  net_tx_bps: number;
  disk_pct: number;
  /** Bölüm başına doluluk (%). */
  disks?: { mount: string; pct: number }[];
  temp?: number | null;
  containers?: { name: string; cpu: number; mem: number }[] | null;
}

export interface StatsSeries {
  range: StatsRange;
  /** Çözünürlük (dakika): 1, 10 veya 60. */
  res: number;
  from: number;
  to: number;
  /** Noktalar arası beklenen süre (sn); bunun 2 katından büyük boşlukta çizgi kesilir. */
  interval: number;
  points: StatsPoint[] | null;
}

export interface ServerEvent {
  id: number;
  metric: ServerMetric;
  /** Disk uyarısında dolan bölüm. */
  mount?: string;
  value: number;
  threshold: number;
  started_at: number;
  ended_at: number | null;
}

export class ApiError extends Error {
  status: number;
  code: string;
  data: unknown;
  constructor(status: number, message: string, code = '', data: unknown = null) {
    super(message);
    this.status = status;
    this.code = code;
    this.data = data;
  }
}

let unauthorizedHandler: (() => void) | null = null;
let passwordChangeHandler: (() => void) | null = null;

/** Oturum düştüğünde (401) çağrılacak fonksiyonu ayarlar. */
export function onUnauthorized(fn: () => void) {
  unauthorizedHandler = fn;
}

/** Sunucu "önce şifrenizi değiştirin" dediğinde (403 password_change_required) çağrılır. */
export function onPasswordChangeRequired(fn: () => void) {
  passwordChangeHandler = fn;
}

// Bu yollarda 401 oturumun düştüğü anlamına gelmez (hatalı şifre/kod, şifreli durum sayfası).
const NO_SESSION_401 = ['/api/auth/login', '/api/auth/login/2fa', '/api/auth/setup'];
const noSession401 = (path: string) => NO_SESSION_401.includes(path) || path.startsWith('/api/public/');

async function request<T>(method: string, path: string, body?: unknown, raw?: { type: string; data: Blob }): Promise<T> {
  // X-Uptime-Lang: sunucu hata mesajlarını arayüzün dilinde döndürsün.
  const headers: Record<string, string> = { Accept: 'application/json', 'X-Uptime-Lang': i18n.locale };
  if (method !== 'GET') {
    headers['X-Uptime'] = '1';
    headers['Content-Type'] = raw ? raw.type : 'application/json';
  }
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: 'same-origin',
      body: raw ? raw.data : body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, t('common.errors.unreachable'));
  }
  let data: unknown = null;
  const text = await res.text();
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) throw failure(path, res.status, data, res.headers.get('Retry-After'));
  return data as T;
}

/** Başarısız yanıttan ApiError üretir; oturum/şifre işleyicilerini tetikler. */
function failure(path: string, status: number, data: unknown, retryAfter: string | null): ApiError {
  const obj = data && typeof data === 'object' ? (data as { error?: unknown; code?: unknown }) : null;
  let msg = typeof obj?.error === 'string' ? obj.error : t('common.errors.requestFailed', { status });
  const code = typeof obj?.code === 'string' ? obj.code : '';
  if (status === 429 && typeof obj?.error !== 'string') {
    const wait = Number(retryAfter);
    msg = wait > 0 ? t('common.errors.tooManyWait', { min: Math.ceil(wait / 60) }) : t('common.errors.tooMany');
  }
  if (status === 413 && typeof obj?.error !== 'string') msg = t('common.errors.tooLarge');
  // Giriş denemesindeki 401 "hatalı şifre" demektir; diğerlerinde oturum düşmüştür.
  if (status === 401 && !noSession401(path)) unauthorizedHandler?.();
  if (status === 403 && code === 'password_change_required') passwordChangeHandler?.();
  return new ApiError(status, msg, code, data);
}

/**
 * Dosyayı multipart/form-data ("file" alanı) olarak yükler. fetch yükleme
 * ilerlemesi bildirmediği için XMLHttpRequest kullanılır. Content-Type'ı
 * tarayıcı (sınır değeriyle) kendisi koyar; CSRF başlığı burada da gönderilir.
 */
export function upload<T>(path: string, file: File, onProgress?: (loaded: number, total: number) => void): Promise<T> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append('file', file, file.name);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', path);
    xhr.withCredentials = true;
    xhr.setRequestHeader('Accept', 'application/json');
    xhr.setRequestHeader('X-Uptime', '1');
    xhr.setRequestHeader('X-Uptime-Lang', i18n.locale);
    if (onProgress) xhr.upload.onprogress = (e) => onProgress(e.loaded, e.lengthComputable ? e.total : file.size);
    xhr.onerror = () => reject(new ApiError(0, t('common.errors.unreachable')));
    xhr.onabort = () => reject(new ApiError(0, t('common.errors.uploadAborted')));
    xhr.onload = () => {
      let data: unknown = null;
      if (xhr.responseText) {
        try {
          data = JSON.parse(xhr.responseText);
        } catch {
          data = null;
        }
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data as T);
      else reject(failure(path, xhr.status, data, xhr.getResponseHeader('Retry-After')));
    };
    xhr.send(form);
  });
}

const get = <T>(p: string) => request<T>('GET', p);
const post = <T>(p: string, b?: unknown) => request<T>('POST', p, b);
const put = <T>(p: string, b?: unknown) => request<T>('PUT', p, b);
const del = <T>(p: string) => request<T>('DELETE', p);

const qs = (params: Record<string, string | number>) => {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== '' && v !== 0) u.set(k, String(v));
  const s = u.toString();
  return s ? `?${s}` : '';
};

export const api = {
  authState: () => get<AuthState>('/api/auth/state'),
  setup: (username: string, password: string) => post<{ user: User }>('/api/auth/setup', { username, password }),
  login: (username: string, password: string) => post<LoginResult>('/api/auth/login', { username, password }),
  login2fa: (challenge: string, code: string) => post<{ user: User }>('/api/auth/login/2fa', { challenge, code }),
  logout: () => post<{ ok: boolean }>('/api/auth/logout'),
  changePassword: (current: string, next: string) =>
    post<{ ok: boolean }>('/api/auth/password', { current, new: next }),
  /** Kendi arayüz dili tercihi; "" = tarayıcı dili. */
  setPreferences: (p: { lang: '' | Locale }) => put<{ user: User }>('/api/auth/preferences', p),

  summary: () => get<Summary>('/api/summary'),
  monitors: () => get<MonitorView[]>('/api/monitors'),
  monitor: (id: number) => get<MonitorDetail>(`/api/monitors/${id}`),
  createMonitor: (m: MonitorInput) => post<MonitorView>('/api/monitors', m),
  updateMonitor: (id: number, m: MonitorInput) => put<MonitorView>(`/api/monitors/${id}`, m),
  deleteMonitor: (id: number) => del<{ ok: boolean }>(`/api/monitors/${id}`),
  pauseMonitor: (id: number) => post<MonitorView>(`/api/monitors/${id}/pause`),
  resumeMonitor: (id: number) => post<MonitorView>(`/api/monitors/${id}/resume`),
  // Listedeki hızlı işlemler
  cloneMonitor: (id: number) => post<MonitorView>(`/api/monitors/${id}/clone`),
  resetMonitorStats: (id: number) => post<MonitorView>(`/api/monitors/${id}/reset-stats`),
  /** Push monitörünün adresini (token) yeniler; eski adres hemen geçersiz olur. */
  regeneratePushToken: (id: number) => post<MonitorView>(`/api/monitors/${id}/push-token`),
  setMonitorNotifications: (id: number, ids: number[]) =>
    put<MonitorView>(`/api/monitors/${id}/notifications`, { notification_ids: ids }),
  bulkMonitors: (ids: number[], a: BulkAction) => post<BulkResult>('/api/monitors/bulk', { ids, ...a }),
  addPageMonitor: (pageId: number, body: { monitor_id: number; section: number; section_title: string; name: string }) =>
    post<StatusPage>(`/api/status-pages/${pageId}/monitors`, body),
  series: (id: number, range: SeriesRange) => get<Series>(`/api/monitors/${id}/series?range=${range}`),
  monitorIncidents: (id: number) => get<Incident[]>(`/api/monitors/${id}/incidents`),
  /** open: yalnızca süren (çözülmemiş) olaylar. */
  incidents: (before: number, limit: number, kind: IncidentFilterKind = '', open = false) =>
    get<Incident[]>(
      `/api/incidents?limit=${limit}${before > 0 ? `&before=${before}` : ''}${kind ? `&kind=${kind}` : ''}${open ? '&open=1' : ''}`,
    ),
  serverIncidents: (id: number) => get<Incident[]>(`/api/servers/${id}/incidents`),
  incident: (id: number) => get<IncidentDetail>(`/api/incidents/${id}`),

  notifications: () => get<NotificationChannel[]>('/api/notifications'),
  createNotification: (n: NotificationInput) => post<NotificationChannel>('/api/notifications', n),
  updateNotification: (id: number, n: NotificationInput) => put<NotificationChannel>(`/api/notifications/${id}`, n),
  deleteNotification: (id: number) => del<{ ok: boolean }>(`/api/notifications/${id}`),
  testNotification: (body: { id?: number; type: NotificationType; config: Record<string, unknown>; lang?: '' | Locale }) =>
    post<{ ok: boolean }>('/api/notifications/test', body),
  /** Kayıtlı kanala her bildirim türünden birer örnek gönderir (arka planda, sırayla). */
  sampleNotifications: (id: number) => post<{ count: number }>(`/api/notifications/${id}/samples`, {}),

  settings: () => get<AppSettings>('/api/settings'),
  saveSettings: (s: AppSettings) => put<AppSettings>('/api/settings', s),

  // Kullanıcılar ve işlem kaydı (yönetici)
  users: () => get<UserRecord[]>('/api/users'),
  createUser: (u: UserCreateInput) => post<UserRecord>('/api/users', u),
  updateUser: (id: number, u: UserInput) => put<UserRecord>(`/api/users/${id}`, u),
  deleteUser: (id: number) => del<{ ok: boolean }>(`/api/users/${id}`),
  resetUserPassword: (id: number, password: string) => post<{ ok: boolean }>(`/api/users/${id}/password`, { password }),
  resetUser2fa: (id: number) => post<{ ok: boolean }>(`/api/users/${id}/2fa/reset`),
  audit: (before: number, limit: number) => get<AuditEntry[]>(`/api/audit${qs({ before, limit })}`),

  // İki adımlı doğrulama
  twoFactor: () => get<TwoFactorStatus>('/api/auth/2fa'),
  twoFactorSetup: (password: string) => post<TwoFactorSetup>('/api/auth/2fa/setup', { password }),
  twoFactorEnable: (code: string) => post<{ recovery_codes: string[] }>('/api/auth/2fa/enable', { code }),
  twoFactorDisable: (password: string, code: string) => post<{ ok: boolean }>('/api/auth/2fa/disable', { password, code }),
  twoFactorRecovery: (password: string, code: string) =>
    post<{ recovery_codes: string[] }>('/api/auth/2fa/recovery-codes', { password, code }),

  // API anahtarları
  apiKeys: (all = false) => get<ApiKey[]>(`/api/api-keys${all ? '?all=1' : ''}`),
  createApiKey: (name: string, role: Role, expires_at: number) =>
    post<{ key: ApiKey; secret: string }>('/api/api-keys', { name, role, expires_at }),
  revokeApiKey: (id: number) => del<{ ok: boolean }>(`/api/api-keys/${id}`),

  // Durum sayfaları
  pages: () => get<StatusPageListItem[]>('/api/status-pages'),
  page: (id: number) => get<StatusPage>(`/api/status-pages/${id}`),
  createPage: (p: PageInput) => post<StatusPage>('/api/status-pages', p),
  updatePage: (id: number, p: PageInput) => put<StatusPage>(`/api/status-pages/${id}`, p),
  deletePage: (id: number) => del<{ ok: boolean }>(`/api/status-pages/${id}`),
  previewPage: (id: number) => get<PublicPage>(`/api/status-pages/${id}/preview`),
  pagePreviewData: (body: {
    page_id?: number;
    monitor_ids: number[];
    bar_range: BarRange;
    show_targets: boolean;
    incident_days?: number;
    uptime_windows?: UptimeWindow[];
  }) =>
    post<PagePreviewData>('/api/status-pages/preview-data', body),
  uploadLogo: (id: number, file: File) =>
    request<StatusPage>('PUT', `/api/status-pages/${id}/logo`, undefined, { type: file.type, data: file }),
  deleteLogo: (id: number) => del<StatusPage>(`/api/status-pages/${id}/logo`),
  announcements: (pageId: number) => get<Announcement[]>(`/api/status-pages/${pageId}/announcements`),
  createAnnouncement: (pageId: number, a: AnnouncementInput) =>
    post<Announcement & { copies?: number }>(`/api/status-pages/${pageId}/announcements`, a),
  updateAnnouncement: (id: number, a: AnnouncementInput) => put<Announcement>(`/api/announcements/${id}`, a),
  deleteAnnouncement: (id: number) => del<{ ok: boolean }>(`/api/announcements/${id}`),

  // Herkese açık durum sayfası (oturum gerekmez)
  publicResolve: () => get<{ slug: string | null }>('/api/public/resolve'),
  publicPage: (slug: string) => get<PublicPage>(`/api/public/pages/${encodeURIComponent(slug)}`),
  publicUnlock: (slug: string, password: string) =>
    post<{ ok: boolean }>(`/api/public/pages/${encodeURIComponent(slug)}/unlock`, { password }),

  // Bakım pencereleri
  maintenance: () => get<Maintenance[]>('/api/maintenance'),
  maintenanceItem: (id: number) => get<Maintenance>(`/api/maintenance/${id}`),
  createMaintenance: (m: MaintenanceInput) => post<Maintenance>('/api/maintenance', m),
  updateMaintenance: (id: number, m: MaintenanceInput) => put<Maintenance>(`/api/maintenance/${id}`, m),
  deleteMaintenance: (id: number) => del<{ ok: boolean }>(`/api/maintenance/${id}`),
  pauseMaintenance: (id: number) => post<Maintenance>(`/api/maintenance/${id}/pause`),
  resumeMaintenance: (id: number) => post<Maintenance>(`/api/maintenance/${id}/resume`),
  /** Süren bakımı hemen bitirir; zamanlama açık kalır. */
  endMaintenance: (id: number) => post<Maintenance>(`/api/maintenance/${id}/end`),

  // Etiketler
  tags: () => get<Tag[]>('/api/tags'),
  createTag: (name: string, color: string) => post<Tag>('/api/tags', { name, color }),
  updateTag: (id: number, name: string, color: string) => put<Tag>(`/api/tags/${id}`, { name, color }),
  deleteTag: (id: number) => del<{ ok: boolean }>(`/api/tags/${id}`),
  setMonitorTags: (id: number, tags: { tag_id: number; value: string }[]) => put<MonitorTag[]>(`/api/monitors/${id}/tags`, tags),

  // Kontrol noktaları
  probes: () => get<Probe[]>('/api/probes'),
  createProbe: (name: string) => post<ProbeSetup>('/api/probes', { name }),
  /** Takip edilecek sunucu ekler (kontrol noktalarından ayrı kayıt). */
  createServer: (name: string) => post<ProbeSetup>('/api/servers', { name }),
  // Yalnızca verilen alanlar gönderilir (sunucu bilinmeyen alanı reddeder):
  // metrics, ip_lock ve reset_ip (kilitli IP'yi sıfırla) isteğe bağlıdır.
  updateProbe: (
    id: number,
    name: string,
    active: boolean,
    metrics?: boolean,
    opts?: { ipLock?: boolean; resetIp?: boolean; notifyOffline?: boolean; notificationIds?: number[] },
  ) => {
    const body: Record<string, unknown> = { name, active };
    if (metrics !== undefined) body.metrics = metrics;
    if (opts?.ipLock !== undefined) body.ip_lock = opts.ipLock;
    if (opts?.resetIp) body.reset_ip = true;
    if (opts?.notifyOffline !== undefined) body.notify_offline = opts.notifyOffline;
    if (opts?.notificationIds !== undefined) body.notification_ids = opts.notificationIds;
    return put<Probe>(`/api/probes/${id}`, body);
  },
  deleteProbe: (id: number) => del<{ ok: boolean }>(`/api/probes/${id}`),
  regenerateProbeToken: (id: number) => post<ProbeSetup>(`/api/probes/${id}/token`),
  monitorLocations: (id: number) => get<MonitorLocations>(`/api/monitors/${id}/locations`),
  setMonitorLocations: (id: number, l: LocationSetup) => put<MonitorLocations>(`/api/monitors/${id}/locations`, l),

  // Sunucu takibi (sunucular kontrol noktalarından ayrıdır; ekleme createServer ile)
  listServers: () => get<{ servers: ServerView[] | null }>('/api/servers').then((r) => r.servers ?? []),
  getServer: (id: number) => get<ServerDetail>(`/api/servers/${id}`),
  getServerStats: (id: number, range: StatsRange) => get<StatsSeries>(`/api/servers/${id}/stats?range=${range}`),
  getServerEvents: (id: number) =>
    get<{ events: ServerEvent[] | null }>(`/api/servers/${id}/events`).then((r) => r.events ?? []),
  putServerAlerts: (id: number, alerts: AlertRuleInput[]) =>
    put<{ alerts: AlertRule[] | null }>(`/api/servers/${id}/alerts`, { alerts }).then((r) => r.alerts ?? []),
  putServerNotifications: (id: number, ids: number[]) =>
    put<{ notification_ids?: number[] | null }>(`/api/servers/${id}/notifications`, { notification_ids: ids }),

  // Yedekle / geri yükle ve içe aktarma (yönetici)
  importBackup: (file: File, mode: 'merge' | 'replace', dryRun: boolean, onProgress?: (l: number, t: number) => void) =>
    upload<ImportSummary>(
      `/api/import?mode=${mode}${dryRun ? '&dry_run=1' : mode === 'replace' ? '&confirm=yes' : ''}`,
      file,
      onProgress,
    ),
  importKuma: (file: File, dryRun: boolean, onProgress?: (l: number, t: number) => void) =>
    upload<ImportSummary>(`/api/import/uptime-kuma${dryRun ? '?dry_run=1' : ''}`, file, onProgress),
  importUptimeRobot: (apiKey: string, dryRun: boolean) =>
    post<ImportSummary>(`/api/import/uptimerobot${dryRun ? '?dry_run=1' : ''}`, { api_key: apiKey }),
};

/** Hata nesnesinden kullanıcıya gösterilecek mesaj. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return t('common.errors.unexpected');
}
