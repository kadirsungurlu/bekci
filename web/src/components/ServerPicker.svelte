<script lang="ts">
  // Çoklu sunucu seçici (müşteri erişimi: kullanıcının görebileceği sunucular).
  import { onMount } from 'svelte';
  import { hostLine, servers, serverTone } from '../lib/servers.svelte';
  import { collator } from '../lib/format';
  import { t } from '../lib/i18n';

  let {
    selected = $bindable([]),
    label,
    id = 'sp',
  }: { selected?: number[]; label?: string; id?: string } = $props();

  const lbl = $derived(label ?? t('servers.list.title'));

  onMount(() => servers.ensure());

  const all = $derived(servers.list.slice().sort((a, b) => collator.compare(a.name, b.name)));
  const set = $derived(new Set(selected));
  const count = $derived(all.filter((s) => set.has(s.id)).length);

  function toggle(sid: number, on: boolean) {
    if (on && !set.has(sid)) selected = [...selected, sid];
    else if (!on) selected = selected.filter((x) => x !== sid);
  }
</script>

<div class="picker">
  <div class="top">
    <span class="lbl" id="{id}-l">{lbl}</span>
    <span class="muted small" aria-live="polite">{t('servers.picker.selected', { n: count, total: all.length })}</span>
  </div>
  <div class="list" role="group" aria-labelledby="{id}-l">
    {#each all as s (s.id)}
      {@const hl = hostLine(s)}
      <label class="check item">
        <input type="checkbox" checked={set.has(s.id)} onchange={(e) => toggle(s.id, e.currentTarget.checked)} />
        <span class="dot {serverTone(s)}" aria-hidden="true"></span>
        <span class="nm">
          <span class="t">{s.name}</span>
          <small title={hl.full !== hl.short ? hl.full : undefined}>{hl.short || t('servers.picker.noData')}</small>
        </span>
      </label>
    {:else}
      <div class="none muted small">{servers.loaded ? t('servers.picker.none') : t('common.loading')}</div>
    {/each}
  </div>
</div>

<style>
  .picker {
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    overflow: hidden;
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }
  .lbl {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-2);
  }
  .list {
    max-height: 220px;
    overflow-y: auto;
    padding: 4px;
    scrollbar-width: thin;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 8px;
    border-radius: 7px;
    cursor: pointer;
  }
  .item:hover {
    background: var(--card-2);
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--paused);
  }
  .dot.up {
    background: var(--up);
  }
  .dot.down {
    background: var(--down);
  }
  .dot.warn {
    background: var(--pending);
  }
  .nm {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .t {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .nm small {
    color: var(--muted);
    font-size: 0.78rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .none {
    padding: 12px;
  }
</style>
