<script lang="ts">
  // Canlı topoloji: sunucular, kontrol noktaları + kanallar, monitörler ve aralarındaki gerçek
  // ilişkiler. Veri lib/topology.svelte.ts (canlı akış + yedek yoklama), model lib/topomodel.ts,
  // çizgiler lib/topolines.ts. Tıklama ve klavye tek yerde toplanır (satır başına dinleyici yok).
  // Seçim, arama ve süzgeç adreste (#/topology?sec=m12&q=..&sorun=1&sekme=srv) durur.
  import { onDestroy, onMount, tick } from 'svelte';
  import '../lib/topology.css';
  import Icon from '../components/Icon.svelte';
  import TopoColumn from '../components/TopoColumn.svelte';
  import TopoDock from '../components/TopoDock.svelte';
  import TopoProblems from '../components/TopoProblems.svelte';
  import TopoRow from '../components/TopoRow.svelte';
  import { t } from '../lib/i18n';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { TopoLines } from '../lib/topolines';
  import { METRICS } from '../lib/topovirt';
  import { POLL_MS, TopologyStore } from '../lib/topology.svelte';
  import { buildGraph, colOf, DEFAULT_BY, effectiveBy, fold, groupIndex, groupOptions, isProblem, MARK_DEFS, relMap, viewChannels, viewGroups, type Col, type GroupBy, type St, type UI } from '../lib/topomodel';

  const KEY_BY = 'bekci-topo-by';
  const KEY_AC = 'bekci-topo-ac';
  const COLS: Col[] = ['srv', 'pro', 'mon'];

  const stored = <T,>(key: string, def: T): T => {
    try {
      return { ...def, ...JSON.parse(localStorage.getItem(key) ?? '{}') };
    } catch {
      return def;
    }
  };
  const save = (key: string, v: unknown) => {
    try {
      localStorage.setItem(key, JSON.stringify(v));
    } catch {
      /* gizli pencere */
    }
  };

  const store = new TopologyStore();
  let now = $state(Math.floor(Date.now() / 1000));
  const clock = setInterval(() => (now = Math.floor(Date.now() / 1000)), 1000);

  let sel = $state.raw<string | null>(null);
  let query = $state('');
  let sorun = $state(false);
  let tab = $state<Col>('mon');
  let by = $state.raw<GroupBy>(stored(KEY_BY, DEFAULT_BY));
  let ac = $state.raw<Record<string, boolean>>(stored(KEY_AC, {}));
  let tam = $state.raw<Record<string, boolean>>({});
  let announce = $state('');

  const narrow = matchMedia('(min-width:1240px)');
  const small = matchMedia('(max-width:900px)');
  const canHover = matchMedia('(hover:hover)');
  let wide = $state(narrow.matches);
  let phone = $state(small.matches);
  const onWide = () => {
    wide = narrow.matches;
    phone = small.matches;
  };
  const metrics = $derived(METRICS[wide ? 'wide' : phone ? 'phone' : 'tab']);

  function readUrl() {
    const [path, qs] = location.hash.slice(1).split('?');
    if (path !== '/topology') return;
    const p = new URLSearchParams(qs ?? '');
    sel = p.get('sec');
    query = p.get('q') ?? '';
    sorun = p.get('sorun') === '1';
    const s = p.get('sekme') as Col | null;
    if (s && COLS.includes(s)) tab = s;
  }
  readUrl();

  $effect(() => {
    const p = new URLSearchParams();
    if (sel) p.set('sec', sel);
    if (query) p.set('q', query);
    if (sorun) p.set('sorun', '1');
    if (tab !== 'mon') p.set('sekme', tab);
    const qs = p.toString();
    const url = `#/topology${qs ? `?${qs}` : ''}`;
    if (location.hash.startsWith('#/topology') && location.hash !== url) history.replaceState(history.state, '', url);
  });

  const d = $derived(store.data);
  const graph = $derived(d ? buildGraph(d) : null);
  const opts = $derived(d ? groupOptions(d) : null);
  const eff = $derived(opts ? effectiveBy(by, opts) : by);
  const gidx = $derived(graph ? groupIndex(graph, eff) : new Map<string, string>());
  const qf = $derived(fold(query.trim()));
  const ui: UI = $derived({ sel, q: qf, sorun, by: eff, ac, tam });
  const rel = $derived(graph ? relMap(graph, sel) : new Map<string, St>());
  const vSrv = $derived(graph ? viewGroups(graph, 'srv', ui, rel) : []);
  const vPro = $derived(graph ? viewGroups(graph, 'pro', ui, rel) : []);
  const vMon = $derived(graph ? viewGroups(graph, 'mon', ui, rel) : []);
  const vCha = $derived(graph ? viewChannels(graph, ui, rel) : { rows: [], rest: 0 });
  const node = $derived(sel && graph ? (graph.nodes.get(sel) ?? null) : null);
  const isEmpty = $derived(!!graph && !graph.mon.length && !graph.pro.length && !graph.srv.length);
  const rowNow = $derived(Math.floor(now / 15) * 15);
  const ago = $derived(store.updatedAt ? Math.max(0, now - Math.floor(store.updatedAt / 1000)) : 0);
  const stale = $derived(!!store.error && !!d);
  const badges = $derived.by(() => {
    const out = { srv: [0, 0], pro: [0, 0], mon: [0, 0] } as Record<Col, [number, number]>;
    if (!graph || !sel) return out;
    for (const [id, c] of rel) {
      const n = graph.nodes.get(id);
      if (!n || n.kind === 'cha') continue;
      out[n.kind][0]++;
      if (c === 'bad') out[n.kind][1]++;
    }
    return out;
  });

  // Seçili düğüm silindiyse seçim düşer; adresten gelen seçim ilk veride görünür yapılır.
  let revealed = false;
  $effect(() => {
    if (!graph) return;
    if (sel && !graph.nodes.has(sel)) sel = null;
    if (!revealed) {
      revealed = true;
      if (sel) tick().then(() => requestAnimationFrame(() => (wide ? scrollIntoColumns() : scrollMobile())));
    }
  });

  let sutunlar = $state<HTMLElement>();
  let svg = $state<SVGSVGElement>();
  let pay = $state<HTMLElement>();
  let rootEl = $state<HTMLElement>();
  let search = $state<HTMLInputElement>();
  let lines: TopoLines | null = null;
  let hov: string | null = null;
  let hovEls: Element[] = [];
  let frame = 0;
  let roveFrame = 0;
  const cols = $state.raw<Partial<Record<Col, TopoColumn>>>({});
  /** Pencere kayınca sekme durağı silinmiş olabilir. */
  const onRender = () => {
    if (!roveFrame)
      roveFrame = requestAnimationFrame(() => {
        roveFrame = 0;
        rove();
      });
  };

  function paint() {
    frame = 0;
    if (!wide) return lines?.clear();
    lines?.update(sel, hov);
  }
  const queue = () => {
    if (!frame) frame = requestAnimationFrame(paint);
  };
  let scrollFrame = 0;
  const onScroll = () => {
    if (!scrollFrame)
      scrollFrame = requestAnimationFrame(() => {
        scrollFrame = 0;
        lines?.draw();
      });
  };

  $effect(() => {
    if (!sutunlar || !svg || lines) return;
    lines = new TopoLines({
      root: sutunlar,
      svg,
      graph: () => graph,
      pos: (id, col) => cols[col]?.yOf(`o-${id}`) ?? cols[col]?.yOf(gidx.get(id)) ?? null,
      chip: (dir, n) => t(dir === 1 ? 'topology.chipDown' : 'topology.chipUp', { n }),
    });
    queue();
  });

  // DOM güncellendikten sonra: sekme durağı, çizgiler, alt sayfa payı.
  $effect(() => {
    void [vSrv, vPro, vMon, vCha, sel, wide];
    if (!rootEl) return;
    rove();
    queue();
    if (!wide && pay) {
      const h = rootEl.querySelector<HTMLElement>('.dock.acik')?.offsetHeight ?? 0;
      pay.style.setProperty('--sheet', h ? `${h + 12}px` : '0');
    }
  });

  /** Sütun başına tek sekme durağı; seçili satır varsa o. */
  function rove() {
    rootEl?.querySelectorAll<HTMLElement>('.kay').forEach((k) => {
      const chosen = k.querySelector<HTMLElement>('.o.sec') ?? k.querySelector<HTMLElement>('[tabindex="0"]') ?? k.querySelector<HTMLElement>('.gh,.o,.kalan');
      k.querySelectorAll<HTMLElement>('[tabindex="0"]').forEach((e) => {
        if (e !== chosen) e.tabIndex = -1;
      });
      if (chosen) chosen.tabIndex = 0;
    });
  }

  function preview() {
    for (const e of hovEls) e.classList.remove('h');
    hovEls = [];
    if (hov && graph)
      for (const r of graph.rel.get(hov) ?? []) {
        const e = document.getElementById(`o-${r.id}`);
        if (e) {
          e.classList.add('h');
          hovEls.push(e);
        }
      }
    queue();
  }
  const setHover = (id: string | null) => {
    if (id !== hov) {
      hov = id;
      preview();
    }
  };

  /** Seçili ya da ilk ilişkili satırı görünür yapar; yer sanal listeden, kanal satırı DOM'dan. */
  const hitOf = (c: Col) => {
    const y = cols[c]?.firstHit() ?? null;
    if (y !== null) return y;
    const k = rootEl?.querySelector<HTMLElement>(`#k-${c}`);
    const e = k?.querySelector<HTMLElement>('.kuyruk .o.sec') ?? k?.querySelector<HTMLElement>('.kuyruk .o.ilg');
    return k && e ? e.getBoundingClientRect().top - k.getBoundingClientRect().top + k.scrollTop : null;
  };
  function scrollIntoColumns() {
    if (!rootEl) return;
    for (const c of COLS) {
      const k = rootEl.querySelector<HTMLElement>(`#k-${c}`);
      const y = hitOf(c);
      if (!k || y === null) continue;
      if (y < k.scrollTop + 4 || y + 27 > k.scrollTop + k.clientHeight - 4) k.scrollTop = y - k.clientHeight / 3;
    }
    queue();
  }
  function scrollMobile() {
    const bar = rootEl?.querySelector<HTMLElement>('.sekme');
    const col = rootEl?.querySelector<HTMLElement>(`#c-${tab}`);
    if (!bar || !col) return;
    const top = (small.matches ? 54 : 0) + 8;
    const start = (bar.parentElement as HTMLElement).getBoundingClientRect().top + scrollY - top;
    const k = col.querySelector<HTMLElement>('.kay');
    const hit = hitOf(tab);
    const y = k && hit !== null ? k.getBoundingClientRect().top + scrollY + hit - top - bar.offsetHeight - 12 - metrics.head : start;
    scrollTo({ top: Math.max(start, y) });
  }

  async function select(id: string | null) {
    sel = sel === id ? null : id;
    tam = {};
    setHover(null);
    const n = sel ? graph?.nodes.get(sel) : null;
    if (n && !wide) tab = colOf(n.kind);
    announce = n ? t('topology.dock.selected', { name: n.name, n: graph?.rel.get(n.id)?.length ?? 0 }) : t('topology.dock.cleared');
    if (!n) return;
    await tick();
    if (wide) scrollIntoColumns();
    else scrollMobile();
  }

  function onClick(e: MouseEvent) {
    const el = e.target as Element;
    if (el.closest('a')) return;
    if (el.closest('[data-close]')) return void select(sel);
    const more = el.closest<HTMLElement>('.tasan');
    if (more) {
      const [col, dir] = (more.dataset.kay ?? '').split(':');
      const k = rootEl?.querySelector<HTMLElement>(`#k-${col}`);
      k?.scrollBy({ top: (k.clientHeight - 60) * +dir });
      return;
    }
    const byBtn = el.closest<HTMLElement>('[data-by]');
    if (byBtn) {
      const [col, v] = (byBtn.dataset.by ?? '').split(':');
      by = { ...by, [col]: v };
      save(KEY_BY, by);
      tam = {};
      return;
    }
    const tabBtn = el.closest<HTMLElement>('button[data-tab]');
    if (tabBtn) {
      tab = tabBtn.dataset.tab as Col;
      if (sel) tick().then(scrollMobile);
      return;
    }
    const full = el.closest<HTMLElement>('[data-tam]');
    if (full?.dataset.tam) {
      tam = { ...tam, [full.dataset.tam]: true };
      return;
    }
    const head = el.closest<HTMLElement>('[data-g]');
    if (head?.dataset.g) {
      const id = head.dataset.g;
      const open = !head.classList.contains('gh') ? true : head.getAttribute('aria-expanded') !== 'true';
      ac = { ...ac, [id]: open };
      save(KEY_AC, ac);
      if (sel) tam = { ...tam, [id]: open };
      return;
    }
    const row = el.closest<HTMLElement>('[data-id]');
    if (row?.dataset.id) return void select(row.dataset.id);
    if (el.closest('.dock,.anah,.ara,.seg,.sekme') || !sel) return;
    void select(sel);
  }

  let lastKey: [Col, string] | null = null;
  function onMove(e: MouseEvent) {
    if (canHover.matches) setHover((e.target as Element).closest<HTMLElement>('.o')?.dataset.id ?? null);
  }
  function onFocus(e: FocusEvent) {
    const el = e.target as HTMLElement;
    const kol = el.closest?.<HTMLElement>('.kay')?.dataset.kol as Col | undefined;
    if (kol && el.id) lastKey = [kol, el.id];
    const id = el.closest?.<HTMLElement>('.o')?.dataset.id;
    if (id) setHover(id);
  }

  function onKey(e: KeyboardEvent) {
    if (document.querySelector('[role="dialog"]')) return;
    const active = document.activeElement;
    if (e.key === 'Escape') {
      if (sel) void select(sel);
      else if (active === search && query) query = '';
      return;
    }
    if (e.key === '/' && !(active instanceof HTMLInputElement) && !(active instanceof HTMLTextAreaElement)) {
      e.preventDefault();
      search?.focus();
      return;
    }
    let target = e.target as HTMLElement;
    // Fareyle kaydırınca odaklı satır çizimden çıkmış olabilir: oklar son satırdan sürer.
    if (target === document.body && lastKey && ['ArrowDown', 'ArrowUp'].includes(e.key)) {
      e.preventDefault();
      const [col, key] = lastKey;
      void cols[col]?.reveal(key).then((el) => el?.focus());
      return;
    }
    const k = target.closest?.<HTMLElement>('.kay');
    if (!k) return;
    const col = k.dataset.kol as Col;
    const c = cols[col];
    const list = c?.focusables() ?? [];
    const i = list.indexOf(target.id);
    if (i < 0) return;
    const go = (key: string | undefined, to: Col = col) => {
      if (!key) return;
      e.preventDefault();
      target.tabIndex = -1;
      void cols[to]?.reveal(key).then((el) => {
        if (!el) return;
        el.tabIndex = 0;
        el.focus();
      });
    };
    if (e.key === 'ArrowDown') go(list[i + 1]);
    else if (e.key === 'ArrowUp') go(list[i - 1]);
    else if (e.key === 'Home') go(list[0]);
    else if (e.key === 'End') go(list[list.length - 1]);
    else if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && wide) {
      const next = COLS[COLS.indexOf(col) + (e.key === 'ArrowLeft' ? -1 : 1)];
      if (!next) return;
      const y = target.getBoundingClientRect().top;
      let best: HTMLElement | undefined;
      let gap = Infinity;
      rootEl?.querySelectorAll<HTMLElement>(`#k-${next} .gh,#k-${next} .o,#k-${next} .kalan`).forEach((x) => {
        const r = x.getBoundingClientRect();
        const kr = (x.closest('.kay') as HTMLElement).getBoundingClientRect();
        if (r.bottom < kr.top || r.top > kr.bottom) return;
        const f = Math.abs(r.top - y);
        if (f < gap) {
          gap = f;
          best = x;
        }
      });
      go(best?.id, next);
    }
  }

  onMount(() => {
    store.start();
    narrow.addEventListener('change', onWide);
    small.addEventListener('change', onWide);
    window.addEventListener('hashchange', readUrl);
    window.addEventListener('resize', queue);
    window.addEventListener('scroll', onPageScroll, { passive: true });
  });
  const onPageScroll = () => {
    if (wide) return;
    for (const c of COLS) if (c === tab) cols[c]?.sync();
  };
  onDestroy(() => {
    window.removeEventListener('scroll', onPageScroll);
    store.stop();
    clearInterval(clock);
    cancelAnimationFrame(frame);
    cancelAnimationFrame(scrollFrame);
    cancelAnimationFrame(roveFrame);
    narrow.removeEventListener('change', onWide);
    small.removeEventListener('change', onWide);
    window.removeEventListener('hashchange', readUrl);
    window.removeEventListener('resize', queue);
  });

  const problemCount = (l: { st: St; kind: 'srv' | 'pro' | 'cha' | 'mon' }[]) => l.filter((n) => isProblem(n as never)).length;
</script>

<svelte:window onkeydown={onKey} />

<svg width="0" height="0" class="tohum" aria-hidden="true"><defs>{@html MARK_DEFS}</defs></svg>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="topo" bind:this={rootEl} style:--sat="{metrics.row}px" style:--sat2="{metrics.alt}px" style:--bas="{metrics.head}px" onclick={onClick} onmouseover={onMove} onmouseleave={() => setHover(null)} onfocusin={onFocus}>
  <div class="bas">
    <div>
      <h1>{t('topology.title')}<span class="dot">.</span></h1>
      <p class="sub">{t('topology.subtitle')}</p>
    </div>
    <div class="kontrol">
      <label class="ara" class:dolu={!!query}>
        <span class="sr">{t('topology.search.label')}</span>
        <Icon name="search" size={15} />
        <input bind:this={search} bind:value={query} type="search" placeholder={t('topology.search.placeholder')} autocomplete="off" data-testid="topology-search" />
        <button type="button" aria-label={t('topology.search.clear')} onclick={() => { query = ''; search?.focus(); }}><Icon name="x" size={13} /></button>
      </label>
      <button class="anah" type="button" role="switch" aria-checked={sorun} data-testid="topology-problems-only" onclick={() => (sorun = !sorun)}><i></i>{t('topology.onlyProblems')}</button>
      <span class="canli" class:yok={!live.connected} data-testid="topology-live" title={live.connected ? t('topology.live.onTip') : t('topology.live.pollTip', { s: POLL_MS / 1000 })}>
        {t(live.connected ? 'topology.live.on' : 'topology.live.poll', { s: ago })}
      </span>
    </div>
  </div>

  {#if stale}<p class="eski">{store.error}</p>{/if}

  {#if store.error && !d}
    <div class="empty-state" data-testid="topology-error">
      <p>{t('topology.error')}: {store.error}</p>
      <button class="btn" type="button" onclick={() => store.fetch()}>{t('topology.retry')}</button>
    </div>
  {:else if !graph}
    <div class="iskelet" data-testid="topology-loading" aria-busy="true">
      {#each [0, 1, 2] as c (c)}
        <div>{#each [0, 1, 2, 3, 4, 5, 6, 7] as i (i)}<span class="skeleton"></span>{/each}</div>
      {/each}
    </div>
  {:else if isEmpty}
    <div class="empty-state" data-testid="topology-empty">
      <h2>{t('topology.empty.title')}</h2>
      {#if session.isAdmin}
        <p>{t('topology.empty.text')}</p>
        <a class="btn primary" href="#/new">{t('topology.empty.add')}</a>
      {:else}
        <p>{t('topology.empty.restricted')}</p>
      {/if}
    </div>
  {:else if opts}
    <TopoProblems g={graph} {sel} now={rowNow} byMon={eff.mon} bySrv={eff.srv} />
    <div class="tahta" class:secili={!!sel} data-testid="topology-board">
      <div class="sekme" role="tablist" aria-label={t('topology.col.tabs')}>
        {#each [['srv', t('topology.col.servers')], ['pro', t('topology.col.tabProbes')], ['mon', t('topology.col.monitors')]] as [k, label] (k)}
          {@const [n, bad] = badges[k as Col]}
          <button type="button" role="tab" aria-selected={tab === k} data-tab={k}>
            {label}
            {#if n}<b class:h={bad > 0} aria-label={bad ? t('topology.dock.tabBadgeBad', { n, k: bad }) : t('topology.dock.tabBadge', { n })}>{n}</b>{/if}
          </button>
        {/each}
      </div>
      <div class="sutunlar" bind:this={sutunlar} data-tab={tab} onscrollcapture={onScroll}>
        <TopoColumn bind:this={cols.srv} active={tab === 'srv'} col="srv" cls="sut-l" title={t('topology.col.servers')} label={t('topology.col.servers')} count={t('topology.count.servers', { n: graph.srv.length, count: problemCount(graph.srv), k: problemCount(graph.srv), count: problemCount(graph.srv) })} options={opts.srv} by={eff.srv} views={vSrv} g={graph} {rel} {sel} {metrics} {wide} now={rowNow} filtered={!!qf} {sorun} onrender={onRender}>
          {#snippet head()}
            <div class="kb kb-srv"><span>{t('topology.head.name')}</span><span>{t('topology.head.cpu')}</span><span>{t('topology.head.mem')}</span><span>{t('topology.head.disk')}</span></div>
          {/snippet}
        </TopoColumn>
        <TopoColumn bind:this={cols.pro} active={tab === 'pro'} col="pro" cls="orta" title={t('topology.col.probes')} label={t('topology.col.midLabel')} count={String(graph.pro.length)} options={opts.pro} by={eff.pro} views={vPro} g={graph} {rel} {sel} {metrics} {wide} now={rowNow} filtered={!!qf} {sorun} onrender={onRender}>
          {#snippet tail()}
            {#if graph && (d?.channels || graph.cha.length)}
              <div class="alt-b">{t('topology.col.channels')} <span>{t('topology.count.channels', { count: graph.cha.length })}</span></div>
              {#each vCha.rows as n (n.id)}
                <TopoRow {n} g={graph} rel={rel.get(n.id)} sel={n.id === sel} now={rowNow} />
              {:else}
                <p class="bos">{t('topology.emptyChannels')}</p>
              {/each}
              {#if vCha.rest}
                <button type="button" class="kalan" data-tam="kanal" tabindex="-1">{t('topology.rest', { n: vCha.rest, what: t('topology.noun.cha', { count: vCha.rest }) })}</button>
              {/if}
            {/if}
          {/snippet}
        </TopoColumn>
        <TopoColumn bind:this={cols.mon} active={tab === 'mon'} col="mon" title={t('topology.col.monitors')} label={t('topology.col.monitors')} count={t('topology.count.monitors', { n: graph.mon.length, k: problemCount(graph.mon) })} options={opts.mon} by={eff.mon} views={vMon} g={graph} {rel} {sel} {metrics} {wide} now={rowNow} filtered={!!qf} {sorun} onrender={onRender}>
          {#snippet head()}
            <div class="kb kb-mon"><span>{t('topology.head.name')}</span><span class="tur">{t('topology.head.type')}</span><span>{t('topology.head.clean')}</span><span>{t('topology.head.ms')}</span></div>
          {/snippet}
        </TopoColumn>
        <svg class="hat" bind:this={svg} aria-hidden="true"></svg>
      </div>
      <TopoDock n={node} g={graph} now={rowNow} admin={!!d?.admin} />
    </div>
    <div class="sayfa-pay" bind:this={pay}></div>
  {/if}
  <div class="sr" role="status" aria-live="polite">{announce}</div>
</div>
