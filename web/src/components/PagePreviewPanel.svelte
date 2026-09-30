<script lang="ts" module>
  import type { BarRange, PageLayout } from '../lib/api';
  import type { Locale } from '../lib/i18n';

  /** Düzenleyicinin kaydedilmemiş hali (önizlemenin girdisi). */
  export interface PreviewDraft {
    pageId?: number;
    title: string;
    description: string;
    footer: string;
    sections: { title: string; monitors: { id: number; name: string }[] }[];
    layout: PageLayout;
    barRange: BarRange;
    showTargets: boolean;
    collapsible: boolean;
    lang: Locale;
    logo: string;
  }
</script>

<script lang="ts">
  // Durum sayfası düzenleyicisindeki canlı önizleme: herkese açık sayfa
  // (StatusView) kaydedilmemiş ayarlarla çizilir. Metin, grup, sıra ve dizilim
  // değişiklikleri anında yansır; monitör verileri (çubuklar, yüzdeler, olaylar)
  // yalnızca monitör listesi veya çubuk ayarı değişince sunucudan alınır.
  import { onDestroy, onMount, untrack } from 'svelte';
  import { api, errorMessage, type OverallStatus, type PagePreviewData, type PublicMonitor, type PublicPage } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';
  import StatusView from './StatusView.svelte';

  let { draft, onclose }: { draft: PreviewDraft; onclose?: () => void } = $props();

  type Device = 'desktop' | 'mobile';
  type Theme = 'dark' | 'light';
  const PREF_KEY = 'durum-onizleme';
  // Seçim yoksa: dar ekranda (tam ekran önizleme) mobil, geniş ekranda masaüstü.
  // svelte-ignore state_referenced_locally
  const defDevice: Device = onclose ? 'mobile' : 'desktop';
  function readPref(): { device: Device; theme: Theme } {
    try {
      const v = JSON.parse(localStorage.getItem(PREF_KEY) ?? '{}');
      const device: Device = v.device === 'mobile' || v.device === 'desktop' ? v.device : defDevice;
      return { device, theme: v.theme === 'light' ? 'light' : 'dark' };
    } catch {
      return { device: defDevice, theme: 'dark' };
    }
  }
  const pref = readPref();
  let device = $state<Device>(pref.device);
  let theme = $state<Theme>(pref.theme);
  $effect(() => {
    try {
      localStorage.setItem(PREF_KEY, JSON.stringify({ device, theme }));
    } catch {
      /* depolama yoksa yalnızca bu oturumda */
    }
  });

  // Monitör verileri ------------------------------------------------------------------------
  let data = $state.raw<PagePreviewData | null>(null);
  let loadError = $state('');
  let seq = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let refresh: ReturnType<typeof setInterval> | undefined;

  const ids = $derived([...new Set(draft.sections.flatMap((s) => s.monitors.map((m) => m.id)))]);
  const req = $derived(
    JSON.stringify({ page_id: draft.pageId, monitor_ids: ids, bar_range: draft.barRange, show_targets: draft.showTargets }),
  );

  async function fetchData(body: string) {
    const my = ++seq;
    try {
      const res = await api.pagePreviewData(JSON.parse(body));
      if (my !== seq) return; // daha yeni bir istek var
      data = res;
      loadError = '';
    } catch (e) {
      if (my === seq) loadError = errorMessage(e);
    }
  }

  // İstek değişince kısa gecikmeyle (yazarken/sıralarken her adımda değil) yeniden al.
  $effect(() => {
    const body = req;
    clearTimeout(timer);
    timer = setTimeout(() => fetchData(body), untrack(() => data) ? 250 : 0);
  });
  onMount(() => {
    refresh = setInterval(() => fetchData(req), 60_000);
  });
  onDestroy(() => {
    clearTimeout(timer);
    clearInterval(refresh);
  });

  // Herkese açık sayfanın verisi (sunucudaki buildPublicPage ile aynı kurallar) ------------------
  function overallStatus(statuses: string[]): OverallStatus {
    let up = 0,
      down = 0,
      pending = 0;
    for (const s of statuses) {
      if (s === 'up') up++;
      else if (s === 'down') down++;
      else if (s === 'pending') pending++;
    }
    if (up === 0 && down === 0) return 'unknown';
    if (down === 0) return 'up';
    if (up === 0 && pending === 0) return 'down';
    return 'partial';
  }

  const page = $derived.by((): PublicPage => {
    const mons = data?.monitors ?? {};
    const names = new Map<number, string>();
    const statuses: string[] = [];
    const showIncidents = draft.layout.blocks.find((b) => b.id === 'incidents')?.visible !== false;
    const sections = draft.sections
      .filter((s) => s.title.trim() || s.monitors.length)
      .map((s) => ({
        title: s.title.trim(),
        monitors: s.monitors.flatMap((m): PublicMonitor[] => {
          const d = mons[String(m.id)];
          const own = d?.name ?? live.byId(m.id)?.name;
          if (!d && !own) return []; // silinmiş monitör
          const name = m.name.trim() || own || `#${m.id}`;
          names.set(m.id, name);
          const pm: PublicMonitor = d
            ? { ...d, name }
            : { name, status: 'pending', uptime: null, uptime_90d: null, bars: [] }; // veri yolda
          statuses.push(pm.status);
          return [pm];
        }),
      }));
    return {
      slug: '',
      title: draft.title.trim() || t('pages.livePreview.untitled'),
      description: draft.description.trim(),
      footer: draft.footer.trim(),
      has_logo: !!draft.logo,
      logo_url: draft.logo || null,
      updated_at: data?.updated_at ?? Math.floor(Date.now() / 1000),
      range: data?.range ?? draft.barRange,
      uptime_window: data?.uptime_window ?? (draft.barRange === '90d' ? '90d' : '24h'),
      status: overallStatus(statuses),
      sections,
      announcements: data?.announcements ?? [],
      incidents: showIncidents
        ? (data?.incidents ?? []).flatMap((i) => {
            const n = names.get(i.monitor_id);
            return n ? [{ monitor: n, started_at: i.started_at, resolved_at: i.resolved_at }] : [];
          })
        : [],
      show_incidents: showIncidents,
      collapsible: draft.collapsible,
      lang: draft.lang,
      layout: draft.layout,
    };
  });

  // Çerçeve: sayfa sanal bir ekran genişliğinde çizilip panele sığacak kadar küçültülür.
  let stageW = $state(600);
  // Masaüstü önizleme sayfanın genişliğine yetecek sanal bir ekranı küçülterek
  // gösterir: "Dar" sayfa (860 px) 1100 px'lik, "Geniş" sayfa (≈1640 px) 1720
  // px'lik ekranda. 1920 px'lik ekran dar sayfada da ~0,25 ölçeğe iniyor ve
  // yazılar okunmuyordu.
  const vw = $derived(device === 'mobile' ? 390 : draft.layout?.width === 'wide' ? 1720 : 1100);
  const scale = $derived(Math.min(1, Math.max(0.2, (stageW - 2) / vw)));
</script>

<div class="pp">
  <div class="pp-head">
    <h2><Icon name="eye" size={16} /> {t('pages.livePreview.title')}</h2>
    <div class="pp-ctl">
      <div class="seg" role="group" aria-label={t('pages.livePreview.device')}>
        <button type="button" class:on={device === 'desktop'} aria-pressed={device === 'desktop'} onclick={() => (device = 'desktop')}>
          {t('pages.livePreview.desktop')}
        </button>
        <button type="button" class:on={device === 'mobile'} aria-pressed={device === 'mobile'} onclick={() => (device = 'mobile')}>
          <Icon name="smartphone" size={13} />
          {t('pages.livePreview.mobile')}
        </button>
      </div>
      <div class="seg" role="group" aria-label={t('pages.livePreview.theme')}>
        <button type="button" class:on={theme === 'dark'} aria-pressed={theme === 'dark'} onclick={() => (theme = 'dark')}>
          {t('pages.livePreview.dark')}
        </button>
        <button type="button" class:on={theme === 'light'} aria-pressed={theme === 'light'} onclick={() => (theme = 'light')}>
          {t('pages.livePreview.light')}
        </button>
      </div>
      {#if onclose}
        <button type="button" class="btn ghost icon sm" aria-label={t('pages.livePreview.close')} onclick={onclose}>
          <Icon name="x" size={16} />
        </button>
      {/if}
    </div>
  </div>
  {#if loadError}
    <div class="alert error small pp-err" role="alert">{t('pages.livePreview.loadFailed', { error: loadError })}</div>
  {/if}
  <div class="pp-stage" class:mobile={device === 'mobile'} bind:clientWidth={stageW}>
    <div class="pp-frame" class:pub-light={theme === 'light'} style="width:{vw}px;zoom:{scale}" role="region" aria-label={t('pages.livePreview.title')}>
      <StatusView {page} logo={draft.logo} lang={draft.lang} light={theme === 'light'} embedded />
    </div>
  </div>
  <p class="pp-note">{t('pages.livePreview.note')}</p>
</div>

<style>
  .pp {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-height: 0;
    height: 100%;
  }
  .pp-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
  }
  .pp-head h2 {
    display: flex;
    align-items: center;
    gap: 7px;
    margin: 0;
    font-size: 1rem;
  }
  .pp-ctl {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .seg {
    display: inline-flex;
    border: 1px solid var(--border-strong);
    border-radius: 8px;
    overflow: hidden;
  }
  .seg button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 30px;
    padding: 0 10px;
    border: none;
    background: transparent;
    color: var(--text-2);
    font: inherit;
    font-size: 0.82rem;
    cursor: pointer;
  }
  .seg button + button {
    border-left: 1px solid var(--border-strong);
  }
  .seg button.on {
    background: var(--accent-soft);
    color: var(--accent-text);
    font-weight: 650;
  }
  .seg button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .pp-err {
    margin: 0;
  }
  .pp-stage {
    flex: 1;
    min-height: 0;
    overflow: auto;
    border: 1px solid var(--border-strong);
    border-radius: 12px;
    background: var(--input);
  }
  .pp-stage.mobile {
    display: flex;
    justify-content: center;
    padding: 12px 0;
  }
  .pp-frame {
    background: var(--bg);
    display: flex;
    flex-direction: column;
    min-height: 100%;
  }
  .pp-stage.mobile .pp-frame {
    min-height: 0;
    height: max-content;
    border: 1px solid var(--border-strong);
    border-radius: 18px;
    overflow: hidden;
    box-shadow: var(--shadow);
  }
  .pp-note {
    margin: 0;
    font-size: 0.8rem;
    color: var(--muted);
  }
</style>
