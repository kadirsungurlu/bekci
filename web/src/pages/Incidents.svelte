<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, errorMessage, type Incident } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { clock } from '../lib/ui.svelte';
  import IncidentTable from '../components/IncidentTable.svelte';

  const LIMIT = 50;

  let items = $state.raw<Incident[]>([]);
  let loading = $state(true);
  let loadingMore = $state(false);
  let hasMore = $state(false);
  let error = $state('');

  async function loadFirst() {
    try {
      const list = await api.incidents(0, LIMIT);
      // Daha önce "daha fazla" ile yüklenenleri koru, üst kısmı yenile.
      const seen = new Set(list.map((i) => i.id));
      const oldest = list.length ? list[list.length - 1].id : Infinity;
      const rest = items.filter((i) => !seen.has(i.id) && i.id < oldest);
      items = [...list, ...rest];
      if (rest.length === 0) hasMore = list.length === LIMIT;
      error = '';
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }

  async function loadMore() {
    if (!items.length) return;
    loadingMore = true;
    try {
      const list = await api.incidents(items[items.length - 1].id, LIMIT);
      items = [...items, ...list];
      hasMore = list.length === LIMIT;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loadingMore = false;
    }
  }

  let timer: ReturnType<typeof setTimeout> | undefined;
  let unsub: (() => void) | undefined;
  const lastStatus = new Map<number, number>();

  onMount(() => {
    loadFirst();
    // Bir monitörün durumu değişince yeni olay açılmış/kapanmış olabilir.
    unsub = live.onBeat((b) => {
      const prev = lastStatus.get(b.monitor_id) ?? live.byId(b.monitor_id)?.status;
      lastStatus.set(b.monitor_id, b.status);
      if (prev !== undefined && prev !== b.status) {
        clearTimeout(timer);
        timer = setTimeout(loadFirst, 800);
      }
    });
  });
  onDestroy(() => {
    unsub?.();
    clearTimeout(timer);
  });

  const ongoing = $derived(items.filter((i) => i.resolved_at === 0).length);
</script>

<div class="page-head">
  <h1>Olaylar<span class="dot">.</span></h1>
  {#if ongoing > 0}<span class="pill down">{ongoing} olay devam ediyor</span>{/if}
</div>

<div class="card">
  {#if loading}
    <div class="skeleton" style="height:200px"></div>
  {:else if error && items.length === 0}
    <div class="empty">
      <h3>Olaylar yüklenemedi</h3>
      <p>{error}</p>
      <button class="btn primary" onclick={loadFirst}>Tekrar dene</button>
    </div>
  {:else}
    <IncidentTable
      incidents={items}
      now={clock.now}
      showMonitor
      emptyText="Henüz hiç olay yok. Bir monitör çalışmadığında burada görünecek."
    />
    {#if hasMore}
      <div class="more">
        <button class="btn" onclick={loadMore} disabled={loadingMore}>
          {#if loadingMore}<span class="spinner"></span>{/if}
          Daha fazla yükle
        </button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .more {
    display: flex;
    justify-content: center;
    padding-top: 16px;
    border-top: 1px solid var(--border);
    margin-top: 4px;
  }
</style>
