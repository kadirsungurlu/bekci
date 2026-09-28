<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { api, errorMessage, onPasswordChangeRequired, onUnauthorized, type User } from './lib/api';
  import { live } from './lib/live.svelte';
  import { servers } from './lib/servers.svelte';
  import { publicSlugFromPath, router } from './lib/router.svelte';
  import { ROLE_LABELS, session } from './lib/session.svelte';
  import { toast } from './lib/ui.svelte';
  import { pwa, stripAppManifest } from './lib/pwa.svelte';
  import Icon, { type IconName } from './components/Icon.svelte';
  import Toasts from './components/Toasts.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import Tooltip from './components/Tooltip.svelte';
  import Auth from './pages/Auth.svelte';
  import ForcePassword from './pages/ForcePassword.svelte';
  import PublicStatus from './pages/PublicStatus.svelte';
  import MonitorList from './pages/MonitorList.svelte';
  import MonitorDetail from './pages/MonitorDetail.svelte';
  import MonitorForm from './pages/MonitorForm.svelte';
  import Incidents from './pages/Incidents.svelte';
  import IncidentDetail from './pages/IncidentDetail.svelte';
  import Notifications from './pages/Notifications.svelte';
  import Settings from './pages/Settings.svelte';
  import StatusPages from './pages/StatusPages.svelte';
  import StatusPageEditor from './pages/StatusPageEditor.svelte';
  import MaintenanceList from './pages/MaintenanceList.svelte';
  import MaintenanceForm from './pages/MaintenanceForm.svelte';
  import More from './pages/More.svelte';
  import ServerList from './pages/ServerList.svelte';
  import ServerDetail from './pages/ServerDetail.svelte';

  type Phase = 'loading' | 'public' | 'setup' | 'login' | 'password' | 'app' | 'error';

  let phase = $state<Phase>('loading');
  let loadError = $state('');
  let publicSlug = $state('');

  async function init() {
    phase = 'loading';
    // Herkese açık durum sayfası: /durum/<kısa-ad> veya sayfaya bağlı özel alan adı.
    // Özel alan adında yönetim API'si (ör. /api/auth/state) 404 verdiği için önce bu sorulur.
    const fromPath = publicSlugFromPath();
    if (fromPath !== null) {
      publicSlug = fromPath;
      phase = 'public';
      stripAppManifest();
      return;
    }
    try {
      const r = await api.publicResolve();
      if (r.slug) {
        publicSlug = r.slug;
        phase = 'public';
        stripAppManifest();
        return;
      }
    } catch {
      /* eski sunucu veya ağ hatası: yönetim paneliyle devam */
    }
    // Yönetim paneli: ana ekran uygulaması için servis çalışanı (yalnızca derlenmiş sürüm).
    pwa.register();
    try {
      const s = await api.authState();
      session.version = s.version;
      if (s.setup_needed) phase = 'setup';
      else if (!s.user) phase = 'login';
      else enter(s.user);
    } catch (e) {
      loadError = errorMessage(e);
      phase = 'error';
    }
  }

  function enter(u: User) {
    session.set(u);
    if (u.must_change_password) {
      live.stop();
      phase = 'password';
      return;
    }
    phase = 'app';
    live.start();
  }

  async function logout() {
    try {
      await api.logout();
    } catch {
      /* oturum zaten kapanmış olabilir */
    }
    live.stop();
    session.set(null);
    phase = 'login';
  }

  onUnauthorized(() => {
    if (phase !== 'app' && phase !== 'password') return;
    live.stop();
    session.set(null);
    phase = 'login';
    toast.info('Oturumunuz sona erdi. Lütfen tekrar giriş yapın.');
  });

  onPasswordChangeRequired(() => {
    if (phase !== 'app') return;
    live.stop();
    if (session.user) session.set({ ...session.user, must_change_password: true });
    phase = 'password';
  });

  onMount(init);

  interface NavItem {
    href: string;
    label: string;
    short?: string;
    icon: IconName;
    match: string[];
  }

  const NAV_MONITORS: NavItem = { href: '#/', label: 'Monitörler', icon: 'activity', match: ['list', 'new', 'detail', 'edit'] };
  const NAV_SERVERS: NavItem = { href: '#/servers', label: 'Sunucular', icon: 'server', match: ['servers', 'server'] };
  const NAV_INCIDENTS: NavItem = { href: '#/incidents', label: 'Olaylar', icon: 'zap', match: ['incidents', 'incident'] };
  const NAV_PAGES: NavItem = {
    href: '#/status-pages',
    label: 'Durum sayfaları',
    short: 'Sayfalar',
    icon: 'layout',
    match: ['pages', 'page-new', 'page-edit'],
  };
  const NAV_MAINT: NavItem = { href: '#/maintenance', label: 'Bakım', icon: 'wrench', match: ['maintenance', 'maint-new', 'maint-edit'] };
  const NAV_NOTIF: NavItem = { href: '#/notifications', label: 'Bildirimler', icon: 'bell', match: ['notifications'] };
  const NAV_SETTINGS: NavItem = { href: '#/settings', label: 'Ayarlar', icon: 'settings', match: ['settings'] };

  // İzleyici bildirim kanallarını ve durum sayfalarını yönetemez; yalnızca seçili
  // monitörleri gören (müşteri) izleyici Sunucular'ı kendisine sunucu atanmışsa görür.
  const nav = $derived<NavItem[]>(
    session.canEdit
      ? [NAV_MONITORS, NAV_SERVERS, NAV_INCIDENTS, NAV_PAGES, NAV_MAINT, NAV_NOTIF, NAV_SETTINGS]
      : session.restricted
        ? session.canSeeServers
          ? [NAV_MONITORS, NAV_SERVERS, NAV_INCIDENTS, NAV_MAINT, NAV_SETTINGS]
          : [NAV_MONITORS, NAV_INCIDENTS, NAV_MAINT, NAV_SETTINGS]
        : [NAV_MONITORS, NAV_SERVERS, NAV_INCIDENTS, NAV_MAINT, NAV_SETTINGS],
  );
  // Mobil sekme çubuğu en fazla 4 öğe: az kullanılanlar "Daha fazla" altında.
  const tabs = $derived<NavItem[]>(
    session.restricted && !session.canSeeServers
      ? nav
      : [
          NAV_MONITORS,
          NAV_SERVERS,
          NAV_INCIDENTS,
          {
            href: '#/more',
            label: 'Daha fazla',
            icon: 'menu',
            match: ['more', 'maintenance', 'maint-new', 'maint-edit', 'notifications', 'settings', 'pages', 'page-new', 'page-edit'],
          },
        ],
  );

  // Kenar çubuğundaki sorunlu sunucu sayısı için liste uygulama açılınca bir kez
  // yüklenir; sonrası canlı akışla güncellenir.
  $effect(() => {
    if (phase === 'app' && session.user && session.canSeeServers) untrack(() => servers.ensure());
  });

  const route = $derived(router.route);
  const downCount = $derived(live.monitors.reduce((n, m) => n + (m.active && m.status === 0 ? 1 : 0), 0));

  const pageTitle = $derived.by(() => {
    switch (route.name) {
      case 'list':
        return 'Monitörler';
      case 'new':
        return 'Yeni monitör';
      case 'detail':
      case 'edit':
        return live.byId(route.id)?.name ?? 'Monitör';
      case 'incidents':
        return 'Olaylar';
      case 'incident':
        return 'Olay';
      case 'notifications':
        return 'Bildirimler';
      case 'pages':
      case 'page-new':
      case 'page-edit':
      case 'page-preview':
        return 'Durum sayfaları';
      case 'maintenance':
      case 'maint-new':
      case 'maint-edit':
        return 'Bakım pencereleri';
      case 'settings':
        return 'Ayarlar';
      case 'servers':
        return 'Sunucular';
      case 'server':
        return servers.byId(route.id)?.name ?? 'Sunucu';
      case 'more':
        return 'Daha fazla';
      default:
        return 'Bulunamadı';
    }
  });

  $effect(() => {
    if (phase === 'public') return; // başlığı sayfanın kendisi belirler
    if (phase === 'app') document.title = `${downCount > 0 ? `(${downCount}) ` : ''}${pageTitle} · Uptime`;
    else if (phase === 'setup') document.title = 'Kurulum · Uptime';
    else if (phase === 'password') document.title = 'Yeni şifre · Uptime';
    else document.title = 'Giriş · Uptime';
  });

  // Ana ekran uygulamasında tarayıcının yenile düğmesi yok: üst çubukta elle yenileme.
  let syncing = $state(false);
  function resync() {
    if (syncing) return;
    syncing = true;
    live.resync();
    if (session.canSeeServers) servers.load();
    setTimeout(() => (syncing = false), 800);
  }

  const editorOnly = $derived(
    ['notifications', 'pages', 'page-new', 'page-edit', 'page-preview', 'new', 'edit', 'maint-new', 'maint-edit'].includes(route.name),
  );
</script>

<Tooltip />
<Toasts />
<ConfirmDialog />

{#if pwa.updateReady && phase !== 'public'}
  <div class="update" role="status">
    <Icon name="refresh" size={16} />
    <span>Yeni sürüm hazır</span>
    <button type="button" class="btn sm primary" onclick={() => pwa.applyUpdate()}>Yenile</button>
    <button type="button" class="btn sm ghost icon" aria-label="Sonra" onclick={() => pwa.dismissUpdate()}><Icon name="x" size={15} /></button>
  </div>
{/if}

{#snippet forbidden()}
  <div class="card empty">
    <div class="lock"><Icon name="lock" size={28} /></div>
    <h3>Bu sayfa için yetkiniz yok</h3>
    <p>Hesabınız yalnızca görüntüleme yetkisine sahip. Değişiklik yapmanız gerekiyorsa yöneticinize başvurun.</p>
    <a class="btn primary" href="#/">Monitörlere dön</a>
  </div>
{/snippet}

{#snippet noServers()}
  <div class="card empty">
    <div class="lock"><Icon name="lock" size={28} /></div>
    <h3>Sunucu takibi hesabınıza açık değil</h3>
    <p>Hesabınız yalnızca size atanan monitörleri görebilir. Sunucu metriklerine erişmeniz gerekiyorsa yöneticinize başvurun.</p>
    <a class="btn primary" href="#/">Monitörlere dön</a>
  </div>
{/snippet}

{#if phase === 'loading'}
  <div class="center"><span class="spinner"></span></div>
{:else if phase === 'public'}
  <PublicStatus slug={publicSlug} />
{:else if phase === 'error'}
  <div class="center">
    <div class="card errcard">
      <h2>Sunucuya bağlanılamadı</h2>
      <p class="muted">{loadError}</p>
      <button class="btn primary" onclick={init}>Tekrar dene</button>
    </div>
  </div>
{:else if phase === 'setup' || phase === 'login'}
  {#key phase}
    <Auth mode={phase} onDone={enter} />
  {/key}
{:else if phase === 'password'}
  <ForcePassword onDone={init} onLogout={logout} />
{:else if route.name === 'page-preview' && session.canEdit}
  {#key route.id}<PublicStatus previewId={route.id} />{/key}
{:else}
  <div class="shell">
    <aside class="sidebar">
      <a class="logo" href="#/"><span class="logo-dot"></span> Uptime</a>
      <nav aria-label="Ana menü">
        {#each nav as n (n.href)}
          <a href={n.href} class:active={n.match.includes(route.name)} aria-current={n.match.includes(route.name) ? 'page' : undefined}>
            <Icon name={n.icon} />
            <span>{n.label}</span>
            {#if n.href === '#/' && downCount > 0}<span class="count">{downCount}</span>{/if}
            {#if n.href === '#/servers' && servers.problems > 0}<span class="count">{servers.problems}</span>{/if}
          </a>
        {/each}
      </nav>
      <div class="side-foot">
        {#if !live.connected && live.loaded}
          <div class="offline" title="Canlı bağlantı yeniden kuruluyor"><Icon name="wifi-off" size={15} /> <span>Bağlantı yok</span></div>
        {/if}
        <a class="who" href="#/settings" title="Hesabım">
          <Icon name="user" size={16} />
          <span class="who-t">
            <span class="who-n">{session.displayName}</span>
            <span class="who-r">{ROLE_LABELS[session.role]}</span>
          </span>
        </a>
        <button class="logout" onclick={logout}><Icon name="logout" size={16} /> Çıkış</button>
      </div>
    </aside>

    <header class="topbar">
      <a class="logo" href="#/"><span class="logo-dot"></span> Uptime</a>
      <div class="top-right">
        {#if !live.connected && live.loaded}
          <span class="offline" title="Canlı bağlantı yeniden kuruluyor"><Icon name="wifi-off" size={14} /> Bağlantı yok</span>
        {/if}
        {#if pwa.standalone}
          <button class="btn ghost icon top-btn" class:spin={syncing} onclick={resync} aria-label="Verileri yenile">
            <Icon name="refresh" size={18} />
          </button>
        {/if}
        <button class="btn ghost top-btn" onclick={logout} aria-label="Çıkış"><Icon name="logout" size={16} /> Çıkış</button>
      </div>
    </header>

    <main class="main">
      <div class="content">
        {#if editorOnly && !session.canEdit}
          {@render forbidden()}
        {:else if (route.name === 'servers' || route.name === 'server') && !session.canSeeServers}
          {@render noServers()}
        {:else if route.name === 'servers'}
          <ServerList />
        {:else if route.name === 'server'}
          {#key route.id}<ServerDetail id={route.id} />{/key}
        {:else if route.name === 'list'}
          <MonitorList />
        {:else if route.name === 'new'}
          <MonitorForm />
        {:else if route.name === 'detail'}
          {#key route.id}<MonitorDetail id={route.id} />{/key}
        {:else if route.name === 'edit'}
          {#key route.id}<MonitorForm id={route.id} />{/key}
        {:else if route.name === 'incidents'}
          <Incidents />
        {:else if route.name === 'incident'}
          {#key route.id}<IncidentDetail id={route.id} />{/key}
        {:else if route.name === 'notifications'}
          <Notifications />
        {:else if route.name === 'pages'}
          <StatusPages />
        {:else if route.name === 'page-new'}
          <StatusPageEditor />
        {:else if route.name === 'page-edit'}
          {#key route.id}<StatusPageEditor id={route.id} />{/key}
        {:else if route.name === 'maintenance'}
          <MaintenanceList />
        {:else if route.name === 'maint-new'}
          <MaintenanceForm />
        {:else if route.name === 'maint-edit'}
          {#key route.id}<MaintenanceForm id={route.id} />{/key}
        {:else if route.name === 'settings'}
          <Settings tab={route.tab} />
        {:else if route.name === 'more'}
          <More onLogout={logout} />
        {:else}
          <div class="card empty">
            <h3>Sayfa bulunamadı</h3>
            <p>Aradığınız sayfa mevcut değil.</p>
            <a class="btn primary" href="#/">Monitörlere dön</a>
          </div>
        {/if}
      </div>
    </main>

    <nav class="tabbar" aria-label="Ana menü" style="--tabs:{tabs.length}">
      {#each tabs as n (n.href)}
        <a href={n.href} class:active={n.match.includes(route.name)} aria-current={n.match.includes(route.name) ? 'page' : undefined}>
          <span class="ti">
            <Icon name={n.icon} size={20} />
            {#if n.href === '#/' && downCount > 0}<span class="count">{downCount}</span>{/if}
            {#if n.href === '#/servers' && servers.problems > 0}<span class="count">{servers.problems}</span>{/if}
          </span>
          <span>{n.short ?? n.label}</span>
        </a>
      {/each}
    </nav>
  </div>
{/if}

<style>
  .center {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
  }
  .errcard {
    max-width: 420px;
    text-align: center;
  }
  .errcard h2 {
    margin-bottom: 8px;
  }
  .lock {
    display: flex;
    justify-content: center;
    color: var(--muted);
  }

  .shell {
    min-height: 100vh;
  }
  .sidebar {
    position: fixed;
    inset: 0 auto 0 0;
    width: var(--sidebar-w);
    background: var(--sidebar-bg);
    border-right: 1px solid var(--sidebar-border);
    display: flex;
    flex-direction: column;
    padding: 20px 14px;
    z-index: 20;
    overflow-y: auto;
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 1.25rem;
    font-weight: 800;
    color: var(--text);
    padding: 4px 10px 22px;
    text-decoration: none;
    letter-spacing: -0.01em;
  }
  .logo:hover {
    text-decoration: none;
  }
  .logo-dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--up);
    box-shadow: 0 0 0 4px var(--up-ring);
  }
  nav {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .sidebar nav a {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    border-radius: 9px;
    color: var(--text-2);
    font-weight: 600;
    font-size: 0.93rem;
    text-decoration: none;
  }
  @media (hover: hover) {
    .sidebar nav a:hover {
      background: var(--sidebar-hover);
      color: var(--text);
    }
    .logout:hover {
      color: var(--down-text-2);
    }
    .who:hover .who-n {
      text-decoration: underline;
    }
  }
  .sidebar nav a.active {
    background: var(--sidebar-active);
    color: var(--text);
  }
  .sidebar nav a:focus-visible {
    outline-offset: -2px;
  }
  .sidebar nav a.active :global(svg) {
    color: var(--up);
  }
  .count {
    margin-left: auto;
    min-width: 20px;
    height: 20px;
    padding: 0 6px;
    border-radius: 10px;
    background: var(--down);
    color: var(--on-down);
    font-size: 0.72rem;
    font-weight: 700;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .side-foot {
    margin-top: auto;
    border-top: 1px solid var(--sidebar-border);
    padding: 14px 8px 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .who {
    display: flex;
    align-items: center;
    gap: 9px;
    color: var(--text-2);
    font-size: 0.9rem;
    overflow: hidden;
    text-decoration: none;
    border-radius: 6px;
  }
  .who-t {
    display: flex;
    flex-direction: column;
    min-width: 0;
    line-height: 1.3;
  }
  .who-n {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text);
    font-weight: 600;
  }
  .who-r {
    font-size: 0.76rem;
    color: var(--muted);
  }
  .logout {
    display: flex;
    align-items: center;
    gap: 8px;
    background: none;
    border: none;
    color: var(--muted);
    font: inherit;
    font-size: 0.88rem;
    padding: 4px 0;
    cursor: pointer;
    text-align: left;
    border-radius: 6px;
  }
  .offline {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 26px;
    padding: 0 10px;
    margin-bottom: 4px;
    border-radius: 999px;
    background: var(--pending-soft);
    color: var(--pending);
    font-size: 0.78rem;
    font-weight: 600;
    white-space: nowrap;
    align-self: flex-start;
  }

  .topbar,
  .tabbar {
    display: none;
  }

  /* "Yeni sürüm hazır" çubuğu (servis çalışanı güncellemesi). */
  .update {
    position: fixed;
    left: calc(var(--sidebar-w) + 20px);
    bottom: 20px;
    z-index: 1050;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 8px 8px 14px;
    border-radius: 12px;
    background: var(--tip-bg);
    border: 1px solid var(--accent-border);
    box-shadow: var(--shadow);
    font-size: 0.9rem;
    font-weight: 600;
  }
  .update > :global(svg) {
    color: var(--accent-text);
  }

  .main {
    margin-left: var(--sidebar-w);
    min-height: 100vh;
  }
  .content {
    max-width: 1320px;
    margin: 0 auto;
    padding: 32px 36px 48px;
  }

  @media (max-width: 900px) {
    .sidebar {
      display: none;
    }
    .main {
      margin-left: 0;
    }
    .content {
      padding: 18px max(16px, env(safe-area-inset-right)) calc(88px + env(safe-area-inset-bottom)) max(16px, env(safe-area-inset-left));
    }
    /* Ana ekran uygulamasında (black-translucent durum çubuğu) içerik çentiğin
       altından başlar; yatay kenarlarda da güvenli alan bırakılır. */
    .topbar {
      position: sticky;
      top: 0;
      z-index: 20;
      display: flex;
      align-items: center;
      justify-content: space-between;
      height: calc(54px + env(safe-area-inset-top));
      padding: env(safe-area-inset-top) max(8px, env(safe-area-inset-right)) 0 max(6px, env(safe-area-inset-left));
      background: var(--topbar-bg);
      border-bottom: 1px solid var(--sidebar-border);
    }
    .top-btn {
      height: 44px;
      min-width: 44px;
      padding: 0 12px;
    }
    .top-btn.icon {
      padding: 0;
    }
    .top-btn.spin :global(svg) {
      animation: spin 0.8s linear infinite;
    }
    .update {
      left: max(12px, env(safe-area-inset-left));
      right: max(12px, env(safe-area-inset-right));
      bottom: calc(72px + env(safe-area-inset-bottom));
    }
    .update span {
      flex: 1;
    }
    .topbar .logo {
      padding: 0 10px;
      font-size: 1.1rem;
    }
    .top-right {
      display: flex;
      align-items: center;
      gap: 6px;
    }
    .top-right .offline {
      height: 24px;
      margin: 0;
      font-size: 0.72rem;
    }
    .tabbar {
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 20;
      display: grid;
      grid-template-columns: repeat(var(--tabs), minmax(0, 1fr));
      background: var(--tabbar-bg);
      backdrop-filter: blur(8px);
      border-top: 1px solid var(--border);
      padding: 0 env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left);
    }
    .tabbar a {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 3px;
      min-height: 52px;
      padding: 9px 2px 8px;
      color: var(--muted);
      font-size: 0.7rem;
      font-weight: 600;
      text-decoration: none;
      white-space: nowrap;
      min-width: 0;
    }
    .tabbar a > span:last-child {
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .tabbar a:focus-visible {
      outline-offset: -2px;
      border-radius: 8px;
    }
    .tabbar a.active {
      color: var(--text);
    }
    .tabbar a.active :global(svg) {
      color: var(--up);
    }
    .ti {
      position: relative;
      display: inline-flex;
    }
    .ti .count {
      position: absolute;
      top: -6px;
      right: -12px;
      height: 16px;
      min-width: 16px;
      font-size: 0.62rem;
      padding: 0 4px;
    }
  }
</style>
