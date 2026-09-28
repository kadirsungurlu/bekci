<script lang="ts">
  // Aranabilir çoklu monitör seçici (kullanıcı erişimi, bakım, grup, durum sayfası).
  import type { MonitorView } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { collator, lower, monitorKind } from '../lib/format';
  import { displayTarget, typeLabel } from '../lib/monitorTypes';
  import StatusIcon from './StatusIcon.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    selected = $bindable([]),
    exclude = [],
    label: labelProp,
    id = 'mp',
  }: { selected?: number[]; exclude?: number[]; label?: string; id?: string } = $props();
  const label = $derived(labelProp ?? t('nav.monitors'));

  let search = $state('');

  const all = $derived(
    live.monitors.filter((m) => !exclude.includes(m.id)).sort((a, b) => collator.compare(a.name, b.name)),
  );
  const visible = $derived.by(() => {
    const q = lower(search.trim());
    if (!q) return all;
    return all.filter((m) => lower(m.name).includes(q) || lower(m.target).includes(q));
  });
  const set = $derived(new Set(selected));
  // Seçili ama listede olmayan (silinmiş/görünmeyen) kimlikler korunur.
  const count = $derived(all.filter((m) => set.has(m.id)).length);

  function toggle(m: MonitorView, on: boolean) {
    if (on && !set.has(m.id)) selected = [...selected, m.id];
    else if (!on) selected = selected.filter((x) => x !== m.id);
  }
  function selectVisible() {
    const add = visible.filter((m) => !set.has(m.id)).map((m) => m.id);
    selected = [...selected, ...add];
  }
  function clearVisible() {
    const drop = new Set(visible.map((m) => m.id));
    selected = selected.filter((x) => !drop.has(x));
  }
</script>

<div class="picker">
  <div class="top">
    <div class="search">
      <span class="s-ic"><Icon name="search" size={15} /></span>
      <input
        id="{id}-q"
        class="input"
        type="search"
        placeholder={t('monitors.picker.searchPh')}
        aria-label={t('monitors.picker.searchAria', { label })}
        bind:value={search}
      />
    </div>
    <div class="tools">
      <span class="muted small" aria-live="polite">{t('monitors.picker.count', { count, total: all.length })}</span>
      <button type="button" class="linkbtn small" onclick={selectVisible}
        >{search ? t('monitors.picker.selectFound') : t('monitors.picker.selectAll')}</button
      >
      <button type="button" class="linkbtn small" onclick={clearVisible}>{t('monitors.picker.clear')}</button>
    </div>
  </div>
  <div class="list" role="group" aria-label={label}>
    {#each visible as m (m.id)}
      <label class="check item">
        <input type="checkbox" checked={set.has(m.id)} onchange={(e) => toggle(m, e.currentTarget.checked)} />
        <StatusIcon kind={monitorKind(m)} size={18} />
        <span class="nm">
          <span class="t">{m.name}</span>
          <small>{typeLabel(m.type)} · {displayTarget(m.target)}</small>
        </span>
      </label>
    {:else}
      <div class="none muted small">{all.length === 0 ? t('monitors.picker.empty') : t('monitors.noMatch')}</div>
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
    gap: 10px;
    flex-wrap: wrap;
    padding: 8px;
    border-bottom: 1px solid var(--border);
  }
  .search {
    position: relative;
    flex: 1 1 200px;
    min-width: 0;
  }
  .s-ic {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    color: var(--muted);
    pointer-events: none;
  }
  .search .input {
    height: 36px;
    padding-left: 32px;
    background: var(--card);
  }
  .tools .linkbtn {
    font-size: 0.85rem;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .list {
    max-height: 260px;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 4px;
  }
  .item {
    align-items: center;
    padding: 7px 8px;
    border-radius: 7px;
  }
  @media (hover: hover) {
    .item:hover {
      background: var(--card);
    }
  }
  .item input {
    margin: 0;
  }
  .nm {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .t {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
    font-size: 0.9rem;
  }
  .nm small {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .none {
    padding: 14px;
    text-align: center;
  }
</style>
