// Topoloji sayfasının çeviriye bağlı metinleri (topomodel saf kalsın diye ayrı).
import { fmtDurationShort } from './format';
import { locale, t } from './i18n';
import { typeLabel } from './monitorTypes';
import { whoSees, type Col, type Graph, type GroupView, type MonNode, type ProNode, type SrvNode, type TopoNode } from './topomodel';

export const pct = (v: number) => (locale() === 'tr' ? `%${Math.round(v)}` : `${Math.round(v)}%`);

/** Türkçe belirtme eki: "3'ü". */
export function ek(n: number): string {
  if (locale() !== 'tr') return String(n);
  const ones = ["'ı", "'i", "'si", "'ü", "'ü", "'i", "'sı", "'si", "'i", "'u"];
  const tens = ["'ü", "'u", "'si", "'u", "'ı", "'si", "'ı", "'i", "'i", "'ı"];
  return String(n) + (n % 10 ? ones[n % 10] : n === 0 ? ones[0] : tens[Math.floor(n / 10) % 10]);
}

export function stateText(n: TopoNode): string {
  if (n.kind === 'mon') return t(`topology.state.${n.m.state}`);
  if (n.st === 'warn') return t('topology.state.warn');
  if (n.kind === 'cha') return t(n.st === 'off' ? 'topology.state.inactive' : 'topology.state.ok');
  if (n.kind === 'srv' && n.s.state === 'offline') return t('topology.state.offline');
  if (n.st === 'off') return t(n.kind === 'pro' ? 'topology.state.offline' : 'topology.state.pending');
  return t('topology.state.ok');
}

export function serverSymptom(n: SrvNode): string {
  const s = n.s;
  if (s.alert) return s.alert;
  if (s.state === 'offline') return t('topology.state.offline');
  return s.firing
    .map((f) => {
      if (f === 'cpu' && s.cpu !== undefined) return `CPU ${pct(s.cpu)}`;
      if (f === 'mem' && s.mem !== undefined) return `RAM ${pct(s.mem)}`;
      if (f === 'disk' && s.disk !== undefined) return `Disk ${pct(s.disk)}`;
      return f;
    })
    .join(', ');
}

export const whereText = (g: Graph, m: MonNode) => {
  const w = whoSees(g, m);
  return w.all ? t('topology.problems.everywhere') : w.parts.join(', ');
};

export const sinceText = (since: number | undefined, now: number) => (since ? fmtDurationShort(Math.max(0, now - since)) : '');

/** Satırın ikinci sırası: yalnız sorunlularda. */
export function monitorAlt(g: Graph, n: MonNode, now: number): string {
  if (n.st !== 'bad' && n.st !== 'warn') return '';
  return [n.m.message, sinceText(n.m.since, now), whereText(g, n)].filter(Boolean).join(', ');
}
export function serverAlt(n: SrvNode, now: number): string {
  if (n.st !== 'bad' && n.st !== 'warn') return '';
  return [serverSymptom(n), sinceText(n.s.alert_since, now)].filter(Boolean).join(', ');
}

export function groupName(col: Col, by: string, key: string): string {
  if (!key) return t('topology.unset');
  return col === 'mon' && by === 'tur' ? typeLabel(key) : key;
}
export function groupSummary(col: Col, g: GroupView): string {
  const a: string[] = [];
  if (g.say.bad) a.push(t('topology.group.bad', { count: g.say.bad }));
  if (g.say.warn) a.push(t('topology.group.warn', { count: g.say.warn }));
  if (g.say.off) a.push(t(col === 'pro' ? 'topology.group.offline' : 'topology.group.paused', { count: g.say.off }));
  if (g.say.ok) a.push(t('topology.group.ok', { count: g.say.ok }));
  return a.join(', ');
}

export const offCount = (g: Graph, m: MonNode) => m.pro.filter((p) => m.off.includes(p) || g.nodes.get(p)?.st === 'off').length;
export const isProbe = (n: TopoNode): n is ProNode => n.kind === 'pro';
