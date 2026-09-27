// Bakım pencereleri için etiketler ve özet metinleri.

import type { Maintenance, MaintStatus, MaintStrategy } from './api';
import { fmtDate, fmtTime, fmtDay } from './format';

export const STRATEGY_LABELS: Record<MaintStrategy, string> = {
  manual: 'Elle',
  once: 'Tek seferlik',
  recurring_weekly: 'Haftalık tekrar',
  recurring_daily: 'Günlük tekrar',
  cron: 'Cron',
};

export const STRATEGY_DESCS: Record<MaintStrategy, string> = {
  manual: 'Siz durdurana kadar sürer',
  once: 'Belirli bir başlangıç ve bitiş',
  recurring_weekly: 'Seçilen günlerde, aynı saatlerde',
  recurring_daily: 'Her gün aynı saatlerde',
  cron: 'Cron ifadesi ve süre',
};

export const MAINT_STATUS: Record<MaintStatus, { l: string; c: string }> = {
  active: { l: 'Bakımda', c: 'maint' },
  scheduled: { l: 'Planlandı', c: 'accent' },
  ended: { l: 'Bitti', c: '' },
  inactive: { l: 'Durduruldu', c: 'paused' },
};

/** Haftanın günleri: Pazartesi'den başlayarak (sunucu: 0 = Pazar … 6 = Cumartesi). */
export const WEEKDAYS: { v: number; short: string; long: string }[] = [
  { v: 1, short: 'Pzt', long: 'Pazartesi' },
  { v: 2, short: 'Sal', long: 'Salı' },
  { v: 3, short: 'Çar', long: 'Çarşamba' },
  { v: 4, short: 'Per', long: 'Perşembe' },
  { v: 5, short: 'Cum', long: 'Cuma' },
  { v: 6, short: 'Cmt', long: 'Cumartesi' },
  { v: 0, short: 'Paz', long: 'Pazar' },
];

export const DEFAULT_TZ = 'Europe/Istanbul';

/** "2026-09-27T14:30" → "27.09.2026 14:30" (pencerenin kendi saat diliminde). */
function localText(v: string): string {
  const m = v.match(/^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}:\d{2}))?$/);
  if (!m) return v;
  return `${m[3]}.${m[2]}.${m[1]}${m[4] ? ' ' + m[4] : ''}`;
}

/** Zamanlamanın tek satırlık özeti. */
export function scheduleText(m: Maintenance): string {
  let s: string;
  switch (m.strategy) {
    case 'manual':
      s = 'Elle başlatılır ve durdurulur';
      break;
    case 'once':
      s = `${localText(m.start)} – ${localText(m.end)}`;
      break;
    case 'recurring_weekly': {
      const days = WEEKDAYS.filter((d) => m.weekdays.includes(d.v)).map((d) => d.short);
      s = `${days.length === 7 ? 'Her gün' : days.join(', ')} · ${m.start_time}–${m.end_time}`;
      break;
    }
    case 'recurring_daily':
      s = `Her gün · ${m.start_time}–${m.end_time}`;
      break;
    case 'cron':
      s = `${m.cron} · ${m.duration_minutes} dk`;
      break;
    default:
      s = '';
  }
  if (m.strategy !== 'manual' && m.strategy !== 'once' && (m.date_from || m.date_to)) {
    s += ` · ${m.date_from ? localText(m.date_from) : '…'} – ${m.date_to ? localText(m.date_to) : '…'}`;
  }
  if (m.timezone && m.timezone !== DEFAULT_TZ) s += ` (${m.timezone})`;
  return s;
}

/** Şu anki veya sonraki pencere hakkında kısa metin. */
export function nextText(m: Maintenance): string {
  if (m.status === 'inactive') return 'Durduruldu; zamanlama çalışmıyor';
  if (m.status === 'ended') return 'Tüm pencereler geçti';
  if (m.status === 'active') {
    if (m.strategy === 'manual' || !m.next_end) return 'Siz durdurana kadar sürüyor';
    return `${fmtDate(m.next_end)} tarihinde bitecek`;
  }
  if (m.next_start) {
    const sameDay = m.next_end && fmtDay(m.next_start) === fmtDay(m.next_end);
    return `Sonraki: ${fmtDate(m.next_start)}${m.next_end ? ` – ${sameDay ? fmtTime(m.next_end) : fmtDate(m.next_end)}` : ''}`;
  }
  return '';
}

/** Durum sırası: bakımda → planlı → durdurulmuş → bitmiş. */
export function maintRank(m: Maintenance): number {
  return { active: 0, scheduled: 1, inactive: 2, ended: 3 }[m.status] ?? 4;
}
