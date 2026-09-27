// Türkçe biçimlendirme yardımcıları. Zamanlar unix saniyesidir.

import { STATUS_DOWN, STATUS_PENDING, STATUS_UP, type Bucket, type MonitorType } from './api';

export const nowSec = () => Math.floor(Date.now() / 1000);

/** "45 sn", "12 dk", "2 sa 5 dk", "3 gün 4 sa" */
export function fmtDuration(sec: number): string {
  sec = Math.max(0, Math.floor(sec));
  if (sec < 60) return `${sec} sn`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} dk`;
  const h = Math.floor(min / 60);
  if (h < 24) {
    const m = min % 60;
    return m ? `${h} sa ${m} dk` : `${h} sa`;
  }
  const d = Math.floor(h / 24);
  const hh = h % 24;
  return hh ? `${d} gün ${hh} sa` : `${d} gün`;
}

/** En büyük birimle kısa süre: "3 dk", "2 sa", "5 gün". */
export function fmtDurationShort(sec: number): string {
  sec = Math.max(0, Math.floor(sec));
  if (sec < 60) return `${sec} sn`;
  if (sec < 3600) return `${Math.floor(sec / 60)} dk`;
  if (sec < 86400) return `${Math.floor(sec / 3600)} sa`;
  return `${Math.floor(sec / 86400)} gün`;
}

/** "3 dk önce" */
export function fmtRelative(ts: number, now: number): string {
  if (!ts) return '—';
  const diff = now - ts;
  if (diff < 5) return 'az önce';
  return `${fmtDurationShort(diff)} önce`;
}

// Sunucu günlük özetleri İstanbul saatine göre kovalar; tarihler de aynı dilimde
// gösterilsin ki başka dilimdeki bir tarayıcıda 90 günlük çubuklar kaymasın.
const TZ = 'Europe/Istanbul';
const dateFmt = new Intl.DateTimeFormat('tr-TR', {
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  timeZone: TZ,
});
const dateOnlyFmt = new Intl.DateTimeFormat('tr-TR', { day: '2-digit', month: '2-digit', year: 'numeric', timeZone: TZ });
const shortDateFmt = new Intl.DateTimeFormat('tr-TR', { day: '2-digit', month: '2-digit', timeZone: TZ });
const timeFmt = new Intl.DateTimeFormat('tr-TR', { hour: '2-digit', minute: '2-digit', timeZone: TZ });
const timeSecFmt = new Intl.DateTimeFormat('tr-TR', { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZone: TZ });

const tzParts = new Intl.DateTimeFormat('en-US', {
  timeZone: TZ,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
});

/** Verilen andaki İstanbul saat dilimi farkı (saniye). */
export function tzOffset(ts: number): number {
  const p: Record<string, number> = {};
  for (const x of tzParts.formatToParts(new Date(ts * 1000))) if (x.type !== 'literal') p[x.type] = Number(x.value);
  const asUtc = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) / 1000;
  return Math.round(asUtc - ts);
}

/** ts'nin İstanbul saatine göre bulunduğu günün başlangıcı (unix sn). */
export function tzDayStart(ts: number): number {
  const off = tzOffset(ts);
  return Math.floor((ts + off) / 86400) * 86400 - off;
}

/** dd.MM.yyyy HH:mm */
export function fmtDate(ts: number): string {
  if (!ts) return '—';
  return dateFmt.format(new Date(ts * 1000));
}
export function fmtDay(ts: number): string {
  return dateOnlyFmt.format(new Date(ts * 1000));
}
/** dd.MM */
export function fmtShortDate(ts: number): string {
  return shortDateFmt.format(new Date(ts * 1000));
}
/** HH:mm */
export function fmtTime(ts: number): string {
  return timeFmt.format(new Date(ts * 1000));
}
/** HH:mm:ss */
export function fmtTimeSec(ts: number): string {
  return timeSecFmt.format(new Date(ts * 1000));
}

/** %99,95 — tam 100 ise %100; yuvarlama asla yukarı doğru yapılmaz. */
export function fmtPct(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—';
  if (v >= 100) return '%100';
  const floored = Math.floor(v * 100) / 100;
  return '%' + floored.toFixed(2).replace('.', ',');
}

const numFmt = new Intl.NumberFormat('tr-TR');
export function fmtNum(n: number): string {
  return numFmt.format(n);
}

/** "123 ms"; -1 → "—" */
export function fmtMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || ms < 0) return '—';
  return `${numFmt.format(Math.round(ms))} ms`;
}

/** Kontrol aralığı: "30 sn", "5 dk", "1 sa", "1 dk 30 sn" */
export function fmtInterval(sec: number): string {
  if (sec >= 3600 && sec % 3600 === 0) return `${sec / 3600} sa`;
  if (sec >= 60 && sec % 60 === 0) return `${sec / 60} dk`;
  if (sec > 60) return `${Math.floor(sec / 60)} dk ${sec % 60} sn`;
  return `${sec} sn`;
}

export const TYPE_LABELS: Record<MonitorType, string> = {
  http: 'HTTP',
  tcp: 'TCP',
  ping: 'PING',
  dns: 'DNS',
  push: 'PUSH',
};

export type StatusKind = 'up' | 'down' | 'pending' | 'paused';

export function statusKind(status: number, active: boolean): StatusKind {
  if (!active) return 'paused';
  if (status === STATUS_UP) return 'up';
  if (status === STATUS_DOWN) return 'down';
  return 'pending';
}

export const STATUS_LABELS: Record<StatusKind, string> = {
  up: 'Çalışıyor',
  down: 'Çalışmıyor',
  pending: 'Bekleniyor',
  paused: 'Durduruldu',
};

export function pointStatusLabel(s: number): string {
  if (s === STATUS_UP) return 'Çalışıyor';
  if (s === STATUS_DOWN) return 'Çalışmıyor';
  if (s === STATUS_PENDING) return 'Tekrar deneniyor';
  return 'Bilinmiyor';
}

export type BarKind = 'up' | 'down' | 'mixed' | 'nodata';

export function barKind(b: Bucket): BarKind {
  const total = b.up + b.down;
  if (total === 0) return 'nodata';
  if (b.down === 0) return 'up';
  return b.up > b.down ? 'mixed' : 'down';
}

export function bucketUptime(b: Bucket): number | null {
  const total = b.up + b.down;
  return total === 0 ? null : (100 * b.up) / total;
}

/** "14:00 – 15:00" */
export function hourRange(t: number): string {
  return `${fmtTime(t)} – ${fmtTime(t + 3600)}`;
}

/** Karşılaştırma için Türkçe küçük harf. */
export function lower(s: string): string {
  return s.toLocaleLowerCase('tr');
}

export const collator = new Intl.Collator('tr', { sensitivity: 'base', numeric: true });

/** Sertifikanın kalan gün sayısı (0 → bilgi yok). */
export function certDaysLeft(expiresAt: number, now: number): number | null {
  if (!expiresAt) return null;
  return Math.floor((expiresAt - now) / 86400);
}
