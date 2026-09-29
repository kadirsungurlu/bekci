<script lang="ts">
  // Herkese açık durum sayfası (/durum/<kısa-ad> veya özel alan adı). Giriş gerektirmez,
  // 60 saniyede bir yenilenir ve sistemin açık/koyu tema tercihini izler.
  // previewId verilirse yönetim panelinden önizleme olarak açılır (yayında/şifre yok sayılır).
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    type BarRange,
    type OverallStatus,
    type PublicBar,
    type PublicMonitor,
    type PublicLocked,
    type PublicMonitorStatus,
    type PublicPage,
    type Severity,
  } from '../lib/api';
  import { clock } from '../lib/ui.svelte';
  import { fmtDate, fmtDay, fmtDuration, fmtPct, fmtTime, fmtTimeSec, hourRange, nowSec } from '../lib/format';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import { i18n, isLocale, setLocale, t } from '../lib/i18n';

  // Sayfa kendi dilinde gösterilir (sayfa ayarı; eski sunucuda tr). Dil
  // tarayıcıda hatırlanmaz; önizlemeden çıkınca panelin dili geri gelir.
  const prevLocale = i18n.locale;
  function applyPageLang(l: unknown) {
    const want = isLocale(l) ? l : 'tr';
    if (i18n.locale !== want) setLocale(want, false);
  }

  /** Web adreslerinde yalnızca alan adı (aydertesisat.com.tr); diğer hedefler olduğu gibi. */
  function targetLabel(t: string): string {
    if (!/^https?:\/\//i.test(t)) return t;
    try {
      return new URL(t).host;
    } catch {
      return t;
    }
  }

  let { slug = '', previewId }: { slug?: string; previewId?: number } = $props();

  // Açılıp kapanan gruplar (sayfa ayarı): ziyaretçinin kapattığı gruplar kendi
  // tarayıcısında hatırlanır. Tarayıcı depolaması kapalıysa yalnızca oturum içinde.
  const foldKey = $derived(`durum-kapali:${previewId ?? slug}`);
  let folded = $state<Set<number>>(new Set());
  $effect(() => {
    try {
      const raw = localStorage.getItem(foldKey);
      folded = new Set(raw ? (JSON.parse(raw) as number[]) : []);
    } catch {
      folded = new Set();
    }
  });
  function toggleFold(si: number) {
    const next = new Set(folded);
    if (next.has(si)) next.delete(si);
    else next.add(si);
    folded = next;
    try {
      localStorage.setItem(foldKey, JSON.stringify([...next]));
    } catch {
      /* depolama yoksa yalnızca bu oturumda */
    }
  }


  let page = $state.raw<PublicPage | null>(null);
  let locked = $state.raw<PublicLocked | null>(null);
  let notFound = $state(false);
  let loadError = $state('');
  let loadedAt = $state(0);

  async function load() {
    try {
      const p = previewId !== undefined ? await api.previewPage(previewId) : await api.publicPage(slug);
      applyPageLang(p.lang);
      page = p;
      locked = null;
      notFound = false;
      loadError = '';
      loadedAt = nowSec();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401 && (e.data as PublicLocked | null)?.password_required) {
        locked = e.data as PublicLocked;
        applyPageLang(locked.lang);
        page = null;
      } else if (e instanceof ApiError && e.status === 404) {
        notFound = true;
        page = null;
      } else if (!page) {
        loadError = errorMessage(e);
      }
    }
  }

  // Tema: yönetim paneli koyu kalır; bu sayfa sistem tercihini izler.
  const root = document.documentElement;
  const metaScheme = document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]');
  const metaTheme = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  const prevScheme = metaScheme?.content ?? 'dark';
  const prevTheme = metaTheme?.content ?? '';

  function applyThemeColor() {
    if (metaTheme) metaTheme.content = getComputedStyle(root).getPropertyValue('--bg').trim() || prevTheme;
  }

  let timer: ReturnType<typeof setInterval> | undefined;
  let mq: MediaQueryList | undefined;
  const onVisible = () => {
    if (!document.hidden && nowSec() - loadedAt > 30) load();
  };

  onMount(() => {
    root.classList.add('public');
    if (metaScheme) metaScheme.content = 'light dark';
    applyThemeColor();
    mq = matchMedia('(prefers-color-scheme: dark)');
    mq.addEventListener('change', applyThemeColor);
    load();
    timer = setInterval(load, 60_000);
    document.addEventListener('visibilitychange', onVisible);
  });

  onDestroy(() => {
    clearInterval(timer);
    document.removeEventListener('visibilitychange', onVisible);
    mq?.removeEventListener('change', applyThemeColor);
    root.classList.remove('public');
    if (metaScheme) metaScheme.content = prevScheme;
    if (metaTheme) metaTheme.content = prevTheme;
    if (i18n.locale !== prevLocale) setLocale(prevLocale, false);
  });

  $effect(() => {
    const title = page?.title ?? locked?.title;
    document.title = notFound ? t('pub.notFoundDoc') : title ? t('pub.docTitle', { title }) : t('pub.docTitleDefault');
  });

  const logo = $derived.by(() => {
    const p = page ?? locked;
    if (!p?.has_logo) return '';
    if (previewId !== undefined) return `/api/status-pages/${previewId}/logo?v=${page?.updated_at ?? 0}`;
    return p.logo_url ?? '';
  });

  // Şifre --------------------------------------------------------------------------------
  let password = $state('');
  let unlockError = $state('');
  let unlocking = $state(false);

  async function unlock(e: SubmitEvent) {
    e.preventDefault();
    unlockError = '';
    if (!password) return (unlockError = t('pub.errPassword'));
    unlocking = true;
    try {
      await api.publicUnlock(slug, password);
      password = '';
      await load();
    } catch (err) {
      unlockError = errorMessage(err);
    } finally {
      unlocking = false;
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

  let width = $state(800);
  // Sayfanın çubuk görünümü (eski sunucu: 90 gün). Dar ekranda çubuklar okunur
  // kalsın diye daha az çubuk gösterilir.
  const range = $derived<BarRange>(page?.range ?? '90d');
  const count = $derived(
    range === '24h' ? 24 : range === 'recent' ? (width >= 600 ? 60 : width >= 440 ? 45 : 30) : width >= 600 ? 90 : width >= 440 ? 60 : 30,
  );

  /** Gösterilecek çubuklar; son kontroller görünümünde az kontrol varsa soldan boşlukla doldurulur. */
  function shown(bars: PublicBar[]): (PublicBar | null)[] {
    const last = bars.slice(-count);
    return range === 'recent' && last.length < count ? [...Array(count - last.length).fill(null), ...last] : last;
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
      const when = `${fmtDate(b.t)} ${fmtTimeSec(b.t)}`;
      return `${when}\n${b.up ? t('pub.barRecentUp') : b.down ? t('pub.barRecentDown') : t('pub.barRecentOther')}`;
    }
    const head = range === '24h' ? hourRange(b.t) : fmtDay(b.t);
    if (total === 0) return `${head}\n${t('common.noData')}`;
    let s = `${head}\n${t('pub.barUptime', { pct: fmtPct((100 * b.up) / total) })}`;
    if (b.down > 0) s += ` · ${t('pub.barFailed', { count: b.down })}`;
    return s;
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

  const uptimeLabel = $derived(page?.uptime_window === '24h' ? t('pub.uptime24h') : t('pub.uptime90d'));
  /** Yüzdenin altındaki kısa pencere adı ("son 24 saat"). */
  const upWin = $derived(page?.uptime_window === '24h' ? t('pub.upWin.24h') : t('pub.upWin.90d'));
  /** Düşük uptime uyarı/kesinti tonuyla gösterilir. */
  function upTone(v: number | null | undefined): string {
    if (v === null || v === undefined) return 'none';
    return v >= 99 ? '' : v >= 95 ? 'warn' : 'bad';
  }
  const upOf = (m: PublicMonitor) => (m.uptime !== undefined ? m.uptime : m.uptime_90d);

  // Dokunmatik ekranda çubuğa dokununca bilgisi çubukların altında gösterilir.
  let picked = $state<{ key: string; i: number } | null>(null);
  function pick(e: MouseEvent, key: string) {
    const el = (e.target as HTMLElement).closest<HTMLElement>('[data-i]');
    if (!el) return;
    const i = Number(el.dataset.i);
    picked = picked && picked.key === key && picked.i === i ? null : { key, i };
  }

  const incidents = $derived(page?.incidents ?? []);
  const issues = (ms: PublicPage['sections'][number]['monitors']) => ms.filter((m) => m.status === 'down').length;
  const inMaint = (ms: PublicPage['sections'][number]['monitors']) => ms.some((m) => m.status === 'maintenance');

  function annWhen(a: { starts_at: number; ends_at: number }): string {
    if (a.ends_at) return `${fmtDate(a.starts_at)} – ${fmtDate(a.ends_at)}`;
    return t('pub.since', { time: fmtDate(a.starts_at) });
  }

  async function focusPw() {
    await tick();
    document.getElementById('pub-pw')?.focus();
  }
  // Şifre ekranı ilk açıldığında odaklan; 60 sn'lik yenilemede (yeni nesne gelse de)
  // odağı tekrar çalma, dokunmatikte ise klavyeyi kendiliğinden açma.
  const isLocked = $derived(!!locked);
  $effect(() => {
    if (isLocked && !matchMedia('(hover: none)').matches) untrack(focusPw);
  });
</script>

<div class="pub" bind:clientWidth={width}>
  {#if previewId !== undefined}
    <div class="preview-bar" role="note">
      <Icon name="eye" size={15} />
      <span>{t('pub.preview')}</span>
      <a href="#/status-pages/{previewId}">{t('pub.backToEdit')}</a>
    </div>
  {/if}

  {#if notFound}
    <div class="center">
      <div class="panel msg">
        <div class="msg-ic"><Icon name="layout" size={26} /></div>
        <h1>{t('pub.notFoundTitle')}</h1>
        <p>{t('pub.notFoundText')}</p>
      </div>
    </div>
  {:else if locked}
    <div class="center">
      <form class="panel lock" onsubmit={unlock} novalidate>
        {#if logo}<img class="lock-logo" src={logo} alt={locked.title} />{/if}
        <h1>{locked.title}</h1>
        <p class="lock-sub"><Icon name="lock" size={15} /> {t('pub.locked')}</p>
        <div class="field">
          <label for="pub-pw">{t('pub.password')}</label>
          <input id="pub-pw" class="input" type="password" autocomplete="current-password" bind:value={password} />
        </div>
        {#if unlockError}<div class="alert error" role="alert">{unlockError}</div>{/if}
        <button class="btn primary big" type="submit" disabled={unlocking}>
          {#if unlocking}<span class="spinner"></span>{/if}
          {t('pub.unlock')}
        </button>
      </form>
    </div>
  {:else if !page}
    {#if loadError}
      <div class="center">
        <div class="panel msg">
          <h1>{t('pub.loadFailed')}</h1>
          <p>{loadError}</p>
          <button class="btn primary" onclick={load}>{t('common.retry')}</button>
        </div>
      </div>
    {:else}
      <div class="center"><span class="spinner"></span></div>
    {/if}
  {:else}
    {@const ov = OVERALL[page.status] ?? OVERALL.unknown}
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

    <main class="wrap">

      <section class="hero st-{page.status}" aria-live="polite">
        <span class="hero-ic"><Icon name={ov.icon} size={26} stroke={2.6} /></span>
        <div>
          <div class="hero-l">{ov.l}</div>
          <div class="hero-s">{t('pub.lastUpdate', { time: fmtTime(page.updated_at) })}</div>
        </div>
      </section>

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

      {#each page.sections as sec, si (si)}
        {#if sec.monitors.length}
          {@const canFold = !!page.collapsible && !!sec.title}
          {@const isFolded = canFold && folded.has(si)}
          <section class="panel group" class:folded={isFolded}>
            {#if sec.title}
              {#snippet gStatus()}
                {#if issues(sec.monitors) > 0}
                  <span class="sb down g-st"><span class="sb-dot" aria-hidden="true"></span>{t('pub.groupDown', { count: issues(sec.monitors) })}</span>
                {:else if inMaint(sec.monitors)}
                  <span class="sb maint g-st"><span class="sb-dot" aria-hidden="true"></span>{t('pub.mon.maintenance')}</span>
                {:else}
                  <span class="sb up g-st"><span class="sb-dot" aria-hidden="true"></span>{t('pub.groupUp')}</span>
                {/if}
              {/snippet}
              {#if canFold}
                <button type="button" class="g-head g-fold" aria-expanded={!isFolded} onclick={() => toggleFold(si)}>
                  <span class="g-chev" aria-hidden="true"><Icon name="chevron-down" size={18} /></span>
                  <span class="g-tt">
                    <h2>{sec.title}</h2>
                    <span class="g-count">{t('pub.serviceCount', { count: sec.monitors.length })}</span>
                  </span>
                  {@render gStatus()}
                </button>
              {:else}
                <div class="g-head">
                  <span class="g-tt">
                    <h2>{sec.title}</h2>
                    <span class="g-count">{t('pub.serviceCount', { count: sec.monitors.length })}</span>
                  </span>
                  {@render gStatus()}
                </div>
              {/if}
            {/if}
            {#if !isFolded}
            {#each sec.monitors as m, mi (mi)}
              {@const key = `${si}-${mi}`}
              {@const st = MON[m.status] ?? MON.pending}
              {@const bars = shown(m.bars)}
              <div class="mon">
                <div class="m-top">
                  <div class="m-name">
                    <span class="m-t">{m.name}</span>
                    {#if m.target}<span class="m-target" title={m.target}>{targetLabel(m.target)}</span>{/if}
                  </div>
                  <div class="m-right">
                    <span class="m-up {upTone(upOf(m))}" title={uptimeLabel}>
                      <b>{fmtPct(upOf(m))}</b>
                      <span>{upWin}</span>
                    </span>
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
                  <span>{range === '90d' ? t('pub.today') : t('pub.now')}</span>
                </div>
                {#if picked?.key === key && bars[picked.i]}
                  <div class="picked">{barTip(bars[picked.i]).replace('\n', ' · ')}</div>
                {/if}
              </div>
            {/each}
            {/if}
          </section>
        {/if}
      {/each}

      {#if page.show_incidents !== false}
      <section class="panel inc">
        <div class="inc-h">
          <h2>{t('pub.incidentsTitle')}</h2>
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
      {/if}
    </main>

    <footer class="foot">
      <div class="wrap foot-in">
        {#if page.footer}<p class="foot-t">{page.footer}</p>{/if}
        <p class="foot-s">{t('pub.footer', { time: fmtDate(page.updated_at) })}</p>
      </div>
    </footer>
  {/if}
</div>

<style>
  .pub {
    min-height: 100vh;
    min-height: 100dvh;
    background: var(--bg);
    color: var(--text);
    display: flex;
    flex-direction: column;
  }
  .wrap {
    width: 100%;
    max-width: 860px;
    margin: 0 auto;
    padding: 0 20px;
  }
  .center {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px 16px;
  }
  .panel {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 14px;
    box-shadow: var(--pub-shadow);
  }
  .preview-bar {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 8px 16px;
    background: var(--maint-soft);
    color: var(--maint-text);
    border-bottom: 1px solid var(--maint-border);
    font-size: 0.86rem;
    text-align: center;
  }
  .preview-bar a {
    color: inherit;
    font-weight: 700;
    text-decoration: underline;
  }

  /* Mesaj ve şifre kartları */
  .msg,
  .lock {
    width: 100%;
    max-width: 400px;
    padding: 28px;
    text-align: center;
  }
  .msg h1,
  .lock h1 {
    font-size: 1.3rem;
    margin-bottom: 8px;
  }
  .msg p {
    color: var(--text-2);
    margin: 0 0 16px;
  }
  .msg-ic {
    display: flex;
    justify-content: center;
    color: var(--muted);
    margin-bottom: 10px;
  }
  .lock {
    display: flex;
    flex-direction: column;
    gap: 14px;
    text-align: left;
  }
  .lock h1 {
    text-align: center;
    margin: 0;
  }
  .lock-logo {
    max-height: 48px;
    max-width: 200px;
    object-fit: contain;
    align-self: center;
  }
  .lock-sub {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    color: var(--text-2);
    margin: -6px 0 4px;
    font-size: 0.92rem;
  }
  .big {
    height: 44px;
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
    background: #0f766e;
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
  @media (prefers-color-scheme: light) {
    .sb.up {
      color: var(--up-text);
    }
    .sb.down {
      color: var(--down-text);
    }
    .sb.pending {
      color: var(--pending-text);
    }
    .sb.maint {
      color: var(--maint-text);
    }
  }

  /* Gruplar ve monitörler */
  .group {
    padding: 0;
    overflow: hidden;
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
  .m-right .sb {
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
  .m-up.warn b {
    color: var(--pending);
  }
  .m-up.bad b {
    color: var(--down-text-2);
  }
  .m-up.none b {
    color: var(--muted);
  }
  @media (prefers-color-scheme: light) {
    .m-up.warn b {
      color: var(--pending-text);
    }
    .m-up.bad b {
      color: var(--down-text-2);
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
  @media (prefers-color-scheme: light) {
    .no-inc {
      color: var(--up-text);
    }
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
    margin: 0 0 6px;
    color: var(--text-2);
    white-space: pre-line;
    overflow-wrap: anywhere;
  }
  .foot-s {
    margin: 0;
    font-size: 0.8rem;
    color: var(--muted);
  }

  @media (max-width: 640px) {
    .wrap {
      padding: 0 16px;
    }
    main.wrap {
      padding-top: 20px;
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
    .m-right .sb {
      min-width: 0;
    }
    .msg,
    .lock {
      padding: 22px 18px;
    }
  }
</style>
