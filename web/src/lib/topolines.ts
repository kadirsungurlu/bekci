// Seçim ve önizleme çizgileri: sütunlar arasında tek SVG. Satır konumları seçim, veri ya da
// boyut değişince bir kez ölçülür (measure); kaydırmada yalnız önbellekten yeniden çizilir
// (draw), böylece kaydırma düzen hesabı istemez. Görünmeyen ilişkiler sütun başına tek çizgi
// ve "Aşağıda N ilişkili" çipine, aynı grup başlığına gidenler tek çizgiye iner.
import { colOf, RANK, type Col, type Graph, type St } from './topomodel';

interface Spot {
  col: Col;
  /** Kaydırma içeriğine göre satır ortası. */
  y: number;
  c: St;
}
interface Layer {
  id: string;
  from: { col: Col; y: number } | null;
  spots: Spot[];
}
export interface LinesHost {
  root: HTMLElement;
  svg: SVGSVGElement;
  graph(): Graph | null;
  /** Kapalı gruptaki düğümün başlık kimliği. */
  groupOf(id: string): string | undefined;
  chip(dir: 1 | -1, n: number): string;
}

const COLS: Col[] = ['srv', 'pro', 'mon'];
const ROW_H = 27;

export class TopoLines {
  private h: LinesHost;
  private sel: Layer | null = null;
  private hov: Layer | null = null;
  private lastSel: string | null = null;
  private box = { L: 0, ML: 0, MR: 0, R: 0, top: 0 };
  private kay = {} as Record<Col, { el: HTMLElement; top: number; bottom: number }>;
  private chips = new Map<string, HTMLButtonElement>();

  constructor(h: LinesHost) {
    this.h = h;
  }

  private target(id: string): HTMLElement | null {
    const e = document.getElementById(`o-${id}`);
    if (e) return e;
    const g = this.h.groupOf(id);
    return g ? document.getElementById(g) : null;
  }

  private spotY(e: HTMLElement, col: Col): number {
    const r = e.getBoundingClientRect();
    const k = this.kay[col];
    return r.top + Math.min(r.height, ROW_H) / 2 - k.top + k.el.scrollTop;
  }

  private layer(id: string | null): Layer | null {
    const g = this.h.graph();
    const node = id ? g?.nodes.get(id) : undefined;
    if (!g || !id || !node) return null;
    const src = this.target(id);
    const col = colOf(node.kind);
    const spots: Spot[] = [];
    const seen = new Map<string, Spot>(); // aynı noktaya (grup başlığı) giden tek çizgi
    for (const r of g.rel.get(id) ?? []) {
      const n = g.nodes.get(r.id);
      const e = n && this.target(r.id);
      if (!n || !e) continue;
      const c = colOf(n.kind);
      const y = this.spotY(e, c);
      const key = `${c}:${Math.round(y)}`;
      const old = seen.get(key);
      if (old) {
        if (RANK[r.c] < RANK[old.c]) old.c = r.c;
        continue;
      }
      const s = { col: c, y, c: r.c };
      seen.set(key, s);
      spots.push(s);
    }
    return { id, from: src ? { col, y: this.spotY(src, col) } : null, spots };
  }

  /** Düzen ölçümü + çizim: seçim, veri, boyut değişiminde. */
  update(sel: string | null, hov: string | null) {
    const root = this.h.root;
    const out = root.getBoundingClientRect();
    const cr = (c: Col) => (root.querySelector(`#c-${c}`) as HTMLElement).getBoundingClientRect();
    const [s, p, m] = [cr('srv'), cr('pro'), cr('mon')];
    this.box = { L: s.right - out.left, ML: p.left - out.left, MR: p.right - out.left, R: m.left - out.left, top: out.top };
    for (const c of COLS) {
      const el = root.querySelector(`#k-${c}`) as HTMLElement;
      const r = el.getBoundingClientRect();
      this.kay[c] = { el, top: r.top, bottom: r.bottom };
    }
    this.sel = this.layer(sel);
    this.hov = hov && hov !== sel ? this.layer(hov) : null;
    this.draw();
  }

  /** Kaydırma: yalnız önbellekteki konumlardan çizim. */
  draw() {
    const live = new Set<string>();
    let html = '';
    if (this.sel) html += this.paint(this.sel, this.sel.id === this.lastSel ? 'sec' : 'sec yeni', live);
    if (this.hov) html += this.paint(this.hov, 'oniz', null);
    this.h.svg.innerHTML = html;
    this.lastSel = this.sel?.id ?? null;
    for (const [k, b] of this.chips)
      if (!live.has(k)) {
        b.remove();
        this.chips.delete(k);
      }
  }

  private clip(col: Col, y: number): { y: number; dir: 0 | 1 | -1 } {
    const k = this.kay[col];
    const abs = y + k.top - k.el.scrollTop;
    if (abs < k.top) return { y: k.top + 8 - this.box.top, dir: -1 };
    if (abs > k.bottom) return { y: k.bottom - 8 - this.box.top, dir: 1 };
    return { y: abs - this.box.top, dir: 0 };
  }

  private paint(l: Layer, cls: string, live: Set<string> | null): string {
    if (!l.from) return '';
    const { L, ML, MR, R } = this.box;
    const ka = l.from.col;
    const y0 = this.clip(ka, l.from.y).y;
    const fan = (x0: number, a: number, x1: number, b: number) => {
      const m = (x0 + x1) / 2;
      return `C${m} ${a} ${m} ${b} ${x1} ${b}`;
    };
    const route = (kb: Col, a: number, b: number): { d: string; x: number; cross?: string } => {
      if (ka === 'srv' && kb === 'pro') return { d: `M${L} ${a}${fan(L, a, ML, b)}`, x: ML };
      if (ka === 'mon' && kb === 'pro') return { d: `M${R} ${a}${fan(R, a, MR, b)}`, x: MR };
      if (ka === 'pro' && kb === 'srv') return { d: `M${ML} ${a}${fan(ML, a, L, b)}`, x: L };
      if (ka === 'pro' && kb === 'mon') return { d: `M${MR} ${a}${fan(MR, a, R, b)}`, x: R };
      // Sunucu ile monitör arası: orta sütunun altından tek gövde geçer, karşı boşlukta yelpazelenir.
      const cross = `M${ML} ${a}H${MR}`;
      if (ka === 'srv') return { d: `M${L} ${a}H${ML}M${MR} ${a}${fan(MR, a, R, b)}`, x: R, cross };
      return { d: `M${R} ${a}H${MR}M${ML} ${a}${fan(ML, a, L, b)}`, x: L, cross };
    };
    // Görünmeyenler sütun+yön başına toplanır.
    const over = new Map<string, { n: number; c: St; y: number; col: Col; dir: 1 | -1 }>();
    const ends: { col: Col; y: number; c: St; o: number }[] = [];
    for (const s of l.spots) {
      const p = this.clip(s.col, s.y);
      if (p.dir) {
        const k = `${s.col}:${p.dir}`;
        const t = over.get(k);
        if (t) {
          t.n++;
          if (RANK[s.c] < RANK[t.c]) t.c = s.c;
        } else over.set(k, { n: 1, c: s.c, y: p.y, col: s.col, dir: p.dir });
      } else ends.push({ col: s.col, y: p.y, c: s.c, o: 0 });
    }
    for (const t of over.values()) ends.push({ col: t.col, y: t.y, c: t.c, o: 0 });
    // Komşu sütuna giden uçlar satır yüksekliğine yayılır; karşı sütuna tek gövde gider.
    for (const kb of COLS) {
      if (kb === ka || (ka !== 'pro' && kb !== 'pro')) continue;
      const u = ends.filter((s) => s.col === kb).sort((a, b) => a.y - b.y);
      u.forEach((s, i) => {
        s.o = u.length > 1 ? (i / (u.length - 1) - 0.5) * 18 : 0;
      });
    }
    ends.sort((a, b) => RANK[b.c] - RANK[a.c]);
    let out = `<g class="${cls}">`;
    let crossed = false;
    const dots = live !== null;
    for (const s of ends) {
      const r = route(s.col, y0 + s.o, s.y);
      out += `<path d="${r.d}" class="${s.c}"/>`;
      if (r.cross && !crossed) {
        crossed = true;
        out += `<path d="${r.cross}" class="${s.c} gecis"/>`;
      }
      if (dots) out += `<circle cx="${r.x}" cy="${s.y}" r="3" class="${s.c}"/>`;
    }
    out += '</g>';
    if (live)
      for (const [k, t] of over) {
        live.add(k);
        let b = this.chips.get(k);
        if (!b) {
          b = document.createElement('button');
          b.type = 'button';
          b.className = `tasan ${t.dir === 1 ? 'alt-c' : 'ust'}`;
          b.dataset.kay = k;
          b.dataset.testid = 'topology-more';
          (this.h.root.querySelector(`#c-${t.col}`) as HTMLElement).append(b);
          this.chips.set(k, b);
        }
        const text = this.h.chip(t.dir, t.n);
        if (b.textContent !== text) b.textContent = text;
      }
    return out;
  }

  clear() {
    this.sel = this.hov = null;
    this.h.svg.innerHTML = '';
    for (const b of this.chips.values()) b.remove();
    this.chips.clear();
  }
}
