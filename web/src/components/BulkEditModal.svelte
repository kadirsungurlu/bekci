<script lang="ts">
  // Toplu işlem: seçili monitörlere etiket veya bildirim kanalı ekle / çıkar.
  import { onMount } from 'svelte';
  import { api, errorMessage, type BulkAction, type NotificationChannel, type Tag } from '../lib/api';
  import { collator } from '../lib/format';
  import { live } from '../lib/live.svelte';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
  import { toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import TagChip from './TagChip.svelte';
  import TagDialog from './TagDialog.svelte';

  let {
    open = $bindable(false),
    kind,
    ids,
    ondone,
  }: {
    open?: boolean;
    kind: 'tag' | 'notify';
    ids: number[];
    ondone?: () => void;
  } = $props();

  let mode = $state<'add' | 'remove'>('add');
  let tags = $state.raw<Tag[]>([]);
  let channels = $state.raw<NotificationChannel[]>([]);
  let loaded = $state(false);
  let pick = $state<number | null>(null);
  let value = $state('');
  let busy = $state(false);
  let error = $state('');
  let tagDialogOpen = $state(false);
  let tagDialogKey = $state(0);

  // svelte-ignore state_referenced_locally
  const n = ids.length;
  const title = $derived(kind === 'tag' ? 'Etiket' : 'Bildirim kanalı');

  onMount(async () => {
    try {
      if (kind === 'tag') tags = (await api.tags()).slice().sort((a, b) => collator.compare(a.name, b.name));
      else channels = (await api.notifications()).slice().sort((a, b) => collator.compare(a.name, b.name));
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loaded = true;
    }
  });

  // Seçili monitörlerden kaçında bu etiket/kanal var (bilgi için).
  function usage(id: number): number {
    let c = 0;
    for (const mid of ids) {
      const m = live.byId(mid);
      if (!m) continue;
      if (kind === 'tag' ? (m.tags ?? []).some((t) => t.id === id) : m.notification_ids.includes(id)) c++;
    }
    return c;
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (pick === null) return (error = kind === 'tag' ? 'Bir etiket seçin.' : 'Bir kanal seçin.');
    let a: BulkAction;
    if (kind === 'tag') a = mode === 'add' ? { action: 'add_tag', tag_id: pick, value: value.trim() } : { action: 'remove_tag', tag_id: pick };
    else a = { action: mode === 'add' ? 'add_notification' : 'remove_notification', notification_id: pick };
    busy = true;
    try {
      const res = await api.bulkMonitors(ids, a);
      live.upsertMany(res.monitors);
      const verb = kind === 'tag' ? (mode === 'add' ? 'etiket eklendi' : 'etiket kaldırıldı') : mode === 'add' ? 'kanal eklendi' : 'kanal çıkarıldı';
      if (res.changed) toast.success(`${res.changed} monitöre ${verb}`);
      else toast.info('Değişiklik gerekmedi: seçili monitörler zaten bu durumda');
      ondone?.();
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  function created(t: Tag) {
    tags = [...tags, t].sort((a, b) => collator.compare(a.name, b.name));
    pick = t.id;
  }
</script>

<Modal bind:open title={kind === 'tag' ? 'Etiket ekle / kaldır' : 'Bildirim kanalı ekle / çıkar'} width={500}>
  <form id="bulkf" class="stack" onsubmit={submit} novalidate>
    <div class="top">
      <span class="count"><b>{n}</b> monitör seçili</span>
      <div class="seg" role="radiogroup" aria-label="İşlem">
        <button type="button" role="radio" aria-checked={mode === 'add'} class:active={mode === 'add'} onclick={() => (mode = 'add')}>
          Ekle
        </button>
        <button type="button" role="radio" aria-checked={mode === 'remove'} class:active={mode === 'remove'} onclick={() => (mode = 'remove')}>
          {kind === 'tag' ? 'Kaldır' : 'Çıkar'}
        </button>
      </div>
    </div>

    {#if !loaded}
      <div class="skeleton" style="height:120px"></div>
    {:else if kind === 'tag' && tags.length === 0 && mode === 'remove'}
      <div class="empty-note">Henüz etiket yok.</div>
    {:else if kind === 'notify' && channels.length === 0}
      <div class="empty-note">
        Henüz bildirim kanalı yok. <a href="#/notifications" onclick={() => (open = false)}>Kanal ekleyin</a>.
      </div>
    {:else}
      <div class="opts" role="radiogroup" aria-label={title}>
        {#if kind === 'tag' && tags.length === 0}
          <div class="empty-note">Henüz etiket yok. Aşağıdan yeni bir etiket oluşturun.</div>
        {:else if kind === 'tag'}
          {#each tags as t (t.id)}
            {@const u = usage(t.id)}
            <label class="opt" class:on={pick === t.id}>
              <input type="radio" name="bulk-pick" value={t.id} checked={pick === t.id} onchange={() => (pick = t.id)} />
              <span class="o-main"><TagChip name={t.name} color={t.color} /></span>
              {#if u}<small class="u">{u === n ? 'hepsinde var' : `${u}/${n} monitörde var`}</small>{/if}
            </label>
          {/each}
        {:else}
          {#each channels as ch (ch.id)}
            {@const st = NOTIFY_STYLE[ch.type]}
            {@const u = usage(ch.id)}
            <label class="opt" class:on={pick === ch.id}>
              <input type="radio" name="bulk-pick" value={ch.id} checked={pick === ch.id} onchange={() => (pick = ch.id)} />
              <span class="ticon" style="--c:{st?.color ?? 'var(--accent)'}"><Icon name={st?.icon ?? 'bell'} size={15} /></span>
              <span class="o-main">
                <span class="nm">{ch.name}</span>
                <small>{NOTIFY_LABELS[ch.type] ?? ch.type}{ch.active ? '' : ' · pasif'}</small>
              </span>
              {#if u}<small class="u">{u === n ? 'hepsinde var' : `${u}/${n} monitörde var`}</small>{/if}
            </label>
          {/each}
        {/if}
      </div>
      {#if kind === 'tag' && mode === 'add'}
        <div class="tag-extra">
          <div class="field">
            <label for="bk-val">Değer <span class="muted">(isteğe bağlı)</span></label>
            <input id="bk-val" class="input" maxlength="100" bind:value placeholder="Ör. canlı" />
          </div>
          <button
            type="button"
            class="btn sm"
            onclick={() => {
              tagDialogKey++;
              tagDialogOpen = true;
            }}><Icon name="plus" size={14} /> Yeni etiket</button
          >
        </div>
      {/if}
      {#if kind === 'tag' && mode === 'remove'}
        <p class="help nomargin">Etiket, seçili monitörlerden tüm değerleriyle kaldırılır.</p>
      {/if}
    {/if}
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
    <button type="submit" form="bulkf" class="btn primary" disabled={busy || pick === null}>
      {#if busy}<span class="spinner"></span>{/if}
      {mode === 'add' ? `${n} monitöre ekle` : `${n} monitörden ${kind === 'tag' ? 'kaldır' : 'çıkar'}`}
    </button>
  {/snippet}
</Modal>

{#key tagDialogKey}
  {#if tagDialogKey > 0}
    <TagDialog bind:open={tagDialogOpen} onsaved={created} />
  {/if}
{/key}

<style>
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .count {
    color: var(--text-2);
    font-size: 0.9rem;
  }
  .count b {
    color: var(--text);
  }
  .opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: min(46vh, 340px);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 2px;
    margin: -2px;
  }
  .opt {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 9px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
    cursor: pointer;
    min-height: 46px;
  }
  .opt.on {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 7%, var(--input));
  }
  @media (hover: hover) {
    .opt:not(.on):hover {
      border-color: var(--border-hover);
    }
  }
  /* Radyo düğmesi: temadaki onay kutusuyla aynı dil, yuvarlak. */
  .opt input {
    appearance: none;
    -webkit-appearance: none;
    width: 18px;
    height: 18px;
    margin: 0;
    border: 1.5px solid var(--border-hover);
    border-radius: 50%;
    background: var(--input);
    flex-shrink: 0;
    cursor: pointer;
    display: grid;
    place-content: center;
  }
  .opt input:checked {
    border-color: var(--accent);
  }
  .opt input:checked::after {
    content: '';
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
  }
  .o-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .o-main :global(.tag-chip) {
    align-self: flex-start;
  }
  .nm {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .o-main small {
    color: var(--muted);
    font-size: 0.8rem;
  }
  .u {
    color: var(--muted);
    font-size: 0.78rem;
    white-space: nowrap;
  }
  .ticon {
    width: 30px;
    height: 30px;
    border-radius: 8px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: color-mix(in srgb, var(--c) 16%, var(--card));
    color: var(--c);
    flex-shrink: 0;
  }
  .tag-extra {
    display: flex;
    align-items: flex-end;
    gap: 10px;
  }
  .tag-extra .field {
    flex: 1;
  }
  .tag-extra .btn {
    height: 40px;
  }
  .empty-note {
    padding: 14px;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    color: var(--muted);
    font-size: 0.9rem;
    text-align: center;
  }
  .nomargin {
    margin: 0;
  }
  .spacer {
    flex: 1;
  }
</style>
