// Durum sayfası ızgarasının sütun ve çubuk sayısı kuralları (StatusView).
// Saf fonksiyonlar: node --test ile birim testi (statusgrid.test.ts).

/** Otomatik sütunda (columns = 0) eşikler: 760 px'ten iki, 1400 px'ten üç sütun. */
export const GRID_MIN = 760;
export const GRID3_MIN = 1400;
export const GRID_GAP = 18;
/** Seçilen sütun sayısında bir kartın en dar hali; daha darsa sütun azalır. */
export const CARD_MIN = 250;
/** Bu genişliğin altında (telefon) her zaman tek sütun. */
export const ONE_COL_MAX = 520;
export const MAX_COLUMNS = 6;

/** Sütun ayarını 0 (otomatik) veya 1-6 tamsayısına çeker. */
export function normalizeColumns(c: unknown): number {
  return typeof c === 'number' && Number.isInteger(c) && c >= 1 && c <= MAX_COLUMNS ? c : 0;
}

/**
 * Izgaradaki grup sütunu sayısı. columns 0 ise eski davranış (genişliğe göre
 * 1-3); seçiliyse en çok o kadar, ama kart CARD_MIN'den daralmaz ve dar
 * ekranda tek sütun. Grup sayısından fazla sütun açılmaz.
 */
export function gridColumnCount(columns: number, groups: number, width: number): number {
  let n: number;
  if (!columns) n = width >= GRID3_MIN ? 3 : width >= GRID_MIN ? 2 : 1;
  else if (width < ONE_COL_MAX) n = 1;
  else n = Math.min(columns, Math.floor((width + GRID_GAP) / (CARD_MIN + GRID_GAP)));
  return Math.max(1, Math.min(groups, n));
}

/** Bir sütunun genişliği. */
export function columnWidth(width: number, cols: number): number {
  return (width - GRID_GAP * (cols - 1)) / cols;
}

/** Kart genişliğine göre çubuk sayısı (24 saat görünümü her zaman 24). */
export function barCount(range: 'recent' | '24h' | '90d', colW: number): number {
  if (range === '24h') return 24;
  if (colW < 300) return 20;
  if (range === 'recent') return colW >= 560 ? 60 : colW >= 420 ? 45 : 30;
  return colW >= 560 ? 90 : colW >= 420 ? 60 : 30;
}
