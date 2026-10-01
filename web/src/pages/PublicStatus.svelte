<script lang="ts">
  // Herkese açık durum sayfası (/durum/<kısa-ad> veya özel alan adı). Giriş gerektirmez,
  // 60 saniyede bir yenilenir ve sistemin açık/koyu tema tercihini izler.
  // previewId verilirse yönetim panelinden önizleme olarak açılır (yayında/şifre yok sayılır).
  // Sayfanın gövdesi StatusView'dedir (düzenleyicideki canlı önizleme de onu kullanır).
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import { api, ApiError, errorMessage, type PublicLocked, type PublicPage } from '../lib/api';
  import { nowSec } from '../lib/format';
  import Icon from '../components/Icon.svelte';
  import StatusView from '../components/StatusView.svelte';
  import { i18n, isLocale, setLocale, t, type Locale } from '../lib/i18n';

  let { slug = '', previewId }: { slug?: string; previewId?: number } = $props();

  // Sayfa kendi dilinde gösterilir (sayfa ayarı; eski sunucuda tr). Dil
  // tarayıcıda hatırlanmaz. Yönetim panelindeki önizlemede arayüzün dili
  // değişmez (önizleme çubuğu ve sekme başlığı yöneticinin dilinde kalır);
  // yalnızca sayfanın gövdesi (StatusView) sayfanın dilinde çizilir.
  const prevLocale = i18n.locale;
  const isPreview = untrack(() => previewId !== undefined);
  let pageLang = $state<Locale>(i18n.locale);
  function applyPageLang(l: unknown) {
    const want = isLocale(l) ? l : 'tr';
    pageLang = want;
    if (!isPreview && i18n.locale !== want) setLocale(want, false);
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

  // Tema: yönetim paneli koyu kalır; bu sayfa sistem tercihini izler (açık
  // tercihte kök öğeye .pub-light, bkz. app.css).
  const root = document.documentElement;
  const metaScheme = document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]');
  const metaTheme = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  const prevScheme = metaScheme?.content ?? 'dark';
  const prevTheme = metaTheme?.content ?? '';
  const lightMq = matchMedia('(prefers-color-scheme: light)');
  let light = $state(lightMq.matches);

  function applyThemeColor() {
    light = lightMq.matches;
    if (metaTheme) metaTheme.content = getComputedStyle(root).getPropertyValue('--bg').trim() || prevTheme;
  }

  let timer: ReturnType<typeof setInterval> | undefined;
  const onVisible = () => {
    if (!document.hidden && nowSec() - loadedAt > 30) load();
  };

  onMount(() => {
    root.classList.add('public');
    if (metaScheme) metaScheme.content = 'light dark';
    applyThemeColor();
    lightMq.addEventListener('change', applyThemeColor);
    load();
    timer = setInterval(load, 60_000);
    document.addEventListener('visibilitychange', onVisible);
  });

  onDestroy(() => {
    clearInterval(timer);
    document.removeEventListener('visibilitychange', onVisible);
    lightMq.removeEventListener('change', applyThemeColor);
    root.classList.remove('public');
    if (metaScheme) metaScheme.content = prevScheme;
    if (metaTheme) metaTheme.content = prevTheme;
    if (i18n.locale !== prevLocale) setLocale(prevLocale, false);
  });

  // Önizlemede sekme başlığını yönetim paneli (App) yönetir.
  $effect(() => {
    if (isPreview) return;
    const title = page?.title ?? locked?.title;
    document.title = notFound ? t('pub.notFoundDoc') : title ? t('pub.docTitle', { title }) : t('pub.docTitleDefault');
  });

  // RSS akışı: gerçek sayfada (önizlemede değil); şifreli sayfanın akışı
  // okuyuculara açık olmadığından bağlantı gösterilmez.
  const feedUrl = $derived(!isPreview && page && !page.has_password ? `/api/public/pages/${page.slug}/feed.xml` : '');
  $effect(() => {
    if (!feedUrl) return;
    const link = document.createElement('link');
    link.rel = 'alternate';
    link.type = 'application/rss+xml';
    link.title = t('pub.rssTitle');
    link.href = feedUrl;
    document.head.appendChild(link);
    return () => link.remove();
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

<div class="pub" class:pub-light={light}>
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
    <StatusView {page} {logo} {light} lang={pageLang} foldKey="durum-kapali:{previewId ?? slug}" {feedUrl} />
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
  @media (max-width: 640px) {
    .msg,
    .lock {
      padding: 22px 18px;
    }
  }
</style>
