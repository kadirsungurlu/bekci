<script lang="ts">
  import { tick } from 'svelte';
  import { api, ApiError, errorMessage, type User } from '../lib/api';
  import Icon from '../components/Icon.svelte';

  let { mode, onDone }: { mode: 'setup' | 'login'; onDone: (u: User) => void } = $props();

  let username = $state('');
  let password = $state('');
  let password2 = $state('');
  let error = $state('');
  let busy = $state(false);

  // İki adımlı doğrulama adımı
  let challenge = $state('');
  let code = $state('');
  let codeInput: HTMLInputElement | undefined = $state();
  // Telefon yoksa kurtarma koduyla giriş: klavye ve alan biçimi değişir.
  let useRecovery = $state(false);

  async function toggleRecovery() {
    useRecovery = !useRecovery;
    code = '';
    error = '';
    await tick();
    codeInput?.focus();
  }

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
      if (mode === 'setup') {
        onDone((await api.setup(u, password)).user);
        return;
      }
      const res = await api.login(u, password);
      if (res.two_factor_required) {
        challenge = res.challenge;
        code = '';
        useRecovery = false;
        password = '';
        await tick();
        codeInput?.focus();
        return;
      }
      onDone(res.user);
    } catch (err) {
      error = errorMessage(err);
      password = '';
      password2 = '';
    } finally {
      busy = false;
    }
  }

  async function submitCode(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const c = code.trim();
    if (!c) {
      error = 'Doğrulama kodunu girin.';
      return;
    }
    busy = true;
    try {
      const res = await api.login2fa(challenge, c);
      onDone(res.user);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'challenge_expired') {
        // Giriş süresi doldu veya çok fazla hatalı deneme: şifre adımına dön.
        challenge = '';
      }
      error = errorMessage(err);
      code = '';
    } finally {
      busy = false;
    }
  }

  function back() {
    challenge = '';
    code = '';
    error = '';
  }
</script>

<div class="wrap">
  <div class="brand"><span class="logo-dot"></span> Uptime</div>
  {#if challenge}
    <form class="card auth" onsubmit={submitCode} novalidate>
      <div class="shield"><Icon name="shield-check" size={26} /></div>
      <h1>Doğrulama kodu<span class="dot">.</span></h1>
      <p class="muted intro">
        {useRecovery ? 'Kaydettiğiniz kurtarma kodlarından birini girin. Her kod bir kez kullanılabilir.' : 'Doğrulama uygulamanızdaki 6 haneli kodu girin.'}
      </p>
      <div class="field">
        <label for="code">{useRecovery ? 'Kurtarma kodu' : 'Doğrulama kodu'}</label>
        {#if useRecovery}
          <input
            id="code"
            class="input code"
            bind:this={codeInput}
            bind:value={code}
            autocomplete="off"
            inputmode="text"
            autocapitalize="none"
            autocorrect="off"
            spellcheck="false"
            maxlength="32"
            placeholder="abcde-fghij"
          />
        {:else}
          <input
            id="code"
            class="input code"
            bind:this={codeInput}
            bind:value={code}
            autocomplete="one-time-code"
            inputmode="numeric"
            pattern="[0-9 ]*"
            autocorrect="off"
            spellcheck="false"
            maxlength="7"
            placeholder="123456"
          />
        {/if}
      </div>
      <button type="button" class="linkbtn toggle" onclick={toggleRecovery}>
        {useRecovery ? 'Doğrulama uygulamasındaki kodu kullan' : 'Telefonuma erişemiyorum — kurtarma kodu kullan'}
      </button>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
      <button class="btn primary big" type="submit" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if}
        Doğrula ve giriş yap
      </button>
      <button type="button" class="linkbtn backlink" onclick={back}><Icon name="chevron-left" size={15} /> Farklı hesapla giriş yap</button>
    </form>
  {:else}
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
  {/if}
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
  .shield {
    width: 48px;
    height: 48px;
    border-radius: 12px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent-text);
  }
  .code {
    font-family: var(--mono);
    font-size: 1.15rem;
    letter-spacing: 0.12em;
    height: 46px;
  }
  .backlink {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    align-self: center;
    font-size: 0.88rem;
    min-height: 44px;
  }
  .toggle {
    align-self: flex-start;
    font-size: 0.86rem;
    min-height: 44px;
    margin: -10px 0 -6px;
    text-align: left;
  }
  @media (max-width: 640px) {
    .auth {
      padding: 22px 18px;
    }
  }
</style>
