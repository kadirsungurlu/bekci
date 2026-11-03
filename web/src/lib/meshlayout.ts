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
