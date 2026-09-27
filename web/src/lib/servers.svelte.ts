// Sunucu takibi: liste deposu (canlı akışla güncellenir) ve ekranlarda ortak
// kullanılan etiketler, eşik bilgileri ve küçük hesaplar.

import { api, errorMessage, type ServerMetric, type ServerState, type ServerStats, type ServerView, type StatsPoint } from './api';
import { collator, fmtDec, fmtPctInt, fmtTemp } from './format';
import { live } from './live.svelte';
import { session } from './session.svelte';

// Metrikler -----------------------------------------------------------------------------

export type MetricUnit = 'pct' | 'load' | 'temp' | 'none';

export interface MetricInfo {
  label: string;
  unit: MetricUnit;
  /** Kural düzenleyicide açıklama. */
  desc: string;
  /** Yeni kural eklenirken önerilen değerler. */
  threshold: number;
  minutes: number;
  min: number;
  max: number;
  step: number;
}

export const METRICS: Record<ServerMetric, MetricInfo> = {
  cpu: { label: 'CPU', unit: 'pct', desc: 'Tüm çekirdeklerin ortalama kullanımı', threshold: 90, minutes: 10, min: 1, max: 100, step: 1 },
  mem: { label: 'RAM', unit: 'pct', desc: 'Önbellek hariç kullanılan bellek', threshold: 90, minutes: 10, min: 1, max: 100, step: 1 },
  swap: { label: 'Swap', unit: 'pct', desc: 'Takas alanı kullanımı', threshold: 50, minutes: 10, min: 1, max: 100, step: 1 },
  disk: { label: 'Disk', unit: 'pct', desc: 'En dolu disk bölümü', threshold: 85, minutes: 5, min: 1, max: 100, step: 1 },
  load: { label: 'Yük', unit: 'load', desc: '1 dakikalık yük ÷ çekirdek sayısı', threshold: 1.5, minutes: 10, min: 0.1, max: 100, step: 0.1 },
  temp: { label: 'Sıcaklık', unit: 'temp', desc: 'En sıcak sensör', threshold: 80, minutes: 5, min: 1, max: 150, step: 1 },
  offline: { label: 'Çevrimdışı', unit: 'none', desc: 'Ajandan bu süre boyunca veri gelmezse', threshold: 0, minutes: 3, min: 0, max: 0, step: 1 },
};

export const METRIC_ORDER: ServerMetric[] = ['offline', 'cpu', 'mem', 'disk', 'swap', 'load', 'temp'];

export const metricLabel = (m: ServerMetric) => METRICS[m]?.label ?? m;

export const UNIT_LABELS: Record<MetricUnit, string> = { pct: '%', load: 'çekirdek başına', temp: '°C', none: '' };

/** Eşik veya ölçülen değeri metriğin birimiyle yazar: "%90", "1,50 / çekirdek", "80 °C". */
export function fmtMetric(m: ServerMetric, v: number): string {
  switch (METRICS[m]?.unit) {
    case 'pct':
      return fmtPctInt(v);
    case 'load':
      return `${fmtDec(v, 2)} / çekirdek`;
    case 'temp':
      return fmtTemp(v);
    default:
      return '';
  }
}

// Durum ---------------------------------------------------------------------------------

export type ServerTone = 'up' | 'down' | 'warn' | 'muted';

export const STATE_LABELS: Record<ServerState, string> = {
  online: 'Çevrimiçi',
  offline: 'Çevrimdışı',
  unavailable: 'Metrik yok',
  waiting: 'Bağlantı bekleniyor',
  disabled: 'Devre dışı',
};

/** Durum noktasının rengi: tetiklenmiş uyarı çevrimiçi sunucuyu turuncuya çevirir. */
export function serverTone(s: ServerView): ServerTone {
  if (s.state === 'offline') return 'down';
  if (s.state !== 'online') return 'muted';
  return s.firing?.length ? 'warn' : 'up';
}

/** Metrik göndermeyen (sönük gösterilen) ajan mı? */
export const isInactive = (s: ServerView) => s.state === 'unavailable' || s.state === 'waiting' || s.state === 'disabled';

/** Sönük ajan için kısa neden. */
export function inactiveReason(s: ServerView): string {
  switch (s.state) {
    case 'unavailable':
      return s.note ? `Ajan metrik toplayamıyor: ${s.note}` : 'Ajan host metriklerini okuyamıyor; kurulum komutunu güncelleyin.';
    case 'waiting':
      return s.last_seen_at
        ? 'Ajan bağlı ama metrik göndermiyor (eski kurulum). Kurulum komutunu güncelleyin.'
        : 'Ajan henüz bağlanmadı. Kurulum komutunu sunucuda çalıştırın.';
    case 'disabled':
      return !s.active ? 'Ajan devre dışı bırakıldı.' : 'Bu ajanda metrik toplama kapalı.';
    default:
      return '';
  }
}

/** Sıralama: çevrimdışı → uyarıda → çevrimiçi → metrik göndermeyenler; sonra ada göre. */
export function serverRank(s: ServerView): number {
  switch (s.state) {
    case 'offline':
      return 0;
    case 'online':
      return s.firing?.length ? 1 : 2;
    case 'unavailable':
      return 3;
    case 'waiting':
      return 4;
    default:
      return 5;
  }
}

export const sortServers = (list: ServerView[]) =>
  list.slice().sort((a, b) => serverRank(a) - serverRank(b) || collator.compare(a.name, b.name));

// Hesaplar ------------------------------------------------------------------------------

const pct = (used: number, total: number) => (total > 0 ? (100 * used) / total : 0);

export const memPct = (s: ServerStats) => pct(s.mem_used, s.mem_total);
export const swapPct = (s: ServerStats) => pct(s.swap_used, s.swap_total);

/** En dolu diskin yüzdesi (disk yoksa null). */
export function diskPct(s: ServerStats): number | null {
  if (!s.disks?.length) return null;
  return Math.max(...s.disks.map((d) => pct(d.used, d.total)));
}

/** Doluluk seviyesi: ≥90 kritik, ≥80 uyarı. */
export function usageLevel(v: number | null | undefined): 'ok' | 'warn' | 'danger' {
  if (v === null || v === undefined) return 'ok';
  return v >= 90 ? 'danger' : v >= 80 ? 'warn' : 'ok';
}

/** Anlık örnekten grafik noktası (canlı güncellemede 1 sa / 24 sa grafiğine eklenir). */
export function pointFromStats(t: number, s: ServerStats, tempMax: number | null): StatsPoint {
  return {
    t,
    cpu: s.cpu,
    load1: s.load1,
    load5: s.load5,
    load15: s.load15,
    mem_used: s.mem_used,
    mem_cache: s.mem_cache,
    mem_total: s.mem_total,
    swap_used: s.swap_used,
    swap_total: s.swap_total,
    disk_read_bps: s.disk_read_bps,
    disk_write_bps: s.disk_write_bps,
    net_rx_bps: s.net_rx_bps,
    net_tx_bps: s.net_tx_bps,
    disk_pct: diskPct(s) ?? 0,
    temp: tempMax,
    containers: s.containers?.map((c) => ({ name: c.name, cpu: c.cpu, mem: c.mem })) ?? null,
  };
}

// Liste deposu --------------------------------------------------------------------------

class Servers {
  list = $state.raw<ServerView[]>([]);
  loaded = $state(false);
  loadError = $state('');

  index = $derived(new Map(this.list.map((s) => [s.id, s])));
  sorted = $derived(sortServers(this.list));
  /** Sorunlu (çevrimdışı veya uyarıda) sunucu sayısı. */
  problems = $derived(this.list.reduce((n, s) => n + (s.state === 'offline' || (s.state === 'online' && s.firing?.length) ? 1 : 0), 0));

  private userId = 0;
  private lastLoad = 0;
  private subscribed = false;
  private req = 0;

  /** Sayfa açılırken çağrılır: gerekirse listeyi yükler ve canlı akışa bir kez abone olur. */
  ensure() {
    const uid = session.user?.id ?? 0;
    if (uid !== this.userId) {
      // Başka kullanıcıya geçildiyse önceki oturumun listesi gösterilmesin.
      this.userId = uid;
      this.list = [];
      this.loaded = false;
    }
    if (!this.subscribed) {
      this.subscribed = true;
      live.onServer((v) => this.apply(v));
      live.onReconnect(() => this.loaded && this.load());
    }
    if (!this.loaded || Date.now() - this.lastLoad > 15_000) this.load();
  }

  async load() {
    const req = ++this.req;
    this.lastLoad = Date.now();
    try {
      const list = await api.listServers();
      if (req !== this.req) return;
      this.list = list;
      this.loaded = true;
      this.loadError = '';
    } catch (e) {
      if (req === this.req && !this.loaded) this.loadError = errorMessage(e);
    }
  }

  /** Canlı akıştan veya kayıttan gelen sunucuyu listeye yazar. */
  apply(v: ServerView) {
    if (!this.loaded || !v || typeof v.id !== 'number') return;
    const i = this.list.findIndex((s) => s.id === v.id);
    const list = this.list.slice();
    if (i >= 0) list[i] = v;
    else list.push(v);
    this.list = list;
  }

  remove(id: number) {
    this.list = this.list.filter((s) => s.id !== id);
  }

  byId(id: number): ServerView | undefined {
    return this.index.get(id);
  }
}

export const servers = new Servers();
