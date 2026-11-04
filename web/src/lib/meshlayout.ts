// Topoloji şeması yerleşimi: internal/mesh/topology.go aynası. İki taraf
// internal/mesh/testdata/layout.json fikstürüyle denetlenir
// (web/e2e/meshlayout.test.mjs). Formül değişirse ikisini birlikte değiştir.

export const BOX_W = 128;
export const BOX_H = 36;
export const PAD = 16;
export const SUMMARY_H = 40;
const MIN_R = 110;
const ARC = 160;

export interface LayoutSize {
  r: number;
  cx: number;
  cy: number;
  width: number;
  height: number;
}

export function layoutSize(n: number): LayoutSize {
  const r = Math.max(MIN_R, Math.round((n * ARC) / (2 * Math.PI)));
  const cx = n <= 2 ? PAD + BOX_W / 2 : PAD + BOX_W / 2 + r;
  const cy = PAD + BOX_H / 2 + r;
  return { r, cx, cy, width: 2 * cx, height: 2 * cy + SUMMARY_H };
}

/** Sıradaki i. düğümün merkezi; 0. (Main) tepede, diğerleri saat yönünde. */
export function layoutPoints(n: number): [number, number][] {
  const { r, cx, cy } = layoutSize(n);
  const pts: [number, number][] = [];
  for (let i = 0; i < n; i++) {
    const a = -Math.PI / 2 + (2 * Math.PI * i) / n;
    pts.push([Math.round(cx + r * Math.cos(a)), Math.round(cy + r * Math.sin(a))]);
  }
  return pts;
}

export type PairState = 'up' | 'down' | 'oneway' | 'unknown';

export interface MeshPairIn {
  a: number;
  b: number;
  state: string;
  ab: string;
  ba: string;
}

export interface TopoNode {
  id: number;
  x: number;
  y: number;
  main: boolean;
}

export interface TopoLink {
  a: number;
  b: number;
  state: PairState;
  /** Tek yönlüde true: a -> b çalışır; false: b -> a. */
  forward: boolean;
}

export interface Topology {
  width: number;
  height: number;
  nodes: TopoNode[];
  links: TopoLink[];
  summary: { up: number; down: number; oneway: number; unknown: number };
}

/** BuildTopology aynası: düğüm kimlikleri (Main = 0) ve çiftlerden şema. */
export function buildTopology(ids: number[], pairs: MeshPairIn[]): Topology {
  const sorted = [...ids].sort((x, y) => x - y);
  const { width, height } = layoutSize(sorted.length);
  const nodes = layoutPoints(sorted.length).map(([x, y], i) => ({ id: sorted[i], x, y, main: sorted[i] === 0 }));
  const byKey = new Map(pairs.map((p) => [`${Math.min(p.a, p.b)}-${Math.max(p.a, p.b)}`, p]));
  const links: TopoLink[] = [];
  const summary = { up: 0, down: 0, oneway: 0, unknown: 0 };
  for (let i = 0; i < sorted.length; i++)
    for (let j = i + 1; j < sorted.length; j++) {
      const a = sorted[i];
      const b = sorted[j];
      const p = byKey.get(`${a}-${b}`);
      let state: PairState = 'unknown';
      if (p && (p.state === 'up' || p.state === 'down' || p.state === 'oneway')) state = p.state;
      summary[state]++;
      // p.a < p.b sunucuda garanti; ab = a'nın b'yi ölçümü
      links.push({ a, b, state, forward: state === 'oneway' && p?.ab === 'up' });
    }
  return { width, height, nodes, links, summary };
}

export interface Segment {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

function distToSegment(px: number, py: number, s: Segment): number {
  const dx = s.x2 - s.x1;
  const dy = s.y2 - s.y1;
  const len2 = dx * dx + dy * dy;
  const k = len2 ? Math.max(0, Math.min(1, ((px - s.x1) * dx + (py - s.y1) * dy) / len2)) : 0;
  return Math.hypot(px - (s.x1 + dx * k), py - (s.y1 + dy * k));
}

// İşaret r=10; başka çizgi kenarına değmesin, iki işaret üst üste gelmesin.
export const MARK_CLEAR = 18;
export const MARK_GAP = 30;
const FRACTIONS = [0.5, 0.42, 0.58, 0.34, 0.66, 0.28, 0.72, 0.22, 0.78];

/**
 * Her çizginin durum işareti için nokta seçer: işaret kendi çizgisinde durur, başka çizginin
 * (kesişimin) üstüne ve başka işaretin üstüne düşmez. Yoğun şemada koşul sağlanamazsa en az
 * sıkışan nokta alınır. Şema PNG'si (internal/mesh/render.go) bu kuralı kullanmaz.
 */
export function placeMarks(segs: Segment[]): [number, number][] {
  const placed: [number, number][] = [];
  segs.forEach((s, i) => {
    let best: [number, number] = [(s.x1 + s.x2) / 2, (s.y1 + s.y2) / 2];
    let bestScore = -1;
    for (const f of FRACTIONS) {
      const p: [number, number] = [s.x1 + (s.x2 - s.x1) * f, s.y1 + (s.y2 - s.y1) * f];
      const clear = Math.min(Infinity, ...segs.filter((_, j) => j !== i).map((o) => distToSegment(p[0], p[1], o)));
      const gap = Math.min(Infinity, ...placed.map(([x, y]) => Math.hypot(p[0] - x, p[1] - y)));
      const score = Math.min(clear / MARK_CLEAR, gap / MARK_GAP);
      if (score > bestScore) {
        best = p;
        bestScore = score;
      }
      if (score >= 1) break;
    }
    placed.push(best);
  });
  return placed;
}
