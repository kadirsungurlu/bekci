// Sunucu takibi: liste deposu (canlı akışla güncellenir) ve ekranlarda ortak
// kullanılan etiketler, eşik bilgileri ve küçük hesaplar.

import { api, errorMessage, type ServerMetric, type ServerState, type ServerStats, type ServerView, type StatsPoint } from './api';
import { collator, fmtDec, fmtPctInt, fmtTemp } from './format';
import { t } from './i18n';
import { live } from './live.svelte';
import { session } from './session.svelte';

// Metrikler -----------------------------------------------------------------------------

export type MetricUnit = 'pct' | 'load' | 'temp' | 'net' | 'none';

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

// Etiket ve açıklama getter'dır: dil değişince yeniden çevrilir.
function metric(m: ServerMetric, unit: MetricUnit, threshold: number, minutes: number, min: number, max: number, step: number): MetricInfo {
  return {
    get label() {
      return t(`servers.metrics.${m}.label`);
    },
    get desc() {
      return t(`servers.metrics.${m}.desc`);
    },
    unit,
    threshold,
    minutes,
    min,
    max,
    step,
  };
}

export const METRICS: Record<ServerMetric, MetricInfo> = {
  cpu: metric('cpu', 'pct', 90, 10, 1, 100, 1),
  mem: metric('mem', 'pct', 90, 10, 1, 100, 1),
  swap: metric('swap', 'pct', 50, 10, 1, 100, 1),
  disk: metric('disk', 'pct', 85, 5, 1, 100, 1),
  load: metric('load', 'load', 1.5, 10, 0.1, 100, 0.1),
  temp: metric('temp', 'temp', 80, 5, 1, 150, 1),
  net: metric('net', 'net', 100, 10, 0.1, 1000000, 10),
  offline: metric('offline', 'none', 0, 3, 0, 0, 1),
};

export const METRIC_ORDER: ServerMetric[] = ['offline', 'cpu', 'mem', 'disk', 'swap', 'load', 'temp', 'net'];

export const metricLabel = (m: ServerMetric) => METRICS[m]?.label ?? m;

export const UNIT_LABELS: Record<MetricUnit, string> = {
  pct: '%',
  get load() {
    return t('servers.units.load');
  },
  temp: '°C',
  get net() {
    return t('servers.units.net');
  },
  none: '',
};

/** Tek ondalıklı yüzde: tr "%42,5", en "42.5%". */
export const fmtPct1 = (v: number) => t('servers.pct', { v: fmtDec(v, 1) });

/** Eşik veya ölçülen değeri metriğin birimiyle yazar: "%90", "1,50 / çekirdek", "80 °C". */
export function fmtMetric(m: ServerMetric, v: number): string {
  switch (METRICS[m]?.unit) {
    case 'pct':
      return fmtPctInt(v);
    case 'load':
      return t('servers.fmt.load', { v: fmtDec(v, 2) });
    case 'temp':
      return fmtTemp(v);
    case 'net':
      return v >= 1000 ? t('servers.fmt.gbit', { v: fmtDec(v / 1000, 2) }) : t('servers.fmt.mbit', { v: Math.round(v) });
    default:
      return '';
  }
}

// Durum ---------------------------------------------------------------------------------

export type ServerTone = 'up' | 'down' | 'warn' | 'muted';

export const STATE_LABELS: Record<ServerState, string> = {
  get online() {
    return t('servers.states.online');
  },
  get offline() {
    return t('servers.states.offline');
  },
  get unavailable() {
    return t('servers.states.unavailable');
  },
  get waiting() {
    return t('servers.states.waiting');
  },
  get disabled() {
    return t('servers.states.disabled');
  },
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
      return s.note ? t('servers.inactive.unavailableNote', { note: s.note }) : t('servers.inactive.unavailable');
    case 'waiting':
      return s.last_seen_at ? t('servers.inactive.waitingSeen') : t('servers.inactive.waitingNever');
    case 'disabled':
      return !s.active ? t('servers.inactive.agentDisabled') : t('servers.inactive.metricsOff');
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

/**
 * Gösterilen (en dolu) bölümün adı: "/", "C:", "/home". Tek bölümde de döner ki
 * listede her sunucunun disk çubuğunun altında hangi bölüm olduğu tutarlı görünsün.
 */
export function fullestMount(s: ServerStats): string {
  if (!s.disks?.length) return '';
  return s.disks.reduce((a, b) => (pct(b.used, b.total) > pct(a.used, a.total) ? b : a)).mount;
}

/** Tüm bölümlerin kısa dökümü: "/ %60 · /home %82" (en: "/ 60% · /home 82%"). */
export function diskSummary(s: ServerStats): string {
  return (s.disks ?? []).map((d) => `${d.mount} ${fmtPctInt(pct(d.used, d.total))}`).join(' · ');
}

/**
 * İşletim sistemi adının listeye sığan kısa hâli. Windows'ta ürün adı uzundur
 * ("Microsoft Windows Server 2022 Datacenter 21H2" → "Windows Server 2022",
 * "Microsoft Windows 11 Pro 23H2" → "Windows 11"); diğerleri olduğu gibi kalır.
 * Tam ad title olarak gösterilir.
 */
export function shortPlatform(p: string | null | undefined): string {
  const s = (p ?? '').trim();
  const m = /^(?:Microsoft\s+)?(Windows(?:\s+Server)?\s+\d+(?:\s+R2)?)\b/i.exec(s);
  return m ? m[1].replace(/\s+/g, ' ') : s;
}

/**
 * Sunucunun alt satırı "host adı · işletim sistemi": listeye sığan kısa ve tam
 * (title) hâli. Host adı sunucu adıyla aynıysa (ör. "WIN-DC01") kısa hâlde tekrarlanmaz.
 */
export function hostLine(s: Pick<ServerView, 'host' | 'name'>): { short: string; full: string } {
  const h = s.host;
  const host = h?.hostname && h.hostname.toLowerCase() !== s.name.trim().toLowerCase() ? h.hostname : '';
  return {
    short: [host, shortPlatform(h?.platform)].filter(Boolean).join(' · '),
    full: [h?.hostname, h?.platform].filter(Boolean).join(' · '),
  };
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

/** Sunucu kaydının ne kadar güncel olduğu (son örnek veya son görülme anı). */
const freshness = (s: ServerView) => Math.max(s.metrics_at ?? 0, s.last_seen_at ?? 0);

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
      live.onResume(() => this.loaded && this.load());
    }
    if (!this.loaded || Date.now() - this.lastLoad > 15_000) this.load();
  }

  async load() {
    const req = ++this.req;
    this.lastLoad = Date.now();
    try {
      const list = await api.listServers();
      if (req !== this.req) return;
      this.list = this.merge(list);
      this.loaded = true;
      this.loadError = '';
    } catch (e) {
      if (req === this.req && !this.loaded) this.loadError = errorMessage(e);
    }
  }

  /**
   * İstek yoldayken canlı akıştan daha yeni bir örnek gelmiş sunucuyu, geç dönen
   * listedeki eski hâliyle geri almaz.
   */
  private merge(list: ServerView[]): ServerView[] {
    if (!this.loaded || !this.list.length) return list;
    const cur = this.index;
    return list.map((n) => {
      const c = cur.get(n.id);
      return c && freshness(c) > freshness(n) ? c : n;
    });
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
