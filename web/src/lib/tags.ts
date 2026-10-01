// Etiket renkleri: hazır palet ve zemin rengine göre okunaklı yazı rengi.

import type { MonitorTag } from './api';

/** Etiket iletişim penceresindeki hazır renkler (mor/çivit yok). */
export const TAG_COLORS = [
  '#64748b', // arduvaz (sunucu varsayılanı)
  '#ef4444',
  '#f97316',
  '#f59e0b',
  '#eab308',
  '#84cc16',
  '#22c55e',
  '#14b8a6',
  '#06b6d4',
  '#0ea5e9',
  '#2563eb',
  '#ec4899',
  '#a16207',
  '#1e293b',
  '#e2e8f0',
];

export const DEFAULT_TAG_COLOR = '#64748b';

const HEX_RE = /^#[0-9a-f]{6}$/i;

export const isHexColor = (c: string) => HEX_RE.test(c);

function luminance(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  const ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
}

const ratio = (a: number, b: number) => (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);

// app.css'teki --tag-ink-dark (#0b1220) ve --tag-ink-light (#ffffff) tonlarının parlaklığı.
const DARK_INK = luminance('#0b1220');
const LIGHT_INK = 1;

/**
 * Zemin rengine göre yazı tonu: koyu veya açık yazıdan hangisi daha yüksek
 * kontrast veriyorsa o (WCAG oranı). Geçersiz renkte açık yazı.
 */
export function tagInk(color: string): 'dark' | 'light' {
  if (!HEX_RE.test(color)) return 'light';
  const l = luminance(color);
  return ratio(l, DARK_INK) >= ratio(l, LIGHT_INK) ? 'dark' : 'light';
}

const MIN_CHIP_CONTRAST = 4.5;

function mix(hex: string, towards: string, amount: number): string {
  const a = parseInt(hex.slice(1), 16);
  const b = parseInt(towards.slice(1), 16);
  const ch = (shift: number) => {
    const x = (a >> shift) & 255;
    const y = (b >> shift) & 255;
    return Math.round(x + (y - x) * amount);
  };
  return '#' + [ch(16), ch(8), ch(0)].map((v) => v.toString(16).padStart(2, '0')).join('');
}

/**
 * Etiket çipinin zemini ve yazı tonu. Seçilen renk hiçbir yazı tonuyla 4,5:1
 * kontrastı vermiyorsa (ör. orta parlaklıktaki mor/turuncu) zemin, yazı
 * okunaklı olana kadar hafifçe açılır ya da koyulaştırılır; renk tanınır kalır.
 */
export function tagSurface(color: string): { bg: string; ink: 'dark' | 'light' } {
  if (!HEX_RE.test(color)) return { bg: DEFAULT_TAG_COLOR, ink: 'light' };
  let bg = color;
  let ink = tagInk(bg);
  for (let i = 0; i < 6; i++) {
    const l = luminance(bg);
    if (ratio(l, ink === 'dark' ? DARK_INK : LIGHT_INK) >= MIN_CHIP_CONTRAST) break;
    bg = mix(bg, ink === 'dark' ? '#ffffff' : '#000000', 0.12);
    ink = tagInk(bg);
  }
  return { bg, ink };
}

/** "ad: değer" veya yalnızca ad. */
export const tagText = (t: Pick<MonitorTag, 'name' | 'value'>) => (t.value ? `${t.name}: ${t.value}` : t.name);
