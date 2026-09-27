// Türkçe biçimlendirme yardımcıları. Zamanlar unix saniyesidir.

import { STATUS_DOWN, STATUS_MAINTENANCE, STATUS_PENDING, STATUS_UP, type Bucket, type MonitorView } from './api';

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

export type StatusKind = 'up' | 'down' | 'pending' | 'paused' | 'maintenance';

export function statusKind(status: number, active: boolean, inMaintenance = false): StatusKind {
  if (!active) return 'paused';
  if (status === STATUS_MAINTENANCE || inMaintenance) return 'maintenance';
  if (status === STATUS_UP) return 'up';
  if (status === STATUS_DOWN) return 'down';
  return 'pending';
}

/** Monitörün şu anki görünen durumu (bakım penceresi dahil). */
export const monitorKind = (m: MonitorView): StatusKind => statusKind(m.status, m.active, m.in_maintenance);

export const STATUS_LABELS: Record<StatusKind, string> = {
  up: 'Çalışıyor',
  down: 'Çalışmıyor',
  pending: 'Bekleniyor',
  paused: 'Durduruldu',
  maintenance: 'Bakımda',
};

export function pointStatusLabel(s: number): string {
  if (s === STATUS_UP) return 'Çalışıyor';
  if (s === STATUS_DOWN) return 'Çalışmıyor';
  if (s === STATUS_PENDING) return 'Tekrar deneniyor';
  if (s === STATUS_MAINTENANCE) return 'Bakımda';
  return 'Bilinmiyor';
}

/** Ham nokta durumunun renk sınıfı (c-up, c-down …). */
export function pointStatusClass(s: number): string {
  if (s === STATUS_UP) return 'up';
  if (s === STATUS_DOWN) return 'down';
  if (s === STATUS_PENDING) return 'pending';
  if (s === STATUS_MAINTENANCE) return 'maint';
  return 'paused';
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

/** <input type="date"> değeri (İstanbul saatine göre): "2026-09-27" */
export function isoDay(ts: number): string {
  const off = tzOffset(ts);
  return new Date((ts + off) * 1000).toISOString().slice(0, 10);
}

/** <input type="datetime-local"> değeri (İstanbul saatine göre): "2026-09-27T14:30" */
export function isoLocal(ts: number): string {
  const off = tzOffset(ts);
  return new Date((ts + off) * 1000).toISOString().slice(0, 16);
}

/** "2026-09-27T14:30" (İstanbul saati) → unix saniye; geçersizse 0. */
export function parseLocal(v: string): number {
  const m = v.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/);
  if (!m) return 0;
  const asUtc = Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]) / 1000;
  // Yaz saati geçişlerinde de doğru sonuç için fark iki kez hesaplanır.
  let ts = asUtc - tzOffset(asUtc);
  ts = asUtc - tzOffset(ts);
  return ts;
}

/** Rastgele, okunaklı geçici şifre (karışabilen 0/O/l/1 yok). */
export function randomPassword(len = 14): string {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789';
  const buf = new Uint32Array(len);
  crypto.getRandomValues(buf);
  let out = '';
  for (const n of buf) out += chars[n % chars.length];
  return out;
}

/** Dosya boyutu: "512 B", "12,3 KB", "4,5 MB". */
export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toLocaleString('tr-TR', { maximumFractionDigits: 1 })} KB`;
  return `${(n / 1024 / 1024).toLocaleString('tr-TR', { maximumFractionDigits: 1 })} MB`;
}

// Sunucu takibi ------------------------------------------------------------------------

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/** Bayt (1024 tabanlı): "512 B", "12,3 KB", "7,8 GB", "1,2 TB". */
export function fmtBytes(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return '—';
  let v = Math.max(0, n);
  let i = 0;
  while (v >= 1024 && i < BYTE_UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  if (i === 0) return `${Math.round(v)} B`;
  return `${v.toLocaleString('tr-TR', { maximumFractionDigits: v < 100 ? 1 : 0 })} ${BYTE_UNITS[i]}`;
}

/** Hız: "1,2 MB/sn". */
export function fmtRate(bps: number | null | undefined): string {
  if (bps === null || bps === undefined || !Number.isFinite(bps)) return '—';
  return `${fmtBytes(bps)}/sn`;
}

/** Tam sayı yüzde: "%42". */
export function fmtPctInt(v: number | null | undefined): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—';
  return '%' + Math.round(v);
}

/** Sabit ondalıklı sayı (virgüllü): fmtDec(0.4213, 2) → "0,42". */
export function fmtDec(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—';
  return v.toLocaleString('tr-TR', { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

/** Sıcaklık: "48 °C". */
export function fmtTemp(c: number | null | undefined): string {
  if (c === null || c === undefined || !Number.isFinite(c)) return '—';
  return `${Math.round(c)} °C`;
}

/** Çalışma süresi: "12 gün 4 sa", "3 sa 20 dk". */
export function fmtUptime(sec: number | null | undefined): string {
  if (!sec || sec <= 0) return '—';
  return fmtDuration(sec);
}
