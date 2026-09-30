// Biçimlendirme yardımcıları. Zamanlar unix saniyesidir. Hepsi geçerli dili
// (i18n.locale) okur: bileşende çağrıldıklarında dil değişince yeniden çizilir.
// Durum sayfası gövdesi (StatusView) sayfanın kendi dilini kullanır: tarih,
// süre ve yüzde yardımcıları isteğe bağlı bir dil (l) alır.

import { STATUS_DOWN, STATUS_MAINTENANCE, STATUS_PENDING, STATUS_UP, type Bucket, type MonitorView } from './api';
import { i18n, intlLocale, t, tIn, type Locale, type TKey, type TParams } from './i18n';

export const nowSec = () => Math.floor(Date.now() / 1000);

// Süre birimleri: tr "5 sn / 12 dk / 2 sa / 3 gün", en "5s / 12m / 2h / 3d".
const tl = (l: Locale | undefined, key: TKey, params?: TParams) => (l ? tIn(l, key, params) : t(key, params));
const uSec = (n: number, l?: Locale) => tl(l, 'status.time.sec', { n });
const uMin = (n: number, l?: Locale) => tl(l, 'status.time.min', { n });
const uHour = (n: number, l?: Locale) => tl(l, 'status.time.hour', { n });
const uDay = (n: number, l?: Locale) => tl(l, 'status.time.day', { n });

/** tr "45 sn", "12 dk", "2 sa 5 dk", "3 gün 4 sa"; en "45s", "12m", "2h 5m", "3d 4h" */
export function fmtDuration(sec: number, l?: Locale): string {
  sec = Math.max(0, Math.floor(sec));
  if (sec < 60) return uSec(sec, l);
  const min = Math.floor(sec / 60);
  if (min < 60) {
    // Bir saatin altında saniye de gösterilir: "3 dk 12 sn".
    const s = sec % 60;
    return s ? `${uMin(min, l)} ${uSec(s, l)}` : uMin(min, l);
  }
  const h = Math.floor(min / 60);
  if (h < 24) {
    const m = min % 60;
    return m ? `${uHour(h, l)} ${uMin(m, l)}` : uHour(h, l);
  }
  const d = Math.floor(h / 24);
  const hh = h % 24;
  return hh ? `${uDay(d, l)} ${uHour(hh, l)}` : uDay(d, l);
}

/** En büyük birimle kısa süre: "3 dk", "2 sa", "5 gün" (en: "3m", "2h", "5d"). */
export function fmtDurationShort(sec: number): string {
  sec = Math.max(0, Math.floor(sec));
  if (sec < 60) return uSec(sec);
  if (sec < 3600) return uMin(Math.floor(sec / 60));
  if (sec < 86400) return uHour(Math.floor(sec / 3600));
  return uDay(Math.floor(sec / 86400));
}

/** "12 sn önce", "1 dk 12 sn önce", "2 sa 5 dk önce" (en: "1m 12s ago"). */
export function fmtRelative(ts: number, now: number): string {
  if (!ts) return '—';
  const diff = now - ts;
  if (diff < 1) return t('status.time.justNow');
  return t('status.time.ago', { d: fmtDuration(diff) });
}

// Sunucu günlük özetleri İstanbul saatine göre kovalar; tarihler de aynı dilimde
// gösterilsin ki başka dilimdeki bir tarayıcıda 90 günlük çubuklar kaymasın.
const TZ = 'Europe/Istanbul';

// Dil başına biçimleyiciler (bir kez oluşturulur). tr: "27.09.2026 14:30";
// en: "Sep 27, 2026, 14:30" (ay adıyla: gün/ay sırası karışmaz; saat 24 saatlik).
interface Fmts {
  date: Intl.DateTimeFormat;
  dateSec: Intl.DateTimeFormat;
  dateOnly: Intl.DateTimeFormat;
  shortDate: Intl.DateTimeFormat;
  time: Intl.DateTimeFormat;
  timeSec: Intl.DateTimeFormat;
  num: Intl.NumberFormat;
}
const fmtCache: Partial<Record<Locale, Fmts>> = {};

function makeFmts(l: Locale): Fmts {
  const tag = intlLocale(l);
  const hm: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: TZ };
  const day: Intl.DateTimeFormatOptions =
    l === 'tr' ? { day: '2-digit', month: '2-digit', year: 'numeric' } : { day: 'numeric', month: 'short', year: 'numeric' };
  const dm: Intl.DateTimeFormatOptions = l === 'tr' ? { day: '2-digit', month: '2-digit' } : { day: 'numeric', month: 'short' };
  return {
    date: new Intl.DateTimeFormat(tag, { ...day, ...hm }),
    dateSec: new Intl.DateTimeFormat(tag, { ...day, ...hm, second: '2-digit' }),
    dateOnly: new Intl.DateTimeFormat(tag, { ...day, timeZone: TZ }),
    shortDate: new Intl.DateTimeFormat(tag, { ...dm, timeZone: TZ }),
    time: new Intl.DateTimeFormat(tag, hm),
    timeSec: new Intl.DateTimeFormat(tag, { ...hm, second: '2-digit' }),
    num: new Intl.NumberFormat(tag),
  };
}

/** Geçerli dilin biçimleyicileri (i18n.locale okunur → reaktif). */
function fmts(l: Locale = i18n.locale): Fmts {
  return (fmtCache[l] ??= makeFmts(l));
}

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

/** tr dd.MM.yyyy HH:mm, en "Sep 27, 2026, 14:30" */
export function fmtDate(ts: number, l?: Locale): string {
  if (!ts) return '—';
  return fmts(l).date.format(new Date(ts * 1000));
}
/** tr dd.MM.yyyy, en "Sep 27, 2026" */
export function fmtDay(ts: number, l?: Locale): string {
  return fmts(l).dateOnly.format(new Date(ts * 1000));
}
/**
 * Saatin okunuşuna göre bulunma eki: "14:30" → "’da", "09:15" → "’te", "12:00" → "’de".
 * Son okunan sayı dakika (00 ise saat) belirler. YALNIZCA Türkçe: İngilizcede ""
 * döner (cümleyi dile göre ayrı bir anahtarla kurun, ör. "at {time}").
 */
export function timeLocative(ts: number): string {
  if (i18n.locale !== 'tr') return '';
  const [h, m] = fmts('tr').time.format(new Date(ts * 1000)).split(':').map(Number);
  const n = m || h;
  const ONES = ['da', 'de', 'de', 'te', 'te', 'te', 'da', 'de', 'de', 'da']; // sıfır/bir/iki/üç/dört/beş/altı/yedi/sekiz/dokuz
  const TENS = ['da', 'da', 'de', 'da', 'ta', 'de']; // sıfır/on/yirmi/otuz/kırk/elli
  return '’' + (n % 10 ? ONES[n % 10] : TENS[Math.floor(n / 10)] ?? 'da');
}
/** tr dd.MM, en "Sep 27" */
export function fmtShortDate(ts: number): string {
  return fmts().shortDate.format(new Date(ts * 1000));
}
/** HH:mm */
export function fmtTime(ts: number, l?: Locale): string {
  return fmts(l).time.format(new Date(ts * 1000));
}
/** HH:mm:ss */
export function fmtTimeSec(ts: number, l?: Locale): string {
  return fmts(l).timeSec.format(new Date(ts * 1000));
}
/** tr dd.MM.yyyy HH:mm:ss, en "Sep 27, 2026, 14:30:05" */
export function fmtDateSec(ts: number): string {
  if (!ts) return '—';
  return fmts().dateSec.format(new Date(ts * 1000));
}

/** Saniyeye kadar süre: "12 sn", "5 dk 12 sn", "2 sa 5 dk 12 sn", "3 gün 2 sa 5 dk" (en: "5m 12s"). */
export function fmtDurationLong(sec: number): string {
  sec = Math.max(0, Math.floor(sec));
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  const parts: string[] = [];
  if (d) parts.push(uDay(d));
  if (h || (d && m)) parts.push(uHour(h));
  if (m || ((d || h) && !d)) parts.push(uMin(m));
  if (!d) parts.push(uSec(s));
  return parts.join(' ');
}

/** Yüzde işaretini dile göre koyar: tr "%42", en "42%". */
function pctSign(n: string, l: Locale = i18n.locale): string {
  return l === 'tr' ? '%' + n : n + '%';
}

/** tr %99,95 / en 99.95% — tam 100 ise %100; yuvarlama asla yukarı doğru yapılmaz. */
export function fmtPct(v: number | null | undefined, l: Locale = i18n.locale): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—';
  if (v >= 100) return pctSign('100', l);
  const floored = (Math.floor(v * 100) / 100).toFixed(2);
  return pctSign(l === 'tr' ? floored.replace('.', ',') : floored, l);
}

/** Binlik ayraçlı sayı: tr 1.234, en 1,234 */
export function fmtNum(n: number): string {
  return fmts().num.format(n);
}

/** "123 ms"; -1 → "—" */
export function fmtMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || ms < 0) return '—';
  return `${fmts().num.format(Math.round(ms))} ms`;
}

/** Kontrol aralığı: "30 sn", "5 dk", "1 sa", "1 dk 30 sn" (en: "30s", "5m", "1h", "1m 30s") */
export function fmtInterval(sec: number): string {
  if (sec >= 3600 && sec % 3600 === 0) return uHour(sec / 3600);
  if (sec >= 60 && sec % 60 === 0) return uMin(sec / 60);
  if (sec > 60) return `${uMin(Math.floor(sec / 60))} ${uSec(sec % 60)}`;
  return uSec(sec);
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

/** Durum adları. Getter: her okumada geçerli dilde döner, STATUS_LABELS[kind] reaktiftir. */
export const STATUS_LABELS: Record<StatusKind, string> = {
  get up() {
    return t('status.up');
  },
  get down() {
    return t('status.down');
  },
  get pending() {
    return t('status.pending');
  },
  get paused() {
    return t('status.paused');
  },
  get maintenance() {
    return t('status.maintenance');
  },
};

export function pointStatusLabel(s: number): string {
  if (s === STATUS_UP) return t('status.up');
  if (s === STATUS_DOWN) return t('status.down');
  if (s === STATUS_PENDING) return t('status.retrying');
  if (s === STATUS_MAINTENANCE) return t('status.maintenance');
  return t('status.unknown');
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
export function hourRange(t: number, l?: Locale): string {
  return `${fmtTime(t, l)} – ${fmtTime(t + 3600, l)}`;
}

/** Karşılaştırma için Türkçe küçük harf (veriler Türkçe olabilir; arayüz dilinden bağımsız). */
export function lower(s: string): string {
  return s.toLocaleLowerCase('tr');
}

/** Sıralama (veriler Türkçe olabilir; arayüz dilinden bağımsız). */
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

/** Dosya boyutu: "512 B", "12,3 KB", "4,5 MB" (en: "12.3 KB"). */
export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toLocaleString(intlLocale(), { maximumFractionDigits: 1 })} KB`;
  return `${(n / 1024 / 1024).toLocaleString(intlLocale(), { maximumFractionDigits: 1 })} MB`;
}

// Sunucu takibi ------------------------------------------------------------------------

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/** Bayt (1024 tabanlı): "512 B", "12,3 KB", "7,8 GB", "1,2 TB" (en: "7.8 GB"). */
export function fmtBytes(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return '—';
  let v = Math.max(0, n);
  let i = 0;
  // 1000'i geçen değer bir üst birime geçer: "1.023 KB" yerine "1 MB".
  while (v >= 1000 && i < BYTE_UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  if (i === 0) return `${Math.round(v)} B`;
  return `${v.toLocaleString(intlLocale(), { maximumFractionDigits: v < 100 ? 1 : 0 })} ${BYTE_UNITS[i]}`;
}

/** Hız: "1,2 MB/sn" (en: "1.2 MB/s"). */
export function fmtRate(bps: number | null | undefined): string {
  if (bps === null || bps === undefined || !Number.isFinite(bps)) return '—';
  return `${fmtBytes(bps)}${t('status.time.perSec')}`;
}

/** Uptime oranının tonu; tüm ekranlarda aynı eşikler: %99,9 ve üstü iyi,
 *  %90–99,9 uyarı (turuncu: kesinti olmuş ama "şu an çalışmıyor" değil),
 *  %90 altı kötü (kırmızı). */
export function uptimeTone(v: number | null | undefined): 'good' | 'warn' | 'bad' | 'none' {
  if (v === null || v === undefined || !Number.isFinite(v)) return 'none';
  return v >= 99.9 ? 'good' : v >= 90 ? 'warn' : 'bad';
}

/** Tam sayı yüzde: "%42" (en: "42%"). */
export function fmtPctInt(v: number | null | undefined): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—';
  return pctSign(String(Math.round(v)));
}

/** Sabit ondalıklı sayı: fmtDec(0.4213, 2) → tr "0,42", en "0.42". */
export function fmtDec(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—';
  return v.toLocaleString(intlLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

/** Sıcaklık: "48 °C". */
export function fmtTemp(c: number | null | undefined): string {
  if (c === null || c === undefined || !Number.isFinite(c)) return '—';
  return `${Math.round(c)} °C`;
}

/** Çalışma süresi: "12 gün 4 sa", "3 sa 20 dk" (en: "12d 4h"). */
export function fmtUptime(sec: number | null | undefined): string {
  if (!sec || sec <= 0) return '—';
  return fmtDuration(sec);
}
