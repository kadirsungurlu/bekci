<script lang="ts">
  // Listeden hızlı işlem: monitörün etiketlerini ekle/kaldır (isteğe bağlı değerleriyle).
  import { onMount } from 'svelte';
  import { api, errorMessage, type MonitorView, type Tag } from '../lib/api';
  import { collator } from '../lib/format';
  import { live } from '../lib/live.svelte';
  import { toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import TagChip from './TagChip.svelte';
  import TagDialog from './TagDialog.svelte';
  import { t, tParts } from '../lib/i18n';

  let { open = $bindable(false), m }: { open?: boolean; m: MonitorView } = $props();

  let allTags = $state.raw<Tag[]>([]);
  let loaded = $state(false);
  // svelte-ignore state_referenced_locally
  let list = $state((m.tags ?? []).map((t) => ({ id: t.id, value: t.value })));
  let pick = $state('');
  let busy = $state(false);
  let error = $state('');
  let tagDialogOpen = $state(false);
  let tagDialogKey = $state(0);

  const byId = $derived(new Map(allTags.map((tg) => [tg.id, tg])));
  const addable = $derived(allTags.filter((tg) => !list.some((x) => x.id === tg.id)).sort((a, b) => collator.compare(a.name, b.name)));

  onMount(async () => {
    try {
      allTags = await api.tags();
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loaded = true;
    }
  });

  function add(idStr: string) {
    const id = Number(idStr);
    pick = '';
    if (!id || list.some((x) => x.id === id)) return;
    list = [...list, { id, value: '' }];
  }

  function created(tag: Tag) {
    allTags = [...allTags, tag];
    add(String(tag.id));
  }

  async function save() {
    error = '';
    busy = true;
    try {
      const tags = await api.setMonitorTags(
        m.id,
        list.map((x) => ({ tag_id: x.id, value: x.value.trim() })),
      );
      live.upsert({ ...(live.byId(m.id) ?? m), tags });
      toast.success(t('monitors.quickTags.saved', { name: m.name }));
      open = false;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open title={t('monitors.tags')} width={520}>
  <div class="stack">
    <p class="lead">
      {#each tParts('monitors.quickTags.lead') as p, i (i)}{#if p.slot === 'name'}<b>{m.name}</b>{:else}{p.text}{/if}{/each}
    </p>
    {#if !loaded}
      <div class="skeleton" style="height:80px"></div>
    {:else}
      {#if list.length}
        <ul class="tags">
          <!-- Aynı etiket farklı değerlerle birden çok kez bağlı olabilir (içe aktarma): anahtar sıradır. -->
          {#each list as mt, i (i)}
            {@const tg = byId.get(mt.id)}
            <li>
              <span class="chip"><TagChip name={tg?.name ?? `#${mt.id}`} color={tg?.color ?? ''} /></span>
              <input
                class="input val"
                maxlength="100"
                bind:value={mt.value}
                placeholder={t('monitors.quickTags.valuePh')}
                aria-label={t('monitors.quickTags.valueAria', { name: tg?.name ?? t('monitors.tagFallback') })}
              />
              <button
                type="button"
                class="btn ghost icon sm"
                aria-label={t('monitors.removeTag', { name: tg?.name ?? '' })}
                onclick={() => (list = list.filter((_, j) => j !== i))}><Icon name="x" size={15} /></button
              >
            </li>
          {/each}
        </ul>
      {:else}
        <div class="empty-tags">{t('monitors.quickTags.empty')}</div>
      {/if}
      <div class="add">
        {#if addable.length}
          <select class="input" bind:value={pick} onchange={() => add(pick)} aria-label={t('monitors.addTag')}>
            <option value="">{t('monitors.addTagOption')}</option>
            {#each addable as tg (tg.id)}<option value={String(tg.id)}>{tg.name}</option>{/each}
          </select>
        {/if}
        <button
          type="button"
          class="btn sm"
          onclick={() => {
            tagDialogKey++;
            tagDialogOpen = true;
          }}><Icon name="plus" size={14} /> {t('monitors.newTag')}</button
        >
      </div>
    {/if}
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </div>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button type="button" class="btn primary" disabled={busy || !loaded} onclick={save}>
      {#if busy}<span class="spinner"></span>{/if}
      {t('common.save')}
    </button>
  {/snippet}
</Modal>

{#key tagDialogKey}
  {#if tagDialogKey > 0}
    <TagDialog bind:open={tagDialogOpen} onsaved={created} />
  {/if}
{/key}

<style>
  .lead {
    margin: 0;
    color: var(--text-2);
    font-size: 0.9rem;
  }
  .lead b {
    color: var(--text);
  }
  .tags {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .tags li {
    display: grid;
    grid-template-columns: minmax(0, 170px) minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
  }
  .chip {
    display: flex;
    min-width: 0;
  }
  .val {
    height: 34px;
  }
  .empty-tags {
    padding: 14px;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--muted);
    font-size: 0.9rem;
    text-align: center;
  }
  .add {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .add select {
    width: auto;
    height: 34px;
    flex: 1 1 200px;
    max-width: 280px;
  }
  .spacer {
    flex: 1;
  }
  @media (max-width: 640px) {
    .tags li {
      grid-template-columns: minmax(0, 1fr) auto;
      row-gap: 8px;
      padding: 8px 6px 10px 10px;
      border: 1px solid var(--border);
      border-radius: var(--radius-sm);
    }
    .val {
      grid-column: 1 / -1;
      grid-row: 2;
      margin-right: 4px;
    }
    .add select {
      max-width: none;
    }
  }
</style>
