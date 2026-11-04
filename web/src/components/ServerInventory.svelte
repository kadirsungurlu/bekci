<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, type DiskInfo, type InvCert, type ServerInventory as Inv, type StatsSeries } from '../lib/api';
  import { collator, fmtBytes, fmtDate, fmtDec, fmtPctInt, fmtRelative, nowSec } from '../lib/format';
  import { t } from '../lib/i18n';
  import { session } from '../lib/session.svelte';
  import { toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import LineChart from './LineChart.svelte';
  import UsageBar from './UsageBar.svelte';

  type Section = 'services' | 'containers' | 'ports' | 'certs' | 'disks';

  let { id, disks = [] }: { id: number; disks?: DiskInfo[] } = $props();

  const SECTIONS = [
    { key: 'services', icon: 'list' },
    { key: 'containers', icon: 'box' },
    { key: 'ports', icon: 'plug' },
    { key: 'certs', icon: 'certificate' },
    { key: 'disks', icon: 'database' },
  ] as const satisfies readonly { key: Section; icon: string }[];
  const FILTERS = {
    services: ['all', 'running', 'failed', 'inactive'],
    containers: ['all', 'running', 'stopped', 'unhealthy'],
    ports: ['all', 'public', 'local', 'tcp', 'udp'],
    certs: ['all', 'expiring', 'expired', 'ignored'],
    disks: [],
  } as const satisfies Record<Section, readonly string[]>;
  type Filter = (typeof FILTERS)[keyof typeof FILTERS][number];
  type NoteCode = 'unsupported' | 'no_systemd' | 'no_docker' | 'no_host' | 'failed';
  const EXPIRING_DAYS = 30;

  let inv = $state<Inv | null>(null);
  let loadError = $state('');
  let section = $state<Section>('services');
  let q = $state('');
  let filter = $state<Filter>('all');
  let open = $state('');
  let stats = $state<StatsSeries | null>(null);
  let statsError = $state(false);
  let busy = $state('');
  let now = $state(nowSec());

  async function load() {
    try {
      inv = await api.getServerInventory(id);
      loadError = '';
    } catch (e) {
      if (!inv) loadError = (e as Error).message;
    }
    now = nowSec();
  }

  let timer: ReturnType<typeof setInterval>;
  onMount(() => {
    load();
    timer = setInterval(() => {
      if (!document.hidden) load();
    }, 60000);
  });
  onDestroy(() => clearInterval(timer));

  function pick(s: Section) {
    section = s;
    filter = 'all';
    open = '';
  }

  const daysLeft = (c: InvCert) => Math.floor((c.not_after - now) / 86400);
  const isPublic = (addr: string) => addr === '0.0.0.0' || addr === '::' || addr === '*';
  const stopped = (state: string) => state !== 'running' && state !== 'restarting' && state !== 'paused';
  const match = (...parts: (string | number | undefined)[]) => {
    const s = q.trim().toLocaleLowerCase();
    return !s || parts.some((p) => String(p ?? '').toLocaleLowerCase().includes(s));
  };

  const services = $derived(
    (inv?.services ?? []).filter(
      (s) =>
        match(s.name, s.description) &&
        (filter === 'all' ||
          (filter === 'running' && s.active === 'active') ||
          (filter === 'failed' && s.active === 'failed') ||
          (filter === 'inactive' && s.active !== 'active' && s.active !== 'failed')),
    ),
  );
  const containers = $derived(
    (inv?.containers ?? []).filter(
      (c) =>
        match(c.name, c.image, c.command) &&
        (filter === 'all' ||
          (filter === 'running' && c.state === 'running') ||
          (filter === 'stopped' && stopped(c.state)) ||
          (filter === 'unhealthy' && (c.health === 'unhealthy' || c.state === 'restarting'))),
    ),
  );
  const ports = $derived(
    (inv?.ports ?? []).filter(
      (p) =>
        match(p.port, p.addr, p.process, p.cmd) &&
        (filter === 'all' ||
          filter === p.proto ||
          (filter === 'public' && isPublic(p.addr)) ||
          (filter === 'local' && !isPublic(p.addr))),
    ),
  );
  const certs = $derived(
    (inv?.certs ?? []).filter(
      (c) =>
        match(c.subject, c.issuer, c.location, (c.sans ?? []).join(' ')) &&
        (filter === 'all' ||
          (filter === 'ignored' && c.ignored) ||
          (filter === 'expired' && daysLeft(c) < 0) ||
          (filter === 'expiring' && !c.ignored && daysLeft(c) >= 0 && daysLeft(c) <= EXPIRING_DAYS)),
    ),
  );
  const invDisks = $derived(
    (inv?.disks ?? []).filter((d) => match(d.mount, d.device, d.fs)).slice().sort((a, b) => collator.compare(a.mount, b.mount)),
  );
  const shown = $derived({ services, containers, ports, certs, disks: invDisks }[section].length);
  const total = $derived((inv?.[section] ?? []).length);
  const counts = $derived({
    services: inv?.services.length ?? 0,
    containers: inv?.containers.length ?? 0,
    ports: inv?.ports.length ?? 0,
    certs: inv?.certs.length ?? 0,
    disks: inv?.disks.length ?? 0,
  });
  const certAlarm = $derived((inv?.certs ?? []).filter((c) => !c.ignored && daysLeft(c) <= 14).length);
  const note = $derived((inv?.notes?.[section] ?? '') as NoteCode | '');
  const collected = $derived(!!inv?.collected_at);

  const stateClass = (s: string) =>
    s === 'active' || s === 'running' ? 'up' : s === 'failed' || s === 'dead' || s === 'exited' ? 'down' : s === 'restarting' ? 'pending' : 'paused';
  const certClass = (c: InvCert) => {
    if (c.ignored) return 'paused';
    const d = daysLeft(c);
    return d < 0 || d <= 1 ? 'down' : d <= 14 ? 'pending' : 'up';
  };
  const certLabel = (c: InvCert) => {
    const d = daysLeft(c);
    return d < 0 ? t('servers.inventory.expired') : t('servers.inventory.daysLeft', { count: d });
  };
  const diskPct = (mount: string) => {
    const d = disks.find((x) => x.mount === mount);
    return d && d.total ? (d.used / d.total) * 100 : null;
  };

  async function toggle(key: string) {
    if (open === key) {
      open = '';
      return;
    }
    open = key;
    if (stats) return;
    statsError = false;
    try {
      stats = await api.getServerStats(id, '24h');
    } catch {
      statsError = true;
    }
  }

  const points = $derived(stats?.points ?? []);
  const times = $derived(points.map((p) => p.t));
  const diskSeries = (mount: string) => [
    {
      label: t('servers.inventory.colUsage'),
      color: 'var(--chart-1)',
      fill: true,
      values: points.map((p) => p.disks?.find((d) => d.mount === mount)?.pct ?? null),
    },
  ];
  const cntSeries = (name: string, f: 'cpu' | 'mem') =>
    points.map((p) => p.containers?.find((c) => c.name === name)?.[f] ?? null);
  const pct1 = (v: number) => t('servers.pct', { v: fmtDec(v, 1) });

  async function setIgnored(c: InvCert) {
    busy = c.fingerprint;
    try {
      await api.setInventoryCertIgnored(id, c.fingerprint, !c.ignored);
      await load();
    } catch (e) {
      toast.error((e as Error).message);
    }
    busy = '';
  }
</script>

{#snippet chart(key: string, kind: 'c' | 'd', name: string)}
  {#if open === key}
    <tr class="chart-row">
      <td colspan="9">
        {#if stats}
          {#if points.length}
            {#if kind === 'c'}{@render cntChart(name)}{:else}{@render diskChart(name)}{/if}
          {:else}
            <p class="muted small nomargin">{t('servers.inventory.noSeries')}</p>
          {/if}
        {:else if statsError}
          <p class="muted small nomargin">{t('servers.inventory.seriesFailed')}</p>
        {:else}
          <div class="skeleton" style="height:160px"></div>
        {/if}
      </td>
    </tr>
  {/if}
{/snippet}

{#snippet expander(key: string, label: string)}
  <button class="exp" aria-expanded={open === key} aria-label={t('servers.inventory.showChart', { name: label })} onclick={() => toggle(key)}>
    <Icon name={open === key ? 'chevron-down' : 'chevron-right'} size={15} />
    <span class="mono-name">{label}</span>
  </button>
{/snippet}

{#if loadError}
  <div class="card empty">
    <h3>{t('servers.inventory.loadFailed')}</h3>
    <p>{loadError}</p>
    <button class="btn primary" onclick={load}>{t('common.retry')}</button>
  </div>
{:else if !inv}
  <div class="skeleton" style="height:320px"></div>
{:else if !collected}
  <div class="card empty">
    <div class="eicon"><Icon name="inbox" size={26} /></div>
    <h3>{t('servers.inventory.emptyTitle')}</h3>
    <p>{t('servers.inventory.emptyHint')}</p>
    <button class="btn" onclick={load}><Icon name="refresh" size={15} /> {t('servers.inventory.refresh')}</button>
  </div>
{:else}
  <div class="sections" role="tablist" aria-label={t('servers.inventory.title')}>
    {#each SECTIONS as s (s.key)}
      <button role="tab" aria-selected={section === s.key} class:active={section === s.key} onclick={() => pick(s.key)}>
        <Icon name={s.icon} size={15} />
        <span>{t(`servers.inventory.sections.${s.key}`)}</span>
        <span class="count" class:warn={s.key === 'certs' && certAlarm > 0}>{counts[s.key]}</span>
      </button>
    {/each}
  </div>

  <section class="card block">
    <div class="tools">
      <div class="search">
        <Icon name="search" size={15} />
        <input class="input" type="search" bind:value={q} placeholder={t('servers.inventory.search')} aria-label={t('servers.inventory.search')} />
      </div>
      {#if FILTERS[section].length}
        <select class="input flt" bind:value={filter} aria-label={t('servers.inventory.filter')}>
          {#each FILTERS[section] as f (f as string)}
            <option value={f}>{t(`servers.inventory.filters.${f}`)}</option>
          {/each}
        </select>
      {/if}
      <span class="muted small stamp" title={fmtDate(inv.collected_at)}>{t('servers.inventory.collected', { ago: fmtRelative(inv.collected_at, now) })}</span>
    </div>

    {#if note}
      <div class="alert info note"><Icon name="info" size={16} /><span>{t(`servers.inventory.notes.${note}`)}</span></div>
    {/if}

    {#if shown === 0}
      <div class="none">
        <Icon name={total ? 'search' : SECTIONS.find((s) => s.key === section)!.icon} size={24} />
        <h3>{total ? t('servers.inventory.noMatch') : t(`servers.inventory.none.${section}`)}</h3>
        {#if total}
          <button class="btn" onclick={() => { q = ''; filter = 'all'; }}>{t('servers.inventory.clear')}</button>
        {/if}
      </div>
    {:else if section === 'services'}
      <table class="table responsive">
        <thead><tr><th>{t('servers.inventory.colService')}</th><th>{t('servers.inventory.colState')}</th><th>{t('servers.inventory.colBoot')}</th><th>{t('servers.inventory.colDesc')}</th></tr></thead>
        <tbody>
          {#each services as s (s.name)}
            <tr>
              <td data-label={t('servers.inventory.colService')}><span class="mono-name">{s.name}</span></td>
              <td data-label={t('servers.inventory.colState')}><span class="pill {stateClass(s.active)}">{s.sub || s.active}</span></td>
              <td data-label={t('servers.inventory.colBoot')}>{s.enabled || '-'}</td>
              <td data-label={t('servers.inventory.colDesc')} class="text-2">{s.description || '-'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {:else if section === 'containers'}
      <table class="table responsive">
        <thead><tr><th>{t('servers.inventory.colContainer')}</th><th>{t('servers.inventory.colImage')}</th><th>{t('servers.inventory.colState')}</th><th class="num">{t('servers.inventory.colRestarts')}</th><th>{t('servers.inventory.colStarted')}</th></tr></thead>
        <tbody>
          {#each containers as c (c.name)}
            <tr>
              <td data-label={t('servers.inventory.colContainer')}>
                {@render expander('c:' + c.name, c.name)}
                {#if c.command}<div class="cmd" title={c.command}>{c.command}</div>{/if}
              </td>
              <td data-label={t('servers.inventory.colImage')} class="break">{c.image}</td>
              <td data-label={t('servers.inventory.colState')}>
                <span class="pill {c.health === 'unhealthy' ? 'down' : stateClass(c.state)}">{c.state}{c.health ? ` · ${c.health}` : ''}</span>
              </td>
              <td data-label={t('servers.inventory.colRestarts')} class="num" class:hot={c.restart_count > 0}>{c.restart_count}</td>
              <td data-label={t('servers.inventory.colStarted')}>{c.started_at ? fmtRelative(c.started_at, now) : '-'}</td>
            </tr>
            {@render chart('c:' + c.name, 'c', c.name)}
          {/each}
        </tbody>
      </table>
    {:else if section === 'ports'}
      <table class="table responsive">
        <thead><tr><th class="num">{t('servers.inventory.colPort')}</th><th>{t('servers.inventory.colProto')}</th><th>{t('servers.inventory.colAddr')}</th><th>{t('servers.inventory.colProcess')}</th></tr></thead>
        <tbody>
          {#each ports as p (`${p.proto}/${p.addr}/${p.port}`)}
            <tr>
              <td data-label={t('servers.inventory.colPort')} class="num"><b>{p.port}</b></td>
              <td data-label={t('servers.inventory.colProto')}>{p.proto.toUpperCase()}</td>
              <td data-label={t('servers.inventory.colAddr')}>
                <span class="mono-name">{p.addr}</span>
                {#if isPublic(p.addr)}<span class="pill pending tag">{t('servers.inventory.public')}</span>{/if}
              </td>
              <td data-label={t('servers.inventory.colProcess')}>
                {p.process || '-'}
                {#if p.cmd}<div class="cmd" title={p.cmd}>{p.cmd}</div>{/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {:else if section === 'certs'}
      <table class="table responsive">
        <thead><tr><th>{t('servers.inventory.colCert')}</th><th>{t('servers.inventory.colLocation')}</th><th>{t('servers.inventory.colIssuer')}</th><th>{t('servers.inventory.colExpires')}</th>{#if session.canEdit}<th></th>{/if}</tr></thead>
        <tbody>
          {#each certs as c (`${c.source}/${c.location}`)}
            <tr>
              <td data-label={t('servers.inventory.colCert')}>
                <span class="mono-name">{c.subject || c.location}</span>
                {#if c.sans?.length}<div class="cmd" title={c.sans.join(', ')}>{c.sans.join(', ')}</div>{/if}
              </td>
              <td data-label={t('servers.inventory.colLocation')} class="break">{c.location}</td>
              <td data-label={t('servers.inventory.colIssuer')}>{c.issuer || '-'}</td>
              <td data-label={t('servers.inventory.colExpires')}>
                <span class="pill {certClass(c)}">{certLabel(c)}</span>
                <div class="muted small">{fmtDate(c.not_after)}</div>
              </td>
              {#if session.canEdit}
                <td data-label="" class="act">
                  <button class="btn sm" disabled={busy === c.fingerprint} title={t('servers.inventory.ignoreHint')} onclick={() => setIgnored(c)}>
                    {c.ignored ? t('servers.inventory.unignore') : t('servers.inventory.ignore')}
                  </button>
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    {:else}
      <table class="table responsive">
        <thead><tr><th>{t('servers.inventory.colMount')}</th><th>{t('servers.inventory.colDevice')}</th><th>{t('servers.inventory.colFs')}</th><th class="num">{t('servers.inventory.colSize')}</th><th>{t('servers.inventory.colUsage')}</th></tr></thead>
        <tbody>
          {#each invDisks as d (d.mount)}
            {@const pct = diskPct(d.mount)}
            <tr>
              <td data-label={t('servers.inventory.colMount')}>{@render expander('d:' + d.mount, d.mount)}</td>
              <td data-label={t('servers.inventory.colDevice')} class="break">{d.device || '-'}</td>
              <td data-label={t('servers.inventory.colFs')}>{d.fs || '-'}</td>
              <td data-label={t('servers.inventory.colSize')} class="num">{fmtBytes(d.total)}</td>
              <td data-label={t('servers.inventory.colUsage')} class="usage">
                {#if pct !== null}<span class="pct">{fmtPctInt(pct)}</span><UsageBar value={pct} bare />{:else}-{/if}
              </td>
            </tr>
            {@render chart('d:' + d.mount, 'd', d.mount)}
          {/each}
        </tbody>
      </table>
    {/if}
  </section>
{/if}

{#snippet cntChart(name: string)}
  {@const from = stats!.from}
  {@const to = stats!.to}
  <div class="charts">
    <LineChart label={t('servers.inventory.chartCpu', { name })} {times} {from} {to} interval={stats!.interval} height={150} format={pct1} axisFormat={fmtPctInt}
      series={[{ label: 'CPU', color: 'var(--chart-1)', fill: true, values: cntSeries(name, 'cpu') }]} />
    <LineChart label={t('servers.inventory.chartMem', { name })} {times} {from} {to} interval={stats!.interval} height={150} bytes format={fmtBytes}
      series={[{ label: 'RAM', color: 'var(--chart-2)', fill: true, values: cntSeries(name, 'mem') }]} />
  </div>
{/snippet}

{#snippet diskChart(mount: string)}
  <LineChart label={t('servers.inventory.chartDisk', { name: mount })} {times} from={stats!.from} to={stats!.to} interval={stats!.interval} height={150} max={100} format={pct1} axisFormat={fmtPctInt} series={diskSeries(mount)} />
{/snippet}

<style>
  .sections {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 14px;
  }
  .sections button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    min-height: 44px;
    padding: 0 13px;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 9px;
    color: var(--text-2);
    font: inherit;
    font-size: 0.86rem;
    font-weight: 600;
    cursor: pointer;
  }
  .sections button:hover {
    color: var(--text);
  }
  .sections button.active {
    color: var(--text);
    border-color: var(--accent);
  }
  .count {
    min-width: 22px;
    padding: 1px 6px;
    border-radius: 7px;
    background: var(--input);
    font-size: 0.76rem;
    text-align: center;
    font-variant-numeric: tabular-nums;
  }
  .count.warn {
    color: var(--warn, var(--text));
    outline: 1px solid currentColor;
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    margin-bottom: 14px;
  }
  .search {
    position: relative;
    flex: 1 1 240px;
    max-width: 420px;
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
  .flt {
    width: auto;
    min-width: 150px;
  }
  .stamp {
    margin-left: auto;
  }
  .note {
    margin-bottom: 14px;
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
  .break {
    overflow-wrap: anywhere;
  }
  .cmd {
    max-width: 380px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--muted);
    font-size: 0.78rem;
  }
  .exp {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-height: 32px;
    padding: 0;
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .hot {
    color: var(--warn, inherit);
    font-weight: 700;
  }
  .tag {
    margin-left: 6px;
  }
  .act {
    text-align: right;
  }
  .usage {
    min-width: 130px;
  }
  .pct {
    display: block;
    font-size: 0.8rem;
    margin-bottom: 3px;
    font-variant-numeric: tabular-nums;
  }
  .chart-row td {
    background: var(--card-2);
  }
  .charts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 14px;
  }
  @media (max-width: 720px) {
    .flt,
    .search {
      flex: 1 1 100%;
      max-width: none;
    }
    .stamp {
      margin-left: 0;
    }
    .sections button {
      flex: 1 1 calc(50% - 8px);
      justify-content: center;
    }
    .cmd {
      max-width: 220px;
    }
    .usage {
      min-width: 0;
    }
    .exp {
      min-height: 44px;
    }
    .act :global(.btn) {
      min-height: 44px;
      width: 100%;
    }
  }
</style>
