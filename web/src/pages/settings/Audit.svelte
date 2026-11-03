<script lang="ts">
  import { onMount } from 'svelte';
  import { navigate } from '../../lib/router.svelte';
  import Pager from '../../components/Pager.svelte';
  import { api, errorMessage, type AuditEntry, type AuditFacets, type AuditQuery } from '../../lib/api';
  import { auditDetail, auditHref, auditLabel, auditTargetLabel, auditTone } from '../../lib/audit';
  import { fmtDate } from '../../lib/format';
  import { STRATEGY_LABELS } from '../../lib/maintenance';
  import { t } from '../../lib/i18n';
  import { live } from '../../lib/live.svelte';
  import Icon from '../../components/Icon.svelte';
  import DateTimeField from '../../components/DateTimeField.svelte';

  // Bakım kayıtlarında ayrıntı strateji kodudur (ör. recurring_weekly).
  const detailText = (a: AuditEntry) =>
    (a.action.startsWith('maintenance.') && STRATEGY_LABELS[a.detail as keyof typeof STRATEGY_LABELS]) || auditDetail(a.detail) || '—';

  const SIZES = [25, 50, 100];
  // Sayfa ve sayfa boyu adreste (#/settings/audit?sayfa=2&adet=50): geri tuşu çalışır.
  function readUrl(): { page: number; perPage: number } {
    const q = new URLSearchParams(location.hash.split('?')[1] ?? '');
    const per = Number(q.get('adet'));
    return { page: Math.max(1, Math.floor(Number(q.get('sayfa'))) || 1), perPage: SIZES.includes(per) ? per : 25 };
  }
  const init = readUrl();

  let items = $state.raw<AuditEntry[]>([]);
  let total = $state(0);
  let page = $state(init.page);
  let perPage = $state(init.perPage);
  let loading = $state(true);
  let error = $state('');
  let facets = $state.raw<AuditFacets>({ users: [], actions: [] });

  // Süzgeçler: kullanıcı, işlem (tam kod ya da "alan." öneki), tarih aralığı, arama.
  let filters = $state({ user: '', action: '', from: '', to: '', q: '' });
  let showFilters = $state(false);
  const active = $derived(!!(filters.user || filters.action || filters.from || filters.to || filters.q.trim()));

  // Yerel günün başlangıcı / sonu (unix).
  const dayStart = (d: string) => (d ? Math.floor(new Date(`${d}T00:00:00`).getTime() / 1000) : 0);
  const dayEnd = (d: string) => (d ? Math.floor(new Date(`${d}T23:59:59`).getTime() / 1000) : 0);
  function query(): AuditQuery & { offset: number } {
    return {
      offset: (page - 1) * perPage,
      limit: perPage,
      user: filters.user,
      action: filters.action,
      from: dayStart(filters.from),
      to: dayEnd(filters.to),
      q: filters.q.trim(),
    };
  }

  // İşlem kutusu: kayıtlarda geçen eylemler alanına göre gruplanır; her grubun
  // başında o alanın tüm eylemlerini seçen "Tümü" seçeneği vardır.
  const groups = $derived.by(() => {
    const out = new Map<string, string[]>();
    for (const a of facets.actions) {
      const g = a.split('.')[0];
      out.set(g, [...(out.get(g) ?? []), a]);
    }
    return [...out.entries()];
  });

  let req = 0;
  async function load() {
    const my = ++req;
    try {
      const res = await api.auditPage(query());
      if (my !== req) return;
      const last = Math.max(1, Math.ceil(res.total / perPage));
      if (res.items.length === 0 && page > last) {
        setPage(last, true);
        return;
      }
      items = res.items;
      total = res.total;
      error = '';
    } catch (e) {
      if (my === req) error = errorMessage(e);
    } finally {
      if (my === req) loading = false;
    }
  }

  function urlOf(): string {
    const q = new URLSearchParams();
    if (page > 1) q.set('sayfa', String(page));
    if (perPage !== 25) q.set('adet', String(perPage));
    const qs = q.toString();
    return '/settings/audit' + (qs ? '?' + qs : '');
  }

  function setPage(p: number, replace = false) {
    page = Math.max(1, p);
    navigate(urlOf(), replace);
    load();
    if (!replace) window.scrollTo(0, 0);
  }

  // Süzgeç değişti: ilk sayfa.
  function loadFirst() {
    loading = true;
    setPage(1, true);
  }

  function onHashChange() {
    if (!location.hash.startsWith('#/settings/audit')) return;
    const u = readUrl();
    if (u.page === page && u.perPage === perPage) return;
    page = u.page;
    perPage = u.perPage;
    loading = true;
    load();
  }

  async function loadFacets() {
    try {
      facets = await api.auditFacets();
    } catch {
      /* süzgeç kutuları boş kalır; liste yine çalışır */
    }
  }

  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  function onSearch() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(loadFirst, 300);
  }
  function clearFilters() {
    filters = { user: '', action: '', from: '', to: '', q: '' };
    loadFirst();
  }
  function refresh() {
    loadFacets();
    loadFirst();
  }

  onMount(() => {
    loadFacets();
    load(); // adresteki sayfa korunur
    window.addEventListener('hashchange', onHashChange);
    // Dil değişince (ayrıntılar sunucuda çevrilir) ve uzun aradan sonra tazele.
    const off = live.onResume(load);
    return () => {
      off();
      window.removeEventListener('hashchange', onHashChange);
    };
  });
</script>

<div class="card">
  <div class="head">
    <h2 class="card-title">{t('audit.title')}</h2>
    <div class="tools">
      <div class="search">
        <Icon name="search" size={15} />
        <input
          type="search"
          class="input"
          placeholder={t('audit.filter.searchPlaceholder')}
          aria-label={t('audit.filter.search')}
          bind:value={filters.q}
          oninput={onSearch}
        />
      </div>
      <button class="btn sm" class:active={showFilters || active} aria-expanded={showFilters} onclick={() => (showFilters = !showFilters)}>
        <Icon name="filter" size={14} /> {t('audit.filter.label')}{#if active}<span class="fdot" aria-hidden="true"></span>{/if}
      </button>
      <button class="btn sm" onclick={refresh} disabled={loading}>{t('common.refresh')}</button>
    </div>
  </div>

  {#if showFilters}
    <div class="fbar">
      <div class="field">
        <label for="au-user">{t('audit.filter.user')}</label>
        <select id="au-user" class="input" bind:value={filters.user} onchange={loadFirst}>
          <option value="">{t('audit.filter.userAny')}</option>
          {#each facets.users as u (u)}<option value={u}>{u}</option>{/each}
        </select>
      </div>
      <div class="field">
        <label for="au-action">{t('audit.filter.action')}</label>
        <select id="au-action" class="input" bind:value={filters.action} onchange={loadFirst}>
          <option value="">{t('audit.filter.actionAny')}</option>
          {#each groups as [g, acts] (g)}
            <optgroup label={auditTargetLabel(g)}>
              <option value="{g}.">{t('audit.filter.allIn', { group: auditTargetLabel(g) })}</option>
              {#each acts as a (a)}<option value={a}>{auditLabel(a)}</option>{/each}
            </optgroup>
          {/each}
        </select>
      </div>
      <div class="field">
        <label for="au-from">{t('audit.filter.from')}</label>
        <DateTimeField id="au-from" type="date" bind:value={filters.from} onchange={loadFirst} />
      </div>
      <div class="field">
        <label for="au-to">{t('audit.filter.to')}</label>
        <DateTimeField id="au-to" type="date" bind:value={filters.to} onchange={loadFirst} />
      </div>
      {#if active}
        <button class="btn sm ghost clear" onclick={clearFilters}><Icon name="x" size={14} /> {t('audit.filter.clear')}</button>
      {/if}
    </div>
  {/if}

  {#if loading && items.length === 0}
    <div class="skeleton" style="height:220px"></div>
  {:else if error && items.length === 0}
    <div class="empty">
      <h3>{t('audit.loadFailed')}</h3>
      <p>{error}</p>
      <button class="btn primary" onclick={loadFirst}>{t('common.retry')}</button>
    </div>
  {:else if items.length === 0}
    <div class="none">{active ? t('audit.filter.noMatch') : t('audit.empty')}</div>
  {:else}
    <table class="table responsive log" class:dim={loading}>
      <thead>
        <tr>
          <th>{t('audit.col.time')}</th>
          <th>{t('audit.col.user')}</th>
          <th>{t('audit.col.action')}</th>
          <th>{t('audit.col.target')}</th>
          <th>{t('audit.col.detail')}</th>
          <th>{t('audit.col.ip')}</th>
        </tr>
      </thead>
      <tbody>
        {#each items as a (a.id)}
          {@const tone = auditTone(a.action)}
          {@const href = auditHref(a.target_type, a.target_id)}
          <tr>
            <td data-label={t('audit.col.time')} class="nowrap tm">{fmtDate(a.time)}</td>
            <td data-label={t('audit.col.user')} class="usr">{a.username || '—'}</td>
            <td data-label={t('audit.col.action')} class="act">
              <span class="badge {tone === 'bad' ? 'down' : tone === 'warn' ? 'pending' : tone === 'good' ? 'up' : ''}" title={a.action}
                >{auditLabel(a.action)}</span
              >
            </td>
            <td data-label={t('audit.col.target')} class="tgt">
              {#if a.target_name}
                <span class="muted small">{auditTargetLabel(a.target_type)}:</span>
                {#if href}<a {href}>{a.target_name}</a>{:else}{a.target_name}{/if}
              {:else}
                —
              {/if}
            </td>
            <td data-label={t('audit.col.detail')} class="det">{detailText(a)}</td>
            <td data-label={t('audit.col.ip')} class="ip mono">{a.ip || '—'}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if error}<div class="alert error">{error}</div>{/if}
    {#if total > 0}
      <Pager {total} {page} {perPage} onpage={(p) => setPage(p)} onperpage={(n) => ((perPage = n), setPage(1))} />
    {/if}
  {/if}
</div>

<style>
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 8px;
  }
  .card-title {
    margin: 0;
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
    width: 240px;
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
    padding: 12px 0 14px;
    margin-bottom: 6px;
    border-bottom: 1px solid var(--border);
  }
  .fbar .field {
    min-width: 160px;
    flex: 0 1 220px;
  }
  .fbar .field label {
    display: block;
    font-size: 0.8rem;
    color: var(--text-2);
    margin-bottom: 4px;
  }
  .fbar .clear {
    margin-bottom: 2px;
  }
  .dim {
    opacity: 0.6;
  }
  .none {
    color: var(--muted);
    padding: 18px 0 6px;
    text-align: center;
  }
  .log .badge {
    letter-spacing: 0.01em;
    font-size: 0.74rem;
    height: 22px;
  }
  .usr {
    font-weight: 600;
  }
  .tgt,
  .det {
    word-break: break-word;
  }
  .det {
    color: var(--text-2);
    max-width: 320px;
  }
  .ip {
    font-size: 0.82rem;
    color: var(--muted);
    white-space: nowrap;
  }
  .tm {
    color: var(--text-2);
  }
  @media (max-width: 720px) {
    .det {
      max-width: none;
    }
    .search input {
      width: 100%;
    }
    .search {
      flex: 1 1 100%;
    }
    .tools {
      width: 100%;
    }
    .fbar .field {
      flex: 1 1 100%;
    }
  }
</style>
