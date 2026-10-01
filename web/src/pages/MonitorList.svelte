<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity';
  import { api, errorMessage, isServerIncident, type Incident, type Maintenance, type MonitorView } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { clock, confirmDialog, toast } from '../lib/ui.svelte';
  import { certDaysLeft, collator, fmtDate, fmtDuration, fmtDurationShort, fmtMs, fmtPct, fmtRelative, lower, monitorKind, uptimeTone } from '../lib/format';
  import { cloneMonitor, deleteMonitor, resetStats, togglePause } from '../lib/actions';
  import MonitorRow from '../components/MonitorRow.svelte';
  import MonitorMenu, { type MenuAction } from '../components/MonitorMenu.svelte';
  import QuickNotifyModal from '../components/QuickNotifyModal.svelte';
  import QuickMaintModal from '../components/QuickMaintModal.svelte';
  import QuickTagsModal from '../components/QuickTagsModal.svelte';
  import QuickPageModal from '../components/QuickPageModal.svelte';
  import BulkEditModal from '../components/BulkEditModal.svelte';
  import StatusIcon from '../components/StatusIcon.svelte';
  import Icon from '../components/Icon.svelte';
  import { i18n, t, tParts } from '../lib/i18n';
  import { onMount, tick } from 'svelte';

  type Filter = 'all' | 'down' | 'up' | 'maint' | 'paused' | 'partial';
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
      for (const tg of m.tags ?? []) {
        const e = seen.get(tg.id);
        if (e) e.count++;
        else seen.set(tg.id, { id: tg.id, name: tg.name, count: 1 });
      }
    return [...seen.values()].sort((a, b) => collator.compare(a.name, b.name));
  });
  // Seçili etiket artık hiçbir monitörde yoksa filtre kendiliğinden kalkar.
  const activeTag = $derived(tagOptions.some((tg) => String(tg.id) === tagFilter) ? tagFilter : '');
  let filter = $state<Filter>(load('uptime.filter', ['all', 'down', 'up', 'maint', 'paused', 'partial'] as const, 'all'));
  let sort = $state<Sort>(load('uptime.sort', ['status', 'name', 'uptime'] as const, 'status'));

  $effect(() => save('uptime.filter', filter));
  $effect(() => save('uptime.sort', sort));

  const counts = $derived.by(() => {
    let up = 0,
      down = 0,
      pending = 0,
      paused = 0,
      maint = 0,
      partial = 0;
    for (const m of live.monitors) {
      const k = monitorKind(m);
      if (locOutage(m)) partial++;
      if (k === 'paused') paused++;
      else if (k === 'maintenance') maint++;
      else if (k === 'up') up++;
      else if (k === 'down') down++;
      else pending++;
    }
    return { up, down, pending, paused, maint, partial, total: live.monitors.length };
  });

  /** Konum kesintisi: monitör genel olarak çalışıyor ama bir konum düşmüş
   *  (satırdaki sarı işaretle aynı koşul, bkz. MonitorRow). */
  function locOutage(m: MonitorView): boolean {
    const k = monitorKind(m);
    return !!m.open_partial_incident_id && k !== 'paused' && k !== 'down';
  }

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
      if (filter === 'partial' && !locOutage(m)) return false;
      if (q && !lower(m.name).includes(q) && !lower(m.target).includes(q) && !(m.tags ?? []).some((tg) => lower(`${tg.name} ${tg.value}`).includes(q)))
        return false;
      if (activeTag && !(m.tags ?? []).some((tg) => String(tg.id) === activeTag)) return false;
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

  // Yan panelin ek kartları (geniş ekran) ----------------------------------------------
  // SSL ve yanıt süresi listeden hesaplanır; olaylar ve bakımlar mevcut uçlardan
  // gelir. Kartlar yalnızca yan panel sütun olarak gösterildiğinde (>1180 px)
  // yüklenir; özet her yenilendiğinde (en geç 60 sn, durum değişince hemen) tazelenir.
  const sslSoon = $derived(
    live.monitors
      .filter((m) => m.type === 'http' && m.active && m.cert_expires_at > 0)
      .map((m) => ({ m, days: certDaysLeft(m.cert_expires_at, clock.now) ?? 0 }))
      .sort((a, b) => a.days - b.days || collator.compare(a.m.name, b.m.name))
      .slice(0, 5),
  );
  // Son 24 saatin ortalama yanıt süresi: saatlik kovaların ortalamaları başarılı
  // kontrol sayısıyla ağırlıklandırılır.
  const slowest = $derived.by(() => {
    const out: { m: MonitorView; avg: number }[] = [];
    for (const m of live.monitors) {
      if (!m.active || m.type === 'push' || m.type === 'group') continue;
      let sum = 0,
        n = 0;
      for (const b of m.bars ?? []) {
        if (b.ping < 0) continue;
        const w = Math.max(1, b.up);
        sum += b.ping * w;
        n += w;
      }
      if (n) out.push({ m, avg: sum / n });
    }
    return out.sort((a, b) => b.avg - a.avg).slice(0, 5);
  });
  const slowMax = $derived(slowest.length ? slowest[0].avg : 1);

  let incidents = $state<Incident[] | null>(null);
  let maints = $state<Maintenance[] | null>(null);
  const upcoming = $derived(
    (maints ?? [])
      .filter((mt) => mt.active && (mt.status === 'active' || (mt.status === 'scheduled' && mt.next_start > 0)))
      .sort((a, b) => (a.status === 'active' ? 0 : 1) - (b.status === 'active' ? 0 : 1) || a.next_start - b.next_start)
      .slice(0, 3),
  );
  const wideMq = window.matchMedia('(min-width: 1181px)');
  let sideWide = $state(wideMq.matches);
  $effect(() => {
    const f = () => (sideWide = wideMq.matches);
    wideMq.addEventListener('change', f);
    return () => wideMq.removeEventListener('change', f);
  });
  let sideAt = 0;
  let sideTimer: ReturnType<typeof setTimeout> | undefined;
  async function loadSide() {
    sideAt = Date.now();
    // Süren olaylar önce: en yeni 5 olay çözülmüş olaylarla dolarsa süren bir
    // kesinti listeden düşmesin.
    const [inc, open, mt] = await Promise.allSettled([api.incidents(0, 5), api.incidents(0, 5, '', true), api.maintenance()]);
    if (inc.status === 'fulfilled') {
      const ongoing = open.status === 'fulfilled' ? open.value : inc.value.filter((i) => !i.resolved_at);
      const seen = new Set(ongoing.map((i) => i.id));
      incidents = [...ongoing, ...inc.value.filter((i) => !seen.has(i.id))].slice(0, 5);
    }
    if (mt.status === 'fulfilled') maints = mt.value;
  }
  // Art arda gelen yenilemeler tek isteğe iner (en az 5 sn arayla).
  function loadSideSoon() {
    clearTimeout(sideTimer);
    sideTimer = setTimeout(loadSide, Math.max(0, 5000 - (Date.now() - sideAt)));
  }
  $effect(() => {
    void live.summary;
    if (sideWide && live.loaded) loadSideSoon();
  });
  onMount(() => {
    const offMaint = live.onMaintenance(loadSideSoon);
    return () => {
      offMaint();
      clearTimeout(sideTimer);
    };
  });

  // Telefonda özet kartları yerine tek satır durum çipleri (aynı zamanda filtre).
  const chips = $derived(
    [
      { v: 'all' as Filter, l: t('common.all'), n: counts.total, c: '' },
      { v: 'down' as Filter, l: t('status.down'), n: counts.down, c: 'down' },
      { v: 'up' as Filter, l: t('status.up'), n: counts.up, c: 'up' },
      { v: 'partial' as Filter, l: t('incidents.kind.partial'), n: counts.partial, c: 'partial' },
      { v: 'maint' as Filter, l: t('status.maintenance'), n: counts.maint, c: 'maint' },
      { v: 'paused' as Filter, l: t('status.paused'), n: counts.paused, c: 'paused' },
    ].filter((c) => c.v === 'all' || c.v === 'down' || c.v === 'up' || c.n > 0 || filter === c.v),
  );

  // Çip satırı telefonda yatay kayar: kenarlarda kalan içerik solan kenarla belli edilir.
  let chipsEl = $state<HTMLElement>();
  let chipFade = $state({ l: false, r: false });
  function updateChipFade() {
    const el = chipsEl;
    if (!el) return;
    const max = el.scrollWidth - el.clientWidth;
    const l = el.scrollLeft > 2;
    const r = el.scrollLeft < max - 2;
    if (l !== chipFade.l || r !== chipFade.r) chipFade = { l, r };
  }
  $effect(() => {
    if (!chipsEl) return;
    const ro = new ResizeObserver(updateChipFade);
    ro.observe(chipsEl);
    return () => ro.disconnect();
  });
  $effect(() => {
    // Çip metinleri (sayılar, dil) değişince taşma yeniden ölçülür.
    void [chips, live.summary?.uptime_24h, live.summary?.incidents_24h, counts.pending, i18n.locale];
    tick().then(updateChipFade);
  });

  // Dar ekranda arama kutusu kısa yer tutucu gösterir (sıralama seçicisiyle aynı satırda).
  const narrowMq = window.matchMedia('(max-width: 640px)');
  let isNarrow = $state(narrowMq.matches);
  $effect(() => {
    const f = () => (isNarrow = narrowMq.matches);
    narrowMq.addEventListener('change', f);
    return () => narrowMq.removeEventListener('change', f);
  });

  // Satır menüsü ---------------------------------------------------------------------
  // Tek menü, listenin dışında çizilir (yüzlerce satırda her satıra menü düşmesin).
  let menu = $state<{ id: number; anchor: HTMLElement; top: number; selectable: boolean } | null>(null);
  const menuMonitor = $derived(menu ? live.byId(menu.id) : undefined);
  const narrow = () => window.matchMedia('(max-width: 640px)').matches;

  function openMenu(m: MonitorView | null, anchor?: HTMLElement) {
    menu =
      m && anchor
        ? { id: m.id, anchor, top: anchor.getBoundingClientRect().top, selectable: narrow() || window.matchMedia('(hover: none)').matches }
        : null;
  }
  // Sayfa veya liste kaydırılıp düğme yerinden oynarsa menü kapanır (sabit konumlu
  // menü düğmeden kopmasın). Açılıştan hemen sonra gelen gecikmeli kaydırma olayı
  // düğmeyi oynatmadığı için menüyü kapatmaz. Dar ekranda alttan açılan sayfa kalır.
  function onAnyScroll() {
    if (!menu || narrow()) return;
    if (Math.abs(menu.anchor.getBoundingClientRect().top - menu.top) > 2) closeMenu();
  }
  function closeMenu(refocus = false) {
    const a = menu?.anchor;
    menu = null;
    if (refocus) a?.focus();
  }

  type DialogKind = 'notify' | 'maint' | 'tags' | 'page';
  let dialog = $state<{ kind: DialogKind; m: MonitorView; key: number } | null>(null);
  let dialogOpen = $state(false);
  let dialogSeq = 0;
  function openDialog(kind: DialogKind, m: MonitorView) {
    dialog = { kind, m, key: ++dialogSeq };
    dialogOpen = true;
  }

  function onMenuAction(a: MenuAction) {
    const m = menuMonitor;
    closeMenu();
    if (!m) return;
    switch (a) {
      case 'select':
        selectMode = true;
        if (!selected.has(m.id)) toggleSelect(m, false);
        break;
      case 'notify':
      case 'maint':
      case 'tags':
      case 'page':
        openDialog(a, m);
        break;
      case 'clone':
        cloneMonitor(m);
        break;
      case 'pause':
        togglePause(m);
        break;
      case 'reset':
        resetStats(m);
        break;
      case 'delete':
        deleteMonitor(m);
        break;
    }
  }

  // Çoklu seçim ----------------------------------------------------------------------
  // Toplu işlemler yalnızca şu an görünen (filtreye uyan) seçili monitörlere uygulanır;
  // filtre değişince gizlenen seçimler sayılmaz.
  const selected = new SvelteSet<number>();
  let selectMode = $state(false); // dokunmatikte seçim modu (hiç seçim yokken de)
  let lastPicked: number | null = null;
  const selIds = $derived(visible.filter((m) => selected.has(m.id)).map((m) => m.id));
  const allSelected = $derived(visible.length > 0 && selIds.length === visible.length);
  const selecting = $derived(selIds.length > 0 || selectMode);

  function toggleSelect(m: MonitorView, range: boolean) {
    if (range && lastPicked !== null && lastPicked !== m.id) {
      const ids = visible.map((x) => x.id);
      const a = ids.indexOf(lastPicked);
      const b = ids.indexOf(m.id);
      if (a >= 0 && b >= 0) {
        const on = !selected.has(m.id);
        for (const id of ids.slice(Math.min(a, b), Math.max(a, b) + 1)) {
          if (on) selected.add(id);
          else selected.delete(id);
        }
        lastPicked = m.id;
        return;
      }
    }
    if (selected.has(m.id)) selected.delete(m.id);
    else selected.add(m.id);
    lastPicked = m.id;
  }

  function toggleAll() {
    if (allSelected) clearSelection();
    else for (const m of visible) selected.add(m.id);
  }

  function clearSelection() {
    selected.clear();
    selectMode = false;
    lastPicked = null;
  }

  let bulkBusy = $state(false);
  let bulkKind = $state<'tag' | 'notify' | null>(null);
  let bulkOpen = $state(false);
  let bulkIds = $state<number[]>([]);
  let bulkSeq = $state(0);
  function openBulk(kind: 'tag' | 'notify') {
    bulkIds = selIds;
    bulkKind = kind;
    bulkSeq++;
    bulkOpen = true;
  }

  async function bulkToggle(action: 'pause' | 'resume') {
    const ids = selIds;
    if (!ids.length) return;
    bulkBusy = true;
    try {
      const res = await api.bulkMonitors(ids, { action });
      live.upsertMany(res.monitors);
      if (res.changed)
        toast.success(t(action === 'pause' ? 'monitors.list.bulkPaused' : 'monitors.list.bulkResumed', { count: res.changed }));
      else toast.info(t(action === 'pause' ? 'monitors.list.alreadyPaused' : 'monitors.list.alreadyRunning'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkDelete() {
    const ids = selIds;
    if (!ids.length) return;
    const one = ids.length === 1 ? live.byId(ids[0]) : undefined;
    const ok = await confirmDialog({
      title: ids.length === 1 ? t('monitors.actions.deleteTitle') : t('monitors.list.bulkDeleteTitle', { count: ids.length }),
      message: one
        ? t('monitors.actions.deleteMessage', { name: one.name })
        : t('monitors.list.bulkDeleteMessage', { count: ids.length }),
      confirmText: ids.length === 1 ? t('common.delete') : t('monitors.list.bulkDeleteTitle', { count: ids.length }),
      danger: true,
    });
    if (!ok) return;
    bulkBusy = true;
    try {
      const res = await api.bulkMonitors(ids, { action: 'delete' });
      live.removeMany(res.deleted);
      clearSelection();
      toast.success(
        res.deleted.length === 1 ? t('monitors.list.deletedOne') : t('monitors.list.deletedMany', { count: res.deleted.length }),
      );
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      bulkBusy = false;
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape' || e.defaultPrevented) return;
    if (menu) return closeMenu(true);
    // Açık bir pencere (onay, düzenleme) varsa Escape onu kapatır; seçim kalır.
    if (document.querySelector('dialog[open]')) return;
    if (selecting) clearSelection();
  }

  // Masaüstünde liste kendi içinde kayar: başlık ve araç çubuğu yerinde kalır,
  // kutu ekranın altına kadar uzanır. Dar ekranda sayfa normal kayar.
  let toolbarEl = $state<HTMLElement>();
  let listEl = $state<HTMLElement>();
  let sideEl = $state<HTMLElement>();
  let listMax = $state(0);
  let sideMax = $state(0);
  // Yan panel kendi içinde kayarken altta/üstte içerik olduğunu solan kenarla belli et.
  let sideMore = $state({ above: false, below: false });
  function updateSideMore() {
    const el = sideEl;
    if (!el || !sideMax) {
      if (sideMore.above || sideMore.below) sideMore = { above: false, below: false };
      return;
    }
    const above = el.scrollTop > 2;
    const below = el.scrollTop + el.clientHeight < el.scrollHeight - 2;
    if (above !== sideMore.above || below !== sideMore.below) sideMore = { above, below };
  }
  $effect(() => {
    if (!sideEl) return;
    const ro = new ResizeObserver(updateSideMore);
    ro.observe(sideEl);
    for (const c of sideEl.children) ro.observe(c);
    return () => ro.disconnect();
  });
  $effect(() => {
    void [sideMax, incidents, maints];
    tick().then(updateSideMore);
  });
  function fitList() {
    if (!listEl || !window.matchMedia('(min-width: 901px)').matches) {
      listMax = sideMax = 0;
      return;
    }
    const top = listEl.getBoundingClientRect().top + window.scrollY;
    listMax = Math.max(320, Math.floor(window.innerHeight - top - 48));
    // Yan panel sütun olarak yanda duruyorsa (geniş ekran) o da ekranın altında
    // biter ve gerekirse kendi içinde kayar: panel ekrandan uzun olunca sayfa da
    // kayıyor, liste ile sayfa iki ayrı kaydırma oluyordu.
    if (sideEl && wideMq.matches) {
      const sTop = sideEl.getBoundingClientRect().top + window.scrollY;
      sideMax = Math.max(320, Math.floor(window.innerHeight - sTop - 48));
    } else sideMax = 0;
  }
  $effect(() => {
    if (!listEl || !toolbarEl) return;
    void sideEl;
    fitList();
    // Araç çubuğu satır atlarsa listenin başladığı yer değişir.
    const ro = new ResizeObserver(fitList);
    ro.observe(toolbarEl);
    return () => ro.disconnect();
  });
</script>

<svelte:window onresize={fitList} onscroll={onAnyScroll} />

<svelte:document onclick={() => menu && closeMenu()} onkeydown={onKeydown} />

<div class="page-head">
  <h1>{t('nav.monitors')}<span class="dot">.</span></h1>
  {#if session.canEdit}
    <a class="btn primary" href="#/monitors/new"><Icon name="plus" size={16} /> {t('monitors.list.new')}</a>
  {/if}
</div>

{#if !live.loaded}
  {#if live.loadError}
    <div class="card empty">
      <h3>{t('monitors.list.loadFailed')}</h3>
      <p>{live.loadError}</p>
      <button class="btn primary" onclick={() => live.refresh()}>{t('common.retry')}</button>
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
      <h3>{t('monitors.list.firstTitle')}</h3>
      <p>{t('monitors.list.firstText')}</p>
      <a class="btn primary" href="#/monitors/new"><Icon name="plus" size={16} /> {t('monitors.list.addMonitor')}</a>
      {#if session.isAdmin}
        <a class="migrate" href="#/settings/backup?tasi=1">
          <span class="mig-ic"><Icon name="log-in" size={18} /></span>
          <span class="mig-t">
            <b>{t('monitors.list.migrateTitle')}</b>
            <span>{t('monitors.list.migrateText')}</span>
          </span>
          <Icon name="chevron-right" size={16} />
        </a>
      {/if}
    {:else}
      <h3>{t('monitors.list.noneTitle')}</h3>
      <p>{t('monitors.list.noneText')}</p>
    {/if}
  </div>
{:else}
  <div class="layout">
    <section class="main-col">
      <div
        class="chips"
        class:fade-l={chipFade.l}
        class:fade-r={chipFade.r}
        role="radiogroup"
        aria-label={t('monitors.list.filterByStatus')}
        bind:this={chipsEl}
        onscroll={updateChipFade}
      >
        {#each chips as c (c.v)}
          <button
            type="button"
            role="radio"
            aria-checked={filter === c.v}
            class="chip {c.c}"
            class:on={filter === c.v}
            onclick={() => (filter = filter === c.v ? 'all' : c.v)}
          >
            {#if c.c}<i class="cdot" aria-hidden="true"></i>{/if}<b>{c.n}</b>
            {c.l}
          </button>
        {/each}
        <a
          class="chip info"
          href="#/incidents"
          aria-label={t('monitors.list.chip24Aria', { pct: fmtPct(summary?.uptime_24h), count: summary?.incidents_24h ?? 0 })}
        >
          {t('monitors.list.chip24')} <b class={({ good: 'c-up', warn: 'c-warn', bad: 'c-down', none: 'c-up' })[uptimeTone(summary?.uptime_24h)]}>{fmtPct(summary?.uptime_24h)}</b>
          {#if summary?.incidents_24h}· {t('monitors.list.incidentCount', { count: summary.incidents_24h })}{/if}
        </a>
        {#if counts.pending > 0}<span class="chip info c-pending">{t('monitors.list.pendingCount', { count: counts.pending })}</span>{/if}
      </div>
      <div class="toolbar" bind:this={toolbarEl}>
        {#if session.canEdit}
          <label class="selbox check" class:on={selIds.length > 0} title={t('monitors.list.selectAllTitle')}>
            <input
              type="checkbox"
              checked={allSelected}
              indeterminate={selIds.length > 0 && !allSelected}
              disabled={visible.length === 0}
              onchange={toggleAll}
              aria-label={t('monitors.list.selectAllAria')}
            />
            <span class="cnt" style="--d:{String(visible.length).length}"><b>{selIds.length}</b> / {visible.length}</span>
          </label>
        {/if}
        <div class="search">
          <span class="s-ic"><Icon name="search" size={16} /></span>
          <input
            class="input"
            type="search"
            placeholder={isNarrow ? t('monitors.list.searchPhShort') : t('monitors.list.searchPh')}
            bind:value={search}
            aria-label={t('monitors.list.searchPh')}
          />
          {#if search}
            <button type="button" class="clear" aria-label={t('monitors.list.clearSearch')} onclick={() => (search = '')}><Icon name="x" size={14} /></button>
          {/if}
        </div>
        <!-- Dar liste sütununda seçiciler ikinci satıra iner (bkz. @container mcol). -->
        <span class="tb-break" aria-hidden="true"></span>
        <select class="input sel fsel" bind:value={filter} aria-label={t('monitors.list.filter')}>
          <option value="all">{t('monitors.list.fAll', { n: counts.total })}</option>
          <option value="down">{t('monitors.list.fDown', { n: counts.down })}</option>
          <option value="up">{t('monitors.list.fUp', { n: counts.up })}</option>
          {#if counts.partial > 0 || filter === 'partial'}<option value="partial">{t('monitors.list.fPartial', { n: counts.partial })}</option>{/if}
          {#if counts.maint > 0 || filter === 'maint'}<option value="maint">{t('monitors.list.fMaint', { n: counts.maint })}</option>{/if}
          <option value="paused">{t('monitors.list.fPaused', { n: counts.paused })}</option>
        </select>
        <select class="input sel ssel" bind:value={sort} aria-label={t('monitors.list.sort')}>
          <option value="status">{t('monitors.list.sStatus')}</option>
          <option value="name">{t('monitors.list.sName')}</option>
          <option value="uptime">{t('monitors.list.sUptime')}</option>
        </select>
        {#if tagOptions.length}
          <select class="input sel tsel" bind:value={tagFilter} aria-label={t('monitors.list.tagFilter')} class:on={!!activeTag}>
            <option value="">{t('monitors.list.allTags')}</option>
            {#each tagOptions as tg (tg.id)}<option value={String(tg.id)}>{tg.name} ({tg.count})</option>{/each}
          </select>
        {/if}
      </div>

      <div
        class="card list"
        class:scroll={listMax > 0}
        style:max-height={listMax ? `${listMax}px` : null}
        bind:this={listEl}
        onscroll={onAnyScroll}
      >
        {#if visible.length}
          <div class="mrow mhead" aria-hidden="true">
            {#if session.canEdit}<span class="mc-sel"></span>{/if}
            <span class="mc-ic"></span>
            <span class="mc-info">{t('overview.cols.monitor')}</span>
            <span class="mc-locs">{t('overview.cols.locations')}</span>
            <span class="mc-resp r">{t('overview.cols.response')}</span>
            <span class="mc-ssl r">{t('overview.cols.ssl')}</span>
            <span class="mc-u7 r">{t('overview.cols.u7')}</span>
            <span class="mc-u30 r">{t('overview.cols.u30')}</span>
            <span class="mc-last r">{t('overview.cols.lastCheck')}</span>
            <span class="mc-int r">{t('overview.cols.interval')}</span>
            <span class="mc-bars">{t('overview.cols.last24')}</span>
            {#if session.canEdit}<span class="mc-menu"></span>{/if}
          </div>
        {/if}
        {#each visible as m (m.id)}
          <MonitorRow
            {m}
            now={clock.now}
            menuOpen={menu?.id === m.id}
            selected={selected.has(m.id)}
            {selecting}
            onmenu={openMenu}
            onselect={session.canEdit ? toggleSelect : undefined}
          />
        {:else}
          <div class="noresult">
            {t('monitors.noMatch')}
            {#if search || filter !== 'all' || activeTag}
              <button
                class="linkbtn"
                onclick={() => {
                  search = '';
                  filter = 'all';
                  tagFilter = '';
                }}>{t('monitors.list.clearFilter')}</button
              >
            {/if}
          </div>
        {/each}
        {#if session.canEdit && selecting}
          <div class="bulkbar" role="toolbar" aria-label={t('monitors.list.bulkAria')}>
            <button
              type="button"
              class="btn ghost icon sm"
              aria-label={t('monitors.list.clearSelection')}
              title={t('monitors.list.clearSelectionTitle')}
              onclick={clearSelection}
            >
              <Icon name="x" size={16} />
            </button>
            <span class="bcount"
              >{#each tParts('monitors.list.selected') as p, i (i)}{#if p.slot === 'n'}<b>{selIds.length}</b>{:else}{p.text}{/if}{/each}</span
            >
            <div class="bactions">
              <button
                type="button"
                class="btn sm"
                disabled={!selIds.length || bulkBusy}
                onclick={() => bulkToggle('pause')}
                title={t('monitors.pause')}
              >
                <Icon name="pause" size={14} /><span class="bl">{t('monitors.pause')}</span>
              </button>
              <button
                type="button"
                class="btn sm"
                disabled={!selIds.length || bulkBusy}
                onclick={() => bulkToggle('resume')}
                title={t('monitors.resume')}
              >
                <Icon name="play" size={14} /><span class="bl">{t('monitors.resume')}</span>
              </button>
              <button
                type="button"
                class="btn sm"
                disabled={!selIds.length || bulkBusy}
                onclick={() => openBulk('tag')}
                title={t('monitors.list.bulkTagTitle')}
              >
                <Icon name="tag" size={14} /><span class="bl">{t('monitors.list.bulkTag')}</span>
              </button>
              <button
                type="button"
                class="btn sm"
                disabled={!selIds.length || bulkBusy}
                onclick={() => openBulk('notify')}
                title={t('monitors.list.bulkNotifyTitle')}
              >
                <Icon name="bell" size={14} /><span class="bl">{t('monitors.list.bulkNotify')}</span>
              </button>
              <span class="bsep" aria-hidden="true"></span>
              <button
                type="button"
                class="btn sm danger"
                disabled={!selIds.length || bulkBusy}
                onclick={bulkDelete}
                title={t('common.delete')}
              >
                {#if bulkBusy}<span class="spinner"></span>{:else}<Icon name="trash" size={14} />{/if}<span class="bl">{t('common.delete')}</span>
              </button>
            </div>
          </div>
        {/if}
      </div>
    </section>

    <aside
      class="side"
      class:scroll={sideMax > 0}
      class:more-below={sideMore.below}
      class:more-above={sideMore.above}
      style:max-height={sideMax ? `${sideMax}px` : null}
      bind:this={sideEl}
      onscroll={updateSideMore}
    >
      <div class="card status-card">
        <h2 class="card-title">{t('monitors.list.currentStatus')}<span class="dot">.</span></h2>
        <div class="big">
          <StatusIcon
            kind={counts.down > 0 ? 'down' : counts.up + counts.pending > 0 ? 'up' : counts.maint > 0 ? 'maintenance' : 'paused'}
            size={52}
            pulse
          />
          <div>
            <div class="big-label {counts.down > 0 ? 'c-down' : counts.up + counts.pending === 0 && counts.maint > 0 ? 'c-maint' : 'c-up'}">
              {counts.down > 0
                ? t('monitors.list.downCount', { count: counts.down })
                : counts.up + counts.pending === 0 && counts.maint > 0
                  ? t('monitors.list.maintOngoing')
                  : t('monitors.list.allGood')}
            </div>
            <div class="muted small">{t('monitors.list.monitoring', { count: counts.total })}</div>
          </div>
        </div>
        <div class="counts" class:four={counts.maint > 0}>
          <div><b class="c-down">{counts.down}</b><span>{t('monitors.list.cDown')}</span></div>
          <div><b class="c-up">{counts.up}</b><span>{t('monitors.list.cUp')}</span></div>
          {#if counts.maint > 0}
            <button type="button" class="cnt-btn" onclick={() => (filter = 'maint')} title={t('monitors.list.showMaint')}>
              <b class="c-maint">{counts.maint}</b><span>{t('monitors.list.cMaint')}</span>
            </button>
          {/if}
          <div><b class="c-paused">{counts.paused}</b><span>{t('monitors.list.cPaused')}</span></div>
        </div>
        {#if counts.pending > 0}
          <div class="pending-note c-pending small">{t('monitors.list.pendingNote', { count: counts.pending })}</div>
        {/if}
      </div>

      <div class="card">
        <h2 class="card-title">{t('monitors.list.last24')}<span class="dot">.</span></h2>
        <div class="counts three">
          <div>
            <b class={({ good: 'c-up', warn: 'c-warn', bad: 'c-down', none: 'c-up' })[uptimeTone(summary?.uptime_24h)]}>{fmtPct(summary?.uptime_24h)}</b>
            <span>{t('monitors.list.overallUptime')}</span>
          </div>
          <div><b>{summary?.incidents_24h ?? '—'}</b><span>{t('monitors.list.incidents')}</span></div>
          <div><b class={counts.down > 0 ? 'c-down' : ''}>{counts.down}</b><span>{t('monitors.list.ongoing')}</span></div>
        </div>
        <a class="more" href="#/incidents">{t('monitors.list.allIncidents')} <Icon name="chevron-right" size={14} /></a>
      </div>

      {#if incidents}
        <div class="card extra">
          <h2 class="card-title">{t('overview.side.incidents')}<span class="dot">.</span></h2>
          {#if incidents.length}
            <ul class="mini">
              {#each incidents as inc (inc.id)}
                {@const partial = inc.kind === 'partial'}
                {@const srv = isServerIncident(inc.kind)}
                <li>
                  <span class="mdot" class:down={!inc.resolved_at && !partial} class:pend={!inc.resolved_at && partial} aria-hidden="true"></span>
                  <a class="mname" href="#/incidents/{inc.id}">{srv ? inc.server_name : inc.monitor_name}</a>
                  <span class="mval" class:c-down={!inc.resolved_at && !partial} class:c-pending={!inc.resolved_at && partial}>
                    {inc.resolved_at ? t('overview.side.lasted', { d: fmtDuration(inc.resolved_at - inc.started_at) }) : t('overview.side.ongoingFor', { d: fmtDuration(clock.now - inc.started_at) })}
                  </span>
                  <span class="msub" title={fmtDate(inc.started_at)}>
                    {#if partial}<span class="badge pending mk">{t('incidents.kind.partial')}</span>{:else if srv}<span class="badge accent mk">{t(inc.kind === 'probe_offline' ? 'incidents.kind.probe' : 'incidents.kind.server')}</span>{/if}
                    {fmtRelative(inc.started_at, clock.now)}{#if inc.cause}{' · '}{inc.cause}{/if}
                  </span>
                </li>
              {/each}
            </ul>
          {:else}
            <p class="quiet">{t('overview.side.incidentsEmpty')}</p>
          {/if}
        </div>
      {/if}

      {#if sslSoon.length}
        <div class="card extra">
          <h2 class="card-title">{t('overview.side.ssl')}<span class="dot">.</span></h2>
          <ul class="mini">
            {#each sslSoon as x (x.m.id)}
              <li>
                <Icon name="lock" size={13} />
                <a class="mname" href="#/monitors/{x.m.id}">{x.m.name}</a>
                <span class="mval {x.days < 7 ? 'c-down' : x.days < 14 ? 'c-pending' : ''}" title={fmtDate(x.m.cert_expires_at)}>
                  {x.days < 0 ? t('overview.side.expired') : t('overview.side.days', { n: x.days, count: x.days })}
                </span>
              </li>
            {/each}
          </ul>
        </div>
      {/if}

      {#if upcoming.length}
        <div class="card extra">
          <h2 class="card-title">{t('overview.side.maint')}<span class="dot">.</span></h2>
          <ul class="mini">
            {#each upcoming as mt (mt.id)}
              <li>
                <Icon name="wrench" size={13} />
                <a class="mname" href={session.canEdit ? `#/maintenance/${mt.id}` : '#/maintenance'}>{mt.title}</a>
                <span class="mval" class:c-maint={mt.status === 'active'}>
                  {mt.status === 'active' ? t('overview.side.maintNow') : t('overview.side.maintIn', { d: fmtDurationShort(mt.next_start - clock.now) })}
                </span>
                {#if mt.status !== 'active'}<span class="msub">{fmtDate(mt.next_start)}</span>{/if}
              </li>
            {/each}
          </ul>
          <a class="more" href="#/maintenance">{t('overview.side.allMaint')} <Icon name="chevron-right" size={14} /></a>
        </div>
      {/if}

      {#if slowest.length}
        <div class="card extra">
          <h2 class="card-title" title={t('overview.side.avgTitle')}>{t('overview.side.slow')}<span class="dot">.</span></h2>
          <ul class="mini slow">
            {#each slowest as x (x.m.id)}
              <li>
                <a class="mname" href="#/monitors/{x.m.id}">{x.m.name}</a>
                <span class="mval">{fmtMs(x.avg)}</span>
                <span class="sbar" aria-hidden="true"><i style="width:{Math.max(4, (100 * x.avg) / slowMax)}%"></i></span>
              </li>
            {/each}
          </ul>
        </div>
      {/if}
    </aside>
  </div>
{/if}

{#if menu && menuMonitor}
  {#key menu.id}
    <MonitorMenu m={menuMonitor} anchor={menu.anchor} selectable={menu.selectable} onclose={closeMenu} onaction={onMenuAction} />
  {/key}
{/if}

{#if dialog}
  {#key dialog.key}
    {#if dialog.kind === 'notify'}
      <QuickNotifyModal bind:open={dialogOpen} m={dialog.m} />
    {:else if dialog.kind === 'maint'}
      <QuickMaintModal bind:open={dialogOpen} m={dialog.m} />
    {:else if dialog.kind === 'tags'}
      <QuickTagsModal bind:open={dialogOpen} m={dialog.m} />
    {:else}
      <QuickPageModal bind:open={dialogOpen} m={dialog.m} />
    {/if}
  {/key}
{/if}

{#if bulkKind}
  {#key bulkSeq}
    <BulkEditModal bind:open={bulkOpen} kind={bulkKind} ids={bulkIds} />
  {/key}
{/if}

<style>
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr) clamp(300px, 17vw, 340px);
    gap: 24px;
    align-items: start;
  }
  /* Sütun başlığı (satırlarla aynı ızgara: components/monitor-grid.css). */
  .mhead {
    position: sticky;
    top: 0;
    z-index: 2;
    padding-top: 9px;
    padding-bottom: 9px;
    background: var(--card);
    border-bottom: 1px solid var(--border);
    border-top-left-radius: var(--radius);
    border-top-right-radius: var(--radius);
    font-size: 0.7rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--muted);
    white-space: nowrap;
  }
  .mhead > span {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .mhead .r {
    text-align: right;
  }
  /* Yan panelin küçük listeleri */
  .mini {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .mini li {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    column-gap: 8px;
    row-gap: 1px;
    font-size: 0.86rem;
    min-width: 0;
  }
  .mini li > :global(svg) {
    color: var(--muted);
  }
  .mini.slow li {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .mname {
    color: var(--text);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .mval {
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    color: var(--text-2);
    font-size: 0.82rem;
  }
  .msub {
    grid-column: 2 / -1;
    font-size: 0.76rem;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mdot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--paused);
  }
  .mdot.down {
    background: var(--down);
    box-shadow: 0 0 0 3px var(--down-soft);
  }
  .mdot.pend {
    background: var(--pending);
    box-shadow: 0 0 0 3px var(--pending-soft);
  }
  .mk {
    height: 16px;
    padding: 0 5px;
    font-size: 0.62rem;
    margin-right: 4px;
  }
  .sbar {
    grid-column: 1 / -1;
    height: 4px;
    border-radius: 2px;
    background: var(--meter-track);
    overflow: hidden;
    margin-top: 3px;
  }
  .sbar i {
    display: block;
    height: 100%;
    border-radius: 2px;
    background: var(--accent);
    opacity: 0.8;
  }
  .quiet {
    margin: 0;
    font-size: 0.85rem;
    color: var(--muted);
  }
  .main-col {
    min-width: 0;
    container: mcol / inline-size;
  }
  .tb-break {
    display: none;
  }
  /* Dar liste sütununda (ör. 1280 px ekran, yan panel açık) araç çubuğu iki
     düzenli satır: seçim kutusu + arama tam genişlikte (yer tutucu kesilmez,
     sayaç görünür), altında seçiciler eşit genişlikte. */
  @container mcol (max-width: 780px) {
    .toolbar {
      gap: 8px;
    }
    .toolbar .tb-break {
      display: block;
      flex-basis: 100%;
      height: 0;
    }
    .toolbar .sel {
      flex: 1 1 0;
      min-width: 0;
    }
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
  /* "☐ 0 / 12": görünen tüm monitörleri seçer. */
  .selbox {
    flex: 0 0 auto;
    align-items: center;
    gap: 9px;
    height: 40px;
    padding: 0 12px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    font-size: 0.88rem;
    color: var(--muted);
    white-space: nowrap;
    user-select: none;
    transition: border-color 0.15s;
  }
  .selbox input {
    margin: 0;
  }
  /* Sayı değişince genişlik oynamasın (arama kutusu kaymasın). */
  .selbox .cnt {
    font-variant-numeric: tabular-nums;
    min-width: calc(var(--d, 2) * 2ch + 1.4em);
  }
  .selbox .cnt b {
    color: var(--text-2);
    font-weight: 600;
  }
  .selbox.on {
    border-color: var(--accent-border);
  }
  .selbox.on .cnt b {
    color: var(--accent-text);
  }
  @media (hover: hover) {
    .selbox:hover {
      border-color: var(--border-hover);
    }
  }
  .selbox input:indeterminate {
    background: var(--accent);
    border-color: var(--accent);
  }
  .selbox input:indeterminate::after {
    content: '';
    position: absolute;
    left: 3.5px;
    top: 6.5px;
    width: 8px;
    height: 2px;
    border-radius: 1px;
    background: var(--accent-contrast);
  }
  .selbox input:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* Toplu işlem çubuğu: listenin altında, görünür alanın dibine yapışır. */
  .bulkbar {
    position: sticky;
    bottom: 12px;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 10px 12px 12px;
    padding: 7px 8px 7px 6px;
    background: var(--bg-elev);
    border: 1px solid var(--accent-border);
    border-radius: 12px;
    box-shadow: var(--shadow);
    animation: bar-in 0.16s ease-out;
  }
  .bcount {
    font-size: 0.9rem;
    color: var(--text-2);
    white-space: nowrap;
    margin-right: auto;
  }
  .bcount b {
    color: var(--text);
  }
  .bactions {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .bactions .btn {
    gap: 6px;
  }
  .bsep {
    width: 1px;
    height: 20px;
    background: var(--border-strong);
    margin: 0 2px;
  }
  @keyframes bar-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
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
  /* Temizle düğmesi yalnızca yazı varken: boşken yer tutucuya yer kalsın. */
  .search .input:placeholder-shown {
    padding-right: 10px;
    text-overflow: ellipsis;
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
  /* Satır düzeni liste genişliğine göre sıkışır (MonitorRow'daki @container mlist). */
  .list {
    padding: 0;
    container: mlist / inline-size;
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
  .side.scroll {
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
    /* Kartların gölgesi/kenarı kırpılmasın. */
    padding: 1px;
    margin: -1px;
  }
  .side > :global(*) {
    flex-shrink: 0;
  }
  .side.more-below {
    -webkit-mask-image: linear-gradient(to bottom, #000 calc(100% - 56px), transparent);
    mask-image: linear-gradient(to bottom, #000 calc(100% - 56px), transparent);
  }
  .side.more-above {
    -webkit-mask-image: linear-gradient(to bottom, transparent, #000 40px);
    mask-image: linear-gradient(to bottom, transparent, #000 40px);
  }
  .side.more-above.more-below {
    -webkit-mask-image: linear-gradient(to bottom, transparent, #000 40px, #000 calc(100% - 56px), transparent);
    mask-image: linear-gradient(to bottom, transparent, #000 40px, #000 calc(100% - 56px), transparent);
  }
  /* Kaydırma çubuğu da görünsün (ince). */
  .side.scroll::-webkit-scrollbar {
    width: 6px;
  }
  .side.scroll::-webkit-scrollbar-thumb {
    background: var(--border-strong);
    border-radius: 3px;
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

  /* Durum çipleri yalnızca telefonda (özet kartlarının yerine). */
  .chips {
    display: none;
  }
  .chip {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: 0 0 auto;
    height: 38px;
    padding: 0 12px;
    border-radius: 999px;
    border: 1px solid var(--border-strong);
    background: var(--card);
    color: var(--text-2);
    font: inherit;
    font-size: 0.86rem;
    font-weight: 600;
    white-space: nowrap;
    cursor: pointer;
    text-decoration: none;
  }
  /* 44 px dokunma alanı (çipler arası boşluğa taşmadan dikeyde). */
  .chip::after {
    content: '';
    position: absolute;
    inset: -3px 0;
  }
  .chip b {
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  /* Uptime tonu (genel .c-* sınıfları .chip b'ye yenilmesin). */
  .chip b.c-up {
    color: var(--up);
  }
  .chip b.c-warn {
    color: var(--warn-text);
  }
  .chip b.c-down {
    color: var(--down-text-2);
  }
  .chip.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--accent-text-soft);
  }
  .chip.info {
    border-style: dashed;
    cursor: default;
    font-weight: 500;
  }
  a.chip.info {
    cursor: pointer;
  }
  .chip.info b {
    font-weight: 700;
  }
  .chip.down b {
    color: var(--down-text-2);
  }
  .cdot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--paused);
  }
  .chip.down .cdot {
    background: var(--down);
  }
  .chip.up .cdot {
    background: var(--up);
  }
  .chip.maint .cdot {
    background: var(--maint);
  }
  .chip.partial .cdot {
    background: var(--pending);
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
    /* Panel listenin üstüne indiğinde yalnızca özet kartları kalır. */
    .side .extra {
      display: none;
    }
    .side {
      order: -1;
      position: static;
      display: grid;
      grid-template-columns: 1fr 1fr;
    }
  }
  /* Sekme çubuğu olan ekranlarda çubuk onun üstünde durur. */
  @media (max-width: 900px) {
    .bulkbar {
      bottom: calc(66px + env(safe-area-inset-bottom));
    }
  }
  @media (max-width: 640px) {
    .bulkbar {
      margin: 8px 6px 10px;
      gap: 4px;
    }
    .bactions {
      gap: 4px;
    }
    .bactions .btn {
      width: 38px;
      height: 38px;
      padding: 0;
    }
    .bl {
      display: none;
    }
    .selbox {
      padding: 0 10px;
    }
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

    /* Telefonda ilk monitör ilk ekranda görünsün: özet kartları yerine tek satır
       çip, filtre açılır listesi yerine çipler; araç çubuğu iki sıkı satır. */
    .side {
      display: none;
    }
    .chips {
      display: flex;
      gap: 6px;
      overflow-x: auto;
      scrollbar-width: none;
      margin: -4px -16px 10px;
      padding: 3px 16px;
    }
    .chips::-webkit-scrollbar {
      display: none;
    }
    /* Kaydırılabilir içerik kenarda solarak belli olur (kesik çip yerine). */
    .chips.fade-r {
      -webkit-mask-image: linear-gradient(to right, #000 calc(100% - 44px), transparent);
      mask-image: linear-gradient(to right, #000 calc(100% - 44px), transparent);
    }
    .chips.fade-l {
      -webkit-mask-image: linear-gradient(to right, transparent, #000 44px);
      mask-image: linear-gradient(to right, transparent, #000 44px);
    }
    .chips.fade-l.fade-r {
      -webkit-mask-image: linear-gradient(to right, transparent, #000 44px, #000 calc(100% - 44px), transparent);
      mask-image: linear-gradient(to right, transparent, #000 44px, #000 calc(100% - 44px), transparent);
    }
    .toolbar {
      gap: 8px;
      margin-bottom: 10px;
    }
    .toolbar .fsel {
      display: none;
    }
    /* Tek satır: seçim kutusu (sayı yalnızca seçim varken) + arama + sıralama. */
    .search {
      flex: 1 1 0;
    }
    /* Sıralama seçicisi yazısına göre genişler: sabit 136 px'te açılır ok işareti
       (base-select'te metnin yanında) kutunun dışına taşıyordu. */
    .toolbar .ssel {
      flex: 0 0 auto;
      max-width: 50%;
    }
    @supports (appearance: base-select) {
      .toolbar .sel {
        padding-right: 12px;
        gap: 8px;
      }
    }
    .toolbar .tsel {
      flex: 1 1 100%;
    }
    .selbox:not(.on) .cnt {
      display: none;
    }
    .selbox {
      padding: 0 12px;
    }
  }
</style>
