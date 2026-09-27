// Go sunucusunun REST API'si için tipler ve küçük bir fetch sarmalayıcısı.

export const MASK = '••••••';

export const STATUS_DOWN = 0;
export const STATUS_UP = 1;
export const STATUS_PENDING = 2;
export const STATUS_MAINTENANCE = 3;

/** Bilinen tipler; yeni tip eklenirken monitorTypes.ts'teki kayıt defterine de eklenir. */
export type MonitorType = 'http' | 'tcp' | 'ping' | 'dns' | 'push' | 'group';

export type Role = 'admin' | 'editor' | 'viewer';

export interface User {
  id: number;
  username: string;
  display_name: string;
  role: Role;
  must_change_password: boolean;
  all_monitors: boolean;
  two_factor_enabled: boolean;
}

/** Kullanıcılar sayfasındaki kayıt (yalnızca yönetici). */
export interface UserRecord extends User {
  disabled: boolean;
  monitor_ids: number[] | null;
  last_login_at: number;
  created_at: number;
}

export interface UserInput {
  display_name: string;
  role: Role;
  disabled: boolean;
  all_monitors: boolean;
  monitor_ids: number[];
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
}

export type UptimeKey = '24h' | '7d' | '30d' | '90d';

export interface MonitorDetail {
  monitor: MonitorView;
  uptime: Record<UptimeKey, number | null>;
  avg_ping_24h: number;
  open_incident_since: number;
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

export interface Incident {
  id: number;
  monitor_id: number;
  monitor_name: string;
  started_at: number;
  resolved_at: number;
  cause: string;
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

export interface NotificationChannel {
  id: number;
  name: string;
  type: NotificationType;
  config: Record<string, unknown>;
  is_default: boolean;
  active: boolean;
  created_at: number;
  updated_at: number;
}

export interface NotificationInput {
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
}

export interface BeatEvent {
  monitor_id: number;
  status: number;
  time: number;
  ping: number;
  message: string;
  last_change_at: number;
  cert_expires_at: number;
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
  published: boolean;
  has_logo: boolean;
  created_at: number;
  updated_at: number;
}

export interface PageInput {
  slug: string;
  title: string;
  description: string;
  footer: string;
  sections: PageSection[];
  custom_domain: string;
  show_targets: boolean;
  published: boolean;
  /** Gönderilmezse değişmez, "" kaldırır, dolu değer yeni şifredir. */
  password?: string;
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
}

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
  uptime_90d: number | null;
  bars: PublicBar[];
  target?: string;
}

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
  status: OverallStatus;
  sections: { title: string; monitors: PublicMonitor[] }[];
  announcements: PublicAnnouncement[];
  incidents: { monitor: string; started_at: number; resolved_at: number }[];
}

/** Şifreli sayfanın 401 yanıtı. */
export interface PublicLocked {
  password_required: true;
  title: string;
  has_logo: boolean;
  logo_url: string | null;
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
  status: MaintStatus;
  next_start: number;
  next_end: number;
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
  const headers: Record<string, string> = { Accept: 'application/json' };
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
    throw new ApiError(0, 'Sunucuya ulaşılamadı. Bağlantınızı kontrol edin.');
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
  if (!res.ok) {
    const obj = data && typeof data === 'object' ? (data as { error?: unknown; code?: unknown }) : null;
    let msg = typeof obj?.error === 'string' ? obj.error : `İstek başarısız oldu (HTTP ${res.status})`;
    const code = typeof obj?.code === 'string' ? obj.code : '';
    if (res.status === 429 && typeof obj?.error !== 'string') {
      const wait = Number(res.headers.get('Retry-After'));
      msg = wait > 0 ? `Çok fazla deneme. ${Math.ceil(wait / 60)} dakika sonra tekrar deneyin.` : 'Çok fazla deneme. Biraz sonra tekrar deneyin.';
    }
    // Giriş denemesindeki 401 "hatalı şifre" demektir; diğerlerinde oturum düşmüştür.
    if (res.status === 401 && !noSession401(path)) unauthorizedHandler?.();
    if (res.status === 403 && code === 'password_change_required') passwordChangeHandler?.();
    throw new ApiError(res.status, msg, code, data);
  }
  return data as T;
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

  summary: () => get<Summary>('/api/summary'),
  monitors: () => get<MonitorView[]>('/api/monitors'),
  monitor: (id: number) => get<MonitorDetail>(`/api/monitors/${id}`),
  createMonitor: (m: MonitorInput) => post<MonitorView>('/api/monitors', m),
  updateMonitor: (id: number, m: MonitorInput) => put<MonitorView>(`/api/monitors/${id}`, m),
  deleteMonitor: (id: number) => del<{ ok: boolean }>(`/api/monitors/${id}`),
  pauseMonitor: (id: number) => post<MonitorView>(`/api/monitors/${id}/pause`),
  resumeMonitor: (id: number) => post<MonitorView>(`/api/monitors/${id}/resume`),
  series: (id: number, range: SeriesRange) => get<Series>(`/api/monitors/${id}/series?range=${range}`),
  monitorIncidents: (id: number) => get<Incident[]>(`/api/monitors/${id}/incidents`),
  incidents: (before: number, limit: number) =>
    get<Incident[]>(`/api/incidents?limit=${limit}${before > 0 ? `&before=${before}` : ''}`),

  notifications: () => get<NotificationChannel[]>('/api/notifications'),
  createNotification: (n: NotificationInput) => post<NotificationChannel>('/api/notifications', n),
  updateNotification: (id: number, n: NotificationInput) => put<NotificationChannel>(`/api/notifications/${id}`, n),
  deleteNotification: (id: number) => del<{ ok: boolean }>(`/api/notifications/${id}`),
  testNotification: (body: { id?: number; type: NotificationType; config: Record<string, unknown> }) =>
    post<{ ok: boolean }>('/api/notifications/test', body),

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
  pages: () => get<StatusPage[]>('/api/status-pages'),
  page: (id: number) => get<StatusPage>(`/api/status-pages/${id}`),
  createPage: (p: PageInput) => post<StatusPage>('/api/status-pages', p),
  updatePage: (id: number, p: PageInput) => put<StatusPage>(`/api/status-pages/${id}`, p),
  deletePage: (id: number) => del<{ ok: boolean }>(`/api/status-pages/${id}`),
  previewPage: (id: number) => get<PublicPage>(`/api/status-pages/${id}/preview`),
  uploadLogo: (id: number, file: File) =>
    request<StatusPage>('PUT', `/api/status-pages/${id}/logo`, undefined, { type: file.type, data: file }),
  deleteLogo: (id: number) => del<StatusPage>(`/api/status-pages/${id}/logo`),
  announcements: (pageId: number) => get<Announcement[]>(`/api/status-pages/${pageId}/announcements`),
  createAnnouncement: (pageId: number, a: AnnouncementInput) =>
    post<Announcement>(`/api/status-pages/${pageId}/announcements`, a),
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
};

/** Hata nesnesinden kullanıcıya gösterilecek mesaj. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return 'Beklenmeyen bir hata oluştu';
}
