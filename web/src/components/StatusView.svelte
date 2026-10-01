<script lang="ts" module>
  import type { PageBlockId, PageLayout } from '../lib/api';

  /** Önceki sabit görünümün sırası (sunucudaki store.DefaultBlockOrder ile aynı). */
  export const BLOCK_ORDER: PageBlockId[] = ['overall', 'announcements', 'groups', 'incidents'];

  /**
   * Dizilimi sunucudaki NormalizeLayout kuralıyla tamamlar: bilinmeyen değerler
   * varsayılana çekilir, eksik bölümler görünür olarak sona eklenir. Olaylar
   * bölümünün görünürlüğü showIncidents'tır (tek kaynak).
   */
  export function normalizeLayout(l: Partial<PageLayout> | null | undefined, showIncidents = true): PageLayout {
    const style = l?.style === 'grid' || l?.style === 'compact' || l?.style === 'rows' ? l.style : 'list';
    const width = l?.width === 'wide' ? 'wide' : 'narrow';
    const blocks: PageLayout['blocks'] = [];
    const seen = new Set<string>();
    const add = (id: PageBlockId, visible: boolean) => {
      if (!BLOCK_ORDER.includes(id) || seen.has(id)) return;
      seen.add(id);
      blocks.push({ id, visible: id === 'incidents' ? showIncidents : visible });
    };
    for (const b of l?.blocks ?? []) add(b.id, b.visible !== false);
    for (const id of BLOCK_ORDER) add(id, true);
    return { style, width, blocks, show_uptime: l?.show_uptime !== false };
  }
</script>

<script lang="ts">
  // Herkese açık durum sayfasının gövdesi (üst başlık, bölümler, alt bilgi).
  // PublicStatus (gerçek sayfa ve tam ekran önizleme) ile düzenleyicideki canlı
  // önizleme aynı çizimi kullanır. Genişliğe göre değişen kurallar ekran değil
  // kendi genişliğine bakar (container query): dar önizleme panelinde de mobil
  // görünüm doğru çizilir.
  import type {
    BarRange,
    OverallStatus,
    PublicBar,
    PublicMonitor,
    PublicMonitorStatus,
    PublicPage,
    Severity,
  } from '../lib/api';
  import { clock } from '../lib/ui.svelte';
  import * as fmt from '../lib/format';
  import { nowSec } from '../lib/format';
  import Icon, { type IconName } from './Icon.svelte';
  import { tIn, type Locale, type TKey, type TParams } from '../lib/i18n';

  let {
    page,
    logo = '',
    lang,
    light = false,
    foldKey = null,
    embedded = false,
    feedUrl = '',
  }: {
    page: PublicPage;
    logo?: string;
    /** Metinlerin dili (sayfanın dili). */
    lang: Locale;
    /** Açık tema (.pub-light). */
    light?: boolean;
    /** Kapatılan grupların tarayıcıda saklandığı anahtar; null: yalnızca bellekte. */
    foldKey?: string | null;
    /** Düzenleyicideki önizleme: tam ekran yüksekliği istenmez. */
    embedded?: boolean;
    /** RSS akışının adresi (boş: bağlantı gösterilmez; önizleme ve şifreli sayfa). */
    feedUrl?: string;
  } = $props();

  const t = (key: TKey, params?: TParams) => tIn(lang, key, params);
  // Tarih, süre ve yüzdeler de sayfanın dilinde (düzenleyicideki canlı önizlemede
  // yönetim arayüzünün dili farklı olabilir).
  const fmtDate = (ts: number) => fmt.fmtDate(ts, lang);
  const fmtDay = (ts: number) => fmt.fmtDay(ts, lang);
  const fmtDuration = (sec: number) => fmt.fmtDuration(sec, lang);
  const fmtPct = (v: number | null | undefined) => fmt.fmtPct(v, lang);
  const fmtTime = (ts: number) => fmt.fmtTime(ts, lang);
  const fmtTimeSec = (ts: number) => fmt.fmtTimeSec(ts, lang);
  const hourRange = (ts: number) => fmt.hourRange(ts, lang);

  /** Web adreslerinde yalnızca alan adı (aydertesisat.com.tr); diğer hedefler olduğu gibi. */
  function targetLabel(v: string): string {
    if (!/^https?:\/\//i.test(v)) return v;
    try {
      return new URL(v).host;
    } catch {
      return v;
    }
  }

  const layout = $derived(normalizeLayout(page.layout, page.show_incidents !== false));

  // Açılıp kapanan gruplar (sayfa ayarı): ziyaretçinin kapattığı gruplar kendi
  // tarayıcısında hatırlanır. Tarayıcı depolaması kapalıysa yalnızca oturum içinde.
  let folded = $state<Set<number>>(new Set());
  $effect(() => {
    if (!foldKey) return;
    try {
      const raw = localStorage.getItem(foldKey);
      folded = new Set(raw ? (JSON.parse(raw) as number[]) : []);
    } catch {
      folded = new Set();
    }
  });
  const foldedHas = (si: number) => folded.has(si);
  function toggleFold(si: number) {
    const next = new Set(folded);
    if (next.has(si)) next.delete(si);
    else next.add(si);
    folded = next;
    if (!foldKey) return;
    try {
      localStorage.setItem(foldKey, JSON.stringify([...next]));
    } catch {
      /* depolama yoksa yalnızca bu oturumda */
    }
  }

  // Görünüm yardımcıları -------------------------------------------------------------------
  const OVERALL: Record<OverallStatus, { l: string; icon: IconName }> = $derived({
    up: { l: t('pub.overall.up'), icon: 'check' },
    partial: { l: t('pub.overall.partial'), icon: 'alert' },
    down: { l: t('pub.overall.down'), icon: 'x' },
    unknown: { l: t('pub.overall.unknown'), icon: 'info' },
  });
  const MON: Record<PublicMonitorStatus, { l: string; c: string }> = $derived({
    up: { l: t('pub.mon.up'), c: 'up' },
    down: { l: t('pub.mon.down'), c: 'down' },
    pending: { l: t('pub.mon.pending'), c: 'pending' },
    paused: { l: t('pub.mon.paused'), c: 'paused' },
    maintenance: { l: t('pub.mon.maintenance'), c: 'maint' },
  });
  const SEV: Record<Severity, { icon: IconName; l: string }> = $derived({
    info: { icon: 'info', l: t('pub.sev.info') },
    warning: { icon: 'alert', l: t('pub.sev.warning') },
    danger: { icon: 'alert-circle', l: t('pub.sev.danger') },
    success: { icon: 'check-circle', l: t('pub.sev.success') },
  });

  // Izgarada gruplar bu genişlikten itibaren iki sütun olur (CSS'teki @container ile aynı).
  const GRID_MIN = 760;
  const GRID3_MIN = 1400;
  const GRID_GAP = 18;
  let mainW = $state(800);
  /** Bir grubun (sütunun) genişliği: çubuk sayısı buna göre seçilir. */
  // Izgara sütun sayısı CSS'teki kapsayıcı sorgularıyla aynı eşiklerde: 760 px'ten
  // iki, 1400 px'ten üç sütun.
  // Grup sayısından fazla sütun açılmaz (iki grupta üçüncü sütun boş kalıyordu).
  const shownSections = $derived(page.sections.map((sec, si) => ({ sec, si })).filter((x) => x.sec.monitors.length > 0));
  const gridCols = $derived(
    layout.style !== 'grid'
      ? 1
      : Math.max(1, Math.min(shownSections.length, mainW >= GRID3_MIN ? 3 : mainW >= GRID_MIN ? 2 : 1)),
  );
  // Izgarada gruplar sütunlara yükseklik dengesiyle dağıtılır (sıradaki grup en
  // kısa sütuna; eşitlikte soldaki): satır ızgarasında kısa grubun yanında
  // boşluk kalmaz, sağ sütun boş durmaz. Yükseklik monitör sayısından tahmin edilir.
  const gridColumns = $derived.by(() => {
    if (gridCols < 2) return [];
    const cols: { sec: (typeof shownSections)[number]['sec']; si: number }[][] = Array.from({ length: gridCols }, () => []);
    const h = new Array(gridCols).fill(0);
    for (const x of shownSections) {
      const folded = !!page.collapsible && !!x.sec.title && foldedHas(x.si);
      let k = 0;
      for (let i = 1; i < gridCols; i++) if (h[i] < h[k]) k = i;
      cols[k].push(x);
      h[k] += 1.4 + (folded ? 0 : x.sec.monitors.length);
    }
    return cols;
  });
  const colW = $derived((mainW - GRID_GAP * (gridCols - 1)) / gridCols);
  // Sayfanın çubuk görünümü (eski sunucu: 90 gün). Dar alanda çubuklar okunur
  // kalsın diye daha az çubuk gösterilir.
  const range = $derived<BarRange>(page.range ?? '90d');
  // Tek satır yerleşiminde çubuklar ad ile yüzde arasındaki alanı doldurur: sayı
  // o alanın ölçülen genişliğine göre seçilir (çubuk + boşluk en az ROW_PITCH px).
  const ROW_PITCH = 8;
  let rowBarsW = $state(0);
  const count = $derived.by(() => {
    if (range === '24h') return 24;
    if (layout.style === 'rows') {
      const w = rowBarsW || mainW * 0.55;
      const opts = [90, 60, 45, 30];
      return opts.find((n) => w / n >= ROW_PITCH) ?? 30;
    }
    if (range === 'recent') return colW >= 560 ? 60 : colW >= 420 ? 45 : 30;
    return colW >= 560 ? 90 : colW >= 420 ? 60 : 30;
  });

  /**
   * Son kontroller görünümünde, kontrolü duran (durdurulmuş, sonuç gelmeyen)
   * monitörün son kontrolünden bu yana geçen süre: kontrol aralığı çubuklardan
   * tahmin edilir; o kadar boş çubuk sağa eklenir. Böylece 18 saat önceki son
   * kontrol "Şimdi"nin hemen üstünde görünmez.
   */
  function medianStep(bars: PublicBar[]): number {
    const gaps: number[] = [];
    for (let i = Math.max(1, bars.length - 20); i < bars.length; i++) gaps.push(bars[i].t - bars[i - 1].t);
    gaps.sort((a, b) => a - b);
    return gaps.length ? gaps[Math.floor(gaps.length / 2)] : 0;
  }
  // Tek kontrolü olan monitör için sayfadaki monitörlerin tipik kontrol aralığı.
  const pageStep = $derived.by(() => {
    const steps = page.sections.flatMap((sec) => sec.monitors.map((m) => medianStep(m.bars))).filter((x) => x > 0);
    steps.sort((a, b) => a - b);
    return steps.length ? steps[Math.floor(steps.length / 2)] : 0;
  });
  function staleSlots(bars: PublicBar[]): number {
    if (range !== 'recent' || bars.length < 1) return 0;
    const step = bars.length >= 2 ? medianStep(bars) : pageStep;
    if (!(step > 0)) return 0;
    const behind = Math.floor((nowSec() - bars[bars.length - 1].t) / step) - 1; // bir aralık tolerans
    return Math.max(0, Math.min(count, behind));
  }

  /** Gösterilecek çubuklar; son kontroller görünümünde az kontrol varsa soldan boşlukla doldurulur. */
  function shown(bars: PublicBar[], paused = false): (PublicBar | null)[] {
    if (range !== 'recent') return bars.slice(-count);
    // Durdurulmuş monitörün çubukları son kontrolde biter (eksen sonu "son kontrol").
    const stale = paused ? 0 : staleSlots(bars);
    const keep = count - stale;
    const last = keep > 0 ? bars.slice(-keep) : [];
    const tail: null[] = Array(stale).fill(null);
    const lead: null[] = Array(Math.max(0, count - last.length - stale)).fill(null);
    return [...lead, ...last, ...tail];
  }

  function barKind(b: PublicBar | null): string {
    if (!b) return 'nodata';
    const total = b.up + b.down;
    if (total === 0) return 'nodata';
    if (b.down === 0) return 'up';
    return (100 * b.up) / total >= 95 ? 'mixed' : 'down';
  }

  function barTip(b: PublicBar | null): string {
    if (!b) return t('pub.noChecksYet');
    const total = b.up + b.down;
    if (range === 'recent') {
      const when = `${fmtDay(b.t)} ${fmtTimeSec(b.t)}`;
      return `${when}\n${b.up ? t('pub.barRecentUp') : b.down ? t('pub.barRecentDown') : t('pub.barRecentOther')}`;
    }
    const head = range === '24h' ? hourRange(b.t) : fmtDay(b.t);
    if (total === 0) return `${head}\n${t('common.noData')}`;
    let s = `${head}\n${t('pub.barUptime', { pct: fmtPct((100 * b.up) / total) })}`;
    if (b.down > 0) s += ` · ${t('pub.barFailed', { count: b.down })}`;
    return s;
  }

  /**
   * Alt eksenin sağ ucu. Son kontroller görünümünde durdurulmuş (veya uzun
   * süredir sonuç gelmeyen) monitörün son çubuğu "şimdi" değildir: son
   * kontrolün zamanı yazılır.
   */
  function axisEnd(m: PublicMonitor, bars: (PublicBar | null)[]): string {
    if (range === '90d') return t('pub.today');
    if (range !== 'recent') return t('pub.now');
    const last = [...bars].reverse().find((b) => b) ?? null;
    if (!last) return t('pub.now');
    const min = Math.max(1, Math.round((nowSec() - last.t) / 60));
    if (m.status !== 'paused' && min < 15) return t('pub.now');
    const ago = min < 120 ? t('pub.minAgo', { count: min }) : t('pub.hoursAgo', { count: Math.round(min / 60) });
    return t('pub.lastCheck', { ago });
  }

  /** Alt eksenin sol ucu: görünümün başladığı an. */
  function axisStart(bars: (PublicBar | null)[]): string {
    if (range === '90d') return t('pub.daysAgo', { count: bars.length });
    if (range === '24h') return t('pub.hours24Ago');
    const first = bars.find((b) => b) ?? null;
    if (!first) return t('pub.noChecksYet');
    const min = Math.max(1, Math.round((nowSec() - first.t) / 60));
    return min < 120 ? t('pub.minAgo', { count: min }) : t('pub.hoursAgo', { count: Math.round(min / 60) });
  }

  const uptimeLabel = $derived(page.uptime_window === '24h' ? t('pub.uptime24h') : t('pub.uptime90d'));
  /** Yüzdenin altındaki kısa pencere adı ("son 24 saat"). */
  const upWin = $derived(page.uptime_window === '24h' ? t('pub.upWin.24h') : t('pub.upWin.90d'));
  /** Düşük uptime uyarı (turuncu) / kesinti tonuyla gösterilir; eşikler panelle
   *  aynı (fmt.uptimeTone). */
  function upTone(v: number | null | undefined): string {
    const tone = fmt.uptimeTone(v);
    return tone === 'good' ? '' : tone;
  }
  const rowTone = upTone;
  const upOf = (m: PublicMonitor) => (m.uptime !== undefined ? m.uptime : m.uptime_90d);
  // 90 günlük görünümde dar alanda daha az gün (ör. 30) çizilir; yüzde ve
  // etiketi de çizilen günlerden hesaplanır ("son 30 gün"): çubuklar "30 gün
  // önce" derken yüzdenin "son 90 gün" demesi çelişiyordu.
  const partialDays = $derived(range === '90d' && page.uptime_window !== '24h' && count < 90);
  function upFor(m: PublicMonitor, bars: (PublicBar | null)[]): number | null | undefined {
    if (!partialDays) return upOf(m);
    let up = 0,
      down = 0;
    for (const b of bars) {
      if (!b) continue;
      up += b.up;
      down += b.down;
    }
    return up + down ? (100 * up) / (up + down) : null;
  }
  const upWinShown = $derived(partialDays ? t('pub.upWinDays', { count }) : upWin);
  const showUptime = $derived(layout.show_uptime);
  // Birden fazla uptime penceresi seçiliyse (24 sa + 30 g + 90 g gibi) her
  // monitörde pencere başına yüzde gösterilir; tek pencerede eski görünüm.
  const multiWins = $derived((page.uptime_windows ?? []).length > 0 ? (page.uptime_windows ?? []) : []);
  const winLabel = (w: string) => t(`pub.upWin.${w}` as TKey);
  const winOf = (m: PublicMonitor, w: string): number | null | undefined => m.uptimes?.[w];
  const incidentDays = $derived(page.incident_days ?? 14);

  // Dokunmatik ekranda çubuğa dokununca bilgisi çubukların altında gösterilir.
  let picked = $state<{ key: string; i: number } | null>(null);
  function pick(e: MouseEvent, key: string) {
    const el = (e.target as HTMLElement).closest<HTMLElement>('[data-i]');
    if (!el) return;
    const i = Number(el.dataset.i);
    picked = picked && picked.key === key && picked.i === i ? null : { key, i };
  }

  const incidents = $derived(page.incidents ?? []);
  const totalMonitors = $derived(page.sections.reduce((n, s) => n + s.monitors.length, 0));
  const issues = (ms: PublicMonitor[]) => ms.filter((m) => m.status === 'down').length;
  const inMaint = (ms: PublicMonitor[]) => ms.some((m) => m.status === 'maintenance');
  const hasGroups = $derived(page.sections.some((s) => s.monitors.length > 0));

  function annWhen(a: { starts_at: number; ends_at: number }): string {
    if (a.ends_at) return `${fmtDate(a.starts_at)} – ${fmtDate(a.ends_at)}`;
    return t('pub.since', { time: fmtDate(a.starts_at) });
  }
</script>

<div class="sv w-{layout.width} st-{layout.style}" class:pub-light={light} class:embedded class:no-up={!showUptime} {lang}>
  <header class="top">
    <div class="wrap top-in">
      <div class="brand">
        {#if logo}
          <img class="logo" src={logo} alt="" />
        {:else}
          <span class="mark" aria-hidden="true"><Icon name="activity" size={22} stroke={2.4} /></span>
        {/if}
        <div class="brand-t">
          <h1>{page.title}</h1>
          {#if page.description}<p class="desc">{page.description}</p>{/if}
        </div>
      </div>
      <div class="refresh">
        <span class="live" aria-hidden="true"></span>{t('pub.refreshInfo', { time: fmtTime(page.updated_at) })}
      </div>
    </div>
  </header>

  <main class="wrap" bind:clientWidth={mainW}>
    {#each layout.blocks as b (b.id)}
      {#if b.visible}
        {#if b.id === 'overall'}
          {@render overall()}
        {:else if b.id === 'announcements'}
          {@render announcements()}
        {:else if b.id === 'groups'}
          {@render groups()}
        {:else if b.id === 'incidents'}
          {@render incidentList()}
        {/if}
      {/if}
    {/each}
  </main>

  <!-- Güncelleme zamanı üst çubukta; alt bilgi sayfanın kendi metni ve (varsa) RSS bağlantısı. -->
  {#if page.footer || feedUrl}
    <footer class="foot">
      <div class="wrap foot-in">
        {#if page.footer}<p class="foot-t">{page.footer}</p>{/if}
        {#if feedUrl}
          <a class="foot-rss" href={feedUrl} title={t('pub.rssTitle')}><Icon name="rss" size={13} /> {t('pub.rss')}</a>
        {/if}
      </div>
    </footer>
  {/if}
</div>

{#snippet overall()}
  {@const ov = OVERALL[page.status] ?? OVERALL.unknown}
  <section class="hero st-{page.status}" aria-live="polite">
    <span class="hero-ic"><Icon name={ov.icon} size={26} stroke={2.6} /></span>
    <div>
      <div class="hero-l">{ov.l}</div>
      {#if totalMonitors > 0}<div class="hero-s">{t('pub.heroCount', { count: totalMonitors })}</div>{/if}
    </div>
  </section>
{/snippet}

{#snippet announcements()}
  {#each page.announcements as a (a.id)}
    <article class="panel ann sev-{a.severity}">
      <span class="ann-ic"><Icon name={SEV[a.severity]?.icon ?? 'info'} size={18} /></span>
      <div class="ann-b">
        <h2>{a.title}</h2>
        {#if a.body}<p>{a.body}</p>{/if}
        <div class="ann-w">{annWhen(a)}</div>
      </div>
    </article>
  {/each}
{/snippet}

{#snippet groupStatus(ms: PublicMonitor[])}
  {#if issues(ms) > 0}
    <span class="sb down g-st"><span class="sb-dot" aria-hidden="true"></span>{t('pub.groupDown', { count: issues(ms) })}</span>
  {:else if inMaint(ms)}
    <span class="sb maint g-st"><span class="sb-dot" aria-hidden="true"></span>{t('pub.mon.maintenance')}</span>
  {:else}
    <span class="sb up g-st"><span class="sb-dot" aria-hidden="true"></span>{t('pub.groupUp')}</span>
  {/if}
{/snippet}

{#snippet barMonitor(m: PublicMonitor, key: string)}
  {@const st = MON[m.status] ?? MON.pending}
  {@const bars = shown(m.bars, m.status === 'paused')}
  <div class="mon">
    <div class="m-top">
      <div class="m-name">
        <span class="m-t">{m.name}</span>
        {#if m.target}<span class="m-target" title={m.target}>{targetLabel(m.target)}</span>{/if}
      </div>
      <div class="m-right">
        {#if showUptime && multiWins.length}
          <span class="m-ups">
            {#each multiWins as w (w)}
              <span class="m-up {upTone(winOf(m, w))}" title="{winLabel(w)}: {fmtPct(winOf(m, w))}">
                <b>{fmtPct(winOf(m, w))}</b>
                <span>{winLabel(w)}</span>
              </span>
            {/each}
          </span>
        {:else if showUptime}
          <span class="m-up {upTone(upFor(m, bars))}" title={partialDays ? upWinShown : uptimeLabel}>
            <b>{fmtPct(upFor(m, bars))}</b>
            <span>{upWinShown}</span>
          </span>
        {/if}
        <span class="sb {st.c}"><span class="sb-dot" aria-hidden="true"></span>{st.l}</span>
      </div>
    </div>
    <!-- Dokunmatik ekranlar için ek kolaylık; aynı bilgi ipucunda ve yüzdede de var. -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
    <div class="bars" onclick={(e) => pick(e, key)} role="img" aria-label="{m.name}: {uptimeLabel.toLowerCase()} {fmtPct(upOf(m))}">
      {#each bars as b, i (i)}
        <span class="bar {barKind(b)}" class:sel={picked?.key === key && picked.i === i} data-i={i} data-tip={barTip(b)}></span>
      {/each}
    </div>
    <div class="axis" aria-hidden="true">
      <span>{axisStart(bars)}</span>
      <span class="axis-line"></span>
      <span>{axisEnd(m, bars)}</span>
    </div>
    {#if picked?.key === key && bars[picked.i]}
      <div class="picked">{barTip(bars[picked.i]).replace('\n', ' · ')}</div>
    {/if}
  </div>
{/snippet}

{#snippet compactMonitor(m: PublicMonitor)}
  {@const st = MON[m.status] ?? MON.pending}
  <li class="crow">
    <span class="c-dot {st.c}" aria-hidden="true"></span>
    <span class="c-name">
      <span class="m-t">{m.name}</span>
      {#if m.target}<span class="m-target" title={m.target}>{targetLabel(m.target)}</span>{/if}
    </span>
    {#if showUptime && multiWins.length}
      <span class="c-ups">
        {#each multiWins as w (w)}
          <span class="c-up {upTone(winOf(m, w))}" title="{winLabel(w)}: {fmtPct(winOf(m, w))}">{fmtPct(winOf(m, w))}<small>{winLabel(w)}</small></span>
        {/each}
      </span>
    {:else if showUptime}
      <span class="c-up {upTone(upOf(m))}" title="{uptimeLabel}: {fmtPct(upOf(m))}">{fmtPct(upOf(m))}</span>
    {:else}
      <span></span>
    {/if}
    <span class="sb {st.c}"><span class="sb-dot" aria-hidden="true"></span>{st.l}</span>
  </li>
{/snippet}

{#snippet rowMonitor(m: PublicMonitor, key: string)}
  {@const st = MON[m.status] ?? MON.pending}
  {@const bars = shown(m.bars)}
  <li class="rrow">
    <!-- Durum ışığı: metin rozeti yerine; durum ekran okuyucuya ve ipucuna yazılır. -->
    <span class="light {st.c}" role="img" aria-label={t('pub.statusLight', { status: st.l })} data-tip={st.l}></span>
    <div class="r-name">
      <span class="m-t">{m.name}</span>
      {#if m.target}<span class="m-target" title={m.target}>{targetLabel(m.target)}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
    <div
      class="bars r-bars"
      bind:clientWidth={rowBarsW}
      onclick={(e) => pick(e, key)}
      role="img"
      aria-label="{m.name}: {uptimeLabel.toLowerCase()} {fmtPct(upOf(m))}"
    >
      {#each bars as b, i (i)}
        <span class="bar {barKind(b)}" class:sel={picked?.key === key && picked.i === i} data-i={i} data-tip={barTip(b)}></span>
      {/each}
    </div>
    {#if showUptime && multiWins.length}
      <span class="r-ups">
        {#each multiWins as w (w)}
          <span class="r-up {rowTone(winOf(m, w))}" title="{winLabel(w)}: {fmtPct(winOf(m, w))}">
            <b>{fmtPct(winOf(m, w))}</b>
            <span>{winLabel(w)}</span>
          </span>
        {/each}
      </span>
    {:else if showUptime}
      <span class="r-up {rowTone(upFor(m, bars))}" title={partialDays ? upWinShown : uptimeLabel}>
        <b>{fmtPct(upFor(m, bars))}</b>
        <span>{upWinShown}</span>
      </span>
    {/if}
    {#if picked?.key === key && bars[picked.i]}
      <div class="picked r-picked">{barTip(bars[picked.i]).replace('\n', ' · ')}</div>
    {/if}
  </li>
{/snippet}

{#snippet groups()}
  {#if hasGroups}
    {#if gridColumns.length > 1}
      <div class="groups gcols" style:grid-template-columns="repeat({gridColumns.length}, minmax(0, 1fr))">
        {#each gridColumns as col, ci (ci)}
          <div class="gcol">
            {#each col as x (x.si)}
              {@render group(x.sec, x.si)}
            {/each}
          </div>
        {/each}
      </div>
    {:else}
      <div class="groups">
        {#each shownSections as x (x.si)}
          {@render group(x.sec, x.si)}
        {/each}
      </div>
    {/if}
  {/if}
{/snippet}

{#snippet group(sec: PublicPage['sections'][number], si: number)}
          {@const canFold = !!page.collapsible && !!sec.title}
          {@const isFolded = canFold && folded.has(si)}
          <section class="panel group" class:folded={isFolded}>
            {#if sec.title}
              {#if canFold}
                <button type="button" class="g-head g-fold" aria-expanded={!isFolded} onclick={() => toggleFold(si)}>
                  <span class="g-chev" aria-hidden="true"><Icon name="chevron-down" size={18} /></span>
                  <span class="g-tt">
                    <h2>{sec.title}</h2>
                    <span class="g-count">{t('pub.serviceCount', { count: sec.monitors.length })}</span>
                  </span>
                  {@render groupStatus(sec.monitors)}
                </button>
              {:else}
                <div class="g-head">
                  <span class="g-tt">
                    <h2>{sec.title}</h2>
                    <span class="g-count">{t('pub.serviceCount', { count: sec.monitors.length })}</span>
                  </span>
                  {@render groupStatus(sec.monitors)}
                </div>
              {/if}
            {/if}
            {#if !isFolded}
              {#if layout.style === 'rows'}
                <ul class="rmons">
                  {#each sec.monitors as m, mi (mi)}
                    {@render rowMonitor(m, `${si}-${mi}`)}
                  {/each}
                </ul>
              {:else if layout.style === 'compact'}
                <ul class="cmons">
                  {#each sec.monitors as m, mi (mi)}
                    {@render compactMonitor(m)}
                  {/each}
                </ul>
              {:else}
                {#each sec.monitors as m, mi (mi)}
                  {@render barMonitor(m, `${si}-${mi}`)}
                {/each}
              {/if}
            {/if}
          </section>
{/snippet}

{#snippet incidentList()}
  <section class="panel inc">
    <div class="inc-h">
      <h2>{t('pub.incidentsTitleN', { count: incidentDays })}</h2>
      {#if incidents.length}<span class="g-count">{incidents.length}</span>{/if}
    </div>
    {#if incidents.length === 0}
      <div class="no-inc"><Icon name="check-circle" size={18} /> {t('pub.noIncidents')}</div>
    {:else}
      <ul>
        {#each incidents as inc, i (i)}
          {@const ongoing = inc.resolved_at === 0}
          <li class:ongoing>
            <span class="i-ic" aria-hidden="true"><Icon name={ongoing ? 'alert-circle' : 'check-circle'} size={18} /></span>
            <div class="i-b">
              <div class="i-t">{inc.monitor}</div>
              <div class="i-w">
                <span>{t('pub.startedAt', { time: fmtDate(inc.started_at) })}</span>
                {#if ongoing}
                  <span class="i-d">{t('pub.lasting', { d: fmtDuration(clock.now - inc.started_at) })}</span>
                {:else}
                  <span class="i-d">{t('pub.duration', { d: fmtDuration(inc.resolved_at - inc.started_at) })}</span>
                  <span class="i-res">{t('pub.resolvedAt', { time: fmtDate(inc.resolved_at) })}</span>
                {/if}
              </div>
            </div>
            {#if ongoing}
              <span class="sb down i-badge"><span class="sb-dot" aria-hidden="true"></span>{t('pub.ongoing')}</span>
            {:else}
              <span class="sb up i-badge"><span class="sb-dot" aria-hidden="true"></span>{t('pub.resolved')}</span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/snippet}

<style>
  .sv {
    flex: 1;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    color: var(--text);
    container: pubv / inline-size;
  }
  /* Düzenleyicideki önizleme: çerçeveyi doldurur. */
  .sv.embedded {
    min-height: 100%;
  }
  .wrap {
    /* Izgara/sık liste sütun sayısı sayfa kutusunun genişliğine göre (ekrana göre değil). */
    container: pubw / inline-size;
    width: 100%;
    max-width: 860px;
    margin: 0 auto;
    padding: 0 20px;
  }
  .w-wide .wrap {
    /* Geniş: tüm yerleşimlerde aynı genişlik (ızgara ve sık liste burada üç sütun). */
    max-width: 1640px;
  }
  .panel {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 14px;
    box-shadow: var(--pub-shadow);
  }

  /* Üst çubuk */
  .top {
    border-bottom: 1px solid var(--border);
    background: var(--bg-elev);
  }
  .top-in {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding-top: 22px;
    padding-bottom: 22px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .logo {
    max-height: 40px;
    max-width: min(160px, 40vw);
    object-fit: contain;
    flex: none;
  }
  .mark {
    width: 40px;
    height: 40px;
    border-radius: 10px;
    flex: none;
    display: grid;
    place-items: center;
    background: #047857;
    color: #fff;
  }
  .brand-t {
    min-width: 0;
  }
  .brand-t h1 {
    margin: 0;
    font-size: 1.2rem;
    line-height: 1.2;
    font-weight: 650;
    letter-spacing: -0.01em;
    overflow-wrap: anywhere;
  }
  .brand-t .desc {
    margin: 2px 0 0;
    font-size: 0.85rem;
    color: var(--muted);
  }
  .refresh {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--muted);
    font-size: 0.82rem;
    white-space: nowrap;
  }
  .live {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--up);
    box-shadow: 0 0 0 3px var(--up-soft);
    flex: none;
  }

  main.wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 18px;
    padding-top: 32px;
    padding-bottom: 40px;
  }
  .desc {
    white-space: pre-line;
    overflow-wrap: anywhere;
  }

  /* Genel durum */
  .hero {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 22px 24px;
    border-radius: 16px;
    background: var(--pub-hero-unknown);
    color: var(--pub-hero-text);
    box-shadow: var(--pub-shadow);
  }
  .hero.st-up {
    background: var(--pub-hero-up);
  }
  .hero.st-partial {
    background: var(--pub-hero-partial);
  }
  .hero.st-down {
    background: var(--pub-hero-down);
  }
  .hero-ic {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--pub-hero-ic);
    flex-shrink: 0;
  }
  .hero-l {
    font-size: 1.3rem;
    font-weight: 800;
    letter-spacing: -0.01em;
    line-height: 1.25;
  }
  .hero-s {
    font-size: 0.86rem;
    color: var(--pub-hero-sub);
    margin-top: 2px;
  }

  /* Duyurular */
  .ann {
    display: flex;
    gap: 14px;
    padding: 16px 18px;
    border-left: 4px solid var(--accent);
  }
  .ann.sev-warning {
    border-left-color: var(--pending);
  }
  .ann.sev-danger {
    border-left-color: var(--down);
  }
  .ann.sev-success {
    border-left-color: var(--up);
  }
  .ann-ic {
    display: inline-flex;
    margin-top: 2px;
    color: var(--accent-text);
  }
  .sev-warning .ann-ic {
    color: var(--pending);
  }
  .sev-danger .ann-ic {
    color: var(--down);
  }
  .sev-success .ann-ic {
    color: var(--up);
  }
  .ann-b {
    min-width: 0;
  }
  .ann h2 {
    font-size: 1rem;
    overflow-wrap: anywhere;
  }
  .ann p {
    margin: 6px 0 0;
    color: var(--text-2);
    white-space: pre-line;
    overflow-wrap: anywhere;
    font-size: 0.93rem;
  }
  .ann-w {
    margin-top: 8px;
    font-size: 0.8rem;
    color: var(--muted);
  }

  /* Durum rozeti: nokta + metin; iki temada da AA kontrastlı. */
  .sb {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 28px;
    padding: 0 11px 0 10px;
    border-radius: 999px;
    border: 1px solid var(--paused-soft);
    background: var(--paused-soft);
    color: var(--paused-text);
    font-size: 0.84rem;
    font-weight: 650;
    line-height: 1;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .sb-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--paused);
    flex-shrink: 0;
  }
  .sb.up {
    background: var(--up-soft);
    border-color: var(--up-border);
    color: var(--up);
  }
  .sb.up .sb-dot {
    background: var(--up);
    box-shadow: 0 0 0 3px var(--up-soft);
  }
  .sb.down {
    background: var(--down-soft);
    border-color: var(--down-border);
    color: var(--down-text-2);
  }
  .sb.down .sb-dot {
    background: var(--down);
    box-shadow: 0 0 0 3px var(--down-soft);
  }
  .sb.pending {
    background: var(--pending-soft);
    border-color: var(--pending-border);
    color: var(--pending);
  }
  .sb.pending .sb-dot {
    background: var(--pending);
  }
  .sb.maint {
    background: var(--maint-soft);
    border-color: var(--maint-border);
    color: var(--maint);
  }
  .sb.maint .sb-dot {
    background: var(--maint);
  }
  /* Açık temada doygun renkler açık zeminde AA'yı tutmaz; koyu metin tonları kullanılır. */
  .pub-light .sb.up {
    color: var(--up-text);
  }
  .pub-light .sb.down {
    color: var(--down-text);
  }
  .pub-light .sb.pending {
    color: var(--pending-text);
  }
  .pub-light .sb.maint {
    color: var(--maint-text);
  }

  /* Gruplar ve monitörler */
  .groups {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .group {
    padding: 0;
    overflow: hidden;
    min-width: 0;
  }
  .g-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 20px;
    border-bottom: 1px solid var(--border);
    background: var(--card-2);
  }
  .g-tt {
    display: flex;
    align-items: baseline;
    gap: 10px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .g-head h2 {
    font-size: 1.06rem;
    font-weight: 700;
    letter-spacing: -0.005em;
    overflow-wrap: anywhere;
  }
  /* Açılıp kapanan grup başlığı (sayfa ayarı) */
  .g-fold {
    width: 100%;
    justify-content: flex-start;
    border: none;
    border-bottom: 1px solid var(--border);
    background: var(--card-2);
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .g-fold .g-st {
    margin-left: auto;
  }
  .g-fold:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  @media (hover: hover) {
    .g-fold:hover {
      background: var(--card-hover);
    }
  }
  .g-chev {
    display: inline-flex;
    align-self: center;
    color: var(--text-2);
    transition: transform 0.15s;
  }
  .group.folded .g-chev {
    transform: rotate(-90deg);
  }
  .group.folded .g-fold {
    border-bottom: none;
  }
  .g-count {
    font-size: 0.84rem;
    font-weight: 500;
    color: var(--text-2);
    white-space: nowrap;
  }
  .mon {
    padding: 16px 20px 14px;
    border-bottom: 1px solid var(--border);
  }
  .mon:last-child {
    border-bottom: none;
  }
  .m-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
    margin-bottom: 12px;
  }
  .m-name {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .m-t {
    font-weight: 700;
    font-size: 1rem;
    overflow-wrap: anywhere;
  }
  .m-target {
    font-size: 0.84rem;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .m-right {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-shrink: 0;
  }
  /* Rozetler aynı genişlikte: yüzdeler satırlar arasında hizalı kalır. */
  .m-right .sb,
  .crow .sb {
    min-width: 7.4em;
  }
  .m-up {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    line-height: 1.15;
  }
  .m-up b {
    font-size: 1.05rem;
    font-weight: 750;
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .m-up span {
    font-size: 0.76rem;
    color: var(--text-2);
    white-space: nowrap;
  }
  .m-up.warn b,
  .c-up.warn {
    color: var(--warn-text);
  }
  .m-up.bad b,
  .c-up.bad {
    color: var(--down-text-2);
  }
  .m-up.none b,
  .c-up.none {
    color: var(--muted);
  }
  /* Birden fazla uptime penceresi: yan yana küçük sütunlar. */
  .m-ups,
  .r-ups {
    display: flex;
    gap: 14px;
    align-items: flex-end;
  }
  .m-ups .m-up b,
  .r-ups .r-up b {
    font-size: 0.95rem;
  }
  .c-ups {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
  }
  .c-ups .c-up {
    display: inline-flex;
    flex-direction: column;
    align-items: flex-end;
    line-height: 1.1;
    font-size: 0.88rem;
  }
  .c-ups .c-up small {
    font-size: 0.66rem;
    font-weight: 500;
    color: var(--text-2);
    white-space: nowrap;
  }
  @media (max-width: 480px) {
    .m-ups,
    .r-ups {
      gap: 8px;
    }
    .m-ups .m-up span,
    .r-ups .r-up span {
      font-size: 0.66rem;
    }
  }
  .bars {
    display: flex;
    gap: 3px;
    height: 32px;
    cursor: pointer;
  }
  .bar {
    flex: 1 1 0;
    min-width: 0;
    border-radius: 999px;
    background: var(--empty-bar);
    transition: opacity 0.1s;
  }
  .bar.up {
    background: var(--up);
  }
  .bar.mixed {
    background: var(--pending);
  }
  .bar.down {
    background: var(--down);
  }
  .bar.sel {
    outline: 2px solid var(--text);
    outline-offset: 1px;
  }
  @media (hover: hover) {
    .bars:hover .bar {
      opacity: 0.6;
    }
    .bars .bar:hover {
      opacity: 1;
    }
  }
  .axis {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-top: 8px;
    font-size: 0.8rem;
    font-weight: 500;
    color: var(--text-2);
  }
  .axis-line {
    flex: 1;
    height: 1px;
    background: var(--border);
  }
  .picked {
    margin-top: 6px;
    font-size: 0.86rem;
    color: var(--text);
  }

  /* Izgara: gruplar sütunlara dengeli dağıtılır (gridColumns); her sütun alt alta kartlar. */
  .st-grid .groups.gcols {
    display: grid;
    align-items: start;
  }
  .gcol {
    display: flex;
    flex-direction: column;
    gap: 18px;
    min-width: 0;
  }
  /* Izgara: geniş alanda gruplar iki sütunda kart; çubuklar biraz daha sık. */
  @container pubw (min-width: 760px) {
    .st-grid .groups {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      align-items: start;
    }
  }
  @container pubw (min-width: 1400px) {
    .st-grid .groups {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }
  .st-grid .mon {
    padding: 14px 18px 12px;
  }
  .st-grid .bars {
    gap: 2px;
    height: 28px;
  }
  .st-grid .m-right {
    gap: 12px;
  }
  .st-grid .m-right .sb {
    min-width: 0;
  }

  /* Sık liste: çubuksuz, monitör başına tek satır. */
  .cmons {
    list-style: none;
    margin: 0 0 -1px;
    padding: 0;
  }
  .crow {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 12px;
    padding: 9px 20px;
    min-height: 48px;
    border-bottom: 1px solid var(--border);
  }
  .c-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--paused);
  }
  .c-dot.up {
    background: var(--up);
    box-shadow: 0 0 0 3px var(--up-soft);
  }
  .c-dot.down {
    background: var(--down);
    box-shadow: 0 0 0 3px var(--down-soft);
  }
  .c-dot.pending {
    background: var(--pending);
  }
  .c-dot.maint {
    background: var(--maint);
  }
  .c-name {
    display: flex;
    align-items: baseline;
    gap: 4px 10px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .c-name .m-t {
    font-size: 0.97rem;
    font-weight: 650;
  }
  .c-name .m-target {
    font-size: 0.8rem;
  }
  .c-up {
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--text);
    font-size: 0.95rem;
  }
  .crow .sb {
    height: 26px;
    font-size: 0.8rem;
  }
  /* Geniş sayfada sık liste iki sütun. */
  @container pubw (min-width: 1000px) {
    .cmons {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      column-gap: 0;
    }
    .crow:nth-child(odd) {
      border-right: 1px solid var(--border);
    }
  }
  /* Geniş sayfada (≈1640 px) sık liste üç sütun. */
  @container pubw (min-width: 1400px) {
    .cmons {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .crow:nth-child(odd) {
      border-right: 0;
    }
    .crow:not(:nth-child(3n)) {
      border-right: 1px solid var(--border);
    }
  }

  /* Tek satır: monitör başına bir satır (ışık, ad, çubuklar, uptime). Sütunlar
     tüm gruplarda hizalı olsun diye satırlar gruplar kabının ızgarasını
     (subgrid) paylaşır: ad sütunu en uzun ada göre, en fazla %30. */
  .st-rows .groups {
    display: grid;
    grid-template-columns: auto fit-content(30%) minmax(0, 1fr) auto;
    gap: 18px 18px;
  }
  .st-rows.no-up .groups {
    grid-template-columns: auto fit-content(30%) minmax(0, 1fr);
  }
  .st-rows .group,
  .rmons,
  .rrow {
    grid-column: 1 / -1;
    display: grid;
    grid-template-columns: subgrid;
    row-gap: 0;
  }
  .st-rows .g-head {
    grid-column: 1 / -1;
  }
  .rmons {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .rrow {
    align-items: center;
    min-height: 54px;
    padding: 8px 20px;
    border-top: 1px solid var(--border);
  }
  .rrow:first-child {
    border-top: none;
  }
  .r-name {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 10rem;
  }
  .r-name .m-t {
    font-size: 0.97rem;
    line-height: 1.25;
  }
  .r-name .m-target {
    font-size: 0.8rem;
    line-height: 1.2;
  }
  .r-bars {
    height: 26px;
    gap: 3px;
  }
  .r-up {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    min-width: 4.4rem;
    line-height: 1.15;
  }
  .r-up b {
    font-size: 1rem;
    font-weight: 750;
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .r-up span {
    font-size: 0.74rem;
    color: var(--text-2);
    white-space: nowrap;
  }
  .r-up.warn b {
    color: var(--warn-text);
  }
  .r-up.bad b {
    color: var(--down-text-2);
  }
  .r-up.none b {
    color: var(--muted);
  }
  .pub-light .r-up.bad b {
    color: var(--down-text);
  }
  .r-picked {
    grid-column: 3 / -1;
    margin-top: 4px;
  }

  /* Durum ışığı: çalışıyorsa yumuşak, kesintide hızlı ve belirgin nabız;
     bakımda sakin nabız; durdurulmuş/bilinmiyor gri ve sabit. */
  .light {
    position: relative;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--paused);
    flex: none;
  }
  .light::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: 50%;
    background: inherit;
    opacity: 0;
    pointer-events: none;
  }
  .light.up {
    background: var(--up);
  }
  .light.up::after {
    animation: sv-halo 2.4s ease-out infinite;
  }
  .light.down {
    background: var(--down);
    box-shadow: 0 0 0 3px var(--down-soft);
  }
  .light.down::after {
    animation: sv-halo-strong 1s ease-out infinite;
  }
  .light.maint {
    background: var(--maint);
  }
  .light.maint::after {
    animation: sv-halo 2.4s ease-out infinite;
  }
  @keyframes sv-halo {
    0% {
      transform: scale(1);
      opacity: 0.55;
    }
    70%,
    100% {
      transform: scale(2.5);
      opacity: 0;
    }
  }
  @keyframes sv-halo-strong {
    0% {
      transform: scale(1);
      opacity: 0.9;
    }
    100% {
      transform: scale(3.1);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .light::after {
      animation: none !important;
    }
    .light.up {
      box-shadow: 0 0 0 3px var(--up-soft);
    }
    .light.maint {
      box-shadow: 0 0 0 3px var(--maint-soft);
    }
  }

  /* Olaylar */
  .inc {
    padding: 0;
    overflow: hidden;
  }
  .inc-h {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 20px;
    background: var(--card-2);
    border-bottom: 1px solid var(--border);
  }
  .inc-h h2 {
    font-size: 1.06rem;
    font-weight: 700;
  }
  .inc-h .g-count {
    min-width: 24px;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--card);
    border: 1px solid var(--border);
    text-align: center;
    font-weight: 650;
  }
  .no-inc {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--up);
    font-weight: 600;
    padding: 16px 20px;
  }
  .pub-light .no-inc {
    color: var(--up-text);
  }
  .inc ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .inc li {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 14px 20px;
    border-top: 1px solid var(--border);
  }
  .inc li:first-child {
    border-top: none;
  }
  .i-ic {
    display: inline-flex;
    margin-top: 1px;
    color: var(--up);
    flex-shrink: 0;
  }
  .ongoing .i-ic {
    color: var(--down);
  }
  .i-b {
    flex: 1;
    min-width: 0;
  }
  .i-t {
    font-weight: 700;
    overflow-wrap: anywhere;
  }
  .i-w {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 14px;
    margin-top: 4px;
    font-size: 0.86rem;
    color: var(--text-2);
  }
  .i-d {
    font-weight: 650;
    color: var(--text);
  }
  .ongoing .i-d {
    color: var(--down-text-2);
  }

  .foot {
    border-top: 1px solid var(--border);
    padding: 20px 0 28px;
  }
  .foot-in {
    text-align: center;
  }
  .foot-t {
    margin: 0;
    color: var(--text-2);
    white-space: pre-line;
    overflow-wrap: anywhere;
  }
  .foot-rss {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    margin-top: 10px;
    font-size: 0.85rem;
    color: var(--muted);
  }
  .foot-rss:hover {
    color: var(--text);
  }

  /* Dar alan (telefon veya dar önizleme): ekran değil sayfanın kendi genişliği. */
  @container pubv (max-width: 640px) {
    .wrap {
      padding: 0 16px;
    }
    main.wrap {
      padding-top: 20px;
      gap: 14px;
    }
    .groups {
      gap: 14px;
    }
    .top-in {
      flex-direction: column;
      align-items: flex-start;
      gap: 10px;
      padding-top: 16px;
      padding-bottom: 16px;
    }
    .refresh {
      white-space: normal;
    }
    .hero {
      padding: 16px 18px;
      gap: 12px;
    }
    .hero-ic {
      width: 40px;
      height: 40px;
    }
    .hero-l {
      font-size: 1.08rem;
    }
    .g-head,
    .mon,
    .st-grid .mon,
    .crow,
    .inc-h,
    .inc li,
    .no-inc {
      padding-left: 14px;
      padding-right: 14px;
    }
    .m-top {
      align-items: flex-start;
      gap: 10px;
    }
    .m-right {
      flex-direction: column-reverse;
      align-items: flex-end;
      gap: 6px;
    }
    .m-up {
      flex-direction: row;
      align-items: baseline;
      gap: 5px;
    }
    .m-up b {
      font-size: 0.98rem;
    }
    .sb {
      height: 26px;
      font-size: 0.8rem;
      padding: 0 9px 0 8px;
      gap: 6px;
    }
    .bars {
      height: 28px;
    }
    .i-w {
      flex-direction: column;
    }
    .i-res {
      display: none;
    }
    .m-right .sb,
    .crow .sb {
      min-width: 0;
    }
    .crow {
      gap: 10px;
    }
    .c-name {
      flex-direction: column;
      gap: 1px;
    }
    /* Tek satır telefonda: ışık + ad + yüzde üstte, çubuklar altta. */
    .st-rows .groups,
    .st-rows.no-up .groups {
      display: flex;
      flex-direction: column;
      gap: 14px;
    }
    .st-rows .group,
    .rmons {
      display: block;
    }
    .rrow {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'light name up'
        'bars bars bars'
        'pick pick pick';
      column-gap: 10px;
      row-gap: 7px;
      padding: 9px 14px 11px;
    }
    .rrow .light {
      grid-area: light;
      align-self: start;
      margin-top: 5px;
    }
    .r-name {
      grid-area: name;
      min-width: 0;
    }
    .r-bars {
      grid-area: bars;
      height: 22px;
    }
    .r-up {
      grid-area: up;
      min-width: 0;
    }
    .r-picked {
      grid-area: pick;
      margin-top: 0;
    }
  }
</style>
