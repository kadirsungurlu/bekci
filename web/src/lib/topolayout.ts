// Canlı topoloji tuvalinin yerleşimi (saf hesap; pages/Topology -> components/TopoCanvas).
// Genişliğe göre gruplar ve kartlar konumlanır; kartlar ızgarada olduğundan üst üste binmez.
// Üst sıra: Monitörler + Bildirim kanalları; orta: Bekci paneli; alt: Kontrol noktaları + Sunucular.
// Bir grupta tek sıra kart varsa her karta ayrı bağlantı çizilir; birden çok sırada (ya da
// gizli kart varsa) gruba tek, toplu bağlantı çizilir ki çizgiler kartların üstünden geçmesin.

export type GroupKey = 'monitors' | 'channels' | 'probes' | 'servers';

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface GroupInput {
  key: GroupKey;
  /** Sıralı öğe kimlikleri (sorunlular önce). */
  ids: number[];
  expanded: boolean;
  /** Kapalıyken gösterilecek en çok sıra. */
  maxRows: number;
  /** Kontrol noktaları: mesh yayları çizilecekse true (2-10 nokta); sığmazsa matrise düşülür. */
  mesh?: boolean;
}

export interface CardPos extends Rect {
  id: number;
}

export interface GroupLayout {
  key: GroupKey;
  box: Rect;
  cards: CardPos[];
  /** "N tane daha" / "daha az" kutusu (yoksa null). */
  more: (Rect & { hidden: number; expanded: boolean }) | null;
  /** Kart başına bağlantı çizilir mi (tek sıra ve gizli kart yok). */
  perCard: boolean;
  /** Mesh yayları bu grupta çiziliyor mu. */
  meshArcs: boolean;
  /** Mesh istenmişti ama sığmadı (matrise bakılır). */
  meshFallback: boolean;
  empty: boolean;
}

export interface TopoLayout {
  width: number;
  height: number;
  hub: Rect;
  groups: Partial<Record<GroupKey, GroupLayout>>;
}

export const PAD = 14; // grup iç boşluğu
export const GAP = 10; // kartlar arası
export const HEAD = 38; // grup başlığı
export const CARD_H = 58;
export const CARD_MIN_W = 168;
export const MESH_CARD_MIN_W = 124;
export const MESH_MAX = 10;
export const ARC_H = 92; // mesh yaylarına ayrılan yükseklik
export const EMPTY_H = 108;
export const ROW_GAP = 76; // gruplar ile panel kartı arası (bağlantılar burada)
export const HUB_W = 300;
export const HUB_H = 86;
export const GROUP_GAP = 16;

/** Tuvalin kullanılabileceği en dar genişlik (altında liste görünümü). */
export const MIN_CANVAS_W = 640;

function grid(gw: number, minW: number): { cols: number; cw: number } {
  const inner = gw - 2 * PAD;
  const cols = Math.max(1, Math.floor((inner + GAP) / (minW + GAP)));
  return { cols, cw: (inner - (cols - 1) * GAP) / cols };
}

/** Grubun iç yerleşimi; x,y grup kutusunun sol üstü, yükseklik içerikten. */
function layoutGroup(g: GroupInput, x: number, y: number, gw: number): GroupLayout {
  const out: GroupLayout = {
    key: g.key,
    box: { x, y, w: gw, h: 0 },
    cards: [],
    more: null,
    perCard: false,
    meshArcs: false,
    meshFallback: false,
    empty: g.ids.length === 0,
  };
  if (out.empty) {
    out.box.h = HEAD + EMPTY_H;
    return out;
  }
  const top = y + HEAD;
  if (g.mesh && g.ids.length >= 2) {
    const m = grid(gw, MESH_CARD_MIN_W);
    if (g.ids.length <= MESH_MAX && g.ids.length <= m.cols) {
      // Tek sıra; kartlar ortalanır, altta yaylar.
      const n = g.ids.length;
      const cw = Math.min(m.cw * (m.cols / n), 240, (gw - 2 * PAD - (n - 1) * GAP) / n);
      const total = n * cw + (n - 1) * GAP;
      const x0 = x + (gw - total) / 2;
      out.cards = g.ids.map((id, i) => ({ id, x: x0 + i * (cw + GAP), y: top, w: cw, h: CARD_H }));
      out.perCard = true;
      out.meshArcs = true;
      out.box.h = HEAD + CARD_H + ARC_H + PAD;
      return out;
    }
    out.meshFallback = true;
  }
  const { cols, cw } = grid(gw, CARD_MIN_W);
  const cap = cols * Math.max(1, g.maxRows);
  let shown = g.ids;
  let hidden = 0;
  if (g.ids.length > cap) {
    if (!g.expanded) {
      shown = g.ids.slice(0, cap - 1);
      hidden = g.ids.length - shown.length;
    }
  }
  const slots = shown.length + (g.ids.length > cap ? 1 : 0);
  const pos = (i: number): Rect => ({
    x: x + PAD + (i % cols) * (cw + GAP),
    y: top + Math.floor(i / cols) * (CARD_H + GAP),
    w: cw,
    h: CARD_H,
  });
  out.cards = shown.map((id, i) => ({ id, ...pos(i) }));
  if (g.ids.length > cap) out.more = { ...pos(shown.length), hidden, expanded: g.expanded };
  const rows = Math.ceil(slots / cols);
  out.perCard = rows === 1 && hidden === 0;
  out.box.h = HEAD + rows * CARD_H + (rows - 1) * GAP + PAD;
  return out;
}

/** İki grubu yan yana koyar (biri yoksa diğeri tam genişlik); genişlik payı öğe sayısına göre. */
function pair(a: GroupInput | null, b: GroupInput | null, y: number, width: number, minShare: number) {
  const res: GroupLayout[] = [];
  if (a && b) {
    const na = Math.max(1, a.ids.length);
    const nb = Math.max(1, b.ids.length);
    let share = na / (na + nb);
    share = Math.min(1 - minShare, Math.max(minShare, share));
    let wa = Math.round((width - GROUP_GAP) * share);
    // Mesh yayları tek sıra ister: sığabiliyorsa noktalara yer açılır (diğer gruba en az 260 px kalır).
    if (a.mesh && a.ids.length >= 2 && a.ids.length <= MESH_MAX) {
      const need = a.ids.length * (MESH_CARD_MIN_W + GAP) - GAP + 2 * PAD;
      if (need > wa && need <= width - GROUP_GAP - 260) wa = need;
    }
    res.push(layoutGroup(a, 0, y, wa), layoutGroup(b, wa + GROUP_GAP, y, width - wa - GROUP_GAP));
  } else if (a || b) {
    res.push(layoutGroup((a ?? b)!, 0, y, width));
  }
  // Aynı sıradaki kutular aynı boyda.
  const h = Math.max(0, ...res.map((r) => r.box.h));
  for (const r of res) r.box.h = h;
  return { groups: res, h };
}

export function layoutTopology(
  width: number,
  input: { monitors: GroupInput; channels: GroupInput | null; probes: GroupInput; servers: GroupInput },
): TopoLayout {
  const w = Math.max(MIN_CANVAS_W, Math.floor(width));
  const topRow = pair(input.monitors, input.channels, 0, w, 0.34);
  const hubY = topRow.h + ROW_GAP;
  const hubW = Math.min(HUB_W, w - 2 * PAD);
  const hub: Rect = { x: (w - hubW) / 2, y: hubY, w: hubW, h: HUB_H };
  const botY = hubY + HUB_H + ROW_GAP;
  const botRow = pair(input.probes, input.servers, botY, w, 0.4);
  const groups: TopoLayout['groups'] = {};
  for (const g of [...topRow.groups, ...botRow.groups]) groups[g.key] = g;
  return { width: w, height: botY + botRow.h, hub, groups };
}

/** Kart ya da grup kenarından panel kartına dikey eğri; yön akış yönüdür (from -> to). */
export function vCurve(x1: number, y1: number, x2: number, y2: number): { d: string; mx: number; my: number } {
  const my = (y1 + y2) / 2;
  return { d: `M${x1},${y1} C${x1},${my} ${x2},${my} ${x2},${y2}`, mx: (x1 + x2) / 2, my };
}

/** Aynı sıradaki iki kart arasında, kartların altında yay. */
export function meshArc(a: Rect, b: Rect, span: number): { d: string; mx: number; my: number } {
  const x1 = a.x + a.w / 2;
  const x2 = b.x + b.w / 2;
  const y = a.y + a.h;
  const depth = Math.min(ARC_H - 8, 18 + span * 12);
  const cy = y + depth * 1.33; // kübik eğrinin tepesi ~0.75 * kontrol derinliği
  return { d: `M${x1},${y} C${x1},${cy} ${x2},${cy} ${x2},${y}`, mx: (x1 + x2) / 2, my: y + depth };
}

/** Testler için: iki dikdörtgen kesişiyor mu. */
export function overlaps(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}
