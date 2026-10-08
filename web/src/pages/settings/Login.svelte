<script lang="ts">
  // Ayarlar › Giriş ve SSO: sistem e-postası (şifre sıfırlama) ve OpenID Connect.
  import { onMount } from 'svelte';
  import { api, errorMessage, MASK, type AppSettings, type NotificationChannel, type OIDCSettings, type Role } from '../../lib/api';
  import { ROLE_LABELS } from '../../lib/session.svelte';
  import { toast } from '../../lib/ui.svelte';
  import { guardUnsaved, snapshot } from '../../lib/forms';
  import CopyButton from '../../components/CopyButton.svelte';
  import Icon from '../../components/Icon.svelte';
  import { i18n, t } from '../../lib/i18n';

  let loaded = $state(false);
  let loadError = $state('');

  // Sistem e-postası
  let settings = $state<AppSettings | null>(null);
  let channels = $state.raw<NotificationChannel[]>([]);
  let mailId = $state('0');
  let mailBusy = $state(false);
  let mailError = $state('');
  const emailChannels = $derived(channels.filter((c) => c.type === 'email'));

  // OIDC
  const EMPTY: OIDCSettings = {
    enabled: false,
    name: '',
    issuer: '',
    client_id: '',
    client_secret: '',
    scopes: 'openid profile email',
    link_email: true,
    auto_provision: false,
    default_role: 'viewer',
    username_claim: '',
    role_claim: '',
    admin_values: '',
    editor_values: '',
    viewer_values: '',
    local_login: true,
  };
  let o = $state<OIDCSettings>({ ...EMPTY });
  let redirectUri = $state('');
  let oBusy = $state(false);
  let oError = $state('');
  let testing = $state(false);
  let testMsg = $state('');
  const ROLES: Role[] = ['admin', 'editor', 'viewer'];

  let baseline = '';
  const current = () => snapshot({ mailId, o });
  onMount(() => guardUnsaved(() => loaded && !oBusy && !mailBusy && !!baseline && current() !== baseline));

  async function load() {
    try {
      const [s, ch, oc] = await Promise.all([api.settings(), api.notifications(), api.oidcSettings()]);
      settings = s;
      channels = ch;
      mailId = String(s.system_mail_channel_id ?? 0);
      o = { ...EMPTY, ...oc.settings };
      redirectUri = oc.redirect_uri;
      loaded = true;
      loadError = '';
      baseline = current();
    } catch (e) {
      loadError = errorMessage(e);
    }
  }
  onMount(load);

  async function saveMail(e: SubmitEvent) {
    e.preventDefault();
    if (!settings) return;
    mailError = '';
    mailBusy = true;
    try {
      const { default_user_agent: _ua, ...rest } = settings;
      void _ua;
      settings = await api.saveSettings({ ...rest, system_mail_channel_id: Number(mailId) || 0 });
      baseline = current();
      toast.success(t('settings.login.mailSaved'));
    } catch (err) {
      mailError = errorMessage(err);
    } finally {
      mailBusy = false;
    }
  }

  async function saveOidc(e: SubmitEvent) {
    e.preventDefault();
    oError = '';
    oBusy = true;
    try {
      const res = await api.saveOidcSettings(o);
      o = { ...EMPTY, ...res.settings };
      redirectUri = res.redirect_uri;
      baseline = current();
      toast.success(t('settings.login.saved'));
    } catch (err) {
      oError = errorMessage(err);
    } finally {
      oBusy = false;
    }
  }

  async function test() {
    testMsg = '';
    testing = true;
    try {
      const r = await api.testOidc(o.issuer);
      testMsg = t('settings.login.testOk', { auth: r.authorization_endpoint });
    } catch (err) {
      testMsg = errorMessage(err);
    } finally {
      testing = false;
    }
  }
  const docsUrl = $derived(i18n.locale === 'tr' ? 'https://bekci.app/docs/giris-ve-sso/' : 'https://bekci.app/en/docs/sign-in-and-sso/');
</script>

{#if !loaded}
  {#if loadError}
    <div class="alert error"><span>{loadError}</span> <button type="button" class="btn sm" onclick={load}>{t('common.retry')}</button></div>
  {:else}
    <div class="skeleton" style="height:320px"></div>
  {/if}
{:else}
  <div class="stack">
    <form class="card stack" onsubmit={saveMail} novalidate>
      <h2 class="card-title"><Icon name="mail" size={17} /> {t('settings.login.mailTitle')}</h2>
      <p class="help nomargin">{t('settings.login.mailIntro')}</p>
      <div class="row">
        <div class="field grow">
          <label for="sm-ch">{t('settings.login.mailChannel')}</label>
          <select id="sm-ch" class="input" bind:value={mailId}>
            <option value="0">{t('settings.login.mailNone')}</option>
            {#each emailChannels as c (c.id)}
              <option value={String(c.id)}>{c.name}{c.active ? '' : ` · ${t('monitors.inactive')}`}</option>
            {/each}
          </select>
          {#if emailChannels.length === 0}<span class="help">{t('settings.login.mailNoChannels')}</span>{/if}
        </div>
        <div class="actions">
          <button class="btn primary" type="submit" disabled={mailBusy}>{#if mailBusy}<span class="spinner"></span>{/if}{t('common.save')}</button>
        </div>
      </div>
      {#if mailError}<div class="alert error" role="alert">{mailError}</div>{/if}
    </form>

    <form class="card stack" onsubmit={saveOidc} novalidate>
      <h2 class="card-title"><Icon name="log-in" size={17} /> {t('settings.login.ssoTitle')}</h2>
      <p class="help nomargin">{t('settings.login.ssoIntro')} <a href={docsUrl} target="_blank" rel="noopener noreferrer">{t('settings.login.docs')}</a></p>
      <div class="field">
        <span class="label">{t('settings.login.redirect')}</span>
        <div class="copybox"><code>{#each redirectUri.split(/(?<=\/)/) as part}{part}<wbr />{/each}</code><CopyButton text={redirectUri} /></div>
        <span class="help">{t('settings.login.redirectHelp')}</span>
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={o.enabled} />
        <span>{t('settings.login.enabled')}</span>
      </label>
      <div class="grid-2">
        <div class="field">
          <label for="o-name">{t('settings.login.name')}</label>
          <input id="o-name" class="input" bind:value={o.name} maxlength="60" placeholder={t('settings.login.namePh')} />
        </div>
        <div class="field">
          <label for="o-iss">{t('settings.login.issuer')}</label>
          <input id="o-iss" class="input mono" bind:value={o.issuer} placeholder="https://accounts.google.com" spellcheck="false" autocomplete="off" />
          <span class="help">{t('settings.login.issuerHelp')}</span>
        </div>
        <div class="field">
          <label for="o-cid">{t('settings.login.clientId')}</label>
          <input id="o-cid" class="input mono" bind:value={o.client_id} spellcheck="false" autocomplete="off" />
        </div>
        <div class="field">
          <label for="o-sec">{t('settings.login.clientSecret')}</label>
          <input id="o-sec" class="input mono" type="password" bind:value={o.client_secret} autocomplete="new-password" />
          {#if o.client_secret === MASK}<span class="help">{t('notifyTypes.form.keptHelp')}</span>{/if}
        </div>
        <div class="field">
          <label for="o-sc">{t('settings.login.scopes')}</label>
          <input id="o-sc" class="input mono" bind:value={o.scopes} spellcheck="false" />
        </div>
        <div class="field">
          <label for="o-uc">{t('settings.login.usernameClaim')}</label>
          <input id="o-uc" class="input mono" bind:value={o.username_claim} placeholder="preferred_username" spellcheck="false" />
          <span class="help">{t('settings.login.usernameClaimHelp')}</span>
        </div>
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={o.link_email} />
        <span>{t('settings.login.linkEmail')}<small>{t('settings.login.linkEmailHelp')}</small></span>
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={o.auto_provision} />
        <span>{t('settings.login.autoProvision')}<small>{t('settings.login.autoProvisionHelp')}</small></span>
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={o.local_login} />
        <span>{t('settings.login.localLogin')}<small>{t('settings.login.localLoginHelp')}</small></span>
      </label>
      <div class="field half">
        <label for="o-role">{t('settings.login.defaultRole')}</label>
        <select id="o-role" class="input" bind:value={o.default_role}>
          {#each ROLES as r (r)}<option value={r}>{ROLE_LABELS[r]}</option>{/each}
        </select>
      </div>

      <h3>{t('settings.login.roleTitle')}</h3>
      <p class="help nomargin">{t('settings.login.roleIntro')}</p>
      <div class="grid-2">
        <div class="field">
          <label for="o-rc">{t('settings.login.roleClaim')}</label>
          <input id="o-rc" class="input mono" bind:value={o.role_claim} placeholder="groups" spellcheck="false" />
        </div>
        <div class="field">
          <label for="o-adm">{t('settings.login.adminValues')}</label>
          <input id="o-adm" class="input" bind:value={o.admin_values} placeholder="bekci-admins" />
          <span class="help">{t('settings.login.valuesHelp')}</span>
        </div>
        <div class="field">
          <label for="o-ed">{t('settings.login.editorValues')}</label>
          <input id="o-ed" class="input" bind:value={o.editor_values} placeholder="bekci-editors, ops" />
        </div>
        <div class="field">
          <label for="o-vw">{t('settings.login.viewerValues')}</label>
          <input id="o-vw" class="input" bind:value={o.viewer_values} placeholder="bekci-viewers" />
        </div>
      </div>
      <p class="help nomargin"><Icon name="shield-check" size={13} /> {t('settings.login.twoFactorNote')}</p>
      {#if testMsg}<div class="alert info small" role="status">{testMsg}</div>{/if}
      {#if oError}<div class="alert error" role="alert">{oError}</div>{/if}
      <div class="actions">
        <button type="button" class="btn" onclick={test} disabled={testing || !o.issuer.trim()}>
          {#if testing}<span class="spinner"></span>{:else}<Icon name="globe" size={15} />{/if}
          {t('settings.login.test')}
        </button>
        <button class="btn primary" type="submit" disabled={oBusy}>{#if oBusy}<span class="spinner"></span>{/if}{t('common.save')}</button>
      </div>
    </form>
  </div>
{/if}

<style>
  .card-title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }
  .row {
    display: flex;
    gap: 12px;
    align-items: flex-end;
    flex-wrap: wrap;
  }
  .grow {
    flex: 1 1 320px;
  }
  .actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    flex-wrap: wrap;
  }
  .half {
    max-width: 320px;
  }
  .copybox {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
  }
  /* Adres yalnızca "/" sonrasından kırılır (kelime ortasından değil). */
  .copybox code {
    flex: 1;
    min-width: 0;
    overflow-wrap: break-word;
    font-size: 0.82rem;
  }
  h3 {
    margin-top: 6px;
  }
  @media (max-width: 640px) {
    .actions .btn {
      flex: 1;
    }
  }
</style>
