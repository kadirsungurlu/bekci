<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, errorMessage, type Maintenance } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { collator } from '../lib/format';
  import { MAINT_STATUS, STRATEGY_LABELS, maintRank, nextText, scheduleText } from '../lib/maintenance';
  import RowMenu, { type MenuItem } from '../components/RowMenu.svelte';
  import Icon from '../components/Icon.svelte';

  let list = $state.raw<Maintenance[]>([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    try {
      list = await api.maintenance();
      error = '';
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }

  let timer: ReturnType<typeof setInterval> | undefined;
  let unsub: (() => void) | undefined;
  onMount(() => {
    load();
    // Durumlar zamanla değişir (planlandı → bakımda → bitti); dakikada bir yenile.
    timer = setInterval(load, 60_000);
    unsub = live.onMaintenance(() => load());
  });
  onDestroy(() => {
    clearInterval(timer);
    unsub?.();
  });

  const sorted = $derived(
    list.slice().sort((a, b) => maintRank(a) - maintRank(b) || (a.next_start || 0) - (b.next_start || 0) || collator.compare(a.title, b.title)),
  );
  const activeCount = $derived(list.filter((m) => m.status === 'active').length);

  function monitorsText(m: Maintenance): string {
    if (m.all_monitors) return 'Tüm monitörler';
    const names = m.monitor_ids.map((id) => live.byId(id)?.name).filter(Boolean) as string[];
    if (names.length === 0) return `${m.monitor_ids.length} monitör`;
    if (names.length <= 3) return names.join(', ');
    return `${names.slice(0, 3).join(', ')} ve ${m.monitor_ids.length - 3} monitör daha`;
  }

  async function toggle(m: Maintenance) {
    try {
      const res = m.active ? await api.pauseMaintenance(m.id) : await api.resumeMaintenance(m.id);
      list = list.map((x) => (x.id === m.id ? res : x));
      toast.success(res.active ? `“${res.title}” başlatıldı` : `“${res.title}” durduruldu`);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function remove(m: Maintenance) {
    const ok = await confirmDialog({
      title: 'Bakımı sil',
      message: `“${m.title}” bakım penceresi silinecek.${m.status === 'active' ? ' Etkilenen monitörler hemen normal izlemeye döner.' : ''}`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteMaintenance(m.id);
      list = list.filter((x) => x.id !== m.id);
      toast.success(`“${m.title}” silindi`);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  function menu(m: Maintenance): MenuItem[] {
    return [
      { label: 'Düzenle', icon: 'edit', href: `#/maintenance/${m.id}` },
      { label: m.active ? 'Durdur' : 'Başlat', icon: m.active ? 'pause' : 'play', onclick: () => toggle(m) },
      { label: 'Sil', icon: 'trash', danger: true, onclick: () => remove(m) },
    ];
  }
</script>

<div class="page-head">
  <h1>Bakım pencereleri<span class="dot">.</span></h1>
  {#if session.canEdit}
    <a class="btn primary" href="#/maintenance/new"><Icon name="plus" size={16} /> Bakım ekle</a>
  {/if}
</div>

<p class="intro text-2">
  Bakım sırasında monitörler <span class="badge maint">Bakımda</span> görünür, bildirim gönderilmez ve bu süre uptime hesabına katılmaz.
</p>

{#if loading}
  <div class="skeleton" style="height:160px"></div>
{:else if error && list.length === 0}
  <div class="card empty">
    <h3>Bakım pencereleri yüklenemedi</h3>
    <p>{error}</p>
    <button class="btn primary" onclick={load}>Tekrar dene</button>
  </div>
{:else if list.length === 0}
  <div class="card empty">
    <div class="big-ic"><Icon name="wrench" size={30} /></div>
    <h3>Planlı bakım yok</h3>
    <p>Sunucu güncellemesi veya planlı bir kesinti öncesinde bakım penceresi ekleyin; o sırada gereksiz alarm almazsınız.</p>
    {#if session.canEdit}
      <a class="btn primary" href="#/maintenance/new"><Icon name="plus" size={16} /> Bakım ekle</a>
    {/if}
  </div>
{:else}
  {#if activeCount > 0}
    <div class="alert maint now">
      <Icon name="wrench" size={16} />
      {activeCount === 1 ? 'Şu anda 1 bakım penceresi sürüyor.' : `Şu anda ${activeCount} bakım penceresi sürüyor.`}
    </div>
  {/if}
  <div class="card list">
    {#each sorted as m (m.id)}
      {@const st = MAINT_STATUS[m.status]}
      <div class="item" class:dim={m.status === 'ended' || m.status === 'inactive'}>
        <span class="ic {m.status}"><Icon name="wrench" size={18} /></span>
        <div class="info">
          <div class="title">
            {#if session.canEdit}
              <a href="#/maintenance/{m.id}">{m.title}</a>
            {:else}
              <span>{m.title}</span>
            {/if}
            <span class="badge {st.c}">{st.l}</span>
          </div>
          <div class="meta">
            <span class="strat">{STRATEGY_LABELS[m.strategy]}</span>
            <span class="sep">·</span>
            <span>{scheduleText(m)}</span>
          </div>
          <div class="meta">
            <Icon name="activity" size={13} />
            <span class="mons">{monitorsText(m)}</span>
          </div>
          {#if nextText(m)}
            <div class="next {m.status === 'active' ? 'c-maint' : ''}"><Icon name="clock" size={13} /> {nextText(m)}</div>
          {/if}
          {#if m.description}<p class="desc">{m.description}</p>{/if}
        </div>
        {#if session.canEdit}
          <div class="acts">
            {#if m.status !== 'ended'}
              <button class="btn sm tgl" onclick={() => toggle(m)}>
                <Icon name={m.active ? 'pause' : 'play'} size={14} />
                {m.active ? 'Durdur' : 'Başlat'}
              </button>
            {/if}
            <RowMenu items={menu(m)} label="{m.title} için işlemler" />
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .intro {
    margin: -8px 0 18px;
    font-size: 0.9rem;
  }
  .big-ic {
    display: flex;
    justify-content: center;
    color: var(--maint);
  }
  .now {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
  }
  .list {
    padding: 0;
  }
  .item {
    display: flex;
    align-items: flex-start;
    gap: 14px;
    padding: 16px 14px 16px 18px;
    border-bottom: 1px solid var(--border);
  }
  .item:last-child {
    border-bottom: none;
  }
  .ic {
    width: 38px;
    height: 38px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--card-2);
    color: var(--muted);
    flex-shrink: 0;
  }
  .ic.active {
    background: var(--maint);
    color: var(--on-maint);
  }
  .ic.scheduled {
    background: var(--maint-soft);
    color: var(--maint);
  }
  .dim .info {
    opacity: 0.65;
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .title {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    font-weight: 700;
    font-size: 0.98rem;
  }
  .title a {
    color: var(--text);
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    font-size: 0.85rem;
    color: var(--text-2);
    min-width: 0;
  }
  .meta :global(svg) {
    color: var(--muted);
    flex-shrink: 0;
  }
  .strat {
    font-weight: 600;
  }
  .sep {
    color: var(--muted);
  }
  .mons {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .next {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 0.83rem;
    color: var(--muted);
  }
  .desc {
    margin: 4px 0 0;
    font-size: 0.85rem;
    color: var(--text-2);
    white-space: pre-line;
  }
  .acts {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  @media (max-width: 640px) {
    .item {
      padding: 14px 8px 14px 14px;
      gap: 12px;
    }
    .ic {
      width: 32px;
      height: 32px;
    }
    .tgl {
      display: none;
    }
  }
</style>
