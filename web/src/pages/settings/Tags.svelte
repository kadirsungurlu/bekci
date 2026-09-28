<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type Tag } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { confirmDialog, toast } from '../../lib/ui.svelte';
  import { collator } from '../../lib/format';
  import RowMenu, { type MenuItem } from '../../components/RowMenu.svelte';
  import TagChip from '../../components/TagChip.svelte';
  import TagDialog from '../../components/TagDialog.svelte';
  import Icon from '../../components/Icon.svelte';
  import { t, tParts } from '../../lib/i18n';

  let tags = $state.raw<Tag[]>([]);
  let loading = $state(true);
  let loadError = $state('');

  async function load() {
    try {
      tags = await api.tags();
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  const sorted = $derived(tags.slice().sort((a, b) => collator.compare(a.name, b.name)));

  let formOpen = $state(false);
  let editing = $state<Tag | null>(null);
  let formKey = $state(0);

  function openForm(tag: Tag | null) {
    editing = tag;
    formKey++;
    formOpen = true;
  }

  function saved(tag: Tag, created: boolean) {
    tags = created
      ? [...tags, { ...tag, monitor_count: 0 }]
      : tags.map((x) => (x.id === tag.id ? { ...tag, monitor_count: x.monitor_count } : x));
    toast.success(created ? t('tags.added', { name: tag.name }) : t('tags.saved'));
    // Monitör listesindeki etiket adları/renkleri güncellensin.
    if (!created) live.refresh();
  }

  async function remove(tag: Tag) {
    const ok = await confirmDialog({
      title: t('tags.deleteTitle'),
      message: t('tags.deleteMsg', { name: tag.name, count: tag.monitor_count }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteTag(tag.id);
      tags = tags.filter((x) => x.id !== tag.id);
      toast.success(t('tags.deleted'));
      live.refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const menu = (tag: Tag): MenuItem[] => [
    { label: t('common.edit'), icon: 'edit', onclick: () => openForm(tag) },
    { label: t('common.delete'), icon: 'trash', danger: true, onclick: () => remove(tag) },
  ];
</script>

<section class="card">
  <div class="head">
    <div>
      <h2 class="card-title">{t('tags.title')}</h2>
      <p class="text-2 small sub">
        {#each tParts('tags.intro') as p, i (i)}
          {#if p.slot === 'example'}<b>{t('tags.example')}</b>{:else}{p.text}{/if}
        {/each}
      </p>
    </div>
    <button class="btn primary" onclick={() => openForm(null)}><Icon name="plus" size={16} /> {t('tags.newTag')}</button>
  </div>

  {#if loading}
    <div class="skeleton" style="height:120px"></div>
  {:else if loadError}
    <div class="alert error">{loadError} <button class="linkbtn" onclick={load}>{t('common.retry')}</button></div>
  {:else if tags.length === 0}
    <div class="none">
      <Icon name="tag" size={22} />
      <span>{t('tags.empty')}</span>
    </div>
  {:else}
    <ul class="tags">
      {#each sorted as tg (tg.id)}
        <li>
          <span class="swatch" style="--sw:{tg.color}" aria-hidden="true"></span>
          <span class="tinfo">
            <TagChip name={tg.name} color={tg.color} />
            <span class="muted small">{t('tags.monitorCount', { count: tg.monitor_count })}</span>
          </span>
          <code class="hex">{tg.color}</code>
          <RowMenu items={menu(tg)} label={t('tags.actionsFor', { name: tg.name })} />
        </li>
      {/each}
    </ul>
  {/if}
</section>

{#key formKey}
  {#if formKey > 0}
    <TagDialog bind:open={formOpen} tag={editing} onsaved={saved} />
  {/if}
{/key}

<style>
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .head > div {
    flex: 1 1 280px;
    min-width: 0;
  }
  .card-title {
    margin-bottom: 4px;
  }
  .sub {
    margin: 0;
    max-width: 720px;
  }
  .none {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--muted);
    padding: 14px 0 4px;
  }
  .tags {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .tags li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
  }
  .tags li:last-child {
    border-bottom: none;
  }
  .swatch {
    width: 14px;
    height: 14px;
    border-radius: 4px;
    background: var(--sw);
    box-shadow: 0 0 0 1px var(--border-strong);
    flex-shrink: 0;
  }
  .tinfo {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .hex {
    color: var(--muted);
  }
  @media (max-width: 640px) {
    .hex {
      display: none;
    }
    .tinfo {
      gap: 4px 10px;
    }
  }
</style>
