<script lang="ts">
  import { api, errorMessage } from '../../lib/api';
  import { ROLE_LABELS, session } from '../../lib/session.svelte';
  import { toast } from '../../lib/ui.svelte';
  import { browserLocale, forgetLocale, isLocale, LOCALES, setLocale, t, type Locale } from '../../lib/i18n';
  import TwoFactor from './TwoFactor.svelte';
  import ApiKeys from './ApiKeys.svelte';

  let current = $state('');
  let next = $state('');
  let next2 = $state('');
  let pwError = $state('');
  let pwBusy = $state(false);

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

<style>
  .top3 {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 16px;
    align-items: stretch;
    margin-bottom: 16px;
  }
  /* İki adımlı doğrulama kartı da diğerleriyle aynı yükseklikte olsun. */
  .top3 > :global(*) {
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
  @media (max-width: 1100px) {
    .top3 {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
    .top3 > :global(:last-child) {
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
