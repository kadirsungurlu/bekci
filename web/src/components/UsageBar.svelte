<script lang="ts">
  // Küçük doluluk çubuğu: %80'de turuncuya, %90'da kırmızıya döner.
  import { fmtPctInt } from '../lib/format';
  import { usageLevel } from '../lib/servers.svelte';
  import { t } from '../lib/i18n';

  let {
    value,
    label = '',
    detail = '',
    inline = false,
    bare = false,
  }: {
    /** Yüzde (0-100); null → veri yok. */
    value: number | null | undefined;
    /** Çubuğun üstündeki kısa ad (ör. "CPU"). */
    label?: string;
    /** Sağda yüzdenin yanında gösterilen ek bilgi (ör. "3,1 / 7,8 GB"). */
    detail?: string;
    /** Etiket ve değer çubuğun yanında, tek satırda. */
    inline?: boolean;
    /** Yalnızca çubuk (değer başka yerde yazılıyorsa). */
    bare?: boolean;
  } = $props();

  const level = $derived(usageLevel(value));
  const has = $derived(value !== null && value !== undefined && Number.isFinite(value));
  const width = $derived(has ? Math.max(0, Math.min(100, value!)) : 0);
</script>

<div class="ub {level}" class:inline role="meter" aria-label={label || t('status.chart.usage')} aria-valuemin={0} aria-valuemax={100} aria-valuenow={has ? Math.round(value!) : undefined}>
  {#if !inline && !bare && (label || has)}
    <div class="top">
      {#if label}<span class="l">{label}</span>{/if}
      <span class="v">{has ? fmtPctInt(value) : '—'}{#if detail}<span class="d">&nbsp;· {detail}</span>{/if}</span>
    </div>
  {/if}
  <div class="track"><div class="fill" style="width:{width}%"></div></div>
  {#if inline}
    <span class="v">{has ? fmtPctInt(value) : '—'}</span>
  {/if}
</div>

<style>
  .ub {
    min-width: 0;
    --fill: var(--accent);
  }
  .ub.warn {
    --fill: var(--pending);
  }
  .ub.danger {
    --fill: var(--down);
  }
  .top {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 6px;
    font-size: 0.78rem;
    margin-bottom: 4px;
    white-space: nowrap;
  }
  .l {
    color: var(--muted);
    font-weight: 600;
  }
  .v {
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .d {
    font-weight: 500;
    color: var(--muted);
  }
  .ub.warn .v {
    color: var(--pending);
  }
  .ub.danger .v {
    color: var(--down-text-2);
  }
  .track {
    height: 6px;
    border-radius: 3px;
    background: var(--meter-track);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    border-radius: 3px;
    background: var(--fill);
    transition: width 0.4s ease;
  }
  .inline {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .inline .track {
    flex: 1;
    min-width: 36px;
  }
  .inline .v {
    font-size: 0.82rem;
    min-width: 34px;
    text-align: right;
  }
</style>
