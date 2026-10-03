<script lang="ts" module>
  import type { Incident, IncidentFilterKind } from '../lib/api';

  /** Liste süzgeçleri (tür dışında). */
  interface Filters {
    q: string;
    open: boolean;
    monitorId: number;
    from: string; // yyyy-mm-dd
    to: string;
  }
  const EMPTY: Filters = { q: '', open: false, monitorId: 0, from: '', to: '' };

  // Olay ayrıntısından geri dönüldüğünde liste (yüklenen sayfalar dahil), süzgeç ve
  // kaydırma konumu korunsun diye son durum modül düzeyinde saklanır.
  const cache: { user: number; kind: IncidentFilterKind; filters: Filters; items: Incident[]; hasMore: boolean; scrollY: number } = {
    user: -1,
    kind: '',
    filters: { ...EMPTY },
    items: [],
    hasMore: false,
    scrollY: 0,
  };
</script>

<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { api, errorMessage, type IncidentQuery } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { clock, toast } from '../lib/ui.svelte';
  import { collator } from '../lib/format';
  import IncidentTable from '../components/IncidentTable.svelte';
  import IncidentEditModal from '../components/IncidentEditModal.svelte';
  import Icon from '../components/Icon.svelte';
  import { t } from '../lib/i18n';

  const LIMIT = 50;

  const uid = session.user?.id ?? 0;
  const cached = cache.user === uid && cache.items.length > 0;

  let kind = $state<IncidentFilterKind>(cache.user === uid ? cache.kind : '');
  let filters = $state<Filters>(cache.user === uid ? { ...cache.filters } : { ...EMPTY });
  let items = $state.raw<Incident[]>(cached ? cache.items : []);
  let loading = $state(!cached);
  let loadingMore = $state(false);
  let hasMore = $state(cached ? cache.hasMore : false);
  let error = $state('');
  let showFilters = $state(cache.user === uid && (cache.filters.q !== '' || cache.filters.open || cache.filters.monitorId !== 0 || cache.filters.from !== '' || cache.filters.to !== ''));

  // Önbelleği güncel tut (sayfadan çıkarken kaydırma konumu zaten sıfırlanmış olur).
  $effect(() => {
    cache.user = uid;
    cache.kind = kind;
    cache.filters = { ...filters };
    cache.items = items;
    cache.hasMore = hasMore;
  });

  // Tarih alanları yerel gün başı/sonu olarak unix saniyeye çevrilir.
  const dayStart = (d: string) => (d ? Math.floor(new Date(d + 'T00:00:00').getTime() / 1000) : 0);
  const dayEnd = (d: string) => (d ? Math.floor(new Date(d + 'T23:59:59').getTime() / 1000) : 0);
  const query = (): IncidentQuery => ({ q: filters.q, monitor_id: filters.monitorId, from: dayStart(filters.from), to: dayEnd(filters.to) });
  const active = $derived(filters.q.trim() !== '' || filters.open || filters.monitorId !== 0 || filters.from !== '' || filters.to !== '');

  // Süzgeç değişince eski türün yanıtı yenisinin üstüne yazılmasın.
  let seq = 0;

  async function loadFirst() {
    const my = ++seq;
    const k = kind;
    try {
      const list = await api.incidents(0, LIMIT, k, filters.open, query());
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
      const list = await api.incidents(items[items.length - 1].id, LIMIT, kind, filters.open, query());
      if (my !== seq) return;
      items = [...items, ...list];
      hasMore = list.length === LIMIT;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loadingMore = false;
    }
  }

  function reset() {
    items = [];
    hasMore = false;
    loading = true;
    loadFirst();
  }

  function setKind(k: IncidentFilterKind) {
    if (k === kind) return;
    kind = k;
    reset();
  }

  // Metin araması yazarken kısa gecikmeyle; diğer süzgeçler hemen.
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  function onSearch() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(reset, 350);
  }
  function clearFilters() {
    filters = { ...EMPTY };
    reset();
  }

  const FILTERS: { k: IncidentFilterKind; label: () => string }[] = [
    { k: '', label: () => t('incidents.filter.all') },
    { k: 'monitor', label: () => t('incidents.filter.monitor') },
    { k: 'server', label: () => t('incidents.filter.server') },
    { k: 'partial', label: () => t('incidents.filter.partial') },
    { k: 'degraded', label: () => t('incidents.filter.degraded') },
    { k: 'manual', label: () => t('incidents.filter.manual') },
  ];

  const monitors = $derived(live.monitors.slice().sort((a, b) => collator.compare(a.name, b.name)));

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
      // Elle olay açıldı / güncelleme yazıldı.
      live.onIncident(() => reloadSoon(500)),
    ];
  });
  onDestroy(() => {
    for (const u of unsubs) u();
    clearTimeout(timer);
    clearTimeout(searchTimer);
  });

  // Elle olay açma penceresi (editör+).
  let newOpen = $state(false);
  let newKey = $state(0);

  const ongoing = $derived(items.filter((i) => i.resolved_at === 0).length);
  // CSV dışa aktarımı geçerli süzgeçle, oturum çerezi ile doğrudan indirilir.
  const csvHref = $derived.by(() => {
    const p = new URLSearchParams({ format: 'csv' });
    if (kind) p.set('kind', kind);
    if (filters.open) p.set('open', '1');
    if (filters.monitorId) p.set('monitor_id', String(filters.monitorId));
    if (filters.q.trim()) p.set('q', filters.q.trim());
    if (filters.from) p.set('from', String(dayStart(filters.from)));
    if (filters.to) p.set('to', String(dayEnd(filters.to)));
    return `/api/incidents?${p}`;
  });
</script>

<svelte:window onscroll={() => (cache.scrollY = window.scrollY)} />

<div class="page-head">
  <h1>{t('incidents.title')}<span class="dot">.</span></h1>
  {#if ongoing > 0}<span class="pill down">{t('incidents.ongoingCount', { count: ongoing })}</span>{/if}
  {#if session.canEdit}
    <button class="btn primary new" onclick={() => { newKey++; newOpen = true; }}><Icon name="plus" size={16} /> {t('incidents.manual.newTitle')}</button>
  {/if}
</div>

<div class="filters">
  <div class="seg" role="radiogroup" aria-label={t('incidents.filter.label')}>
    {#each FILTERS as f (f.k)}
      <button role="radio" aria-checked={kind === f.k} class:active={kind === f.k} onclick={() => setKind(f.k)}>{f.label()}</button>
    {/each}
  </div>
  <div class="tools">
    <div class="search">
      <Icon name="search" size={15} />
      <input
        type="search"
        class="input"
        placeholder={t('incidents.filter.searchPlaceholder')}
        aria-label={t('incidents.filter.search')}
        bind:value={filters.q}
        oninput={onSearch}
      />
    </div>
    <button class="btn sm" class:active={showFilters || active} aria-expanded={showFilters} onclick={() => (showFilters = !showFilters)}>
      <Icon name="filter" size={14} /> {t('incidents.filter.more')}{#if active}<span class="fdot" aria-hidden="true"></span>{/if}
    </button>
    {#if items.length > 0}
      <a class="btn sm csv" href={csvHref} download title={t('incidents.exportCsvTitle')}><Icon name="download" size={14} /> {t('incidents.exportCsv')}</a>
    {/if}
  </div>
</div>

{#if showFilters}
  <div class="card fbar">
    <label class="check inline">
      <input type="checkbox" bind:checked={filters.open} onchange={reset} />
      <span>{t('incidents.filter.openOnly')}</span>
    </label>
    <div class="field">
      <label for="f-mon">{t('incidents.filter.monitor')}</label>
      <select id="f-mon" class="input" bind:value={filters.monitorId} onchange={reset}>
        <option value={0}>{t('incidents.filter.monitorAny')}</option>
        {#each monitors as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="f-from">{t('incidents.filter.from')}</label>
      <input id="f-from" class="input" type="date" bind:value={filters.from} onchange={reset} />
    </div>
    <div class="field">
      <label for="f-to">{t('incidents.filter.to')}</label>
      <input id="f-to" class="input" type="date" bind:value={filters.to} onchange={reset} />
    </div>
    {#if active}
      <button class="btn sm ghost clear" onclick={clearFilters}><Icon name="x" size={14} /> {t('incidents.filter.clear')}</button>
    {/if}
  </div>
{/if}

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
      emptyText={kind || active ? t('incidents.emptyKind') : t('incidents.emptyAll')}
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

{#key newKey}
  {#if newKey > 0}
    <IncidentEditModal
      bind:open={newOpen}
      onsaved={() => {
        toast.success(t('incidents.manual.created'));
        reloadSoon(100);
      }}
    />
  {/if}
{/key}

<style>
  .page-head .new {
    margin-left: auto;
  }
  .filters {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
    margin: -4px 0 14px;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-left: auto;
  }
  .search {
    position: relative;
    display: flex;
    align-items: center;
  }
  .search :global(svg) {
    position: absolute;
    left: 10px;
    color: var(--muted);
    pointer-events: none;
  }
  .search input {
    padding-left: 32px;
    width: 260px;
    height: 34px;
  }
  .fdot {
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    margin-left: 6px;
  }
  .fbar {
    display: flex;
    align-items: flex-end;
    gap: 14px;
    flex-wrap: wrap;
    padding: 12px 16px;
    margin-bottom: 14px;
  }
  .fbar .field {
    min-width: 170px;
    flex: 0 1 220px;
  }
  .fbar .check.inline {
    padding: 8px 0;
  }
  .clear {
    margin-left: auto;
  }
  .more {
    display: flex;
    justify-content: center;
    padding-top: 16px;
    border-top: 1px solid var(--border);
    margin-top: 4px;
  }
  @media (max-width: 720px) {
    .tools,
    .page-head .new {
      margin-left: 0;
    }
    .search input {
      width: 100%;
    }
    .search {
      flex: 1 1 100%;
    }
    .fbar .field {
      flex: 1 1 100%;
    }
  }
</style>
