// Sütun sanallaştırma: görünüm listesi düz öğelere iner, her öğenin yüksekliği hesaptan bellidir
// (DOM ölçümü yok). Satır yükseklikleri buradan CSS değişkeni olarak verilir, tek kaynak burası.
// Böylece seçimde yalnız görünen ~50 satır çizilir; çizgi uçları da DOM'a bakmadan konum alır.
import { hasAlt, type GroupView, type St, type TopoNode } from './topomodel';

export interface Metrics {
  row: number;
  alt: number;
  head: number;
  /** "N diğer" satırının metin kısmı. */
  rest: number;
}
export const METRICS: Record<'wide' | 'tab' | 'phone', Metrics> = {
  wide: { row: 27, alt: 41, head: 28, rest: 32 },
  tab: { row: 38, alt: 41, head: 38, rest: 32 },
  phone: { row: 44, alt: 44, head: 44, rest: 44 },
};

export type Item =
  | { k: 'g'; key: string; v: GroupView; first: boolean }
  | { k: 'r'; key: string; n: TopoNode }
  | { k: 'c'; key: string; v: GroupView; h: number; w: number }
  | { k: 'x'; key: string; v: GroupView; h: number; w: number };

export interface VList {
  items: Item[];
  /** items.length + 1 eleman; son eleman toplam yükseklik. */
  tops: number[];
  index: Map<string, number>;
}

const CELL = 10; // 8 px kare + 2 px boşluk
const cellsH = (n: number, width: number) => {
  const per = Math.max(1, Math.floor((width + 2) / CELL));
  return Math.ceil(n / per) * CELL - 2;
};

/** Hücreler tek SVG: durum başına bir yol. 1000 hücre DOM'da 4 eleman, yerleşim maliyeti yok. */
export function cellSvg(cells: St[], width: number): string {
  const per = Math.max(1, Math.floor((width + 2) / CELL));
  const d: Partial<Record<St, string>> = {};
  cells.forEach((st, i) => {
    d[st] = (d[st] ?? '') + `M${(i % per) * CELL + 0.75} ${Math.floor(i / per) * CELL + 0.75}h6.5v6.5h-6.5z`;
  });
  let out = `<svg width="${per * CELL - 2}" height="${cellsH(cells.length, width)}" aria-hidden="true">`;
  for (const st of ['ok', 'off', 'warn', 'bad'] as St[]) if (d[st]) out += `<path class="${st}" d="${d[st]}"/>`;
  return out + '</svg>';
}

export function layoutCol(views: GroupView[], m: Metrics, width: number): VList {
  const items: Item[] = [];
  const tops: number[] = [];
  const index = new Map<string, number>();
  let y = 0;
  const push = (it: Item, h: number) => {
    index.set(it.key, items.length);
    items.push(it);
    tops.push(y);
    y += h;
  };
  views.forEach((v, i) => {
    push({ k: 'g', key: v.id, v, first: i === 0 }, m.head);
    for (const n of v.rows) push({ k: 'r', key: `o-${n.id}`, n }, hasAlt(n) ? m.alt : m.row);
    if (v.rest) {
      const h = Math.max(m.rest, 32) + cellsH(v.rest, width - 40);
      push({ k: 'x', key: `x-${v.id}`, v, h, w: width - 40 }, h);
    } else if (!v.open) {
      const h = cellsH(v.size, width - 52) + 7;
      push({ k: 'c', key: `c-${v.id}`, v, h, w: width - 52 }, h);
    }
  });
  tops.push(y);
  return { items, tops, index };
}

/** tops içinde y'yi kapsayan öğe. */
function bisect(tops: number[], y: number): number {
  let a = 0;
  let b = tops.length - 1;
  while (a < b) {
    const c = (a + b) >> 1;
    if (tops[c + 1] <= y) a = c + 1;
    else b = c;
  }
  return a;
}

const OVER = 240;
/** Görünen öğe aralığı [a, b): liste kısalınca üst sınır toplama kırpılır. */
export function range(l: VList, top: number, height: number): [number, number] {
  const n = l.items.length;
  if (!n) return [0, 0];
  const total = l.tops[n];
  const t = Math.min(top, Math.max(0, total - height));
  return [bisect(l.tops, Math.max(0, t - OVER)), Math.min(n, bisect(l.tops, t + height + OVER) + 1)];
}
