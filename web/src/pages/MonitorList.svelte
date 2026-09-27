<script lang="ts">
  import { STATUS_DOWN, STATUS_UP, type MonitorView } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { clock } from '../lib/ui.svelte';
  import { collator, fmtPct, lower } from '../lib/format';
  import { deleteMonitor, togglePause } from '../lib/actions';
  import MonitorRow from '../components/MonitorRow.svelte';
  import StatusIcon from '../components/StatusIcon.svelte';
  import Icon from '../components/Icon.svelte';

  type Filter = 'all' | 'down' | 'up' | 'paused';
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
  let filter = $state<Filter>(load('uptime.filter', ['all', 'down', 'up', 'paused'] as const, 'all'));
  let sort = $state<Sort>(load('uptime.sort', ['status', 'name', 'uptime'] as const, 'status'));
  let menuFor = $state<number | null>(null);

  $effect(() => save('uptime.filter', filter));
  $effect(() => save('uptime.sort', sort));

  const counts = $derived.by(() => {
    let up = 0,
      down = 0,
      pending = 0,
      paused = 0;
    for (const m of live.monitors) {
      if (!m.active) paused++;
      else if (m.status === STATUS_UP) up++;
      else if (m.status === STATUS_DOWN) down++;
      else pending++;
    }
    return { up, down, pending, paused, total: live.monitors.length };
  });

  // Önce çalışmayanlar: çalışmayan → bekleyen → çalışan → durdurulan
  function rank(m: MonitorView): number {
    if (!m.active) return 3;
    if (m.status === STATUS_DOWN) return 0;
    if (m.status === STATUS_UP) return 2;
    return 1;
  }

  const visible = $derived.by(() => {
    const q = lower(search.trim());
    let list = live.monitors.filter((m) => {
      if (filter === 'down' && !(m.active && m.status === STATUS_DOWN)) return false;
      if (filter === 'up' && !(m.active && m.status === STATUS_UP)) return false;
      if (filter === 'paused' && m.active) return false;
      if (q && !lower(m.name).includes(q) && !lower(m.target).includes(q)) return false;
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
</script>

<svelte:document onclick={() => (menuFor = null)} onkeydown={(e) => e.key === 'Escape' && (menuFor = null)} />

<div class="page-head">
  <h1>Monitörler<span class="dot">.</span></h1>
  <a class="btn primary" href="#/monitors/new"><Icon name="plus" size={16} /> Yeni</a>
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
    <h3>İlk monitörünüzü ekleyin</h3>
    <p>Web sitelerinizi, sunucularınızı ve zamanlanmış işlerinizi izlemeye başlayın. Bir sorun olduğunda size hemen haber verelim.</p>
    <a class="btn primary" href="#/monitors/new"><Icon name="plus" size={16} /> Monitör ekle</a>
  </div>
{:else}
  <div class="layout">
    <section class="main-col">
      <div class="toolbar">
        <div class="search">
          <Icon name="search" size={16} />
          <input class="input" type="search" placeholder="Ad veya adrese göre ara" bind:value={search} aria-label="Ara" />
        </div>
        <select class="input sel" bind:value={filter} aria-label="Filtre">
          <option value="all">Tümü ({counts.total})</option>
          <option value="down">Çalışmayanlar ({counts.down})</option>
          <option value="up">Çalışanlar ({counts.up})</option>
          <option value="paused">Durdurulanlar ({counts.paused})</option>
        </select>
        <select class="input sel" bind:value={sort} aria-label="Sıralama">
          <option value="status">Önce çalışmayanlar</option>
          <option value="name">Ada göre</option>
          <option value="uptime">Uptime'a göre</option>
        </select>
      </div>

      <div class="card list">
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
            {#if search || filter !== 'all'}
              <button
                class="linkbtn"
                onclick={() => {
                  search = '';
                  filter = 'all';
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
          <StatusIcon kind={counts.down > 0 ? 'down' : counts.up + counts.pending > 0 ? 'up' : 'paused'} size={52} pulse />
          <div>
            <div class="big-label {counts.down > 0 ? 'c-down' : 'c-up'}">
              {counts.down > 0 ? `${counts.down} monitör çalışmıyor` : 'Her şey yolunda'}
            </div>
            <div class="muted small">{counts.total} monitör izleniyor</div>
          </div>
        </div>
        <div class="counts">
          <div><b class="c-down">{counts.down}</b><span>Çalışmayan</span></div>
          <div><b class="c-up">{counts.up}</b><span>Çalışan</span></div>
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
  .search :global(svg) {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--muted);
    pointer-events: none;
  }
  .search .input {
    padding-left: 36px;
  }
  .sel {
    width: auto;
    flex: 0 1 auto;
  }
  .list {
    padding: 0;
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
  .linkbtn {
    background: none;
    border: none;
    color: var(--accent-text);
    font: inherit;
    cursor: pointer;
    padding: 0 4px;
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
    margin: 6px 0 18px;
    padding-left: 4px;
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
      flex: 1 1 140px;
    }
  }
</style>
