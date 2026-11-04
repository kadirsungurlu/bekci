<script lang="ts">
  // Grup köşesindeki sayaçlar: her rozette durum işareti ve ipucu metni; kapalı/duraklatılmış
  // sayısı da ayrı rozet (toplamla fark anlaşılsın).
  import { t } from '../lib/i18n';
  import type { Cls, Counts } from '../lib/topoview';
  import StateMark from './StateMark.svelte';

  let { c }: { c: Counts } = $props();

  const pills = $derived(
    ([['bad', c.bad], ['warn', c.warn], ['ok', c.ok], ['off', c.off]] as [Cls & keyof Counts, number][]).filter(([k, n]) => n > 0 || k === 'ok'),
  );
</script>

<span class="cnt" role="group" aria-label={t('topology.countsAria', { ok: c.ok, warn: c.warn, bad: c.bad, off: c.off })}>
  {#each pills as [k, n] (k)}
    <span class={`n ${k}`} data-count={k} title={t(`topology.cnt.${k}`)}><StateMark cls={k} size={12} label={t(`topology.cnt.${k}`)} />{n}</span>
  {/each}
  <span class="tot">/ {c.total}</span>
</span>

<style>
  .cnt {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    font-size: 0.78rem;
    font-variant-numeric: tabular-nums;
  }
  .n {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 7px 1px 3px;
    border-radius: 999px;
    font-weight: 600;
  }
  .n.ok {
    background: var(--up-soft);
    color: var(--up-text);
  }
  .n.warn {
    background: var(--pending-soft);
    color: var(--pending-text);
  }
  .n.bad {
    background: var(--down-soft);
    color: var(--down-text);
  }
  .n.off {
    background: var(--border);
    color: var(--text-2);
  }
  .tot {
    color: var(--muted);
  }
</style>
