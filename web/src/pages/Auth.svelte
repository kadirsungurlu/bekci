<script lang="ts">
  import { api, errorMessage, type User } from '../lib/api';

  let { mode, onDone }: { mode: 'setup' | 'login'; onDone: (u: User) => void } = $props();

  let username = $state('');
  let password = $state('');
  let password2 = $state('');
  let error = $state('');
  let busy = $state(false);

  const USERNAME_RE = /^[a-zA-Z0-9._-]{3,32}$/;

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const u = username.trim();
    if (mode === 'setup') {
      if (!USERNAME_RE.test(u)) {
        error = 'Kullanıcı adı 3-32 karakter olmalı; harf, rakam, nokta, tire ve alt çizgi kullanılabilir.';
        return;
      }
      if (password.length < 8) {
        error = 'Şifre en az 8 karakter olmalı.';
        return;
      }
      if (password !== password2) {
        error = 'Şifreler eşleşmiyor.';
        return;
      }
    } else if (!u || !password) {
      error = 'Kullanıcı adı ve şifre gerekli.';
      return;
    }
    busy = true;
    try {
      const res = mode === 'setup' ? await api.setup(u, password) : await api.login(u, password);
      onDone(res.user);
    } catch (err) {
      error = errorMessage(err);
      password = '';
      password2 = '';
    } finally {
      busy = false;
    }
  }
</script>

<div class="wrap">
  <div class="brand"><span class="logo-dot"></span> Uptime</div>
  <form class="card auth" onsubmit={submit} novalidate>
    {#if mode === 'setup'}
      <h1>Hoş geldiniz<span class="dot">.</span></h1>
      <p class="muted intro">İlk kurulum: yönetici hesabınızı oluşturun. Bu hesapla giriş yapıp monitörlerinizi yöneteceksiniz.</p>
    {:else}
      <h1>Giriş yap<span class="dot">.</span></h1>
      <p class="muted intro">Devam etmek için hesabınızla giriş yapın.</p>
    {/if}

    <div class="field">
      <label for="u">Kullanıcı adı</label>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        id="u"
        class="input"
        bind:value={username}
        autocomplete="username"
        autocapitalize="none"
        spellcheck="false"
        autofocus
        maxlength="32"
      />
      {#if mode === 'setup'}<span class="help">3-32 karakter: harf, rakam, nokta, tire, alt çizgi.</span>{/if}
    </div>
    <div class="field">
      <label for="p">Şifre</label>
      <input
        id="p"
        class="input"
        type="password"
        bind:value={password}
        autocomplete={mode === 'setup' ? 'new-password' : 'current-password'}
      />
      {#if mode === 'setup'}<span class="help">En az 8 karakter.</span>{/if}
    </div>
    {#if mode === 'setup'}
      <div class="field">
        <label for="p2">Şifre (tekrar)</label>
        <input id="p2" class="input" type="password" bind:value={password2} autocomplete="new-password" />
      </div>
    {/if}

    {#if error}<div class="alert error" role="alert">{error}</div>{/if}

    <button class="btn primary big" type="submit" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {mode === 'setup' ? 'Hesabı oluştur' : 'Giriş yap'}
    </button>
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
    max-width: 400px;
    padding: 28px;
    display: flex;
    flex-direction: column;
    gap: 16px;
    box-shadow: var(--shadow);
  }
  .intro {
    margin: -6px 0 4px;
    font-size: 0.92rem;
  }
  .big {
    height: 44px;
    margin-top: 4px;
  }
  @media (max-width: 640px) {
    .auth {
      padding: 22px 18px;
    }
  }
</style>
