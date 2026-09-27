<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, onUnauthorized, type User } from './lib/api';
  import { live } from './lib/live.svelte';
  import { router } from './lib/router.svelte';
  import { toast } from './lib/ui.svelte';
  import Icon, { type IconName } from './components/Icon.svelte';
  import Toasts from './components/Toasts.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import Tooltip from './components/Tooltip.svelte';
  import Auth from './pages/Auth.svelte';
  import MonitorList from './pages/MonitorList.svelte';
  import MonitorDetail from './pages/MonitorDetail.svelte';
  import MonitorForm from './pages/MonitorForm.svelte';
  import Incidents from './pages/Incidents.svelte';
  import Notifications from './pages/Notifications.svelte';
  import Settings from './pages/Settings.svelte';

  type Phase = 'loading' | 'setup' | 'login' | 'app' | 'error';

  let phase = $state<Phase>('loading');
  let user = $state<User | null>(null);
  let version = $state('');
  let loadError = $state('');

  async function init() {
    phase = 'loading';
    try {
      const s = await api.authState();
      version = s.version;
      if (s.setup_needed) phase = 'setup';
      else if (!s.user) phase = 'login';
      else enter(s.user);
    } catch (e) {
      loadError = errorMessage(e);
      phase = 'error';
    }
  }

  function enter(u: User) {
    user = u;
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
    user = null;
    phase = 'login';
  }

  onUnauthorized(() => {
    if (phase !== 'app') return;
    live.stop();
    user = null;
    phase = 'login';
    toast.info('Oturumunuz sona erdi. Lütfen tekrar giriş yapın.');
  });

  onMount(init);

  const nav: { href: string; label: string; icon: IconName; match: string[] }[] = [
    { href: '#/', label: 'Monitörler', icon: 'activity', match: ['list', 'new', 'detail', 'edit'] },
    { href: '#/incidents', label: 'Olaylar', icon: 'zap', match: ['incidents'] },
    { href: '#/notifications', label: 'Bildirimler', icon: 'bell', match: ['notifications'] },
    { href: '#/settings', label: 'Ayarlar', icon: 'settings', match: ['settings'] },
  ];

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
      case 'notifications':
        return 'Bildirimler';
      case 'settings':
        return 'Ayarlar';
      default:
        return 'Bulunamadı';
    }
  });

  $effect(() => {
    if (phase === 'app') document.title = `${downCount > 0 ? `(${downCount}) ` : ''}${pageTitle} · Uptime`;
    else if (phase === 'setup') document.title = 'Kurulum · Uptime';
    else document.title = 'Giriş · Uptime';
  });
</script>

<Tooltip />
<Toasts />
<ConfirmDialog />

{#if phase === 'loading'}
  <div class="center"><span class="spinner"></span></div>
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
{:else}
  <div class="shell">
    <aside class="sidebar">
      <a class="logo" href="#/"><span class="logo-dot"></span> Uptime</a>
      <nav>
        {#each nav as n (n.href)}
          <a href={n.href} class:active={n.match.includes(route.name)}>
            <Icon name={n.icon} />
            <span>{n.label}</span>
            {#if n.href === '#/' && downCount > 0}<span class="count">{downCount}</span>{/if}
          </a>
        {/each}
      </nav>
      <div class="side-foot">
        <div class="who"><Icon name="user" size={16} /> <span>{user?.username}</span></div>
        <button class="logout" onclick={logout}><Icon name="logout" size={16} /> Çıkış</button>
      </div>
    </aside>

    <header class="topbar">
      <a class="logo" href="#/"><span class="logo-dot"></span> Uptime</a>
      <div class="top-right">
        {#if !live.connected && live.loaded}<span class="offline" title="Canlı bağlantı yeniden kuruluyor">Bağlantı yok</span>{/if}
        <button class="btn ghost sm" onclick={logout} aria-label="Çıkış"><Icon name="logout" size={16} /> Çıkış</button>
      </div>
    </header>

    <main class="main">
      <div class="content">
        {#if route.name === 'list'}
          <MonitorList />
        {:else if route.name === 'new'}
          <MonitorForm />
        {:else if route.name === 'detail'}
          {#key route.id}<MonitorDetail id={route.id} />{/key}
        {:else if route.name === 'edit'}
          {#key route.id}<MonitorForm id={route.id} />{/key}
        {:else if route.name === 'incidents'}
          <Incidents />
        {:else if route.name === 'notifications'}
          <Notifications />
        {:else if route.name === 'settings'}
          <Settings {version} username={user?.username ?? ''} />
        {:else}
          <div class="card empty">
            <h3>Sayfa bulunamadı</h3>
            <p>Aradığınız sayfa mevcut değil.</p>
            <a class="btn primary" href="#/">Monitörlere dön</a>
          </div>
        {/if}
      </div>
    </main>

    <nav class="tabbar">
      {#each nav as n (n.href)}
        <a href={n.href} class:active={n.match.includes(route.name)}>
          <span class="ti">
            <Icon name={n.icon} size={20} />
            {#if n.href === '#/' && downCount > 0}<span class="count">{downCount}</span>{/if}
          </span>
          <span>{n.label}</span>
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
    box-shadow: 0 0 0 4px rgba(59, 214, 113, 0.2);
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
  .sidebar nav a:hover {
    background: rgba(255, 255, 255, 0.05);
    color: var(--text);
  }
  .sidebar nav a.active {
    background: var(--sidebar-active);
    color: #fff;
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
    color: #fff;
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
    gap: 8px;
    color: var(--text-2);
    font-size: 0.9rem;
    overflow: hidden;
  }
  .who span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
  }
  .logout:hover {
    color: var(--down);
  }

  .topbar,
  .tabbar {
    display: none;
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
      padding: 18px 16px calc(88px + env(safe-area-inset-bottom));
    }
    .topbar {
      position: sticky;
      top: 0;
      z-index: 20;
      display: flex;
      align-items: center;
      justify-content: space-between;
      height: 54px;
      padding: 0 8px 0 6px;
      background: linear-gradient(90deg, #0f2a26, #111a33);
      border-bottom: 1px solid var(--sidebar-border);
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
    .offline {
      font-size: 0.75rem;
      color: var(--pending);
    }
    .tabbar {
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 20;
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      background: rgba(13, 20, 33, 0.96);
      backdrop-filter: blur(8px);
      border-top: 1px solid var(--border);
      padding-bottom: env(safe-area-inset-bottom);
    }
    .tabbar a {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 3px;
      padding: 9px 2px 8px;
      color: var(--muted);
      font-size: 0.7rem;
      font-weight: 600;
      text-decoration: none;
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
