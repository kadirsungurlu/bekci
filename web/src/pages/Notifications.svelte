<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type NotificationChannel } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { toast } from '../lib/ui.svelte';
  import { collator } from '../lib/format';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
  import { t } from '../lib/i18n';
  import NotificationForm from '../components/NotificationForm.svelte';
  import Icon from '../components/Icon.svelte';

  let list = $state.raw<NotificationChannel[]>([]);
  let loading = $state(true);
  let error = $state('');

  let formOpen = $state(false);
  let editing = $state<NotificationChannel | null>(null);
  let formKey = $state(0);

  async function load() {
    try {
      list = await api.notifications();
      error = '';
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function openForm(ch: NotificationChannel | null) {
    editing = ch;
    formKey++;
    formOpen = true;
  }

  function saved(ch: NotificationChannel, created: boolean) {
    list = created ? [...list, ch] : list.map((x) => (x.id === ch.id ? ch : x));
    toast.success(created ? t('notifications.list.added', { name: ch.name }) : t('notifications.list.saved'));
    // "Mevcut tüm monitörlere ekle" monitörlerin kanal listesini değiştirmiş olabilir.
    live.refresh();
  }

  function deleted(id: number) {
    const ch = list.find((x) => x.id === id);
    list = list.filter((x) => x.id !== id);
    toast.success(ch ? t('notifications.list.deletedNamed', { name: ch.name }) : t('notifications.list.deleted'));
    live.refresh();
  }

  // Kaç monitöre bağlı olduğu
  const usage = $derived.by(() => {
    const m = new Map<number, number>();
    for (const mon of live.monitors) for (const id of mon.notification_ids) m.set(id, (m.get(id) ?? 0) + 1);
    return m;
  });

  const sorted = $derived(list.slice().sort((a, b) => collator.compare(a.name, b.name)));
</script>

<div class="page-head">
  <h1>{t('notifications.list.title')}<span class="dot">.</span></h1>
  <button class="btn primary" onclick={() => openForm(null)}><Icon name="plus" size={16} /> {t('notifications.list.newChannel')}</button>
</div>

{#if loading}
  <div class="skeleton" style="height:160px"></div>
{:else if error}
  <div class="card empty">
    <h3>{t('notifications.list.loadFailed')}</h3>
    <p>{error}</p>
    <button class="btn primary" onclick={load}>{t('common.retry')}</button>
  </div>
{:else if list.length === 0}
  <div class="card empty">
    <div class="bell"><Icon name="bell" size={30} /></div>
    <h3>{t('notifications.list.emptyTitle')}</h3>
    <p>{t('notifications.list.emptyText')}</p>
    <button class="btn primary" onclick={() => openForm(null)}><Icon name="plus" size={16} /> {t('notifications.list.addChannel')}</button>
  </div>
{:else}
  <div class="card list">
    {#each sorted as ch (ch.id)}
      {@const st = NOTIFY_STYLE[ch.type]}
      <button type="button" class="item" class:inactive={!ch.active} onclick={() => openForm(ch)}>
        <span class="ticon" style="--c:{st?.color ?? 'var(--accent)'}"><Icon name={st?.icon ?? 'bell'} size={18} /></span>
        <span class="info">
          <span class="name">
            {ch.name}
            {#if ch.is_default}<span class="badge accent">{t('notifications.list.default')}</span>{/if}
            {#if !ch.active}<span class="badge paused">{t('notifications.list.disabled')}</span>{/if}
          </span>
          <span class="sub">
            {NOTIFY_LABELS[ch.type] ?? ch.type} · {t('notifications.list.monitorCount', { count: usage.get(ch.id) ?? 0 })}
          </span>
        </span>
        <span class="edit"><Icon name="edit" size={15} /> <span class="lbl">{t('common.edit')}</span></span>
      </button>
    {/each}
  </div>
  <p class="help foot">{t('notifications.list.footHelp')}</p>
{/if}

{#key formKey}
  {#if formKey > 0}
    <NotificationForm bind:open={formOpen} channel={editing} onsaved={saved} ondeleted={deleted} />
  {/if}
{/key}

<style>
  .list {
    padding: 0;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 14px;
    width: 100%;
    padding: 14px 18px;
    background: none;
    border: none;
    border-bottom: 1px solid var(--border);
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .item:last-child {
    border-bottom: none;
  }
  @media (hover: hover) {
    .item:hover {
      background: var(--card-hover);
    }
  }
  .item:first-child {
    border-radius: var(--radius) var(--radius) 0 0;
  }
  .item:last-child {
    border-radius: 0 0 var(--radius) var(--radius);
  }
  .item:only-child {
    border-radius: var(--radius);
  }
  .inactive .ticon,
  .inactive .info {
    opacity: 0.55;
  }
  .ticon {
    width: 38px;
    height: 38px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: color-mix(in srgb, var(--c) 16%, var(--card));
    color: var(--c);
    flex-shrink: 0;
  }
  .item:focus-visible {
    outline-offset: -2px;
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    font-weight: 700;
  }
  .sub {
    font-size: 0.84rem;
    color: var(--muted);
  }
  .edit {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
    font-size: 0.85rem;
  }
  .bell {
    display: flex;
    justify-content: center;
    color: var(--accent-text);
  }
  .foot {
    margin-top: 12px;
  }
  @media (max-width: 640px) {
    .item {
      padding: 12px 14px;
    }
    .lbl {
      display: none;
    }
  }
</style>
