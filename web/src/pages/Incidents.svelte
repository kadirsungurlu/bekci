<script lang="ts" module>
  import type { IncidentFilterKind } from '../lib/api';

  /** Liste süzgeçleri (tür dışında). */
  interface Filters {
    q: string;
    open: boolean;
    monitorId: number;
    from: string; // yyyy-mm-dd
    to: string;
  }
  const EMPTY: Filters = { q: '', open: false, monitorId: 0, from: '', to: '' };
  const KINDS: IncidentFilterKind[] = ['', 'monitor', 'server', 'partial', 'degraded', 'manual'];
  const SIZES = [25, 50, 100];

  // Sayfa, tür ve süzgeçler adreste durur (#/incidents?sayfa=2&tur=server...):
  // geri tuşu ve olay ayrıntısından dönüş aynı listeyi açar. Kaydırma konumu
  // adres başına saklanır.
  const scrollByUrl = new Map<string, number>();

  interface ListState {
    kind: IncidentFilterKind;
    filters: Filters;
    page: number;
    perPage: number;
  }

  function readUrl(): ListState {
    const q = new URLSearchParams(location.hash.split('?')[1] ?? '');
    const kind = (q.get('tur') ?? '') as IncidentFilterKind;
    const per = Number(q.get('adet'));
    return {
      kind: KINDS.includes(kind) ? kind : '',
      filters: {
        q: q.get('q') ?? '',
        open: q.get('suren') === '1',
        monitorId: Number(q.get('monitor')) || 0,
        from: /^\d{4}-\d{2}-\d{2}$/.test(q.get('bas') ?? '') ? q.get('bas')! : '',
        to: /^\d{4}-\d{2}-\d{2}$/.test(q.get('bit') ?? '') ? q.get('bit')! : '',
      },
      page: Math.max(1, Math.floor(Number(q.get('sayfa'))) || 1),
      perPage: SIZES.includes(per) ? per : 25,
    };
  }

  function urlOf(s: ListState): string {
    const q = new URLSearchParams();
    if (s.kind) q.set('tur', s.kind);
    if (s.filters.q.trim()) q.set('q', s.filters.q.trim());
    if (s.filters.open) q.set('suren', '1');
    if (s.filters.monitorId) q.set('monitor', String(s.filters.monitorId));
    if (s.filters.from) q.set('bas', s.filters.from);
    if (s.filters.to) q.set('bit', s.filters.to);
    if (s.page > 1) q.set('sayfa', String(s.page));
    if (s.perPage !== 25) q.set('adet', String(s.perPage));
    const qs = q.toString();
    return '/incidents' + (qs ? '?' + qs : '');
  }
</script>

<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { api, errorMessage, type Incident, type IncidentQuery } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { navigate } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { clock, toast } from '../lib/ui.svelte';
  import { collator } from '../lib/format';
  import IncidentTable from '../components/IncidentTable.svelte';
  import IncidentEditModal from '../components/IncidentEditModal.svelte';
  import Icon from '../components/Icon.svelte';
  import Pager from '../components/Pager.svelte';
  import { t } from '../lib/i18n';
  import DateTimeField from '../components/DateTimeField.svelte';

  const init = readUrl();
  let kind = $state<IncidentFilterKind>(init.kind);
  let filters = $state<Filters>(init.filters);
  let page = $state(init.page);
  let perPage = $state(init.perPage);
  let items = $state.raw<Incident[]>([]);
  let total = $state(0);
  let loading = $state(true);
  let error = $state('');
  let showFilters = $state(
    init.filters.q !== '' || init.filters.open || init.filters.monitorId !== 0 || init.filters.from !== '' || init.filters.to !== '',
  );

  // Tarih alanları yerel gün başı/sonu olarak unix saniyeye çevrilir.
  const dayStart = (d: string) => (d ? Math.floor(new Date(d + 'T00:00:00').getTime() / 1000) : 0);
  const dayEnd = (d: string) => (d ? Math.floor(new Date(d + 'T23:59:59').getTime() / 1000) : 0);
  const query = (): IncidentQuery => ({ q: filters.q, monitor_id: filters.monitorId, from: dayStart(filters.from), to: dayEnd(filters.to) });
  const active = $derived(filters.q.trim() !== '' || filters.open || filters.monitorId !== 0 || filters.from !== '' || filters.to !== '');
  const current = (): ListState => ({ kind, filters: { ...filters }, page, perPage });

  // Süzgeç değişince eski yanıt yenisinin üstüne yazılmasın.
  let seq = 0;
  let restoreScroll = false;

  async function load() {
    const my = ++seq;
    try {
      const res = await api.incidentsPage((page - 1) * perPage, perPage, kind, filters.open, query());
      if (my !== seq) return;
      // Kayıtlar silindi/azaldı: son sayfaya düş.
      const last = Math.max(1, Math.ceil(res.total / perPage));
      if (res.items.length === 0 && page > last) {
        setPage(last, true);
        return;
      }
      items = res.items;
      total = res.total;
      error = '';
      if (restoreScroll) {
        restoreScroll = false;
        const y = scrollByUrl.get(urlOf(current())) ?? 0;
        tick().then(() => requestAnimationFrame(() => window.scrollTo(0, y)));
      }
    } catch (e) {
      if (my === seq) error = errorMessage(e);
    } finally {
      if (my === seq) loading = false;
    }
  }

  // Adresi günceller; push=false iken geçmişe yeni kayıt eklenmez (arama yazarken).
  function sync(push = true) {
    navigate(urlOf(current()), !push);
  }

  // Olay ayrıntısındaki "geri" bağlantısı aynı sayfaya/süzgece dönsün.
  $effect(() => {
    try {
      sessionStorage.setItem('bekci-olay-listesi', urlOf({ kind, filters: { ...filters }, page, perPage }));
    } catch {
      /* gizli mod */
    }
  });

  function setPage(p: number, replace = false) {
    page = Math.max(1, p);
    sync(!replace);
    loading = items.length === 0;
    load();
    if (!replace) window.scrollTo(0, 0);
  }

  function setPerPage(n: number) {
    perPage = n;
    setPage(1);
  }

  // Süzgeç değişti: ilk sayfaya dön.
  function reset(push = true) {
    page = 1;
    sync(push);
    loading = true;
    load();
  }

  // Geri/ileri tuşu: adresteki durumu uygula.
  function onHashChange() {
    if (!location.hash.startsWith('#/incidents')) return;
    const u = readUrl();
    if (urlOf(u) === urlOf(current())) return;
    kind = u.kind;
    filters = u.filters;
    page = u.page;
    perPage = u.perPage;
    restoreScroll = true;
    loading = true;
    load();
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
    searchTimer = setTimeout(() => reset(false), 350);
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
    timer = setTimeout(load, ms);
  }

  onMount(() => {
    restoreScroll = true;
    load();
    window.addEventListener('hashchange', onHashChange);
    unsubs = [
      () => window.removeEventListener('hashchange', onHashChange),
      live.onResume(load),
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

<svelte:window onscroll={() => scrollByUrl.set(urlOf(current()), window.scrollY)} />

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
      <input type="checkbox" bind:checked={filters.open} onchange={() => reset()} />
      <span>{t('incidents.filter.openOnly')}</span>
    </label>
    <div class="field">
      <label for="f-mon">{t('incidents.filter.monitor')}</label>
      <select id="f-mon" class="input" bind:value={filters.monitorId} onchange={() => reset()}>
        <option value={0}>{t('incidents.filter.monitorAny')}</option>
        {#each monitors as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="f-from">{t('incidents.filter.from')}</label>
      <DateTimeField id="f-from" type="date" bind:value={filters.from} onchange={() => reset()} />
    </div>
    <div class="field">
      <label for="f-to">{t('incidents.filter.to')}</label>
      <DateTimeField id="f-to" type="date" bind:value={filters.to} onchange={() => reset()} />
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
      <button class="btn primary" onclick={load}>{t('common.retry')}</button>
    </div>
  {:else}
    <IncidentTable
      incidents={items}
      now={clock.now}
      showMonitor
      emptyText={kind || active ? t('incidents.emptyKind') : t('incidents.emptyAll')}
    />
    {#if total > 0}
      <Pager {total} {page} {perPage} onpage={(p) => setPage(p)} onperpage={setPerPage} />
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
