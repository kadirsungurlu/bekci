<script lang="ts">
  // Yönetici geçici şifre verdiğinde: yeni şifre belirlenmeden başka hiçbir şey açılmaz.
  import { api, errorMessage } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { toast } from '../lib/ui.svelte';
  import Icon from '../components/Icon.svelte';

  let { onDone, onLogout }: { onDone: () => void; onLogout: () => void } = $props();

  let current = $state('');
  let next = $state('');
  let next2 = $state('');
  let error = $state('');
  let busy = $state(false);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (!current) return (error = 'Size verilen geçici şifreyi girin.');
    if (next.length < 8) return (error = 'Yeni şifre en az 8 karakter olmalı.');
    if (next === current) return (error = 'Yeni şifre geçici şifreden farklı olmalı.');
    if (next !== next2) return (error = 'Yeni şifreler birbiriyle aynı değil.');
    busy = true;
    try {
      await api.changePassword(current, next);
      toast.success('Şifreniz belirlendi. Hoş geldiniz!');
      onDone();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="wrap">
  <div class="brand"><span class="logo-dot"></span> Uptime</div>
  <form class="card auth" onsubmit={submit} novalidate>
    <div class="ic"><Icon name="lock" size={24} /></div>
    <h1>Yeni şifre belirleyin<span class="dot">.</span></h1>
    <p class="muted intro">
      Hesabınıza geçici bir şifreyle giriş yaptınız{session.user ? ` (${session.user.username})` : ''}. Devam etmeden önce yalnızca
      sizin bildiğiniz yeni bir şifre belirleyin.
    </p>
    <input type="text" name="username" autocomplete="username" value={session.user?.username ?? ''} hidden readonly />
    <div class="field">
      <label for="fp-cur">Geçici şifre</label>
      <!-- svelte-ignore a11y_autofocus -->
      <input id="fp-cur" class="input" type="password" autocomplete="current-password" bind:value={current} autofocus />
    </div>
    <div class="field">
      <label for="fp-new">Yeni şifre</label>
      <input id="fp-new" class="input" type="password" autocomplete="new-password" bind:value={next} />
      <span class="help">En az 8 karakter.</span>
    </div>
    <div class="field">
      <label for="fp-new2">Yeni şifre (tekrar)</label>
      <input id="fp-new2" class="input" type="password" autocomplete="new-password" bind:value={next2} />
    </div>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    <button class="btn primary big" type="submit" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      Şifreyi kaydet ve devam et
    </button>
    <button type="button" class="linkbtn out" onclick={onLogout}>Çıkış yap</button>
  </form>
</div>

<style>
  .wrap {
    min-height: 100vh;
    min-height: 100dvh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 24px 16px;
    background:
      radial-gradient(900px 500px at 15% -10%, var(--auth-glow-1), transparent 60%),
      radial-gradient(800px 500px at 110% 110%, var(--auth-glow-2), transparent 60%),
      var(--bg);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 1.35rem;
    font-weight: 800;
    margin-bottom: 20px;
  }
  .logo-dot {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--up);
    box-shadow: 0 0 0 5px var(--up-ring);
  }
  .auth {
    width: 100%;
    max-width: 420px;
    padding: 28px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    box-shadow: var(--shadow);
  }
  .ic {
    width: 48px;
    height: 48px;
    border-radius: 12px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--pending-soft);
    color: var(--pending);
  }
  .intro {
    margin: -6px 0 4px;
    font-size: 0.92rem;
  }
  .big {
    height: 44px;
    margin-top: 4px;
  }
  .out {
    align-self: center;
    font-size: 0.88rem;
    color: var(--muted);
  }
  @media (max-width: 640px) {
    .auth {
      padding: 22px 18px;
    }
  }
</style>
