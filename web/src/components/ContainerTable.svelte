<script lang="ts">
  // Docker konteynerleri: sütuna göre sıralanır, 10'dan fazlaysa filtre kutusu çıkar.
  import type { ContainerInfo } from '../lib/api';
  import { collator, fmtBytes, fmtDec, fmtRate, lower } from '../lib/format';
  import Icon from './Icon.svelte';

  let { containers }: { containers: ContainerInfo[] } = $props();

  type Key = 'name' | 'cpu' | 'mem' | 'rx' | 'tx';
  const COLS: { key: Key; label: string }[] = [
    { key: 'name', label: 'Ad' },
    { key: 'cpu', label: 'CPU' },
    { key: 'mem', label: 'RAM' },
    { key: 'rx', label: 'Ağ ↓' },
    { key: 'tx', label: 'Ağ ↑' },
  ];
  const LIMIT = 10;

  let sortKey = $state<Key>('cpu');
  let desc = $state(true);
  let q = $state('');
  let showAll = $state(false);

  function setSort(k: Key) {
    if (sortKey === k) desc = !desc;
    else {
      sortKey = k;
      desc = k !== 'name';
    }
  }

  const val = (c: ContainerInfo, k: Key): number =>
    k === 'cpu' ? c.cpu : k === 'mem' ? c.mem : k === 'rx' ? c.net_rx_bps : c.net_tx_bps;

  const rows = $derived.by(() => {
    const needle = lower(q.trim());
    const list = needle ? containers.filter((c) => lower(c.name).includes(needle)) : containers.slice();
    list.sort((a, b) => {
      const r = sortKey === 'name' ? collator.compare(a.name, b.name) : val(a, sortKey) - val(b, sortKey);
      return (desc ? -r : r) || collator.compare(a.name, b.name);
    });
    return list;
  });
  const shown = $derived(showAll || q.trim() ? rows : rows.slice(0, LIMIT));
  const ariaSort = (k: Key) => (sortKey === k ? (desc ? 'descending' : 'ascending') : 'none');
</script>

{#if containers.length > LIMIT}
  <div class="tools">
    <div class="search">
      <span class="s-ic"><Icon name="search" size={15} /></span>
      <input class="input" type="search" placeholder="Konteyner ara" bind:value={q} aria-label="Konteyner ara" />
    </div>
    <select class="input msort" aria-label="Sıralama" value={sortKey} onchange={(e) => setSort((e.currentTarget as HTMLSelectElement).value as Key)}>
      {#each COLS as c (c.key)}<option value={c.key}>{c.label}</option>{/each}
    </select>
  </div>
{/if}

<div class="ct" role="table" aria-label="Konteynerler">
  <div class="hrow" role="row">
    {#each COLS as c (c.key)}
      <span role="columnheader" aria-sort={ariaSort(c.key)} class="h-{c.key}">
        <button type="button" class:on={sortKey === c.key} onclick={() => setSort(c.key)}>
          {c.label}
          {#if sortKey === c.key}<Icon name={desc ? 'arrow-down' : 'arrow-up'} size={12} />{/if}
        </button>
      </span>
    {/each}
  </div>
  {#each shown as c (c.id || c.name)}
    <div class="crow" role="row">
      <span class="c-name" role="cell" title={c.name}>{c.name}</span>
      <span class="c-cpu" role="cell"><span class="ml">CPU</span>%{fmtDec(c.cpu, 1)}</span>
      <span class="c-mem" role="cell">
        <span class="ml">RAM</span>{fmtBytes(c.mem)}{#if c.mem_limit}<span class="lim">&nbsp;/ {fmtBytes(c.mem_limit)}</span>{/if}
      </span>
      <span class="c-rx" role="cell"><span class="ml">↓</span>{fmtRate(c.net_rx_bps)}</span>
      <span class="c-tx" role="cell"><span class="ml">↑</span>{fmtRate(c.net_tx_bps)}</span>
    </div>
  {:else}
    <div class="empty-row">Eşleşen konteyner yok.</div>
  {/each}
</div>

{#if !q.trim() && rows.length > LIMIT}
  <button type="button" class="linkbtn more" onclick={() => (showAll = !showAll)}>
    {showAll ? 'Daha az göster' : `Tümünü göster (${rows.length})`}
  </button>
{/if}

<style>
  .tools {
    display: flex;
    gap: 8px;
    margin-bottom: 10px;
  }
  .search {
    position: relative;
    flex: 1 1 200px;
    max-width: 320px;
  }
  .search .input {
    padding-left: 34px;
    height: 36px;
  }
  .s-ic {
    position: absolute;
    left: 11px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    color: var(--muted);
    pointer-events: none;
  }
  .msort {
    display: none;
    width: auto;
    height: 36px;
  }
  .hrow,
  .crow {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(64px, 0.6fr) minmax(110px, 1.1fr) minmax(96px, 0.9fr) minmax(96px, 0.9fr);
    gap: 12px;
    align-items: center;
    padding: 8px 4px;
  }
  .hrow {
    border-bottom: 1px solid var(--border);
    padding-top: 0;
  }
  .hrow button {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    background: none;
    border: none;
    padding: 2px 0;
    font: inherit;
    font-size: 0.72rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--muted);
    cursor: pointer;
    border-radius: 4px;
  }
  .hrow button.on {
    color: var(--text);
  }
  .crow {
    border-bottom: 1px solid var(--border);
    font-size: 0.88rem;
    font-variant-numeric: tabular-nums;
  }
  .crow:last-child {
    border-bottom: none;
  }
  .c-name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .c-rx,
  .c-tx,
  .c-mem {
    color: var(--text-2);
    white-space: nowrap;
  }
  .lim {
    color: var(--muted);
  }
  .ml {
    display: none;
  }
  .empty-row {
    padding: 14px 4px;
    color: var(--muted);
  }
  .more {
    margin-top: 8px;
    font-size: 0.86rem;
  }
  @media (max-width: 640px) {
    .msort {
      display: block;
    }
    .hrow {
      display: none;
    }
    .crow {
      display: flex;
      flex-wrap: wrap;
      gap: 2px 14px;
      padding: 9px 2px;
      font-size: 0.82rem;
    }
    .c-name {
      flex: 1 1 100%;
      min-width: 0;
      font-size: 0.9rem;
    }
    /* Satır düzeni: ad / CPU + RAM / ağ ↓↑ */
    .c-cpu,
    .c-mem {
      order: 2;
    }
    .crow::after {
      content: '';
      flex-basis: 100%;
      order: 3;
    }
    .c-rx,
    .c-tx {
      order: 4;
    }
    .ml {
      display: inline;
      color: var(--muted);
      margin-right: 4px;
      font-weight: 600;
    }
  }
</style>
