// Küçük i18n katmanı: t('ad.alani.anahtar', { param }) — bkz. README.md.
//
// Sözlükler ad alanı başına ayrı dosyadadır (tr/<ad>.ts, en/<ad>.ts). tr kaynak
// metindir; en aynı anahtarları taşımak ZORUNDADIR (eksik/fazla anahtar tip hatası).

import tr from './tr';
import en from './en';
import { i18n, type Locale } from './locale.svelte';

export {
  i18n,
  setLocale,
  forgetLocale,
  browserLocale,
  deviceLocale,
  isLocale,
  LOCALES,
  DEFAULT_LOCALE,
} from './locale.svelte';
export type { Locale } from './locale.svelte';

/** tr sözlüğünün biçimi: en sözlüğü bununla aynı anahtarları taşır. */
export type Dict = typeof tr;

/** "a.b.c" biçiminde tüm yaprak anahtarlar. */
type Leaves<T> = {
  [K in keyof T & string]: T[K] extends string ? K : `${K}.${Leaves<T[K]>}`;
}[keyof T & string];

export type TKey = Leaves<Dict>;
export type TParams = Record<string, string | number>;

const DICTS: Record<Locale, unknown> = { tr, en };

function lookup(dict: unknown, key: string): string | undefined {
  let cur: unknown = dict;
  for (const part of key.split('.')) {
    if (cur === null || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[part];
  }
  return typeof cur === 'string' ? cur : undefined;
}

const plurals: Partial<Record<Locale, Intl.PluralRules>> = {};

function format(locale: Locale, s: string, params?: TParams): string {
  if (!params) return s;
  // Çoğul: "tekil|çoğul" (yalnızca count verilirse). Türkçe metinler genelde tek biçimdir.
  if (typeof params.count === 'number' && s.includes('|')) {
    const forms = s.split('|');
    const rule = (plurals[locale] ??= new Intl.PluralRules(locale)).select(params.count);
    s = rule === 'one' ? forms[0] : forms[forms.length - 1];
  }
  return s.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m));
}

/** Verilen dilde çeviri (dil geçerli dilden bağımsız olmalıysa). */
export function tIn(locale: Locale, key: TKey, params?: TParams): string {
  const s = lookup(DICTS[locale], key) ?? lookup(DICTS.tr, key) ?? key;
  return format(locale, s, params);
}

/**
 * Geçerli dilde çeviri. Svelte bileşenlerinde doğrudan kullanılır:
 * {t('nav.monitors')}; dil değişince yeniden çizilir (i18n.locale okunur).
 * {name} biçimindeki yer tutucular params'tan doldurulur; "tekil|çoğul"
 * metinlerde params.count'a göre biçim seçilir.
 */
export function t(key: TKey, params?: TParams): string {
  return tIn(i18n.locale, key, params);
}

/**
 * Anahtarı çalışma anında oluşturulan (ör. `status.${kind}`) metinler için:
 * anahtar yoksa fallback döner. Tip denetimi yoktur; mümkünse t() kullanın.
 */
export function tOr(key: string, fallback: string, params?: TParams): string {
  const s = lookup(DICTS[i18n.locale], key) ?? lookup(DICTS.tr, key);
  return s === undefined ? fallback : format(i18n.locale, s, params);
}

export type TPart = { text: string; slot?: undefined } | { slot: string; text?: undefined };

/**
 * İçinde bağlantı, <code>, <b> gibi biçimli parçalar olan cümleler için:
 * params'ta OLMAYAN {ad} yer tutucuları "slot" parçası olarak döner, bileşen
 * onları kendisi çizer ({@html} gerekmez; kelime sırası dile göre değişebilir).
 *
 *   {#each tParts('settings.general.promAlt') as p}
 *     {#if p.slot === 'a'}<code>basic_auth</code>{:else if p.slot === 'b'}<code>…</code>{:else}{p.text}{/if}
 *   {/each}
 */
export function tParts(key: TKey, params?: TParams): TPart[] {
  const s = lookup(DICTS[i18n.locale], key) ?? lookup(DICTS.tr, key) ?? key;
  const out: TPart[] = [];
  let last = 0;
  for (const m of s.matchAll(/\{(\w+)\}/g)) {
    const name = m[1];
    if (params && name in params) continue;
    if (m.index > last) out.push({ text: format(i18n.locale, s.slice(last, m.index), params) });
    out.push({ slot: name });
    last = m.index + m[0].length;
  }
  if (last < s.length) out.push({ text: format(i18n.locale, s.slice(last), params) });
  return out;
}

/** Anahtar sözlükte var mı? */
export function hasKey(key: string): key is TKey {
  return lookup(DICTS.tr, key) !== undefined;
}

/** Geçerli dil (reaktif okuma). */
export const locale = (): Locale => i18n.locale;

/** Intl API'leri için BCP 47 etiketi: tr-TR / en-US. */
export const intlLocale = (l: Locale = i18n.locale) => (l === 'tr' ? 'tr-TR' : 'en-US');
