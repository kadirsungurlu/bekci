// Canlı topoloji ilişki modeli: GET /api/topology?view=summary yanıtından düğümler, gerçek
// ilişkiler (monitör-nokta, monitör-kanal, monitör-sunucu, sunucu-kanal), gruplar, süzgeç ve
// sorun listesi. Saf modül: DOM ve çeviri yok, metinler bileşenlerde (node --test ile sınanır:
// e2e/topomodel.test.mjs). Sunucu-nokta bağı veride yok, bu yüzden gösterilmez.
import type { Topology, TopoChannel, TopoMonitor, TopoProbe, TopoServer } from './api';

/** Durum işareti sınıfı. */
export type Cls = 'ok' | 'warn' | 'bad' | 'pend' | 'off' | 'maint';
/** Görsel durum (satır, çizgi, küçük hücre). */
export type St = 'bad' | 'warn' | 'off' | 'ok';
export type Kind = 'srv' | 'pro' | 'cha' | 'mon';
/** Sütun: kanallar noktaların sütunundadır. */
export type Col = 'srv' | 'pro' | 'mon';

const ST: Record<Cls, St> = { ok: 'ok', warn: 'warn', bad: 'bad', pend: 'off', off: 'off', maint: 'off' };
export const RANK: Record<St, number> = { bad: 0, warn: 1, off: 2, ok: 3 };

const MON_CLS: Record<TopoMonitor['state'], Cls> = {
  up: 'ok',
  partial: 'warn',
  down: 'bad',
  pending: 'pend',
  paused: 'off',
  maintenance: 'maint',
};

export interface MonNode {
  kind: 'mon';
  id: string;
  name: string;
  cls: Cls;
  st: St;
  m: TopoMonitor;
  /** Denetleyen / hata gören / sonucu gelmeyen noktalar (düğüm kimliği). */
  pro: string[];
  bad: string[];
  off: string[];
  tag: string;
  customer: string;
}
export interface SrvNode {
  kind: 'srv';
  id: string;
  name: string;
  cls: Cls;
  st: St;
  s: TopoServer;
  customer: string;
}
export interface ProNode {
  kind: 'pro';
  id: string;
  name: string;
  cls: Cls;
  st: St;
  p: TopoProbe;
  code: string;
  /** Ana sunucu: nokta kaydı yok (konum 0). */
  local: boolean;
}
export interface ChaNode {
  kind: 'cha';
  id: string;
  name: string;
  cls: Cls;
  st: St;
  c: TopoChannel;
}
export type TopoNode = MonNode | SrvNode | ProNode | ChaNode;

export interface Rel {
  id: string;
  c: St;
}

export interface Graph {
  nodes: Map<string, TopoNode>;
  rel: Map<string, Rel[]>;
  mon: MonNode[];
  srv: SrvNode[];
  /** Önem sırasıyla. */
  pro: ProNode[];
  cha: ChaNode[];
  /** Sorun çizelgesi sütunları için kararlı sıra (durum değişince oynamaz). */
  proFixed: ProNode[];
}

const cmp = new Intl.Collator('tr', { sensitivity: 'base', numeric: true });
const bySt = <T extends { st: St; name: string }>(l: T[]) =>
  l.sort((a, b) => RANK[a.st] - RANK[b.st] || cmp.compare(a.name, b.name));

export const colOf = (kind: Kind): Col => (kind === 'cha' ? 'pro' : kind);

/** Kod boşsa addan kısalt: "İstanbul-1" -> "IST1", "New York Kuzey" -> "NYK". */
export function shortCode(name: string): string {
  const clean = name.toLocaleUpperCase('tr').replace(/İ/g, 'I').replace(/Ş/g, 'S').replace(/Ğ/g, 'G').replace(/Ü/g, 'U').replace(/Ö/g, 'O').replace(/Ç/g, 'C');
  const words = clean.split(/[^A-Z0-9]+/).filter(Boolean);
  if (!words.length) return '?';
  const digits = words.slice(1).filter((w) => /^\d+$/.test(w)).join('');
  const rest = words.filter((w, i) => i === 0 || !/^\d+$/.test(w));
  const head = rest.length > 1 ? rest.map((w) => w[0]).join('') : rest[0].slice(0, 3);
  return (head + digits).slice(0, 5);
}

const probeCls = (p: TopoProbe): Cls => (!p.online ? 'off' : p.late || p.uplink === 'oob' ? 'warn' : 'ok');
const serverCls = (s: TopoServer): Cls =>
  s.state === 'offline' ? 'bad' : s.firing.length ? 'warn' : s.state === 'online' ? 'ok' : 'off';
const channelCls = (c: TopoChannel): Cls =>
  !c.active ? 'off' : (c.last_send && !c.last_send.ok) || (c.failed_24h ?? 0) > 0 ? 'warn' : 'ok';

export function buildGraph(d: Topology): Graph {
  const nodes = new Map<string, TopoNode>();
  const rel = new Map<string, Rel[]>();
  const tagName = new Map((d.tags ?? []).map((x) => [x.id, x.name]));
  const custName = new Map((d.customers ?? []).map((x) => [x.id, x.name]));
  const firstName = (ids: number[] | undefined, names: Map<number, string>) => {
    let best = '';
    for (const id of ids ?? []) {
      const n = names.get(id);
      if (n && (!best || cmp.compare(n, best) < 0)) best = n;
    }
    return best;
  };

  const pro: ProNode[] = d.probes.map((p) => {
    const cls = probeCls(p);
    return { kind: 'pro', id: `p${p.id}`, name: p.name, cls, st: ST[cls], p, code: p.code || shortCode(p.name), local: false };
  });
  if (d.monitors.some((m) => m.loc_ids?.includes(0))) {
    const name = d.local_name || 'Panel';
    const p: TopoProbe = { id: 0, name, online: true, last_seen_at: d.now };
    pro.unshift({ kind: 'pro', id: 'p0', name, cls: 'ok', st: 'ok', p, code: shortCode(name), local: true });
  }
  const srv: SrvNode[] = d.servers.map((s) => {
    const cls = serverCls(s);
    return { kind: 'srv', id: `s${s.id}`, name: s.name, cls, st: ST[cls], s, customer: firstName(s.customers, custName) };
  });
  const cha: ChaNode[] = (d.channels ?? []).map((c) => {
    const cls = channelCls(c);
    return { kind: 'cha', id: `c${c.id}`, name: c.name, cls, st: ST[cls], c };
  });
  const toP = (ids: number[] | undefined) => (ids ?? []).map((i) => `p${i}`);
  const mon: MonNode[] = d.monitors.map((m) => {
    const cls = MON_CLS[m.state] ?? 'off';
    return {
      kind: 'mon',
      id: `m${m.id}`,
      name: m.name,
      cls,
      st: ST[cls],
      m,
      pro: toP(m.loc_ids),
      bad: toP(m.loc_bad),
      off: toP(m.loc_off),
      tag: firstName(m.tags, tagName),
      customer: firstName(m.customers, custName),
    };
  });
  for (const n of [...pro, ...srv, ...cha, ...mon]) {
    nodes.set(n.id, n);
    rel.set(n.id, []);
  }
  const bag = (a: string, b: string, c: St) => {
    const ra = rel.get(a);
    const rb = rel.get(b);
    if (!ra || !rb) return; // yetkiyle gizlenmiş uç
    ra.push({ id: b, c });
    rb.push({ id: a, c });
  };
  const chanRel = (id: string, off: boolean): St => (off ? 'off' : nodes.get(id)?.st === 'ok' ? 'ok' : (nodes.get(id)?.st ?? 'off'));
  for (const n of mon) {
    const off = n.st === 'off';
    for (const p of n.pro) {
      const po = nodes.get(p);
      bag(n.id, p, off || po?.st === 'off' || n.off.includes(p) ? 'off' : n.bad.includes(p) ? 'bad' : 'ok');
    }
    for (const c of n.m.channels ?? []) bag(n.id, `c${c}`, chanRel(`c${c}`, off));
    if (n.m.server_id) {
      const sid = `s${n.m.server_id}`;
      const st = nodes.get(sid)?.st;
      bag(n.id, sid, off ? 'off' : st === 'bad' ? 'bad' : n.st !== 'ok' || st === 'warn' ? 'warn' : 'ok');
    }
  }
  for (const n of srv) for (const c of n.s.channels ?? []) bag(n.id, `c${c}`, chanRel(`c${c}`, false));

  const proFixed = [...pro].sort((a, b) => a.p.id - b.p.id);
  return { nodes, rel, mon: bySt(mon), srv: bySt(srv), pro: bySt(pro), cha: bySt(cha), proFixed };
}

// --- Gruplama -------------------------------------------------------------------------------

export type GroupBy = { srv: string; pro: string; mon: string };
export const DEFAULT_BY: GroupBy = { srv: 'rol', pro: 'bolge', mon: 'etiket' };

/** Kullanıcının yetkisine göre gruplama seçenekleri (veride olmayan alan önerilmez). */
export function groupOptions(d: Topology): Record<Col, string[]> {
  const cust = !!d.customers?.length;
  const tags = !!d.tags?.length;
  return {
    srv: ['rol', 'yer', ...(cust ? ['musteri'] : [])],
    pro: ['bolge', ...(d.admin ? ['saglayici'] : [])],
    mon: [...(tags ? ['etiket'] : []), 'tur', ...(cust ? ['musteri'] : [])],
  };
}
/** Seçilen gruplama bu kullanıcıda yoksa ilk seçeneğe düş. */
export const effectiveBy = (by: GroupBy, opts: Record<Col, string[]>): GroupBy => ({
  srv: opts.srv.includes(by.srv) ? by.srv : opts.srv[0],
  pro: opts.pro.includes(by.pro) ? by.pro : opts.pro[0],
  mon: opts.mon.includes(by.mon) ? by.mon : opts.mon[0],
});

/** Grup anahtarı; '' üst bilgi boş (arayüzde "Belirtilmemiş"). */
function groupKey(n: TopoNode, by: string): string {
  if (n.kind === 'srv') return (by === 'yer' ? n.s.region : by === 'musteri' ? n.customer : n.s.role) ?? '';
  if (n.kind === 'pro') return (by === 'saglayici' ? n.p.provider : n.p.region) ?? '';
  if (n.kind === 'mon') return by === 'tur' ? n.m.type : by === 'musteri' ? n.customer : n.tag;
  return '';
}

export interface Group {
  id: string;
  key: string;
  items: TopoNode[];
  st: St;
}
export function groups(g: Graph, col: Col, by: string): Group[] {
  const h = new Map<string, TopoNode[]>();
  for (const n of g[col]) {
    const k = groupKey(n, by);
    const l = h.get(k);
    if (l) l.push(n);
    else h.set(k, [n]);
  }
  const out: Group[] = [];
  for (const [key, items] of h) out.push({ id: `g-${col}-${by}-${key}`, key, items, st: items[0].st });
  return out.sort((a, b) => RANK[a.st] - RANK[b.st] || b.items.length - a.items.length || cmp.compare(a.key, b.key));
}

// --- Süzgeç ve görünüm ----------------------------------------------------------------------

/** Aramada aksan yok sayılır. */
export const fold = (s: string) =>
  s.toLocaleLowerCase('tr').replace(/ı/g, 'i').replace(/ş/g, 's').replace(/ğ/g, 'g').replace(/ü/g, 'u').replace(/ö/g, 'o').replace(/ç/g, 'c');

export const isProblem = (n: TopoNode) => n.st === 'bad' || n.st === 'warn' || (n.kind === 'pro' && n.st === 'off');

function haystack(n: TopoNode): string {
  switch (n.kind) {
    case 'mon':
      return fold(`${n.name} ${n.m.type} ${n.tag} ${n.customer}`);
    case 'srv':
      return fold(`${n.name} ${n.s.role ?? ''} ${n.s.region ?? ''} ${n.customer}`);
    case 'pro':
      return fold(`${n.name} ${n.code} ${n.p.region ?? ''} ${n.p.provider ?? ''}`);
    default:
      return fold(`${n.name} ${n.c.type}`);
  }
}

export interface UI {
  sel: string | null;
  /** fold edilmiş arama. */
  q: string;
  sorun: boolean;
  by: GroupBy;
  /** Elle açılan/kapatılan gruplar. */
  ac: Record<string, boolean>;
  /** Seçimde "N diğer" ile tamamı açılan gruplar. */
  tam: Record<string, boolean>;
}

/** Seçimin ilişki haritası: kimlik -> çizgi sınıfı. */
export function relMap(g: Graph, sel: string | null): Map<string, St> {
  const r = new Map<string, St>();
  if (sel) for (const x of g.rel.get(sel) ?? []) {
    const old = r.get(x.id);
    if (old === undefined || RANK[x.c] < RANK[old]) r.set(x.id, x.c);
  }
  return r;
}

export interface GroupView {
  id: string;
  key: string;
  st: St;
  say: Record<St, number>;
  open: boolean;
  /** Seçimle ilgisi yok: soluk. */
  dim: boolean;
  /** Gruptaki tüm düğümler; bir kez görünen satır DOM'da gizli kalır, seçim yalnız sınıf değiştirir. */
  all: TopoNode[];
  rows: TopoNode[];
  /** Kapalı grubun küçük hücreleri (rows boşken) ya da "diğer" satırı. */
  cells: string;
  rest: number;
}

const cellsOf = (items: TopoNode[]) => {
  let s = '';
  for (const n of items) s += `<i class="${n.st}"></i>`;
  return s;
};

export function viewGroups(g: Graph, col: Col, ui: UI, rel: Map<string, St>): GroupView[] {
  const by = ui.by[col];
  const out: GroupView[] = [];
  const selKind = ui.sel ? g.nodes.get(ui.sel)?.kind : undefined;
  const other = !!selKind && colOf(selKind) !== col;
  const filtered = !!ui.q || ui.sorun;
  const visible = (n: TopoNode) => {
    if (n.id === ui.sel || rel.has(n.id)) return true;
    if (ui.sorun && !isProblem(n)) return false;
    return !ui.q || haystack(n).includes(ui.q);
  };
  for (const gr of groups(g, col, by)) {
    const vis = gr.items.filter(visible);
    if (!vis.length) continue;
    const say: Record<St, number> = { ok: 0, warn: 0, bad: 0, off: 0 };
    for (const n of gr.items) say[n.st]++;
    const base = { id: gr.id, key: gr.key, st: gr.st, say, all: gr.items };
    if (other && !ui.tam[gr.id]) {
      const rows = vis.filter((n) => rel.has(n.id));
      if (!rows.length) {
        out.push({ ...base, open: false, dim: true, rows: [], cells: cellsOf(gr.items), rest: 0 });
        continue;
      }
      const inRows = new Set(rows);
      const rest = gr.items.filter((n) => !inRows.has(n));
      out.push({ ...base, open: true, dim: false, rows, cells: cellsOf(rest), rest: rest.length });
      continue;
    }
    const here = gr.items.some((n) => n.id === ui.sel);
    const open = filtered || here || !!ui.tam[gr.id] || (ui.ac[gr.id] ?? isProblem({ st: gr.st, kind: gr.items[0].kind } as TopoNode));
    out.push({ ...base, open, dim: false, rows: open ? vis : [], cells: open ? '' : cellsOf(gr.items), rest: 0 });
  }
  return out;
}

export interface ChannelView {
  rows: ChaNode[];
  rest: number;
}
export function viewChannels(g: Graph, ui: UI, rel: Map<string, St>): ChannelView {
  const selKind = ui.sel ? g.nodes.get(ui.sel)?.kind : undefined;
  const other = !!selKind && selKind !== 'cha';
  const vis = g.cha.filter((n) => {
    if (n.id === ui.sel || rel.has(n.id)) return true;
    if (ui.sorun && !isProblem(n)) return false;
    return !ui.q || haystack(n).includes(ui.q);
  });
  if (other && !ui.tam.kanal) {
    const rows = vis.filter((n) => rel.has(n.id));
    return { rows, rest: g.cha.length - rows.length };
  }
  return { rows: vis, rest: 0 };
}

/** Grup başlığı kimliği: kapalı gruptaki düğüme çizgi başlığa gider. */
export function groupIndex(g: Graph, by: GroupBy): Map<string, string> {
  const m = new Map<string, string>();
  for (const col of ['srv', 'pro', 'mon'] as const)
    for (const gr of groups(g, col, by[col])) for (const n of gr.items) m.set(n.id, gr.id);
  return m;
}

// --- Sorun çizelgesi ------------------------------------------------------------------------

export type ProblemKind = 'mon' | 'probe' | 'server';
export interface Problem {
  node: TopoNode;
  tone: 'bad' | 'warn';
  kind: ProblemKind;
}
export function problems(g: Graph): Problem[] {
  const mon = g.mon
    .filter((n) => n.st === 'bad' || n.st === 'warn')
    .sort((a, b) => RANK[a.st] - RANK[b.st] || b.bad.length - a.bad.length);
  const out: Problem[] = mon.map((node) => ({ node, tone: node.st as 'bad' | 'warn', kind: 'mon' }));
  for (const n of g.pro) if (isProblem(n)) out.push({ node: n, tone: n.st === 'warn' ? 'warn' : 'bad', kind: 'probe' });
  for (const n of g.srv) if (isProblem(n)) out.push({ node: n, tone: n.st as 'bad' | 'warn', kind: 'server' });
  return out.sort((a, b) => (a.tone === b.tone ? 0 : a.tone === 'bad' ? -1 : 1));
}

/** Matris hücresi: bu nokta bu monitörde ne görüyor. */
export function cellClass(m: MonNode, p: ProNode): string {
  if (!m.pro.includes(p.id)) return 'h-yok';
  if (p.st === 'off' || m.off.includes(p.id)) return 'h-off';
  return m.bad.includes(p.id) ? 'h-bad' : 'h-ok';
}

/** Kim görüyor: bir bölgedeki çevrimiçi noktaların hepsi görüyorsa bölge adı, yoksa nokta adı. */
export function whoSees(g: Graph, m: MonNode): { all: boolean; parts: string[] } {
  const online = (id: string) => g.nodes.get(id)?.st !== 'off';
  const sees = m.bad.filter(online);
  const voting = m.pro.filter((p) => online(p) && !m.off.includes(p));
  if (sees.length && sees.length === voting.length) return { all: true, parts: [] };
  const set = new Set(sees);
  const parts: string[] = [];
  const done = new Set<string>();
  for (const id of sees) {
    if (done.has(id)) continue;
    const n = g.nodes.get(id) as ProNode;
    const region = n.p.region;
    const peers = region ? voting.filter((v) => (g.nodes.get(v) as ProNode).p.region === region) : [];
    if (region && peers.length > 1 && peers.every((v) => set.has(v))) {
      parts.push(region);
      peers.forEach((v) => done.add(v));
    } else {
      parts.push(n.name);
      done.add(id);
    }
  }
  return { all: false, parts };
}

/** Son 30 değerden çizilebilir ölçümler (-1 ölçüm yok, -2 kesinti). */
export const sparkPoints = (series: number[] | undefined) => (series ?? []).filter((v) => v >= 0);

/** Satır işareti: renge ek olarak şekil. Sabit SVG metni, binlerce satırda bileşen maliyeti yok. */
export const MARK: Record<Cls, string> = {
  bad: '<svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="6.5" fill="var(--down)"/><path d="m4.6 4.6 4.8 4.8m0-4.8L4.6 9.4" stroke="#fff" stroke-width="1.7" stroke-linecap="round"/></svg>',
  warn: '<svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M7 .9 13.5 12.6H.5z" fill="var(--warn)"/><path d="M7 5v3.6" stroke="#fff" stroke-width="1.5" stroke-linecap="round"/><circle cx="7" cy="10.7" r=".9" fill="#fff"/></svg>',
  off: '<svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="4.6" fill="none" stroke="var(--paused)" stroke-width="1.8"/></svg>',
  pend: '<svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="4.6" fill="none" stroke="var(--pending)" stroke-width="1.8"/></svg>',
  maint: '<svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="4.6" fill="none" stroke="var(--maint)" stroke-width="1.8"/></svg>',
  ok: '<svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><circle cx="7" cy="7" r="3.4" fill="var(--up)"/></svg>',
};
/** Satırlar <use> ile bağlanır: binlerce satırda SVG ağacı kopyalanmaz. */
export const MARK_DEFS = (Object.keys(MARK) as Cls[])
  .map((c) => MARK[c].replace(/^<svg[^>]*>/, `<symbol id="tm-${c}" viewBox="0 0 14 14">`).replace('</svg>', '</symbol>'))
  .join('');
export const esc = (s: string) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] as string);
