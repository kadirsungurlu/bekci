// Bakım pencereleri için etiketler ve özet metinleri. Etiket tabloları getter'dır:
// her okumada geçerli dilde döner (bkz. lib/i18n/README.md).

import type { Maintenance, MaintStatus, MaintStrategy } from './api';
import { fmtDate, fmtTime, fmtDay } from './format';
import { i18n, intlLocale, t } from './i18n';

const STRATEGIES: MaintStrategy[] = ['manual', 'once', 'recurring_weekly', 'recurring_daily', 'cron'];

function labels<K extends string>(keys: K[], get: (k: K) => string): Record<K, string> {
  const out = {} as Record<K, string>;
  for (const k of keys) Object.defineProperty(out, k, { get: () => get(k), enumerable: true });
  return out;
}

export const STRATEGY_LABELS: Record<MaintStrategy, string> = labels(STRATEGIES, (k) => t(`maintenance.strategy.${k}`));

export const STRATEGY_DESCS: Record<MaintStrategy, string> = labels(STRATEGIES, (k) => t(`maintenance.strategyDesc.${k}`));

const STATUS_CLASS: Record<MaintStatus, string> = { active: 'maint', scheduled: 'accent', ended: '', inactive: 'paused' };

export const MAINT_STATUS: Record<MaintStatus, { l: string; c: string }> = {
  active: {
    get l() {
      return t('maintenance.status.active');
    },
    c: STATUS_CLASS.active,
  },
  scheduled: {
    get l() {
      return t('maintenance.status.scheduled');
    },
    c: STATUS_CLASS.scheduled,
  },
  ended: {
    get l() {
      return t('maintenance.status.ended');
    },
    c: STATUS_CLASS.ended,
  },
  inactive: {
    get l() {
      return t('maintenance.status.inactive');
    },
    c: STATUS_CLASS.inactive,
  },
};

// Gün adları dile göre Intl'den (tr: "Pzt"/"Pazartesi", en: "Mon"/"Monday").
const dayNames: Record<string, { short: Intl.DateTimeFormat; long: Intl.DateTimeFormat }> = {};
function dayName(v: number, style: 'short' | 'long'): string {
  const tag = intlLocale();
  const f = (dayNames[tag] ??= {
    short: new Intl.DateTimeFormat(tag, { weekday: 'short', timeZone: 'UTC' }),
    long: new Intl.DateTimeFormat(tag, { weekday: 'long', timeZone: 'UTC' }),
  });
  // 7 Ocak 2024 Pazar'dır: v = 0 (Pazar) … 6 (Cumartesi).
  return f[style].format(new Date(Date.UTC(2024, 0, 7 + v)));
}

/** Haftanın günleri: Pazartesi'den başlayarak (sunucu: 0 = Pazar … 6 = Cumartesi). */
export const WEEKDAYS: { v: number; short: string; long: string }[] = [1, 2, 3, 4, 5, 6, 0].map((v) => ({
  v,
  get short() {
    return dayName(v, 'short');
  },
  get long() {
    return dayName(v, 'long');
  },
}));

export const DEFAULT_TZ = 'Europe/Istanbul';

const enLocal: Record<'date' | 'dateTime', Intl.DateTimeFormat> = {
  date: new Intl.DateTimeFormat('en-US', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' }),
  dateTime: new Intl.DateTimeFormat('en-US', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
    timeZone: 'UTC',
  }),
};

/** "2026-09-27T14:30" → tr "27.09.2026 14:30", en "Sep 27, 2026, 14:30" (pencerenin kendi saat diliminde). */
function localText(v: string): string {
  const m = v.match(/^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?$/);
  if (!m) return v;
  if (i18n.locale === 'tr') return `${m[3]}.${m[2]}.${m[1]}${m[4] ? ` ${m[4]}:${m[5]}` : ''}`;
  const d = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3], m[4] ? +m[4] : 0, m[5] ? +m[5] : 0));
  return (m[4] ? enLocal.dateTime : enLocal.date).format(d);
}

/** Zamanlamanın tek satırlık özeti. */
export function scheduleText(m: Maintenance): string {
  let s: string;
  switch (m.strategy) {
    case 'manual':
      s = t('maintenance.sched.manual');
      break;
    case 'once':
      s = `${localText(m.start)} – ${localText(m.end)}`;
      break;
    case 'recurring_weekly': {
      const days = WEEKDAYS.filter((d) => m.weekdays.includes(d.v)).map((d) => d.short);
      s = `${days.length === 7 ? t('maintenance.sched.everyDay') : days.join(', ')} · ${m.start_time}–${m.end_time}`;
      break;
    }
    case 'recurring_daily':
      s = `${t('maintenance.sched.everyDay')} · ${m.start_time}–${m.end_time}`;
      break;
    case 'cron':
      s = `${m.cron} · ${t('status.time.min', { n: m.duration_minutes })}`;
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
  if (m.status === 'inactive') return t('maintenance.next.inactive');
  if (m.status === 'ended') return t('maintenance.next.ended');
  if (m.status === 'active') {
    if (m.strategy === 'manual' || !m.next_end) return t('maintenance.next.untilStopped');
    return t('maintenance.next.endsAt', { date: fmtDate(m.next_end) });
  }
  if (m.next_start) {
    const sameDay = m.next_end && fmtDay(m.next_start) === fmtDay(m.next_end);
    const range = `${fmtDate(m.next_start)}${m.next_end ? ` – ${sameDay ? fmtTime(m.next_end) : fmtDate(m.next_end)}` : ''}`;
    return t('maintenance.next.next', { range });
  }
  return '';
}

/** Durum sırası: bakımda → planlı → durdurulmuş → bitmiş. */
export function maintRank(m: Maintenance): number {
  return { active: 0, scheduled: 1, inactive: 2, ended: 3 }[m.status] ?? 4;
}
