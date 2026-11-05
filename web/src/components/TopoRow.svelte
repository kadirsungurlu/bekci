<script lang="ts">
  // Topoloji satırı. Veri yenilenince yalnız değişen metin ve sınıf yazılır, DOM korunur.
  import Icon from './Icon.svelte';
  import { fmtRelative } from '../lib/format';
  import { t } from '../lib/i18n';
  import { typeLabel } from '../lib/monitorTypes';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
  import type { NotificationType } from '../lib/api';
  import type { Graph, St, TopoNode } from '../lib/topomodel';
  import { monitorAlt, offCount, serverAlt, stateText } from '../lib/topotext';

  let { n, g, rel, sel, hide = false, now }: { n: TopoNode; g: Graph; rel: St | undefined; sel: boolean; hide?: boolean; now: number } = $props();

  const cls = $derived(`o ${n.kind} ${n.st}${rel ? ` ilg i-${rel}` : ''}${sel ? ` sec i-${n.st}` : ''}${hide ? ' gz' : ''}`);
  const num = (v: number | undefined) => (v === undefined ? '' : String(Math.round(v)));
  const alt = $derived.by(() => {
    if (n.kind === 'mon') return monitorAlt(g, n, now);
    if (n.kind === 'srv') return serverAlt(n, now);
    if (n.kind === 'cha') return n.st === 'warn' ? (n.c.last_send?.error ?? t('topology.row.failed', { n: n.c.failed_24h ?? 0 })) : '';
    return n.st === 'off' && !n.local ? t('topology.row.seen', { ago: fmtRelative(n.p.last_seen_at, now) }) : '';
  });
  const label = $derived(`${n.name}, ${stateText(n)}${alt ? `, ${alt}` : ''}`);
</script>

<button type="button" class={cls} id="o-{n.id}" data-id={n.id} tabindex="-1" aria-pressed={sel} aria-label={label}>
  {#if n.kind === 'mon'}
    <svg width="14" height="14" aria-hidden="true"><use href="#tm-{n.cls}" /></svg><span class="ad">{n.name}</span><span class="silik tur">{typeLabel(n.m.type)}</span>
    <span class="say-n">{n.st === 'off' ? '' : `${n.pro.length - n.bad.length - offCount(g, n)}/${n.pro.length}`}</span>
    <span class="say-n">{n.m.ping_ms > 0 ? Math.round(n.m.ping_ms) : ''}</span>
  {:else if n.kind === 'srv'}
    <svg width="14" height="14" aria-hidden="true"><use href="#tm-{n.cls}" /></svg><span class="ad">{n.name}</span>
    <span class="say-n" class:yuk={(n.s.cpu ?? 0) >= 90}>{num(n.s.cpu)}</span><span class="say-n" class:yuk={(n.s.mem ?? 0) >= 90}>{num(n.s.mem)}</span><span
      class="say-n"
      class:yuk={(n.s.disk ?? 0) >= 90}>{num(n.s.disk)}</span
    >
  {:else if n.kind === 'pro'}
    <svg width="14" height="14" aria-hidden="true"><use href="#tm-{n.cls}" /></svg><span class="ad">{n.name}</span><span class="silik">{n.code}</span>
    <span class="say-n">{n.st !== 'off' && n.p.panel_rtt_ms !== undefined ? `${Math.round(n.p.panel_rtt_ms)} ms` : ''}</span>
  {:else}
    {@const s = NOTIFY_STYLE[n.c.type as NotificationType]}
    <span class="ic-kanal" style:color={s?.color}><Icon name={s?.icon ?? 'send'} size={14} /></span><span class="ad">{n.name}</span>
    <span class="silik">{NOTIFY_LABELS[n.c.type as NotificationType] ?? n.c.type}</span>
  {/if}
  {#if alt}<span class="alt" class:gri={n.st === 'off'}>{alt}</span>{/if}
</button>
