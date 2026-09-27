<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type NotificationChannel } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { toast } from '../lib/ui.svelte';
  import { collator } from '../lib/format';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
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
    toast.success(created ? `“${ch.name}” eklendi` : 'Kanal kaydedildi');
    // "Mevcut tüm monitörlere ekle" monitörlerin kanal listesini değiştirmiş olabilir.
    live.refresh();
  }

  function deleted(id: number) {
    const ch = list.find((x) => x.id === id);
    list = list.filter((x) => x.id !== id);
    toast.success(ch ? `“${ch.name}” silindi` : 'Kanal silindi');
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
  <h1>Bildirimler<span class="dot">.</span></h1>
  <button class="btn primary" onclick={() => openForm(null)}><Icon name="plus" size={16} /> Yeni kanal</button>
</div>

{#if loading}
  <div class="skeleton" style="height:160px"></div>
{:else if error}
  <div class="card empty">
    <h3>Kanallar yüklenemedi</h3>
    <p>{error}</p>
    <button class="btn primary" onclick={load}>Tekrar dene</button>
  </div>
{:else if list.length === 0}
  <div class="card empty">
    <div class="bell"><Icon name="bell" size={30} /></div>
    <h3>Henüz bildirim kanalı yok</h3>
    <p>Bir monitör çalışmadığında haberdar olmak için WhatsApp, Telegram, e-posta veya başka bir kanal ekleyin.</p>
    <button class="btn primary" onclick={() => openForm(null)}><Icon name="plus" size={16} /> Kanal ekle</button>
  </div>
{:else}
  <div class="card list">
    {#each sorted as ch (ch.id)}
      {@const st = NOTIFY_STYLE[ch.type]}
      <button type="button" class="item" class:inactive={!ch.active} onclick={() => openForm(ch)}>
        <span class="ticon" style="background:{st?.color ?? 'var(--accent)'}">{st?.short ?? '?'}</span>
        <span class="info">
          <span class="name">
            {ch.name}
            {#if ch.is_default}<span class="badge accent">Varsayılan</span>{/if}
            {#if !ch.active}<span class="badge paused">Pasif</span>{/if}
          </span>
          <span class="sub">
            {NOTIFY_LABELS[ch.type] ?? ch.type} · {usage.get(ch.id) ?? 0} monitör
          </span>
        </span>
        <span class="edit"><Icon name="edit" size={15} /> <span class="lbl">Düzenle</span></span>
      </button>
    {/each}
  </div>
  <p class="help foot">
    “Varsayılan” kanallar yeni eklenen monitörlerde otomatik seçili gelir. Her monitörün kanallarını monitör düzenleme
    sayfasından değiştirebilirsiniz.
  </p>
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
  .item:hover {
    background: var(--card-hover);
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
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: #fff;
    font-weight: 800;
    font-size: 0.95rem;
    flex-shrink: 0;
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
