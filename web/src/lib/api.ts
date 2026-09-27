// Go sunucusunun REST API'si için tipler ve küçük bir fetch sarmalayıcısı.

export const MASK = '••••••';

export const STATUS_DOWN = 0;
export const STATUS_UP = 1;
export const STATUS_PENDING = 2;

export type MonitorType = 'http' | 'tcp' | 'ping' | 'dns' | 'push';

export interface User {
  id: number;
  username: string;
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
  | 'pushover';

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

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

let unauthorizedHandler: (() => void) | null = null;

/** Oturum düştüğünde (401) çağrılacak fonksiyonu ayarlar. */
export function onUnauthorized(fn: () => void) {
  unauthorizedHandler = fn;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (method !== 'GET') {
    headers['X-Uptime'] = '1';
    headers['Content-Type'] = 'application/json';
  }
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
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
    const msg =
      data && typeof data === 'object' && typeof (data as { error?: unknown }).error === 'string'
        ? (data as { error: string }).error
        : `İstek başarısız oldu (HTTP ${res.status})`;
    // Giriş denemesindeki 401 "hatalı şifre" demektir; diğerlerinde oturum düşmüştür.
    if (res.status === 401 && path !== '/api/auth/login' && path !== '/api/auth/setup') unauthorizedHandler?.();
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

const get = <T>(p: string) => request<T>('GET', p);
const post = <T>(p: string, b?: unknown) => request<T>('POST', p, b);
const put = <T>(p: string, b?: unknown) => request<T>('PUT', p, b);
const del = <T>(p: string) => request<T>('DELETE', p);

export const api = {
  authState: () => get<AuthState>('/api/auth/state'),
  setup: (username: string, password: string) => post<{ user: User }>('/api/auth/setup', { username, password }),
  login: (username: string, password: string) => post<{ user: User }>('/api/auth/login', { username, password }),
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
};

/** Hata nesnesinden kullanıcıya gösterilecek mesaj. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return 'Beklenmeyen bir hata oluştu';
}
