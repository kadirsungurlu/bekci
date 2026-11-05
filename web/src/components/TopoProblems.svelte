<script lang="ts">
  // Sorun çizelgesi: hangi nokta neyi görüyor. Satıra tıklamak öğeyi seçer.
  import { t } from '../lib/i18n';
  import { cellClass, esc, MARK, problems, type Graph, type MonNode, type Problem } from '../lib/topomodel';
  import { groupName, serverSymptom, sinceText, whereText } from '../lib/topotext';

  let { g, sel, now, byMon, bySrv }: { g: Graph; sel: string | null; now: number; byMon: string; bySrv: string } = $props();

  const MAX = 50;
  const all = $derived(problems(g));
  const rows = $derived(all.slice(0, MAX));
  const kinds = $derived({
    mon: all.filter((p) => p.kind === 'mon').length,
    pro: all.filter((p) => p.kind === 'probe').length,
    srv: all.filter((p) => p.kind === 'server').length,
  });
  const online = $derived(g.pro.filter((p) => p.st !== 'off'));
  const srvWarn = $derived(g.srv.filter((s) => s.st === 'warn').length);
  const showCodes = $derived(g.proFixed.length <= 16);
  const matrix = (m: MonNode) => {
    let s = '';
    for (const p of g.proFixed) s += `<span class="${cellClass(m, p)}" title="${esc(p.name)}"></span>`;
    return s;
  };
  const codes = $derived.by(() => {
    let s = '';
    for (const p of g.proFixed) s += `<span title="${esc(p.name)}">${esc(p.code)}</span>`;
    return s;
  });
  const voters = (m: MonNode) => m.bad.filter((p) => g.nodes.get(p)?.st !== 'off').length;
  const group = (p: Problem) => {
    const n = p.node;
    if (n.kind === 'mon') return groupName('mon', byMon, byMon === 'tur' ? n.m.type : byMon === 'musteri' ? n.customer : n.tag);
    if (n.kind === 'srv') return groupName('srv', bySrv, (bySrv === 'yer' ? n.s.region : bySrv === 'musteri' ? n.customer : n.s.role) ?? '');
    if (n.kind === 'pro') return n.p.region || t('topology.unset');
    return '';
  };
</script>

<section class="sorun" aria-label={t('topology.problems.label')} data-testid="topology-problems">
  <div class="sorun-b">
    {#if all.length}
      <b>{t('topology.problems.count', { count: all.length })}</b>
      <span class="ozet">{t('topology.problems.summary', { m: kinds.mon, p: kinds.pro, s: kinds.srv })}</span>
      <span class="lej">
        <i class="h-bad"></i>{t('topology.problems.legendBad')}<i class="h-ok"></i>{t('topology.problems.legendOk')}<i class="h-off"></i>{t('topology.problems.legendOff')}<i class="h-yok"></i>{t('topology.problems.legendNone')}
      </span>
    {:else}
      <b class="temiz">{t('topology.problems.none')}</b>
    {/if}
    <em>
      {t('topology.problems.working', {
        mon: g.mon.filter((m) => m.st === 'ok').length,
        monTotal: g.mon.length,
        paused: g.mon.filter((m) => m.m.state === 'paused').length,
        pro: online.length,
        proTotal: g.pro.length,
        srv: g.srv.filter((s) => s.st !== 'bad').length,
        srvTotal: g.srv.length,
        warn: srvWarn,
      })}
    </em>
  </div>
  {#if all.length}
    <div class="s-liste">
      <div class="s-sat bs">
        <span></span><span class="s-gr">{t('topology.problems.where')}</span><span class="s-gr s-gk">{t('topology.problems.group')}</span>
        <span class="s-gr">{t('topology.problems.symptom')}</span><span class="s-gr">{t('topology.problems.duration')}</span>
        <span class="mat kod">{#if showCodes}{@html codes}{/if}</span>
        <span class="s-gr">{t('topology.problems.who')}</span>
      </div>
      {#each rows as p (p.node.id)}
        {@const n = p.node}
        <button type="button" class="s-sat {p.tone}" data-id={n.id} aria-pressed={sel === n.id}>
          {@html MARK[p.tone]}
          {#if n.kind === 'mon'}
            <span class="s-ad">{n.name}</span><span class="s-gr s-gk">{group(p)}</span>
            <span class="s-ha" title={n.m.message}>{n.m.message || t(`topology.state.${n.m.state}`)}</span><span class="s-su">{sinceText(n.m.since, now) || '—'}</span>
            <span class="mat">{@html matrix(n)}</span>
            <span class="s-so"><b>{t('topology.problems.whoN', { n: voters(n), on: online.length })}</b>, {whereText(g, n)}</span>
          {:else if n.kind === 'pro'}
            <span class="s-ad">{t('topology.problems.probeName', { name: n.name })}</span><span class="s-gr s-gk">{group(p)}</span>
            <span class="s-ha">{n.st === 'off' ? t('topology.problems.offline') : n.p.uplink === 'oob' ? t('topology.problems.oob') : t('topology.problems.late')}</span>
            <span class="s-su">{(n.st === 'off' && sinceText(n.p.last_seen_at, now)) || '—'}</span>
            <span class="mat-y">{n.st === 'off' ? t('topology.problems.noHeartbeat') : t('topology.problems.agentReports')}</span>
            <span class="s-so">{t('topology.problems.affects', { n: (g.rel.get(n.id) ?? []).length })}</span>
          {:else if n.kind === 'srv'}
            <span class="s-ad">{n.name}</span><span class="s-gr s-gk">{group(p)}</span>
            <span class="s-ha" title={serverSymptom(n)}>{serverSymptom(n)}</span><span class="s-su">{sinceText(n.s.alert_since, now) || '—'}</span>
            <span class="mat-y">{t('topology.problems.agentReports')}</span>
            <span class="s-so">{t('topology.dock.monN', { count: (g.rel.get(n.id) ?? []).filter((r) => r.id[0] === 'm').length })}</span>
          {/if}
        </button>
      {/each}
      {#if all.length > MAX}
        <a class="s-daha" href="#/incidents">{t('topology.problems.more', { count: all.length - MAX })}</a>
      {/if}
    </div>
  {/if}
</section>
