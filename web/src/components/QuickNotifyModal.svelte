<script lang="ts">
  // Listeden hızlı işlem: monitörün bildirim kanalları. Her değişiklik hemen kaydedilir.
  import { onMount } from 'svelte';
  import { api, errorMessage, type MonitorView, type NotificationChannel } from '../lib/api';
  import { collator } from '../lib/format';
  import { live } from '../lib/live.svelte';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
  import { toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import { t, tParts } from '../lib/i18n';

  let { open = $bindable(false), m }: { open?: boolean; m: MonitorView } = $props();

  let channels = $state.raw<NotificationChannel[]>([]);
  let loaded = $state(false);
  let loadError = $state('');
  // svelte-ignore state_referenced_locally
  let selected = $state<number[]>([...m.notification_ids]);
  let saving = $state<number | null>(null);
  let savedAt = $state(0);

  onMount(async () => {
    try {
      const list = await api.notifications();
      channels = list.slice().sort((a, b) => Number(b.active) - Number(a.active) || collator.compare(a.name, b.name));
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loaded = true;
    }
  });

  async function toggle(id: number, on: boolean) {
    const prev = selected;
    selected = on ? [...selected, id] : selected.filter((x) => x !== id);
    saving = id;
    try {
      const res = await api.setMonitorNotifications(m.id, selected);
      live.upsert(res);
      selected = [...res.notification_ids];
      savedAt = Date.now();
    } catch (e) {
      selected = prev;
      toast.error(errorMessage(e));
    } finally {
      saving = null;
    }
  }
</script>

<Modal bind:open title={t('monitors.quickNotify.title')} width={500}>
  <p class="lead">
    {#each tParts('monitors.quickNotify.lead') as p, i (i)}{#if p.slot === 'name'}<b>{m.name}</b>{:else}{p.text}{/if}{/each}
  </p>
  {#if !loaded}
    <div class="skeleton" style="height:120px"></div>
  {:else if loadError}
    <div class="alert error">{loadError}</div>
  {:else if channels.length === 0}
    <div class="none">
      <Icon name="bell" size={22} />
      <p>{t('monitors.quickNotify.none')}</p>
      <a class="btn sm" href="#/notifications" onclick={() => (open = false)}><Icon name="plus" size={14} /> {t('monitors.quickNotify.addChannel')}</a>
    </div>
  {:else}
    <div class="list" role="group" aria-label={t('monitors.quickNotify.title')}>
      {#each channels as ch (ch.id)}
        {@const st = NOTIFY_STYLE[ch.type]}
        {@const on = selected.includes(ch.id)}
        <label class="ch" class:on class:off={!ch.active}>
          <span class="ticon" style="--c:{st?.color ?? 'var(--accent)'}"><Icon name={st?.icon ?? 'bell'} size={16} /></span>
          <span class="info">
            <span class="nm">{ch.name}</span>
            <small>{NOTIFY_LABELS[ch.type] ?? ch.type}{ch.active ? '' : ` · ${t('monitors.inactive')}`}</small>
          </span>
          {#if saving === ch.id}<span class="spinner"></span>{/if}
          <span class="check">
            <input
              type="checkbox"
              checked={on}
              disabled={saving !== null}
              onchange={(e) => toggle(ch.id, e.currentTarget.checked)}
              aria-label={t('monitors.quickNotify.sendTo', { name: ch.name })}
            />
          </span>
        </label>
      {/each}
    </div>
  {/if}
  {#snippet footer()}
    <a class="manage" href="#/notifications" onclick={() => (open = false)}>{t('monitors.quickNotify.manage')}</a>
    <div class="spacer"></div>
    {#if savedAt}<span class="saved"><Icon name="check" size={14} /> {t('common.saved')}</span>{/if}
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.close')}</button>
  {/snippet}
</Modal>

<style>
  .lead {
    margin: 0 0 14px;
    color: var(--text-2);
    font-size: 0.9rem;
  }
  .lead b {
    color: var(--text);
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .ch {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
    cursor: pointer;
    transition: border-color 0.12s;
  }
  .ch.on {
    border-color: var(--accent-border);
  }
  .ch.off .ticon,
  .ch.off .info {
    opacity: 0.6;
  }
  @media (hover: hover) {
    .ch:hover {
      border-color: var(--border-hover);
    }
    .ch.on:hover {
      border-color: var(--accent);
    }
  }
  .ticon {
    width: 32px;
    height: 32px;
    border-radius: 9px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: color-mix(in srgb, var(--c) 16%, var(--card));
    color: var(--c);
    flex-shrink: 0;
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .nm {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .info small {
    color: var(--muted);
    font-size: 0.8rem;
  }
  .check input {
    margin: 0;
  }
  .none {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-align: center;
    padding: 12px 8px 4px;
    color: var(--muted);
  }
  .none p {
    margin: 0 0 6px;
    font-size: 0.9rem;
  }
  .manage {
    font-size: 0.88rem;
  }
  .saved {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--up);
    font-size: 0.85rem;
    font-weight: 600;
  }
  .spacer {
    flex: 1;
  }
</style>
