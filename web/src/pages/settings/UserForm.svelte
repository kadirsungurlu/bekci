<script lang="ts">
  import { api, errorMessage, type Role, type TagRule, type UserRecord } from '../../lib/api';
  import { ROLE_DESCS, ROLE_LABELS, session } from '../../lib/session.svelte';
  import { randomPassword } from '../../lib/format';
  import Modal from '../../components/Modal.svelte';
  import MonitorPicker from '../../components/MonitorPicker.svelte';
  import ServerPicker from '../../components/ServerPicker.svelte';
  import TagRulePicker from '../../components/TagRulePicker.svelte';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import { t } from '../../lib/i18n';

  let {
    open = $bindable(false),
    user,
    onsaved,
  }: { open?: boolean; user: UserRecord | null; onsaved: (u: UserRecord, created: boolean) => void } = $props();

  // svelte-ignore state_referenced_locally
  const orig = user;
  const isSelf = orig !== null && orig.id === session.user?.id;
  const ROLES: Role[] = ['admin', 'editor', 'viewer'];

  let username = $state(orig?.username ?? '');
  let displayName = $state(orig?.display_name ?? '');
  let email = $state(orig?.email ?? '');
  let role = $state<Role>(orig?.role ?? 'viewer');
  let password = $state('');
  let disabled = $state(orig?.disabled ?? false);
  let restricted = $state(orig ? orig.role === 'viewer' && !orig.all_monitors : false);
  // Açık seçim (etiketle görünenler hariç); eski sunucuda picked_monitor_ids yok → monitor_ids.
  let monitorIds = $state<number[]>([...(orig?.picked_monitor_ids ?? orig?.monitor_ids ?? [])]);
  let tagRules = $state<TagRule[]>((orig?.tag_rules ?? []).map((r) => ({ ...r })));
  let serverIds = $state<number[]>(orig?.server_ids ? [...orig.server_ids] : []);
  let showPw = $state(true);

  let error = $state('');
  let busy = $state(false);

  const USERNAME_RE = /^[a-zA-Z0-9._-]{3,32}$/;

  function generate() {
    password = randomPassword();
    showPw = true;
  }

  async function save() {
    error = '';
    if (!orig && !USERNAME_RE.test(username.trim()))
      return (error = t('users.form.errUsername'));
    if (displayName.trim().length > 100) return (error = t('users.form.errDisplayName'));
    if (email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) return (error = t('users.form.errEmail'));
    if (!orig && password.length < 8) return (error = t('users.errTempPassword'));
    const onlySelected = role === 'viewer' && restricted;
    // Yalnızca sunucu görecek müşteri de olabilir: en az bir monitör veya sunucu yeterli.
    if (onlySelected && monitorIds.length === 0 && serverIds.length === 0 && tagRules.length === 0)
      return (error = t('users.form.errScope'));
    const body = {
      display_name: displayName.trim(),
      email: email.trim().toLowerCase(),
      role,
      disabled,
      all_monitors: !onlySelected,
      monitor_ids: onlySelected ? monitorIds : [],
      server_ids: onlySelected ? serverIds : [],
      tag_rules: onlySelected ? tagRules.map((r) => ({ tag_id: r.tag_id, value: r.value })) : [],
    };
    busy = true;
    try {
      const res = orig
        ? await api.updateUser(orig.id, body)
        : await api.createUser({ ...body, username: username.trim(), password });
      onsaved(res, !orig);
      open = false;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open title={orig ? t('users.form.editTitle') : t('users.form.addTitle')} width={620}>
  <form
    id="uf"
    class="stack"
    novalidate
    onsubmit={(e) => {
      e.preventDefault();
      save();
    }}
  >
    <div class="grid-2">
      <div class="field">
        <label for="uf-u">{t('common.username')}</label>
        {#if orig}
          <input id="uf-u" class="input" value={orig.username} disabled />
          <span class="help">{t('users.form.usernameFixed')}</span>
        {:else}
          <input id="uf-u" class="input" bind:value={username} maxlength="32" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder={t('users.form.usernamePlaceholder')} />
          <span class="help">{t('users.form.usernameHelp')}</span>
        {/if}
      </div>
      <div class="field">
        <label for="uf-d">{t('users.form.displayName')} <span class="muted">{t('users.form.optional')}</span></label>
        <input id="uf-d" class="input" bind:value={displayName} maxlength="100" placeholder={t('users.form.displayNamePlaceholder')} />
      </div>
    </div>
    <div class="field">
      <label for="uf-e">{t('users.form.email')} <span class="muted">{t('users.form.optional')}</span></label>
      <input id="uf-e" class="input" type="email" bind:value={email} maxlength="254" autocomplete="off" spellcheck="false" placeholder="ad@ornek.com" />
      <span class="help">{t('users.form.emailHelp')}</span>
    </div>

    <fieldset class="roles">
      <legend class="label">{t('users.form.role')}</legend>
      {#if isSelf}<span class="help">{t('users.form.ownRole')}</span>{/if}
      <div class="role-list">
        {#each ROLES as r (r)}
          <label class="role" class:active={role === r} class:off={isSelf && role !== r}>
            <input type="radio" name="uf-role" value={r} bind:group={role} disabled={isSelf} />
            <span>
              <span class="rl">{ROLE_LABELS[r]}</span>
              <span class="rd">{ROLE_DESCS[r]}</span>
            </span>
          </label>
        {/each}
      </div>
    </fieldset>

    {#if role === 'viewer'}
      <div class="access stack">
        <label class="check">
          <input type="checkbox" bind:checked={restricted} />
          <span>
            {t('users.form.restricted')}
            <small>{t('users.form.restrictedHelp')}</small>
          </span>
        </label>
        {#if restricted}
          <MonitorPicker bind:selected={monitorIds} label={t('users.form.monitors')} id="uf-mp" />
          <TagRulePicker bind:rules={tagRules} id="uf-tr" label={t('users.form.tagRules')} help={t('users.form.tagRulesHelp')} />
          <ServerPicker bind:selected={serverIds} label={t('users.form.servers')} id="uf-sp" />
        {/if}
      </div>
    {/if}

    {#if !orig}
      <div class="field">
        <label for="uf-p">{t('users.tempPassword')}</label>
        <div class="pw">
          <input
            id="uf-p"
            class="input mono"
            type={showPw ? 'text' : 'password'}
            bind:value={password}
            autocomplete="new-password"
            spellcheck="false"
          />
          <button type="button" class="btn" onclick={generate}><Icon name="refresh" size={15} /> {t('users.form.generate')}</button>
          <CopyButton class="btn icon" iconOnly ariaLabel={t('users.copyPassword')} text={password} disabled={!password} size={15} />
        </div>
        <span class="help">{t('users.form.passwordHelp')}</span>
      </div>
    {:else if !isSelf}
      <label class="check">
        <input type="checkbox" bind:checked={disabled} />
        <span>{t('users.form.disabled')}<small>{t('users.form.disabledHelp')}</small></span>
      </label>
    {/if}

    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button type="submit" form="uf" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {orig ? t('common.save') : t('users.form.submitAdd')}
    </button>
  {/snippet}
</Modal>

<style>
  .spacer {
    flex: 1;
  }
  .roles {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .roles legend {
    padding: 0;
    margin-bottom: 8px;
  }
  .role-list {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
  .role {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 12px;
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    background: var(--input);
    cursor: pointer;
  }
  .role.active {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .role.off {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .role input {
    margin: 3px 0 0;
    accent-color: var(--accent);
  }
  .role input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .role.active .rd {
    color: var(--text-2);
  }
  .rl {
    display: block;
    font-weight: 700;
    font-size: 0.92rem;
  }
  .rd {
    display: block;
    font-size: 0.78rem;
    color: var(--muted);
    line-height: 1.35;
    margin-top: 2px;
  }
  .access {
    padding: 12px;
    border: 1px dashed var(--border-strong);
    border-radius: 10px;
    gap: 12px;
  }
  .pw {
    display: flex;
    gap: 8px;
  }
  .pw .input {
    flex: 1;
  }
  @media (max-width: 640px) {
    .role-list {
      grid-template-columns: minmax(0, 1fr);
    }
    .pw {
      flex-wrap: wrap;
    }
    .pw .input {
      flex: 1 1 100%;
    }
    .pw .btn:not(.icon) {
      flex: 1;
    }
  }
</style>
