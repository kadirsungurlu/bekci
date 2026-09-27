<script lang="ts">
  // Herkese açık durum sayfası (/durum/<kısa-ad> veya özel alan adı). Giriş gerektirmez,
  // 60 saniyede bir yenilenir ve sistemin açık/koyu tema tercihini izler.
  // previewId verilirse yönetim panelinden önizleme olarak açılır (yayında/şifre yok sayılır).
  import { onDestroy, onMount, tick } from 'svelte';
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
      page = p;
      locked = null;
      notFound = false;
      loadError = '';
      loadedAt = nowSec();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401 && (e.data as PublicLocked | null)?.password_required) {
        locked = e.data as PublicLocked;
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
  });

  $effect(() => {
    const t = page?.title ?? locked?.title;
    document.title = notFound ? 'Sayfa bulunamadı' : t ? `${t} · Durum` : 'Durum';
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
    if (!password) return (unlockError = 'Şifreyi girin.');
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
  const OVERALL: Record<OverallStatus, { l: string; icon: IconName }> = {
    up: { l: 'Tüm sistemler çalışıyor', icon: 'check' },
    partial: { l: 'Bazı sistemlerde sorun var', icon: 'alert' },
    down: { l: 'Sistemlerde kesinti var', icon: 'x' },
    unknown: { l: 'Durum bilinmiyor', icon: 'info' },
  };
  const MON: Record<PublicMonitorStatus, { l: string; c: string }> = {
    up: { l: 'Çalışıyor', c: 'up' },
    down: { l: 'Kesinti', c: 'down' },
    pending: { l: 'Kontrol ediliyor', c: 'pending' },
    paused: { l: 'Duraklatıldı', c: 'paused' },
    maintenance: { l: 'Bakımda', c: 'maint' },
  };
  const SEV: Record<Severity, { icon: IconName; l: string }> = {
    info: { icon: 'info', l: 'Bilgi' },
    warning: { icon: 'alert', l: 'Uyarı' },
    danger: { icon: 'alert-circle', l: 'Sorun' },
    success: { icon: 'check-circle', l: 'Çözüldü' },
  };

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
    if (!b) return 'Henüz kontrol yok';
    const total = b.up + b.down;
    if (range === 'recent') {
      const when = `${fmtDate(b.t)} ${fmtTimeSec(b.t)}`;
      return `${when}\n${b.up ? 'Çalışıyor' : b.down ? 'Kesinti' : 'Kontrol ediliyor / bakımda'}`;
    }
    const head = range === '24h' ? hourRange(b.t) : fmtDay(b.t);
    if (total === 0) return `${head}\nVeri yok`;
    let s = `${head}\nUptime ${fmtPct((100 * b.up) / total)}`;
    if (b.down > 0) s += ` · ${b.down} başarısız kontrol`;
    return s;
  }

  /** Alt eksenin sol ucu: görünümün başladığı an. */
  function axisStart(bars: (PublicBar | null)[]): string {
    if (range === '90d') return `${bars.length} gün önce`;
    if (range === '24h') return '24 saat önce';
    const first = bars.find((b) => b) ?? null;
    if (!first) return 'Henüz kontrol yok';
    const min = Math.max(1, Math.round((nowSec() - first.t) / 60));
    return min < 120 ? `${min} dk önce` : `${Math.round(min / 60)} saat önce`;
  }

  const uptimeLabel = $derived(page?.uptime_window === '24h' ? 'Son 24 saat' : '90 günlük uptime');
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

  function annWhen(a: { starts_at: number; ends_at: number }): string {
    if (a.ends_at) return `${fmtDate(a.starts_at)} – ${fmtDate(a.ends_at)}`;
    return `${fmtDate(a.starts_at)} itibarıyla`;
  }

  async function focusPw() {
    await tick();
    document.getElementById('pub-pw')?.focus();
  }
  $effect(() => {
    if (locked) focusPw();
  });
</script>

<div class="pub" bind:clientWidth={width}>
  {#if previewId !== undefined}
    <div class="preview-bar" role="note">
      <Icon name="eye" size={15} />
      <span>Önizleme — yayın durumu ve şifre yok sayılır.</span>
      <a href="#/status-pages/{previewId}">Düzenlemeye dön</a>
    </div>
  {/if}

  {#if notFound}
    <div class="center">
      <div class="panel msg">
        <div class="msg-ic"><Icon name="layout" size={26} /></div>
        <h1>Durum sayfası bulunamadı</h1>
        <p>Adres yanlış olabilir veya sayfa yayından kaldırılmış olabilir.</p>
      </div>
    </div>
  {:else if locked}
    <div class="center">
      <form class="panel lock" onsubmit={unlock} novalidate>
        {#if logo}<img class="lock-logo" src={logo} alt={locked.title} />{/if}
        <h1>{locked.title}</h1>
        <p class="lock-sub"><Icon name="lock" size={15} /> Bu sayfa şifre korumalı</p>
        <div class="field">
          <label for="pub-pw">Şifre</label>
          <input id="pub-pw" class="input" type="password" autocomplete="current-password" bind:value={password} />
        </div>
        {#if unlockError}<div class="alert error" role="alert">{unlockError}</div>{/if}
        <button class="btn primary big" type="submit" disabled={unlocking}>
          {#if unlocking}<span class="spinner"></span>{/if}
          Aç
        </button>
      </form>
    </div>
  {:else if !page}
    {#if loadError}
      <div class="center">
        <div class="panel msg">
          <h1>Sayfa yüklenemedi</h1>
          <p>{loadError}</p>
          <button class="btn primary" onclick={load}>Tekrar dene</button>
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
          <span class="live" aria-hidden="true"></span>Son güncelleme {fmtTime(page.updated_at)} · 60 sn'de bir yenilenir
        </div>
      </div>
    </header>

    <main class="wrap">

      <section class="hero st-{page.status}" aria-live="polite">
        <span class="hero-ic"><Icon name={ov.icon} size={26} stroke={2.6} /></span>
        <div>
          <div class="hero-l">{ov.l}</div>
          <div class="hero-s">Son güncelleme: {fmtTime(page.updated_at)}</div>
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
                  <span class="g-st down">{issues(sec.monitors)} serviste kesinti</span>
                {:else}
                  <span class="g-st up">Çalışıyor</span>
                {/if}
              {/snippet}
              {#if canFold}
                <button type="button" class="g-head g-fold" aria-expanded={!isFolded} onclick={() => toggleFold(si)}>
                  <span class="g-chev" aria-hidden="true"><Icon name="chevron-down" size={18} /></span>
                  <h2>{sec.title}</h2>
                  <span class="g-count">{sec.monitors.length}</span>
                  {@render gStatus()}
                </button>
              {:else}
                <div class="g-head">
                  <h2>{sec.title}</h2>
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
                    <span class="m-dot {st.c}" aria-hidden="true"></span>
                    <span class="m-t">{m.name}</span>
                    {#if m.target}<span class="m-target" title={m.target}>{targetLabel(m.target)}</span>{/if}
                  </div>
                  <div class="m-right">
                    <span class="m-up">{fmtPct(upOf(m))}</span>
                    <span class="m-st {st.c}">{st.l}</span>
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
                  <span class="axis-mid">{uptimeLabel}: {fmtPct(upOf(m))}</span>
                  <span>{range === '90d' ? 'Bugün' : 'Şimdi'}</span>
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
        <h2>Son 14 günün olayları</h2>
        {#if incidents.length === 0}
          <div class="no-inc"><Icon name="check-circle" size={18} /> Olay yok</div>
        {:else}
          <ul>
            {#each incidents as inc, i (i)}
              {@const ongoing = inc.resolved_at === 0}
              <li class:ongoing>
                <span class="i-dot" aria-hidden="true"></span>
                <div class="i-b">
                  <div class="i-t">
                    <b>{inc.monitor}</b>
                    {#if ongoing}
                      <span class="i-badge">Devam ediyor</span>
                    {:else}
                      <span class="i-badge ok">Çözüldü</span>
                    {/if}
                  </div>
                  <div class="i-w">
                    {fmtDate(inc.started_at)} ·
                    {ongoing ? `${fmtDuration(clock.now - inc.started_at)} sürüyor` : `${fmtDuration(inc.resolved_at - inc.started_at)} sürdü`}
                  </div>
                </div>
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
        <p class="foot-s">Son güncelleme: {fmtDate(page.updated_at)} · Sayfa her dakika kendiliğinden yenilenir.</p>
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

  /* Gruplar ve monitörler */
  .group {
    padding: 4px 0;
    overflow: hidden;
  }
  .g-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 20px;
    border-bottom: 1px solid var(--border);
  }
  .g-head h2 {
    font-size: 1rem;
    overflow-wrap: anywhere;
  }
  /* Açılıp kapanan grup başlığı (sayfa ayarı) */
  .g-fold {
    width: 100%;
    justify-content: flex-start;
    border: none;
    border-bottom: 1px solid var(--border);
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .g-fold h2 {
    flex: 0 1 auto;
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
      background: var(--card-2);
    }
  }
  .g-chev {
    display: inline-flex;
    color: var(--muted);
    transition: transform 0.15s;
  }
  .group.folded .g-chev {
    transform: rotate(-90deg);
  }
  .group.folded .g-fold {
    border-bottom: none;
  }
  .g-count {
    min-width: 22px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--card-2);
    color: var(--muted);
    font-size: 0.75rem;
    text-align: center;
  }
  .g-st {
    font-size: 0.8rem;
    font-weight: 700;
    white-space: nowrap;
  }
  .g-st.up {
    color: var(--up);
  }
  .g-st.down {
    color: var(--down);
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
    gap: 12px;
    margin-bottom: 10px;
  }
  .m-name {
    display: flex;
    align-items: center;
    gap: 9px;
    min-width: 0;
    flex-wrap: wrap;
  }
  .m-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--paused);
    flex-shrink: 0;
  }
  .m-dot.up {
    background: var(--up);
  }
  .m-dot.down {
    background: var(--down);
  }
  .m-dot.pending {
    background: var(--pending);
  }
  .m-dot.maint {
    background: var(--maint);
  }
  .m-t {
    font-weight: 700;
    overflow-wrap: anywhere;
  }
  .m-target {
    font-size: 0.82rem;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .m-right {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }
  .m-up {
    display: none;
    font-size: 0.86rem;
    color: var(--text-2);
    font-weight: 600;
  }
  .m-st {
    font-size: 0.85rem;
    font-weight: 700;
    color: var(--paused-text);
  }
  .m-st.up {
    color: var(--up);
  }
  .m-st.down {
    color: var(--down);
  }
  .m-st.pending {
    color: var(--pending);
  }
  .m-st.maint {
    color: var(--maint);
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
    justify-content: space-between;
    gap: 8px;
    margin-top: 6px;
    font-size: 0.74rem;
    color: var(--muted);
  }
  .axis-mid {
    flex: 1;
    text-align: center;
    position: relative;
  }
  .picked {
    margin-top: 6px;
    font-size: 0.82rem;
    color: var(--text-2);
  }

  /* Olaylar */
  .inc {
    padding: 18px 20px;
  }
  .inc h2 {
    font-size: 1rem;
    margin-bottom: 10px;
  }
  .no-inc {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--up);
    font-weight: 600;
    padding: 4px 0;
  }
  .inc ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .inc li {
    display: flex;
    gap: 12px;
    padding: 10px 0;
    border-top: 1px solid var(--border);
  }
  .inc li:first-child {
    border-top: none;
  }
  .i-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--up);
    margin-top: 7px;
    flex-shrink: 0;
  }
  .ongoing .i-dot {
    background: var(--down);
  }
  .i-b {
    min-width: 0;
  }
  .i-t {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .i-t b {
    overflow-wrap: anywhere;
  }
  .i-badge {
    font-size: 0.72rem;
    font-weight: 700;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--down-soft);
    color: var(--down-text);
  }
  .i-badge.ok {
    background: var(--up-soft);
    color: var(--up-text);
  }
  .i-w {
    font-size: 0.84rem;
    color: var(--muted);
    margin-top: 2px;
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
    .inc {
      padding-left: 14px;
      padding-right: 14px;
    }
    .m-top {
      align-items: flex-start;
    }
    .m-up {
      display: inline;
    }
    .m-right {
      flex-direction: column;
      align-items: flex-end;
      gap: 0;
    }
    .bars {
      height: 28px;
    }
    .axis-mid {
      display: none;
    }
    .msg,
    .lock {
      padding: 22px 18px;
    }
  }
</style>
