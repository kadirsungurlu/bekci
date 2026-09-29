<script lang="ts" module>
  import type { Incident, IncidentFilterKind } from '../lib/api';

  // Olay ayrıntısından geri dönüldüğünde liste (yüklenen sayfalar dahil), süzgeç ve
  // kaydırma konumu korunsun diye son durum modül düzeyinde saklanır.
  const cache: { user: number; kind: IncidentFilterKind; items: Incident[]; hasMore: boolean; scrollY: number } = {
    user: -1,
    kind: '',
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

  let kind = $state<IncidentFilterKind>(cache.user === uid ? cache.kind : '');
  let items = $state.raw<Incident[]>(cached ? cache.items : []);
  let loading = $state(!cached);
  let loadingMore = $state(false);
  let hasMore = $state(cached ? cache.hasMore : false);
  let error = $state('');

  // Önbelleği güncel tut (sayfadan çıkarken kaydırma konumu zaten sıfırlanmış olur).
  $effect(() => {
    cache.user = uid;
    cache.kind = kind;
    cache.items = items;
    cache.hasMore = hasMore;
  });

  // Süzgeç değişince eski türün yanıtı yenisinin üstüne yazılmasın.
  let seq = 0;

  async function loadFirst() {
    const my = ++seq;
    const k = kind;
    try {
      const list = await api.incidents(0, LIMIT, k);
      if (my !== seq) return;
      // Daha önce "daha fazla" ile yüklenenleri koru, üst kısmı yenile.
      const seen = new Set(list.map((i) => i.id));
      const oldest = list.length ? list[list.length - 1].id : Infinity;
      const rest = items.filter((i) => !seen.has(i.id) && i.id < oldest);
      items = [...list, ...rest];
      if (rest.length === 0) hasMore = list.length === LIMIT;
      error = '';
    } catch (e) {
      if (my === seq) error = errorMessage(e);
    } finally {
      if (my === seq) loading = false;
    }
  }

  async function loadMore() {
    if (!items.length) return;
    loadingMore = true;
    const my = seq;
    try {
      const list = await api.incidents(items[items.length - 1].id, LIMIT, kind);
      if (my !== seq) return;
      items = [...items, ...list];
      hasMore = list.length === LIMIT;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loadingMore = false;
    }
  }

  function setKind(k: IncidentFilterKind) {
    if (k === kind) return;
    kind = k;
    items = [];
    hasMore = false;
    loading = true;
    loadFirst();
  }

  const FILTERS: { k: IncidentFilterKind; label: () => string }[] = [
    { k: '', label: () => t('incidents.filter.all') },
    { k: 'monitor', label: () => t('incidents.filter.monitor') },
    { k: 'server', label: () => t('incidents.filter.server') },
    { k: 'partial', label: () => t('incidents.filter.partial') },
  ];

  let timer: ReturnType<typeof setTimeout> | undefined;
  let unsubs: (() => void)[] = [];
  const lastStatus = new Map<number, number>();
  const serverAlerts = new Map<number, string>();

  function reloadSoon(ms = 800) {
    clearTimeout(timer);
    timer = setTimeout(loadFirst, ms);
  }

  onMount(() => {
    if (cached) {
      const y = cache.scrollY;
      tick().then(() => requestAnimationFrame(() => window.scrollTo(0, y)));
    } else cache.scrollY = 0;
    loadFirst();
    unsubs = [
      live.onResume(loadFirst),
      // Bir monitörün durumu değişince yeni olay açılmış/kapanmış olabilir.
      live.onBeat((b) => {
        const prev = lastStatus.get(b.monitor_id) ?? live.byId(b.monitor_id)?.status;
        lastStatus.set(b.monitor_id, b.status);
        if (prev !== undefined && prev !== b.status) reloadSoon();
      }),
      // Bir konumun durumu değişti: kısmi kesinti açılmış/kapanmış olabilir.
      live.onLocations(() => reloadSoon(1500)),
      // Sunucunun uyarı durumu değişince sunucu olayı açılır/kapanır.
      live.onServer((v) => {
        const sig = `${v.state}|${(v.firing ?? []).join(',')}`;
        const prev = serverAlerts.get(v.id);
        serverAlerts.set(v.id, sig);
        if (prev !== undefined && prev !== sig) reloadSoon();
      }),
    ];
  });
  onDestroy(() => {
    for (const u of unsubs) u();
    clearTimeout(timer);
  });

  const ongoing = $derived(items.filter((i) => i.resolved_at === 0).length);
</script>

<svelte:window onscroll={() => (cache.scrollY = window.scrollY)} />

<div class="page-head">
  <h1>{t('incidents.title')}<span class="dot">.</span></h1>
  {#if ongoing > 0}<span class="pill down">{t('incidents.ongoingCount', { count: ongoing })}</span>{/if}
</div>

<div class="filters">
  <div class="seg" role="radiogroup" aria-label={t('incidents.filter.label')}>
    {#each FILTERS as f (f.k)}
      <button role="radio" aria-checked={kind === f.k} class:active={kind === f.k} onclick={() => setKind(f.k)}>{f.label()}</button>
    {/each}
  </div>
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
      emptyText={kind ? t('incidents.emptyKind') : t('incidents.emptyAll')}
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
  .filters {
    display: flex;
    margin: -4px 0 14px;
  }
  .more {
    display: flex;
    justify-content: center;
    padding-top: 16px;
    border-top: 1px solid var(--border);
    margin-top: 4px;
  }
</style>
