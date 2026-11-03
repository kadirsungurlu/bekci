<script lang="ts">
  // Sayfa gezgini: toplam kayıt, sayfa başına (25/50/100), önceki/sonraki ve
  // sayfa numaraları (uzun listede kısaltılmış). Sayfa 1'den başlar.
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    total,
    page,
    perPage,
    onpage,
    onperpage,
  }: {
    total: number;
    page: number;
    perPage: number;
    onpage: (p: number) => void;
    onperpage: (n: number) => void;
  } = $props();

  const SIZES = [25, 50, 100];
  const pages = $derived(Math.max(1, Math.ceil(total / perPage)));
  const from = $derived(total === 0 ? 0 : (page - 1) * perPage + 1);
  const to = $derived(Math.min(total, page * perPage));

  // 1 … p-1 p p+1 … son (0 = boşluk)
  const items = $derived.by(() => {
    const out: number[] = [];
    for (let i = 1; i <= pages; i++) {
      if (i === 1 || i === pages || Math.abs(i - page) <= 1) out.push(i);
      else if (out[out.length - 1] !== 0) out.push(0);
    }
    return out;
  });
</script>

<nav class="pager" aria-label={t('common.pager.label')} data-testid="pager">
  <span class="info" data-testid="pager-total">
    {total > perPage ? t('common.pager.range', { from, to, n: total }) : t('common.pager.total', { n: total })}
  </span>
  {#if pages > 1}
    <div class="pages">
      <button type="button" class="btn sm icon" onclick={() => onpage(page - 1)} disabled={page <= 1} aria-label={t('common.pager.prev')} data-testid="pager-prev">
        <Icon name="chevron-left" size={15} />
      </button>
      {#each items as n, i (n === 0 ? `g${i}` : n)}
        {#if n === 0}
          <span class="gap" aria-hidden="true">…</span>
        {:else}
          <button
            type="button"
            class="btn sm num"
            class:cur={n === page}
            aria-current={n === page ? 'page' : undefined}
            aria-label={t('common.pager.page', { n })}
            onclick={() => onpage(n)}>{n}</button
          >
        {/if}
      {/each}
      <button type="button" class="btn sm icon" onclick={() => onpage(page + 1)} disabled={page >= pages} aria-label={t('common.pager.next')} data-testid="pager-next">
        <Icon name="chevron-right" size={15} />
      </button>
    </div>
  {/if}
  <label class="size">
    <span>{t('common.pager.perPage')}</span>
    <select class="input sm" value={perPage} onchange={(e) => onperpage(Number((e.currentTarget as HTMLSelectElement).value))} data-testid="pager-size">
      {#each SIZES as n (n)}<option value={n}>{n}</option>{/each}
    </select>
  </label>
</nav>

<style>
  .pager {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 10px 16px;
    margin-top: 14px;
    font-size: 0.86rem;
    color: var(--text-2);
  }
  .pages {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .num {
    min-width: 34px;
    justify-content: center;
    font-variant-numeric: tabular-nums;
  }
  .num.cur {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 600;
  }
  .gap {
    padding: 0 4px;
    color: var(--muted);
  }
  .size {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .size select {
    width: auto;
    min-width: 72px;
  }
  @media (max-width: 640px) {
    .pager {
      justify-content: center;
    }
    .info {
      flex-basis: 100%;
      text-align: center;
    }
    .pages :global(.btn) {
      min-height: 44px;
      min-width: 44px;
    }
  }
</style>
