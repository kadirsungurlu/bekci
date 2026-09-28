<script lang="ts" module>
  import type { Incident } from '../lib/api';

  // Olay ayrıntısından geri dönüldüğünde liste (yüklenen sayfalar dahil) ve kaydırma
  // konumu korunsun diye son durum modül düzeyinde saklanır.
  const cache: { user: number; items: Incident[]; hasMore: boolean; scrollY: number } = {
    user: -1,
    items: [],
    hasMore: false,
    scrollY: 0,
  };
</script>

<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { api, errorMessage } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { clock } from '../lib/ui.svelte';
  import IncidentTable from '../components/IncidentTable.svelte';
  import { t } from '../lib/i18n';

  const LIMIT = 50;

  const uid = session.user?.id ?? 0;
  const cached = cache.user === uid && cache.items.length > 0;

  let items = $state.raw<Incident[]>(cached ? cache.items : []);
  let loading = $state(!cached);
  let loadingMore = $state(false);
  let hasMore = $state(cached ? cache.hasMore : false);
  let error = $state('');

  // Önbelleği güncel tut (sayfadan çıkarken kaydırma konumu zaten sıfırlanmış olur).
  $effect(() => {
    cache.user = uid;
    cache.items = items;
    cache.hasMore = hasMore;
  });

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
  let unsubResume: (() => void) | undefined;
  const lastStatus = new Map<number, number>();

  onMount(() => {
    if (cached) {
      const y = cache.scrollY;
      tick().then(() => requestAnimationFrame(() => window.scrollTo(0, y)));
    } else cache.scrollY = 0;
    loadFirst();
    unsubResume = live.onResume(loadFirst);
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
    unsubResume?.();
    clearTimeout(timer);
  });

  const ongoing = $derived(items.filter((i) => i.resolved_at === 0).length);
</script>

<svelte:window onscroll={() => (cache.scrollY = window.scrollY)} />

<div class="page-head">
  <h1>{t('incidents.title')}<span class="dot">.</span></h1>
  {#if ongoing > 0}<span class="pill down">{t('incidents.ongoingCount', { count: ongoing })}</span>{/if}
</div>

<div class="card">
  {#if loading}
    <div class="skeleton" style="height:200px"></div>
  {:else if error && items.length === 0}
    <div class="empty">
      <h3>{t('incidents.loadFailed')}</h3>
      <p>{error}</p>
      <button class="btn primary" onclick={loadFirst}>{t('common.retry')}</button>
    </div>
  {:else}
    <IncidentTable
      incidents={items}
      now={clock.now}
      showMonitor
      emptyText={t('incidents.emptyAll')}
    />
    {#if hasMore}
      <div class="more">
        <button class="btn" onclick={loadMore} disabled={loadingMore}>
          {#if loadingMore}<span class="spinner"></span>{/if}
          {t('incidents.loadMore')}
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
