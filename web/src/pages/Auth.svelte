<script lang="ts">
  import { tick } from 'svelte';
  import { api, ApiError, errorMessage, type AuthState, type User } from '../lib/api';
  import { tOr } from '../lib/i18n';
  import Icon from '../components/Icon.svelte';
  import LangSwitch from '../components/LangSwitch.svelte';
  import { t } from '../lib/i18n';
  import { APP_NAME } from '../lib/brand';

  let {
    mode,
    info = {},
    onDone,
  }: { mode: 'setup' | 'login'; info?: Pick<AuthState, 'password_reset' | 'oidc'>; onDone: (u: User) => void } = $props();

  // Şifremi unuttum / sıfırlama / SSO (E-13). Sıfırlama bağlantısı #/reset/<token>;
  // SSO hatası #/login?sso_error=<kod> ile gelir.
  type View = 'login' | 'forgot' | 'sent' | 'reset' | 'resetDone';
  const hashReset = location.hash.match(/^#\/reset\/([A-Za-z0-9_-]+)/);
  const ssoErr = location.hash.match(/[?&]sso_error=([a-z_]+)/)?.[1] ?? '';
  // svelte-ignore state_referenced_locally
  let view = $state<View>(mode === 'login' && hashReset ? 'reset' : 'login');
  let resetToken = hashReset?.[1] ?? '';
  let forgotLogin = $state('');
  let newPw = $state('');
  let newPw2 = $state('');
  if (hashReset || ssoErr) history.replaceState(null, '', '#/');
  const sso = $derived(info.oidc?.enabled ? info.oidc : null);
  // SSO açık ve şifre formu gizliyse form bağlantıyla açılır.
  // svelte-ignore state_referenced_locally
  let showLocal = $state(!(info.oidc?.enabled && info.oidc.local_login === false));
  const ssoHref = '/api/auth/oidc/start';

  async function submitForgot(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (!forgotLogin.trim()) return (error = t('auth.errCredentialsRequired'));
    busy = true;
    try {
      await api.forgotPassword(forgotLogin.trim());
      view = 'sent';
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function submitReset(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (newPw.length < 8) return (error = t('auth.errPasswordLength'));
    if (newPw !== newPw2) return (error = t('auth.errPasswordMismatch'));
    busy = true;
    try {
      const r = await api.resetPassword(resetToken, newPw);
      username = r.username;
      newPw = newPw2 = '';
      view = 'resetDone';
    } catch (err) {
      error = err instanceof ApiError && err.code === 'reset_invalid' ? t('auth.resetInvalid') : errorMessage(err);
    } finally {
      busy = false;
    }
  }

  let username = $state('');
  let password = $state('');
  let password2 = $state('');
  let error = $state(ssoErr ? tOr(`auth.ssoError.${ssoErr}`, t('auth.ssoError.unknown')) : '');
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
        error = t('auth.errUsername');
        return;
      }
      if (password.length < 8) {
        error = t('auth.errPasswordLength');
        return;
      }
      if (password !== password2) {
        error = t('auth.errPasswordMismatch');
        return;
      }
    } else if (!u || !password) {
      error = t('auth.errCredentialsRequired');
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
      error = t('auth.errCodeRequired');
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
  <div class="brand"><span class="logo-dot"></span> {APP_NAME}</div>
  {#if challenge}
    <form class="card auth" onsubmit={submitCode} novalidate>
      <div class="shield"><Icon name="shield-check" size={26} /></div>
      <h1>{t('auth.codeTitle')}<span class="dot">.</span></h1>
      <p class="muted intro">
        {useRecovery ? t('auth.codeIntroRecovery') : t('auth.codeIntroApp')}
      </p>
      <div class="field">
        <label for="code">{useRecovery ? t('auth.recoveryCodeLabel') : t('auth.codeLabel')}</label>
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
        {useRecovery ? t('auth.useAppCode') : t('auth.useRecoveryCode')}
      </button>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
      <button class="btn primary big" type="submit" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if}
        {t('auth.verifyAndLogin')}
      </button>
      <button type="button" class="linkbtn backlink" onclick={back}><Icon name="chevron-left" size={15} /> {t('auth.otherAccount')}</button>
    </form>
  {:else if view === 'forgot' || view === 'sent'}
    <form class="card auth" onsubmit={submitForgot} novalidate>
      <div class="shield"><Icon name="key" size={26} /></div>
      <h1>{t('auth.forgotTitle')}<span class="dot">.</span></h1>
      {#if view === 'sent'}
        <div class="alert success" role="status">{t('auth.forgotSent')}</div>
      {:else}
        <p class="muted intro">{t('auth.forgotIntro')}</p>
        <div class="field">
          <label for="fl">{t('auth.forgotLogin')}</label>
          <!-- svelte-ignore a11y_autofocus -->
          <input id="fl" class="input" bind:value={forgotLogin} autocomplete="username" autocapitalize="none" spellcheck="false" autofocus maxlength="254" />
        </div>
        {#if error}<div class="alert error" role="alert">{error}</div>{/if}
        <button class="btn primary big" type="submit" disabled={busy}>
          {#if busy}<span class="spinner"></span>{/if}
          {t('auth.forgotSend')}
        </button>
      {/if}
      <button type="button" class="linkbtn backlink" onclick={() => { view = 'login'; error = ''; }}><Icon name="chevron-left" size={15} /> {t('auth.backToLogin')}</button>
    </form>
  {:else if view === 'reset' || view === 'resetDone'}
    <form class="card auth" onsubmit={submitReset} novalidate>
      <div class="shield"><Icon name="key" size={26} /></div>
      <h1>{t('auth.resetTitle')}<span class="dot">.</span></h1>
      {#if view === 'resetDone'}
        <div class="alert success" role="status">{t('auth.resetDone')}</div>
        <button type="button" class="btn primary big" onclick={() => { view = 'login'; error = ''; }}>{t('auth.login')}</button>
      {:else}
        <p class="muted intro">{t('auth.resetIntro')}</p>
        <div class="field">
          <label for="np">{t('auth.force.newPassword')}</label>
          <!-- svelte-ignore a11y_autofocus -->
          <input id="np" class="input" type="password" bind:value={newPw} autocomplete="new-password" autofocus />
          <span class="help">{t('common.minChars', { n: 8 })}</span>
        </div>
        <div class="field">
          <label for="np2">{t('auth.force.newPasswordAgain')}</label>
          <input id="np2" class="input" type="password" bind:value={newPw2} autocomplete="new-password" />
        </div>
        {#if error}<div class="alert error" role="alert">{error}</div>{/if}
        <button class="btn primary big" type="submit" disabled={busy}>
          {#if busy}<span class="spinner"></span>{/if}
          {t('auth.resetSubmit')}
        </button>
        <button type="button" class="linkbtn backlink" onclick={() => { view = 'login'; error = ''; }}><Icon name="chevron-left" size={15} /> {t('auth.backToLogin')}</button>
      {/if}
    </form>
  {:else}
    <form class="card auth" onsubmit={submit} novalidate>
      {#if mode === 'setup'}
        <h1>{t('auth.setupTitle')}<span class="dot">.</span></h1>
        <p class="muted intro">{t('auth.setupIntro')}</p>
      {:else}
        <h1>{t('auth.loginTitle')}<span class="dot">.</span></h1>
        <p class="muted intro">{t('auth.loginIntro')}</p>
      {/if}

      {#if mode === 'login' && sso}
        <a class="btn big sso" href={ssoHref} data-sveltekit-reload><Icon name="log-in" size={17} /> {t('auth.sso', { name: sso.name })}</a>
        {#if !showLocal}
          {#if error}<div class="alert error" role="alert">{error}</div>{/if}
          <button type="button" class="linkbtn toggle local" onclick={() => (showLocal = true)}>{t('auth.ssoLocal')}</button>
        {:else}
          <div class="or"><span>{t('auth.ssoOr')}</span></div>
        {/if}
      {/if}

      {#if mode === 'setup' || showLocal}
      <div class="field">
        <label for="u">{t('common.username')}</label>
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
        {#if mode === 'setup'}<span class="help">{t('auth.usernameHelp')}</span>{/if}
      </div>
      <div class="field">
        <label for="p">{t('common.password')}</label>
        <input
          id="p"
          class="input"
          type="password"
          bind:value={password}
          autocomplete={mode === 'setup' ? 'new-password' : 'current-password'}
        />
        {#if mode === 'setup'}<span class="help">{t('common.minChars', { n: 8 })}</span>{/if}
      </div>
      {#if mode === 'setup'}
        <div class="field">
          <label for="p2">{t('auth.passwordAgain')}</label>
          <input id="p2" class="input" type="password" bind:value={password2} autocomplete="new-password" />
        </div>
      {/if}

      {#if error}<div class="alert error" role="alert">{error}</div>{/if}

      <button class="btn primary big" type="submit" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if}
        {mode === 'setup' ? t('auth.createAccount') : t('auth.login')}
      </button>
      {#if mode === 'login' && info.password_reset}
        <button type="button" class="linkbtn backlink" onclick={() => { view = 'forgot'; error = ''; }}>{t('auth.forgot')}</button>
      {/if}
      {/if}
    </form>
  {/if}
  <LangSwitch />
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
  .sso {
    height: 44px;
    gap: 10px;
  }
  .or {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--muted);
    font-size: 0.8rem;
    margin: -4px 0;
  }
  .or::before,
  .or::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--border);
  }
  .toggle.local {
    align-self: center;
    margin: 0;
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
