// Canlı topoloji görünüm yardımcıları: durum sınıfı, sıralama (sorunlular önce), kart alt
// satırı, bağlantı (kenar) sınıfı ve ipucu satırları. Tuval (TopoCanvas) ve dar ekran listesi
// (TopoList) aynı kuralları kullanır.
import { t } from './i18n';
import { fmtMs, fmtRelative, fmtTimeSec } from './format';
import { typeLabel } from './monitorTypes';
import { NOTIFY_LABELS } from './notifyTypes';
import type { Topology, TopoChannel, TopoMonitor, TopoProbe, TopoServer } from './api';

/** Kart/nokta sınıfı (renk). */
export type Cls = 'ok' | 'warn' | 'bad' | 'pend' | 'off' | 'maint';
/** Bağlantı çizgi türü. */
export type EdgeCls = 'check' | 'link' | 'metric' | 'mesh' | 'notify' | 'down' | 'warn' | 'off';

const MON_CLS: Record<TopoMonitor['state'], Cls> = {
  up: 'ok',
  partial: 'warn',
  down: 'bad',
  pending: 'pend',
  paused: 'off',
  maintenance: 'maint',
};
const RANK: Record<Cls, number> = { bad: 0, warn: 1, pend: 2, maint: 3, ok: 4, off: 5 };

export const monClass = (m: TopoMonitor): Cls => MON_CLS[m.state] ?? 'off';
export const probeClass = (p: TopoProbe): Cls => (!p.online ? 'bad' : p.uplink === 'oob' ? 'warn' : 'ok');
export const serverClass = (s: TopoServer): Cls =>
  s.state === 'offline' ? 'bad' : s.firing.length ? 'warn' : s.state === 'online' ? 'ok' : 'off';
export const channelClass = (c: TopoChannel): Cls =>
  !c.active || !c.last_send ? 'off' : c.last_send.ok ? 'ok' : 'bad';

function bySeverity<T extends { name: string }>(list: T[], cls: (x: T) => Cls): T[] {
  return [...list].sort((a, b) => RANK[cls(a)] - RANK[cls(b)] || a.name.localeCompare(b.name));
}
export const sortMonitors = (l: TopoMonitor[]) => bySeverity(l, monClass);
export const sortProbes = (l: TopoProbe[]) => bySeverity(l, probeClass);
export const sortServers = (l: TopoServer[]) => bySeverity(l, serverClass);
export const sortChannels = (l: TopoChannel[]) => bySeverity(l, channelClass);

/** Grup köşesindeki sayılar: çalışan / uyarı / sorunlu. */
export function counts(classes: Cls[]): { ok: number; warn: number; bad: number; total: number } {
  const r = { ok: 0, warn: 0, bad: 0, total: classes.length };
  for (const c of classes) {
    if (c === 'bad') r.bad++;
    else if (c === 'warn' || c === 'pend') r.warn++;
    else if (c === 'ok' || c === 'maint') r.ok++;
  }
  return r;
}
/** Grubun genel durumu (kutu kenar rengi). */
export const groupLevel = (c: { ok: number; warn: number; bad: number; total: number }): Cls =>
  c.bad ? 'bad' : c.warn ? 'warn' : c.ok ? 'ok' : 'off';

const pct = (v: number | undefined) => (v === undefined ? '—' : `${Math.round(v)}%`);
export const notifyLabel = (type: string) => (NOTIFY_LABELS as Record<string, string>)[type] ?? type;

/** Zaman değeri: saat + göreli (0: hiç). */
export const when = (ts: number | undefined, now: number) =>
  ts ? `${fmtTimeSec(ts)} · ${fmtRelative(ts, now)}` : '—';

export function monitorSub(m: TopoMonitor): string {
  const parts = [typeLabel(m.type)];
  if (m.state === 'up' || m.state === 'partial') {
    if (m.ping_ms >= 0) parts.push(fmtMs(m.ping_ms));
  } else parts.push(t(`topology.state.${m.state}`));
  return parts.join(' · ');
}
export function probeSub(p: TopoProbe, admin: boolean): string {
  if (!p.online) return t('topology.probe.offline');
  const parts: string[] = [];
  if (admin && p.panel_rtt_ms !== undefined) parts.push(`${t('topology.probe.rtt')} ${fmtMs(p.panel_rtt_ms)}`);
  if (p.uplink === 'oob') parts.push(t('topology.probe.oob'));
  if (admin && p.peers) parts.push(t('topology.probe.peersShort', { up: p.peers_up ?? 0, n: p.peers }));
  return parts.join(' · ') || t('topology.probe.online');
}
export function serverSub(s: TopoServer): string {
  if (s.state === 'offline') return t('topology.server.offline');
  if (s.cpu === undefined) return t('topology.server.noMetrics');
  return `${t('topology.server.cpu')} ${pct(s.cpu)} · ${t('topology.server.mem')} ${pct(s.mem)} · ${t('topology.server.disk')} ${pct(s.disk)}`;
}
export function channelSub(c: TopoChannel, now: number): string {
  if (!c.active) return t('topology.channel.inactive');
  if (!c.last_send) return `${notifyLabel(c.type)} · ${t('topology.channel.never')}`;
  return `${notifyLabel(c.type)} · ${fmtRelative(c.last_send.at, now)}`;
}

// --- Bağlantılar ---------------------------------------------------------------------------

export function checkEdge(m: TopoMonitor): EdgeCls {
  const c = monClass(m);
  return c === 'bad' ? 'down' : c === 'warn' ? 'warn' : c === 'off' || c === 'maint' ? 'off' : 'check';
}
export const linkEdge = (p: TopoProbe): EdgeCls => (!p.online ? 'down' : p.uplink === 'oob' ? 'warn' : 'link');
export const metricEdge = (s: TopoServer): EdgeCls =>
  s.state === 'offline' ? 'down' : s.firing.length ? 'warn' : s.cpu === undefined ? 'off' : 'metric';
export const notifyEdge = (c: TopoChannel): EdgeCls =>
  !c.active ? 'off' : c.last_send && !c.last_send.ok ? 'down' : 'notify';
export function meshEdge(ab: string, ba: string): EdgeCls {
  if (ab === 'up' && ba === 'up') return 'mesh';
  if (ab === 'down' && ba === 'down') return 'down';
  if (ab === 'up' || ba === 'up') return 'warn';
  return ab === 'down' || ba === 'down' ? 'down' : 'off';
}
/** Toplu bağlantı: en kötü durumun çizgisi, hepsi normalse grubun olağan türü. */
export function worstEdge(list: EdgeCls[], normal: EdgeCls): EdgeCls {
  if (list.includes('down')) return 'down';
  if (list.includes('warn')) return 'warn';
  if (list.length && list.every((e) => e === 'off')) return 'off';
  return normal;
}

export interface EdgeTip {
  title: string;
  rows: [string, string][];
}

const latest = (xs: (number | undefined)[]) => Math.max(0, ...xs.map((x) => x ?? 0));

/**
 * Bağlantı kimliğinden güncel ipucu: chk:<id|*>, lnk:<id|*>, met:<id|*>, ntf:<id|*>, msh:<a>:<b>.
 * Veri değiştikçe yeniden hesaplanır (açık ipucu canlı kalır); öğe artık yoksa null.
 */
export function edgeTip(id: string, d: Topology, now: number): EdgeTip | null {
  const [kind, a, b] = id.split(':');
  const panel = t('topology.col.panel');
  const tm = t('topology.tip.time');
  if (kind === 'chk') {
    if (a === '*') {
      const c = counts(d.monitors.map(monClass));
      return {
        title: `${panel} → ${t('topology.col.monitors')}`,
        rows: [
          [t('topology.tip.total'), String(c.total)],
          [t('topology.tip.ok'), String(c.ok)],
          [t('topology.tip.problem'), String(c.bad + c.warn)],
          [tm, when(latest(d.monitors.map((m) => m.last_check_at)), now)],
        ],
      };
    }
    const m = d.monitors.find((x) => x.id === +a);
    if (!m) return null;
    const ok = m.locations.filter((l) => l.status === 'up').length;
    const rows: [string, string][] = [
      [t('topology.tip.state'), t(`topology.state.${m.state}`)],
      [t('topology.tip.type'), typeLabel(m.type)],
      [t('topology.tip.ping'), m.ping_ms >= 0 ? fmtMs(m.ping_ms) : '—'],
    ];
    if (m.locations.length) rows.push([t('topology.tip.locations'), `${ok}/${m.locations.length}`]);
    rows.push([tm, when(m.last_check_at, now)]);
    return { title: `${panel} → ${m.name}`, rows };
  }
  if (kind === 'lnk') {
    if (a === '*') {
      return {
        title: `${t('topology.col.probes')} → ${panel}`,
        rows: [
          [t('topology.tip.online'), `${d.stats.probes_online}/${d.stats.probes_total}`],
          [tm, when(latest(d.probes.map((p) => p.last_seen_at)), now)],
        ],
      };
    }
    const p = d.probes.find((x) => x.id === +a);
    if (!p) return null;
    const rows: [string, string][] = [[t('topology.tip.state'), p.online ? t('topology.probe.online') : t('topology.probe.offline')]];
    if (d.admin) {
      rows.push([t('topology.probe.rtt'), p.panel_rtt_ms !== undefined ? fmtMs(p.panel_rtt_ms) : '—']);
      rows.push([t('topology.probe.uplink'), p.uplink === 'oob' ? t('topology.probe.oob') : t('topology.probe.primary')]);
      if (p.peers) rows.push([t('topology.probe.peers'), `${p.peers_up ?? 0}/${p.peers}`]);
      if (p.version) rows.push([t('topology.probe.version'), p.version]);
    }
    rows.push([t('topology.tip.lastSeen'), when(p.last_seen_at, now)]);
    return { title: `${p.name} → ${panel}`, rows };
  }
  if (kind === 'met') {
    if (a === '*') {
      const c = counts(d.servers.map(serverClass));
      return {
        title: `${t('topology.col.servers')} → ${panel}`,
        rows: [
          [t('topology.tip.total'), String(c.total)],
          [t('topology.tip.problem'), String(c.bad + c.warn)],
          [tm, when(latest(d.servers.map((s) => s.metrics_at)), now)],
        ],
      };
    }
    const s = d.servers.find((x) => x.id === +a);
    if (!s) return null;
    const rows: [string, string][] = [];
    if (s.cpu !== undefined) {
      rows.push([t('topology.server.cpu'), pct(s.cpu)], [t('topology.server.mem'), pct(s.mem)], [t('topology.server.disk'), pct(s.disk)]);
    } else rows.push([t('topology.tip.state'), s.state === 'offline' ? t('topology.server.offline') : t('topology.server.noMetrics')]);
    if (s.firing.length) rows.push([t('topology.tip.firing'), s.firing.join(', ')]);
    rows.push([tm, when(s.metrics_at || s.last_seen_at, now)]);
    return { title: `${s.name} → ${panel}`, rows };
  }
  if (kind === 'ntf') {
    const chans = d.channels ?? [];
    if (a === '*') {
      const sent = chans.filter((c) => c.last_send);
      return {
        title: `${panel} → ${t('topology.col.channels')}`,
        rows: [
          [t('topology.tip.total'), String(chans.length)],
          [t('topology.tip.failed'), String(sent.filter((c) => !c.last_send!.ok).length)],
          [t('topology.tip.lastSend'), when(latest(sent.map((c) => c.last_send!.at)), now)],
        ],
      };
    }
    const c = chans.find((x) => x.id === +a);
    if (!c) return null;
    const res = !c.active
      ? t('topology.channel.inactive')
      : !c.last_send
        ? t('topology.channel.never')
        : c.last_send.ok
          ? t('topology.tip.sendOk')
          : t('topology.tip.sendFail');
    return {
      title: `${panel} → ${c.name}`,
      rows: [
        [t('topology.tip.type'), notifyLabel(c.type)],
        [t('topology.tip.state'), res],
        [t('topology.tip.lastSend'), when(c.last_send?.at, now)],
      ],
    };
  }
  if (kind === 'msh' && d.mesh) {
    const na = d.mesh.nodes.find((n) => n.id === +a);
    const nb = d.mesh.nodes.find((n) => n.id === +b);
    if (!na || !nb) return null;
    const dir = (src: number, dst: number): [string, string] => {
      const l = d.mesh!.links?.find((x) => x.src === src && x.dst === dst);
      const p = d.mesh!.pairs.find((x) => (x.a === src && x.b === dst) || (x.a === dst && x.b === src));
      const st = l?.state ?? (p ? (p.a === src ? p.ab : p.ba) : 'unknown');
      const v = st === 'up' ? (l?.rtt_ms !== undefined ? fmtMs(l.rtt_ms) : t('topology.mesh.up')) : t(`topology.mesh.${st === 'down' ? 'down' : 'unknown'}`);
      const name = (id: number) => d.mesh!.nodes.find((n) => n.id === id)?.name ?? String(id);
      return [`${name(src)} → ${name(dst)}`, v];
    };
    const ls = (d.mesh.links ?? []).filter((x) => (x.src === na.id && x.dst === nb.id) || (x.src === nb.id && x.dst === na.id));
    const loss = Math.max(0, ...ls.map((x) => x.loss_streak ?? 0));
    const rows: [string, string][] = [dir(na.id, nb.id), dir(nb.id, na.id)];
    if (loss) rows.push([t('topology.tip.loss'), String(loss)]);
    rows.push([t('topology.tip.lastOk'), when(latest(ls.map((x) => x.last_ok_at)), now)]);
    return { title: `${na.name} ↔ ${nb.name}`, rows };
  }
  return null;
}
