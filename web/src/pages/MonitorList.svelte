<script lang="ts">
  import type { MonitorView } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { clock } from '../lib/ui.svelte';
  import { collator, fmtPct, lower, monitorKind } from '../lib/format';
  import { deleteMonitor, togglePause } from '../lib/actions';
  import MonitorRow from '../components/MonitorRow.svelte';
  import StatusIcon from '../components/StatusIcon.svelte';
  import Icon from '../components/Icon.svelte';

  type Filter = 'all' | 'down' | 'up' | 'maint' | 'paused';
  type Sort = 'status' | 'name' | 'uptime';

  function load<T extends string>(key: string, allowed: readonly T[], def: T): T {
    try {
      const v = localStorage.getItem(key) as T | null;
      return v && allowed.includes(v) ? v : def;
    } catch {
      return def;
    }
  }
  function save(key: string, v: string) {
    try {
      localStorage.setItem(key, v);
    } catch {
      /* gizli pencere vb. */
    }
  }

  let search = $state('');
  // Etiket filtresi: etiket kimliği ('' = tümü). Liste görünümündeki etiketlerden türetilir.
  let tagFilter = $state(loadTag());
  function loadTag(): string {
    try {
      return localStorage.getItem('uptime.tag') ?? '';
    } catch {
      return '';
    }
  }
  $effect(() => save('uptime.tag', tagFilter));
  const tagOptions = $derived.by(() => {
    const seen = new Map<number, { id: number; name: string; count: number }>();
    for (const m of live.monitors)
      for (const t of m.tags ?? []) {
        const e = seen.get(t.id);
        if (e) e.count++;
        else seen.set(t.id, { id: t.id, name: t.name, count: 1 });
      }
    return [...seen.values()].sort((a, b) => collator.compare(a.name, b.name));
  });
  // Seçili etiket artık hiçbir monitörde yoksa filtre kendiliğinden kalkar.
  const activeTag = $derived(tagOptions.some((t) => String(t.id) === tagFilter) ? tagFilter : '');
  let filter = $state<Filter>(load('uptime.filter', ['all', 'down', 'up', 'maint', 'paused'] as const, 'all'));
  let sort = $state<Sort>(load('uptime.sort', ['status', 'name', 'uptime'] as const, 'status'));
  let menuFor = $state<number | null>(null);

  $effect(() => save('uptime.filter', filter));
  $effect(() => save('uptime.sort', sort));

  const counts = $derived.by(() => {
    let up = 0,
      down = 0,
      pending = 0,
      paused = 0,
      maint = 0;
    for (const m of live.monitors) {
      const k = monitorKind(m);
      if (k === 'paused') paused++;
      else if (k === 'maintenance') maint++;
      else if (k === 'up') up++;
      else if (k === 'down') down++;
      else pending++;
    }
    return { up, down, pending, paused, maint, total: live.monitors.length };
  });

  // Önce çalışmayanlar: çalışmayan → bekleyen → bakımda → çalışan → durdurulan
  const RANK = { down: 0, pending: 1, maintenance: 2, up: 3, paused: 4 };
  const rank = (m: MonitorView) => RANK[monitorKind(m)];

  const visible = $derived.by(() => {
    const q = lower(search.trim());
    let list = live.monitors.filter((m) => {
      const k = monitorKind(m);
      if (filter === 'down' && k !== 'down') return false;
      if (filter === 'up' && k !== 'up') return false;
      if (filter === 'maint' && k !== 'maintenance') return false;
      if (filter === 'paused' && k !== 'paused') return false;
      if (q && !lower(m.name).includes(q) && !lower(m.target).includes(q) && !(m.tags ?? []).some((t) => lower(`${t.name} ${t.value}`).includes(q)))
        return false;
      if (activeTag && !(m.tags ?? []).some((t) => String(t.id) === activeTag)) return false;
      return true;
    });
    list = list.slice();
    if (sort === 'name') list.sort((a, b) => collator.compare(a.name, b.name));
    else if (sort === 'uptime')
      list.sort((a, b) => {
        const ua = a.uptime_24h ?? Infinity;
        const ub = b.uptime_24h ?? Infinity;
        return ua - ub || collator.compare(a.name, b.name);
      });
    else list.sort((a, b) => rank(a) - rank(b) || collator.compare(a.name, b.name));
    return list;
  });

  const summary = $derived(live.summary);

  // Masaüstünde liste kendi içinde kayar: başlık ve araç çubuğu yerinde kalır,
  // kutu ekranın altına kadar uzanır. Dar ekranda sayfa normal kayar.
  let toolbarEl = $state<HTMLElement>();
  let listEl = $state<HTMLElement>();
  let listMax = $state(0);
  function fitList() {
    if (!listEl || !window.matchMedia('(min-width: 901px)').matches) {
      listMax = 0;
      return;
    }
    const top = listEl.getBoundingClientRect().top + window.scrollY;
    listMax = Math.max(320, Math.floor(window.innerHeight - top - 48));
  }
  $effect(() => {
    if (!listEl || !toolbarEl) return;
    fitList();
    // Araç çubuğu satır atlarsa listenin başladığı yer değişir.
    const ro = new ResizeObserver(fitList);
    ro.observe(toolbarEl);
    return () => ro.disconnect();
  });
</script>

<svelte:window onresize={fitList} onscroll={() => (menuFor = null)} />

<svelte:document onclick={() => (menuFor = null)} onkeydown={(e) => e.key === 'Escape' && (menuFor = null)} />

<div class="page-head">
  <h1>Monitörler<span class="dot">.</span></h1>
  {#if session.canEdit}
    <a class="btn primary" href="#/monitors/new"><Icon name="plus" size={16} /> Yeni</a>
  {/if}
</div>

{#if !live.loaded}
  {#if live.loadError}
    <div class="card empty">
      <h3>Monitörler yüklenemedi</h3>
      <p>{live.loadError}</p>
      <button class="btn primary" onclick={() => live.refresh()}>Tekrar dene</button>
    </div>
  {:else}
    <div class="layout">
      <div class="card list">
        {#each Array(5) as _, i (i)}
          <div class="sk-row"><div class="skeleton" style="width:32px;height:32px;border-radius:50%"></div><div class="skeleton" style="flex:1;height:30px"></div></div>
        {/each}
      </div>
      <div></div>
    </div>
  {/if}
{:else if live.monitors.length === 0}
  <div class="card empty first">
    <div class="pulse-wrap"><StatusIcon kind="up" size={56} pulse /></div>
    {#if session.canEdit}
      <h3>İlk monitörünüzü ekleyin</h3>
      <p>Web sitelerinizi, sunucularınızı ve zamanlanmış işlerinizi izlemeye başlayın. Bir sorun olduğunda size hemen haber verelim.</p>
      <a class="btn primary" href="#/monitors/new"><Icon name="plus" size={16} /> Monitör ekle</a>
      {#if session.isAdmin}
        <a class="migrate" href="#/settings/backup?tasi=1">
          <span class="mig-ic"><Icon name="log-in" size={18} /></span>
          <span class="mig-t">
            <b>UptimeRobot veya Uptime Kuma’dan taşıyın</b>
            <span>Monitörlerinizi, bildirim kanallarınızı ve etiketlerinizi birkaç tıkla aktarın.</span>
          </span>
          <Icon name="chevron-right" size={16} />
        </a>
      {/if}
    {:else}
      <h3>Görüntülenecek monitör yok</h3>
      <p>Hesabınıza henüz monitör atanmamış. Yöneticiniz monitör eklediğinde veya erişim verdiğinde burada görünecek.</p>
    {/if}
  </div>
{:else}
  <div class="layout">
    <section class="main-col">
      <div class="toolbar" bind:this={toolbarEl}>
        <div class="search">
          <span class="s-ic"><Icon name="search" size={16} /></span>
          <input class="input" type="search" placeholder="Ad veya adrese göre ara" bind:value={search} aria-label="Ara" />
          {#if search}
            <button type="button" class="clear" aria-label="Aramayı temizle" onclick={() => (search = '')}><Icon name="x" size={14} /></button>
          {/if}
        </div>
        <select class="input sel" bind:value={filter} aria-label="Filtre">
          <option value="all">Tümü ({counts.total})</option>
          <option value="down">Çalışmayanlar ({counts.down})</option>
          <option value="up">Çalışanlar ({counts.up})</option>
          {#if counts.maint > 0 || filter === 'maint'}<option value="maint">Bakımda ({counts.maint})</option>{/if}
          <option value="paused">Durdurulanlar ({counts.paused})</option>
        </select>
        <select class="input sel" bind:value={sort} aria-label="Sıralama">
          <option value="status">Duruma göre</option>
          <option value="name">Ada göre</option>
          <option value="uptime">Uptime'a göre</option>
        </select>
        {#if tagOptions.length}
          <select class="input sel" bind:value={tagFilter} aria-label="Etikete göre filtrele" class:on={!!activeTag}>
            <option value="">Tüm etiketler</option>
            {#each tagOptions as t (t.id)}<option value={String(t.id)}>{t.name} ({t.count})</option>{/each}
          </select>
        {/if}
      </div>

      <div
        class="card list"
        class:scroll={listMax > 0}
        style:max-height={listMax ? `${listMax}px` : null}
        bind:this={listEl}
        onscroll={() => (menuFor = null)}
      >
        {#each visible as m (m.id)}
          <MonitorRow
            {m}
            now={clock.now}
            menuOpen={menuFor === m.id}
            onmenu={(id) => (menuFor = id)}
            onpause={togglePause}
            ondelete={deleteMonitor}
          />
        {:else}
          <div class="noresult">
            Eşleşen monitör yok.
            {#if search || filter !== 'all' || activeTag}
              <button
                class="linkbtn"
                onclick={() => {
                  search = '';
                  filter = 'all';
                  tagFilter = '';
                }}>Filtreyi temizle</button
              >
            {/if}
          </div>
        {/each}
      </div>
    </section>

    <aside class="side">
      <div class="card status-card">
        <h2 class="card-title">Mevcut durum<span class="dot">.</span></h2>
        <div class="big">
          <StatusIcon
            kind={counts.down > 0 ? 'down' : counts.up + counts.pending > 0 ? 'up' : counts.maint > 0 ? 'maintenance' : 'paused'}
            size={52}
            pulse
          />
          <div>
            <div class="big-label {counts.down > 0 ? 'c-down' : counts.up + counts.pending === 0 && counts.maint > 0 ? 'c-maint' : 'c-up'}">
              {counts.down > 0
                ? `${counts.down} monitör çalışmıyor`
                : counts.up + counts.pending === 0 && counts.maint > 0
                  ? 'Bakım sürüyor'
                  : 'Her şey yolunda'}
            </div>
            <div class="muted small">{counts.total} monitör izleniyor</div>
          </div>
        </div>
        <div class="counts" class:four={counts.maint > 0}>
          <div><b class="c-down">{counts.down}</b><span>Çalışmayan</span></div>
          <div><b class="c-up">{counts.up}</b><span>Çalışan</span></div>
          {#if counts.maint > 0}
            <button type="button" class="cnt-btn" onclick={() => (filter = 'maint')} title="Bakımdakileri göster">
              <b class="c-maint">{counts.maint}</b><span>Bakımda</span>
            </button>
          {/if}
          <div><b class="c-paused">{counts.paused}</b><span>Durdurulan</span></div>
        </div>
        {#if counts.pending > 0}
          <div class="pending-note c-pending small">{counts.pending} monitör kontrol bekliyor</div>
        {/if}
      </div>

      <div class="card">
        <h2 class="card-title">Son 24 saat<span class="dot">.</span></h2>
        <div class="counts three">
          <div>
            <b class={summary?.uptime_24h != null && summary.uptime_24h < 99 ? 'c-down' : 'c-up'}>{fmtPct(summary?.uptime_24h)}</b>
            <span>Genel uptime</span>
          </div>
          <div><b>{summary?.incidents_24h ?? '—'}</b><span>Olay</span></div>
          <div><b class={counts.down > 0 ? 'c-down' : ''}>{counts.down}</b><span>Süren sorun</span></div>
        </div>
        <a class="more" href="#/incidents">Tüm olaylar <Icon name="chevron-right" size={14} /></a>
      </div>
    </aside>
  </div>
{/if}

<style>
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
    gap: 24px;
    align-items: start;
  }
  .main-col {
    min-width: 0;
  }
  .toolbar {
    display: flex;
    gap: 10px;
    margin-bottom: 14px;
    flex-wrap: wrap;
  }
  .search {
    position: relative;
    flex: 1 1 220px;
    min-width: 0;
  }
  .s-ic {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    color: var(--muted);
    pointer-events: none;
  }
  .sel.on {
    border-color: var(--accent-border);
  }
  .migrate {
    display: flex;
    align-items: center;
    gap: 12px;
    max-width: 460px;
    margin: 22px auto 0;
    padding: 12px 14px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    background: var(--card-2);
    color: var(--text);
    text-align: left;
    text-decoration: none;
  }
  .migrate > :global(svg) {
    color: var(--muted);
    flex-shrink: 0;
  }
  @media (hover: hover) {
    .migrate:hover {
      border-color: var(--accent-border);
      text-decoration: none;
    }
  }
  .mig-ic {
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent-text);
    flex-shrink: 0;
  }
  .mig-t {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .mig-t span {
    font-size: 0.83rem;
    color: var(--muted);
  }
  .search .input {
    padding-left: 36px;
    padding-right: 36px;
  }
  .clear {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    width: 28px;
    height: 28px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: 6px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }
  @media (hover: hover) {
    .clear:hover {
      color: var(--text);
      background: var(--card-2);
    }
  }
  .sel {
    width: auto;
    flex: 0 1 auto;
  }
  .list {
    padding: 0;
  }
  .list.scroll {
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }
  .list :global(.row:last-child) {
    border-bottom: none;
  }
  .list :global(.row:first-child) {
    border-top-left-radius: var(--radius);
    border-top-right-radius: var(--radius);
  }
  .list :global(.row:last-child) {
    border-bottom-left-radius: var(--radius);
    border-bottom-right-radius: var(--radius);
  }
  .sk-row {
    display: flex;
    gap: 16px;
    align-items: center;
    padding: 16px 18px;
    border-bottom: 1px solid var(--border);
  }
  .noresult {
    padding: 36px 20px;
    text-align: center;
    color: var(--muted);
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 16px;
    position: sticky;
    top: 24px;
  }
  .big {
    display: flex;
    align-items: center;
    gap: 16px;
    margin: 8px 0 18px;
    padding-left: 6px;
  }
  .big-label {
    font-weight: 700;
    font-size: 1.02rem;
  }
  .counts {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
    border-top: 1px solid var(--border);
    padding-top: 14px;
  }
  .counts.four {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    row-gap: 12px;
  }
  .cnt-btn {
    background: none;
    border: none;
    padding: 0;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    border-radius: 6px;
  }
  .cnt-btn span {
    font-size: 0.78rem;
    color: var(--muted);
  }
  .cnt-btn b {
    font-size: 1.35rem;
    font-weight: 700;
    line-height: 1.2;
  }
  .counts div {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .counts b {
    font-size: 1.35rem;
    font-weight: 700;
    line-height: 1.2;
  }
  .counts span {
    font-size: 0.78rem;
    color: var(--muted);
  }
  .counts.three {
    border-top: none;
    padding-top: 0;
  }
  .pending-note {
    margin-top: 10px;
  }
  .more {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    margin-top: 14px;
    font-size: 0.85rem;
  }
  .first {
    padding: 64px 20px;
  }
  .pulse-wrap {
    display: flex;
    justify-content: center;
    margin-bottom: 8px;
  }

  @media (max-width: 1180px) {
    .layout {
      grid-template-columns: minmax(0, 1fr);
    }
    .side {
      order: -1;
      position: static;
      display: grid;
      grid-template-columns: 1fr 1fr;
    }
  }
  @media (max-width: 640px) {
    .layout {
      gap: 16px;
    }
    .side {
      grid-template-columns: minmax(0, 1fr);
      gap: 12px;
    }
    .side .card {
      padding: 14px 16px;
    }
    .side .card-title {
      margin-bottom: 8px;
      font-size: 0.95rem;
    }
    .big {
      margin: 0 0 12px;
      gap: 12px;
    }
    .big :global(.si) {
      width: 40px !important;
      height: 40px !important;
    }
    .counts {
      padding-top: 10px;
    }
    .counts b {
      font-size: 1.15rem;
    }
    .more {
      margin-top: 8px;
    }
    .toolbar .sel {
      flex: 1 1 130px;
      padding-right: 30px;
      background-position: right 10px center;
    }
  }
</style>
