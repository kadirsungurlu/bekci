<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, type SecPackage, type ServerSecurity } from '../lib/api';
  import { fmtDate, fmtRelative, nowSec } from '../lib/format';
  import { t, tOr } from '../lib/i18n';
  import { fmtDays } from '../lib/servers.svelte';
  import Icon from './Icon.svelte';

  let { id }: { id: number } = $props();

  // Paket dizini bundan eskiyse sonuç bayat olabilir (gün).
  const INDEX_STALE_DAYS = 7;
  const DAY = 86400;

  let data = $state<ServerSecurity | null>(null);
  let loadError = $state('');
  let onlySec = $state(false);
  let q = $state('');

  async function load() {
    try {
      data = await api.getServerSecurity(id);
      loadError = '';
    } catch (e) {
      if (!data) loadError = (e as Error).message;
    }
  }

  let timer: ReturnType<typeof setInterval>;
  onMount(() => {
    load();
    timer = setInterval(() => {
      if (!document.hidden) load();
    }, 60000);
  });
  onDestroy(() => clearInterval(timer));

  const now = $derived(data?.now || nowSec());
  const upd = $derived(data?.report?.updates ?? null);
  const reboot = $derived(data?.report?.reboot ?? null);
  const notes = $derived(data?.notes ?? {});
  const pkgs = $derived(data?.packages ?? []);
  const secCount = $derived(pkgs.filter((p) => p.security).length);

  const waitDays = (p: SecPackage) => Math.max(0, (now - p.first_seen) / DAY);
  const oldestSec = $derived.by(() => {
    let min = 0;
    for (const p of pkgs) if (p.security && p.first_seen && (!min || p.first_seen < min)) min = p.first_seen;
    return min;
  });
  const oldestDays = $derived(oldestSec ? (now - oldestSec) / DAY : 0);
  const oldestOver = $derived(!!data?.age_threshold && oldestSec > 0 && oldestDays >= data.age_threshold);
  const indexAge = $derived(upd?.index_time ? (now - upd.index_time) / DAY : 0);
  const rebootDays = $derived(data?.reboot_since ? Math.floor((now - data.reboot_since) / DAY) : -1);

  const shown = $derived(
    pkgs.filter((p) => {
      if (onlySec && !p.security) return false;
      const s = q.trim().toLocaleLowerCase();
      return !s || p.name.toLocaleLowerCase().includes(s);
    }),
  );

  const noteText = (code: string | undefined) => (code ? tOr(`servers.security.notes.${code}`, code) : '');
  const sevLabel = (s: string) => tOr(`servers.security.pkgs.severity.${s.toLowerCase()}`, s);
  const sevClass = (s: string) => {
    const v = s.toLowerCase();
    return v === 'critical' ? 'down' : v === 'important' ? 'pending' : 'paused';
  };
  const waitText = (p: SecPackage) => (waitDays(p) < 1 ? t('servers.security.pkgs.today') : fmtDays(waitDays(p)));
  const pkgOver = (p: SecPackage) => p.security && !!data?.age_threshold && waitDays(p) >= data.age_threshold;
</script>

{#if loadError}
  <div class="card empty">
    <h3>{t('servers.security.load')}</h3>
    <p>{loadError}</p>
    <button class="btn primary" onclick={load}>{t('common.retry')}</button>
  </div>
{:else if !data}
  <div class="skeleton" style="height:320px"></div>
{:else if !data.received_at}
  <div class="card empty">
    <div class="eicon"><Icon name="shield" size={26} /></div>
    {#if data.interval === 0}
      <p>{t('servers.security.disabled')}</p>
    {:else}
      <h3>{t('servers.security.noReportTitle')}</h3>
      <p>{t('servers.security.noReportHint')}</p>
      <button class="btn" onclick={load}><Icon name="refresh" size={15} /> {t('servers.inventory.refresh')}</button>
    {/if}
  </div>
{:else}
  {#if data.interval === 0}
    <div class="alert info note"><Icon name="info" size={16} /><span>{t('servers.security.disabled')}</span></div>
  {/if}
  <div class="cards">
    <section class="card sc">
      <div class="sh">
        <h2 class="card-title"><Icon name="box" size={16} /> {t('servers.security.updates.title')}</h2>
        <span class="muted small" title={fmtDate(data.received_at)}>{t('servers.security.collected', { ago: fmtRelative(data.received_at, now) })}</span>
      </div>
      {#if notes.updates}
        <div class="alert {notes.updates === 'failed' || notes.updates === 'locked' ? 'warning' : 'info'} note"><Icon name="info" size={16} /><span>{noteText(notes.updates)}</span></div>
      {/if}
      {#if upd}
        <div class="nums">
          <div class="nb">
            <span class="nv">{upd.total}</span>
            <span class="nl">{t('servers.security.updates.total')}</span>
          </div>
          <div class="nb">
            <span class="nv" class:c-pending={upd.security > 0 && !oldestOver} class:c-down={upd.security > 0 && oldestOver}>{upd.security < 0 ? '—' : upd.security}</span>
            <span class="nl">{t('servers.security.updates.security')}</span>
          </div>
        </div>
        {#if upd.security < 0}
          <p class="muted small line">{t('servers.security.updates.noSecInfo')}</p>
        {:else if oldestSec}
          <p class="line" class:c-down={oldestOver}>
            {#if oldestOver}<Icon name="alert" size={14} />{/if}
            {t('servers.security.updates.oldest', { d: oldestDays < 1 ? t('servers.security.pkgs.today') : fmtDays(oldestDays) })}
            {#if data.age_threshold}<span class="muted small"> · {t('servers.security.updates.oldestOver', { d: fmtDays(data.age_threshold) })}</span>{/if}
          </p>
        {:else if upd.total === 0}
          <p class="line ok"><Icon name="check-circle" size={14} /> {t('servers.security.updates.none')}</p>
        {/if}
        <dl class="meta">
          <dt>{t('servers.security.updates.manager')}</dt>
          <dd>{upd.manager || '—'}</dd>
          <dt>{t('servers.security.updates.index')}</dt>
          <dd>{upd.index_time ? fmtRelative(upd.index_time, now) : t('servers.security.updates.unknown')}</dd>
          <dt>{t('servers.security.updates.checked')}</dt>
          <dd>{upd.checked_at ? fmtRelative(Math.floor(upd.checked_at / 1000), now) : t('servers.security.updates.unknown')}</dd>
        </dl>
        {#if indexAge > INDEX_STALE_DAYS}
          <p class="muted small line">{t('servers.security.updates.indexOld', { d: fmtDays(indexAge) })}</p>
        {/if}
      {/if}
    </section>

    <section class="card sc">
      <div class="sh">
        <h2 class="card-title"><Icon name="rotate-ccw" size={16} /> {t('servers.security.reboot.title')}</h2>
      </div>
      {#if notes.reboot}
        <div class="alert {notes.reboot === 'failed' ? 'warning' : 'info'} note"><Icon name="info" size={16} /><span>{noteText(notes.reboot)}</span></div>
      {/if}
      {#if reboot}
        {#if reboot.required}
          <div class="rb">
            <span class="pill pending">{t('servers.security.reboot.required')}</span>
            {#if rebootDays >= 0}
              <span class="small" class:c-pending={rebootDays > 0} title={fmtDate(data.reboot_since ?? 0)}>
                {rebootDays > 0 ? t('servers.security.reboot.since', { d: rebootDays }) : t('servers.security.reboot.sinceToday')}
              </span>
            {/if}
          </div>
          <p class="text-2 small line">{reboot.reason ? tOr(`servers.security.reboot.reasons.${reboot.reason}`, reboot.reason) : t('servers.security.reboot.unknownReason')}</p>
          {#if reboot.packages?.length}
            <div class="muted small">{t('servers.security.reboot.packages')}</div>
            <ul class="rpk">
              {#each reboot.packages as p (p)}<li>{p}</li>{/each}
            </ul>
          {/if}
        {:else}
          <div class="rb"><span class="pill up">{t('servers.security.reboot.notRequired')}</span></div>
          <p class="muted small line">{t('servers.security.reboot.notRequiredHint')}</p>
        {/if}
      {/if}
    </section>
  </div>

  {#if pkgs.length}
    <section class="card block">
      <div class="tools">
        <h2 class="card-title ttl">{t('servers.security.pkgs.title')}</h2>
        <div class="search">
          <Icon name="search" size={15} />
          <input class="input" type="search" bind:value={q} placeholder={t('servers.security.pkgs.search')} aria-label={t('servers.security.pkgs.search')} />
        </div>
        <div class="seg" role="group" aria-label={t('servers.security.pkgs.title')}>
          <button aria-pressed={!onlySec} class:active={!onlySec} onclick={() => (onlySec = false)}>{t('servers.security.pkgs.all')} <span class="cnt">{pkgs.length}</span></button>
          <button aria-pressed={onlySec} class:active={onlySec} onclick={() => (onlySec = true)}>{t('servers.security.pkgs.onlySec')} <span class="cnt">{secCount}</span></button>
        </div>
      </div>
      {#if upd?.truncated}
        <div class="alert info note"><Icon name="info" size={16} /><span>{t('servers.security.pkgs.truncated')}</span></div>
      {/if}
      {#if shown.length === 0}
        <div class="none">
          <Icon name="search" size={24} />
          <h3>{t('servers.security.pkgs.noMatch')}</h3>
          <button class="btn" onclick={() => { q = ''; onlySec = false; }}>{t('servers.security.pkgs.clear')}</button>
        </div>
      {:else}
        <table class="table responsive">
          <thead>
            <tr>
              <th>{t('servers.security.pkgs.colName')}</th>
              <th>{t('servers.security.pkgs.colCurrent')}</th>
              <th>{t('servers.security.pkgs.colNew')}</th>
              <th>{t('servers.security.pkgs.colKind')}</th>
              <th class="num">{t('servers.security.pkgs.colWait')}</th>
            </tr>
          </thead>
          <tbody>
            {#each shown as p (p.name)}
              <tr>
                <td data-label={t('servers.security.pkgs.colName')}><span class="mono-name">{p.name}</span></td>
                <td data-label={t('servers.security.pkgs.colCurrent')} class="ver">{p.current || '—'}</td>
                <td data-label={t('servers.security.pkgs.colNew')} class="ver">{p.new}</td>
                <td data-label={t('servers.security.pkgs.colKind')}>
                  <span class="tags">
                    {#if p.security}<span class="pill pending sm">{t('servers.security.pkgs.sec')}</span>{:else}<span class="muted small">{t('servers.security.pkgs.regular')}</span>{/if}
                    {#if p.severity}<span class="pill {sevClass(p.severity)} sm">{sevLabel(p.severity)}</span>{/if}
                    {#if p.esm}<span class="pill maintenance sm">{t('servers.security.pkgs.esm')}</span>{/if}
                  </span>
                </td>
                <td data-label={t('servers.security.pkgs.colWait')} class="num" class:hot={pkgOver(p)} title={fmtDate(p.first_seen)}>{waitText(p)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  {/if}
{/if}

<style>
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 14px;
    margin-bottom: 14px;
  }
  .sc {
    min-width: 0;
  }
  .sh {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
  }
  .card-title {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .note {
    margin-bottom: 12px;
  }
  .nums {
    display: flex;
    gap: 28px;
    margin-bottom: 8px;
  }
  .nb {
    display: flex;
    flex-direction: column;
  }
  .nv {
    font-size: 1.8rem;
    font-weight: 700;
    line-height: 1.1;
    font-variant-numeric: tabular-nums;
  }
  .nl {
    color: var(--muted);
    font-size: 0.8rem;
  }
  .line {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    margin: 6px 0;
  }
  .ok {
    color: var(--up);
  }
  .meta {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 4px 14px;
    margin: 12px 0 0;
    font-size: 0.85rem;
  }
  .meta dt {
    color: var(--muted);
  }
  .meta dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .rb {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-bottom: 4px;
  }
  .rpk {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
  }
  .rpk li {
    padding: 2px 8px;
    border-radius: 6px;
    background: var(--input);
    font-size: 0.8rem;
    overflow-wrap: anywhere;
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    margin-bottom: 14px;
  }
  .ttl {
    margin: 0 auto 0 0;
  }
  .search {
    position: relative;
    flex: 0 1 260px;
    min-width: 0;
  }
  .search :global(svg) {
    position: absolute;
    left: 11px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--muted);
    pointer-events: none;
  }
  .search input {
    width: 100%;
    padding-left: 33px;
  }
  .cnt {
    margin-left: 4px;
    opacity: 0.75;
    font-variant-numeric: tabular-nums;
  }
  .none {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 34px 12px;
    color: var(--muted);
    text-align: center;
  }
  .none h3 {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--text-2);
  }
  .eicon {
    color: var(--muted);
    margin-bottom: 6px;
  }
  .mono-name {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .ver {
    font-size: 0.82rem;
    overflow-wrap: anywhere;
  }
  .tags {
    display: inline-flex;
    flex-wrap: wrap;
    gap: 5px;
  }
  .pill.sm {
    height: 22px;
    font-size: 0.74rem;
  }
  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .hot {
    color: var(--down-text-2);
    font-weight: 700;
  }
  @media (max-width: 720px) {
    .ttl {
      flex: 1 1 100%;
    }
    .search,
    .seg {
      flex: 1 1 100%;
    }
    .seg button {
      flex: 1;
    }
    .num {
      text-align: left;
    }
  }
</style>
