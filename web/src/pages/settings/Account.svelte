<script lang="ts">
  import { api, errorMessage } from '../../lib/api';
  import { ROLE_LABELS, session } from '../../lib/session.svelte';
  import { toast } from '../../lib/ui.svelte';
  import { live } from '../../lib/live.svelte';
  import { browserLocale, forgetLocale, isLocale, LOCALES, setLocale, t, type Locale } from '../../lib/i18n';
  import TwoFactor from './TwoFactor.svelte';
  import { onMount } from 'svelte';
  import { guardUnsaved } from '../../lib/forms';
  import ApiKeys from './ApiKeys.svelte';
  import Modal from '../../components/Modal.svelte';
  import Icon from '../../components/Icon.svelte';

  // E-posta (şifre sıfırlama adresi): şifre onayıyla değiştirilir.
  let emailOpen = $state(false);
  let emailNew = $state('');
  let emailPw = $state('');
  let emailBusy = $state(false);
  let emailError = $state('');
  function openEmail() {
    emailNew = session.user?.email ?? '';
    emailPw = '';
    emailError = '';
    emailOpen = true;
  }
  async function saveEmail(e: SubmitEvent | null, remove = false) {
    e?.preventDefault();
    emailError = '';
    const v = remove ? '' : emailNew.trim();
    if (!remove && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v)) return (emailError = t('users.form.errEmail'));
    if (!session.user?.oidc && !emailPw) return (emailError = t('account.twoFactor.errPassword'));
    emailBusy = true;
    try {
      const res = await api.changeEmail(emailPw, v);
      session.set(res.user);
      emailOpen = false;
      toast.success(v ? t('account.email.saved') : t('account.email.removed'));
    } catch (err) {
      emailError = errorMessage(err);
    } finally {
      emailBusy = false;
    }
  }

  let current = $state('');
  let next = $state('');
  let next2 = $state('');
  let pwError = $state('');
  let pwBusy = $state(false);

  // Şifre formuna yazılmışken sayfadan ayrılırken sorulur.
  onMount(() => guardUnsaved(() => !pwBusy && !!(current || next || next2)));

  async function changePassword(e: SubmitEvent) {
    e.preventDefault();
    pwError = '';
    if (!current) return (pwError = t('account.errCurrent'));
    if (next.length < 8) return (pwError = t('account.errLength'));
    if (next !== next2) return (pwError = t('account.errMismatch'));
    pwBusy = true;
    try {
      await api.changePassword(current, next);
      current = next = next2 = '';
      toast.success(t('account.passwordChanged'));
    } catch (err) {
      pwError = errorMessage(err);
    } finally {
      pwBusy = false;
    }
  }

  // Dil tercihi: "" = tarayıcı dili; hesapta saklanır (her cihazda geçerli).
  const langPref = $derived<'' | Locale>(isLocale(session.user?.lang) ? session.user.lang : '');
  let langBusy = $state(false);

  async function changeLang(e: Event) {
    const v = (e.currentTarget as HTMLSelectElement).value;
    const lang: '' | Locale = isLocale(v) ? v : '';
    langBusy = true;
    try {
      const res = await api.setPreferences({ lang });
      session.set(res.user);
      if (lang) setLocale(lang);
      else forgetLocale();
      // Sunucudan gelen metinler (kontrol mesajları, olaylar) de yeni dilde gelsin.
      live.relocalize();
      toast.success(t('account.language.saved'));
    } catch (err) {
      toast.error(errorMessage(err));
      (e.currentTarget as HTMLSelectElement).value = langPref;
    } finally {
      langBusy = false;
    }
  }
</script>

<!-- Üst sıra: hesap özeti, dil ve iki adımlı doğrulama yan yana (benzer
     yükseklikte kartlar); altında şifre değiştirme tek satırlık form. Geniş
     ekranda boş sütun kalmaz. -->
<div class="top3">
  <div class="card">
    <h2 class="card-title">{t('account.account')}</h2>
    <dl>
      <dt>{t('common.username')}</dt>
      <dd>{session.user?.username}</dd>
      {#if session.user?.display_name}
        <dt>{t('account.displayName')}</dt>
        <dd>{session.user.display_name}</dd>
      {/if}
      <dt>{t('account.role')}</dt>
      <dd>
        {ROLE_LABELS[session.role]}{session.user && !session.user.all_monitors
          ? session.canSeeServers
            ? t('account.onlySelectedMonitorsServers')
            : t('account.onlySelectedMonitors')
          : ''}
      </dd>
      <dt>{t('account.email.title')}</dt>
      <dd class="email-dd">
        <span class:muted={!session.user?.email}>{session.user?.email || t('account.email.none')}</span>
        <button type="button" class="linkbtn" onclick={openEmail}>{session.user?.email ? t('account.email.change') : t('account.email.add')}</button>
      </dd>
      {#if session.user?.oidc}
        <dt></dt>
        <dd><span class="badge accent"><Icon name="log-in" size={12} /> {t('account.email.sso')}</span></dd>
      {/if}
      <dt>{t('common.version')}</dt>
      <dd class="mono">{session.version || '—'}</dd>
    </dl>
  </div>
  <div class="card stack">
    <h2 class="card-title">{t('account.language.title')}</h2>
    <div class="field">
      <label for="ui-lang">{t('account.language.label')}</label>
      <select id="ui-lang" class="input" value={langPref} onchange={changeLang} disabled={langBusy}>
        <option value="">{t('account.language.auto', { lang: t(`common.languages.${browserLocale()}`) })}</option>
        {#each LOCALES as l (l)}
          <option value={l} lang={l}>{t(`common.languages.${l}`)}</option>
        {/each}
      </select>
      <span class="help">{t('account.language.help')}</span>
    </div>
  </div>
  <TwoFactor />
</div>

<form class="card stack pw" onsubmit={changePassword} novalidate>
  <h2 class="card-title">{t('account.changePassword')}</h2>
  <input type="text" name="username" autocomplete="username" value={session.user?.username ?? ''} hidden readonly />
  <div class="pw-row">
    <div class="field">
      <label for="cur">{t('account.currentPassword')}</label>
      <input id="cur" class="input" type="password" autocomplete="current-password" bind:value={current} />
    </div>
    <div class="field">
      <label for="new">{t('account.newPassword')}</label>
      <input id="new" class="input" type="password" autocomplete="new-password" bind:value={next} />
      <span class="help">{t('common.minChars', { n: 8 })}</span>
    </div>
    <div class="field">
      <label for="new2">{t('account.newPasswordAgain')}</label>
      <input id="new2" class="input" type="password" autocomplete="new-password" bind:value={next2} />
    </div>
    <div class="actions">
      <button class="btn primary" type="submit" disabled={pwBusy}>
        {#if pwBusy}<span class="spinner"></span>{/if}
        {t('account.submitPassword')}
      </button>
    </div>
  </div>
  {#if pwError}<div class="alert error" role="alert">{pwError}</div>{/if}
</form>

<ApiKeys />

<Modal bind:open={emailOpen} title={t('account.email.modalTitle')} width={460}>
  <form id="email-form" class="stack" onsubmit={(e) => saveEmail(e)} novalidate>
    <p class="help nomargin">{t('account.email.help')}</p>
    <div class="field">
      <label for="em-new">{t('account.email.label')}</label>
      <input id="em-new" class="input" type="email" bind:value={emailNew} autocomplete="email" maxlength="254" spellcheck="false" />
    </div>
    {#if !session.user?.oidc}
      <div class="field">
        <label for="em-pw">{t('account.currentPassword')}</label>
        <input id="em-pw" class="input" type="password" bind:value={emailPw} autocomplete="current-password" />
        <span class="help">{t('account.email.passwordHelp')}</span>
      </div>
    {/if}
    {#if emailError}<div class="alert error" role="alert">{emailError}</div>{/if}
  </form>
  {#snippet footer()}
    {#if session.user?.email}
      <button type="button" class="btn danger" onclick={() => saveEmail(null, true)} disabled={emailBusy}>{t('account.email.remove')}</button>
    {/if}
    <span class="spacer"></span>
    <button type="button" class="btn" onclick={() => (emailOpen = false)}>{t('common.cancel')}</button>
    <button type="submit" form="email-form" class="btn primary" disabled={emailBusy}>
      {#if emailBusy}<span class="spinner"></span>{/if}
      {t('common.save')}
    </button>
  {/snippet}
</Modal>

<style>
  .top3 {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 16px;
    align-items: stretch;
    margin-bottom: 16px;
  }
  /* İki adımlı doğrulama kartı da diğerleriyle aynı yükseklikte olsun. Kural
     yalnızca kartlara uygulanır: TwoFactor bileşeni kartın yanında pencereler
     (<dialog>) de çizer; onların margin:auto ile ortalanması bozulmamalı. */
  .top3 > :global(.card) {
    height: 100%;
    margin: 0;
  }
  .pw {
    margin-bottom: 16px;
  }
  .pw-row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr)) auto;
    gap: 16px;
    align-items: start;
  }
  /* Düğme, giriş kutularıyla aynı hizada (etiket yüksekliği kadar aşağıda). */
  .actions {
    display: flex;
    justify-content: flex-end;
    padding-top: 1.55rem;
  }
  .card-title {
    margin-bottom: 0;
  }
  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 8px 16px;
    margin: 14px 0 0;
    font-size: 0.92rem;
  }
  dt {
    color: var(--muted);
  }
  dd {
    margin: 0;
    word-break: break-word;
  }
  .email-dd {
    display: flex;
    gap: 10px;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .spacer {
    flex: 1;
  }
  @media (max-width: 1100px) {
    .top3 {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
    /* Üçüncü kart (iki adımlı doğrulama) tam genişlik; son çocuk bir <dialog> olabilir. */
    .top3 > :global(.card:nth-of-type(3)) {
      grid-column: 1 / -1;
    }
    .pw-row {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
    .pw-row > .field:first-child {
      grid-column: 1 / -1;
    }
    .actions {
      grid-column: 1 / -1;
      padding-top: 0;
    }
  }
  @media (max-width: 640px) {
    .top3,
    .pw-row {
      grid-template-columns: minmax(0, 1fr);
    }
    .actions .btn {
      flex: 1;
    }
  }
</style>
