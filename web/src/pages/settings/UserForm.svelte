<script lang="ts">
  import { api, errorMessage, type Role, type UserRecord } from '../../lib/api';
  import { ROLE_DESCS, ROLE_LABELS, session } from '../../lib/session.svelte';
  import { randomPassword } from '../../lib/format';
  import Modal from '../../components/Modal.svelte';
  import MonitorPicker from '../../components/MonitorPicker.svelte';
  import ServerPicker from '../../components/ServerPicker.svelte';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';

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
  let role = $state<Role>(orig?.role ?? 'viewer');
  let password = $state('');
  let disabled = $state(orig?.disabled ?? false);
  let restricted = $state(orig ? orig.role === 'viewer' && !orig.all_monitors : false);
  let monitorIds = $state<number[]>(orig?.monitor_ids ? [...orig.monitor_ids] : []);
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
      return (error = 'Kullanıcı adı 3-32 karakter olmalı; harf, rakam, nokta, tire ve alt çizgi kullanılabilir.');
    if (displayName.trim().length > 100) return (error = 'Görünen ad en fazla 100 karakter olabilir.');
    if (!orig && password.length < 8) return (error = 'Geçici şifre en az 8 karakter olmalı.');
    const onlySelected = role === 'viewer' && restricted;
    if (onlySelected && monitorIds.length === 0) return (error = 'Müşteri erişimi için en az bir monitör seçin.');
    const body = {
      display_name: displayName.trim(),
      role,
      disabled,
      all_monitors: !onlySelected,
      monitor_ids: onlySelected ? monitorIds : [],
      server_ids: onlySelected ? serverIds : [],
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

<Modal bind:open title={orig ? 'Kullanıcıyı düzenle' : 'Kullanıcı ekle'} width={620}>
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
        <label for="uf-u">Kullanıcı adı</label>
        {#if orig}
          <input id="uf-u" class="input" value={orig.username} disabled />
          <span class="help">Kullanıcı adı değiştirilemez.</span>
        {:else}
          <input id="uf-u" class="input" bind:value={username} maxlength="32" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="ör. ayse.yilmaz" />
          <span class="help">3-32 karakter: harf, rakam, nokta, tire, alt çizgi.</span>
        {/if}
      </div>
      <div class="field">
        <label for="uf-d">Görünen ad <span class="muted">(isteğe bağlı)</span></label>
        <input id="uf-d" class="input" bind:value={displayName} maxlength="100" placeholder="ör. Ayşe Yılmaz" />
      </div>
    </div>

    <fieldset class="roles">
      <legend class="label">Rol</legend>
      {#if isSelf}<span class="help">Kendi rolünüzü değiştiremezsiniz.</span>{/if}
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
            Müşteri erişimi: yalnızca seçili monitörleri ve sunucuları görsün
            <small>Kapalıysa tüm monitörleri ve sunucuları görebilir. Açıksa listede, olaylarda ve canlı akışta yalnızca seçtikleriniz görünür; sunucu seçilmezse Sunucular sekmesi gizlenir.</small>
          </span>
        </label>
        {#if restricted}
          <MonitorPicker bind:selected={monitorIds} label="Görebileceği monitörler" id="uf-mp" />
          <ServerPicker bind:selected={serverIds} label="Görebileceği sunucular" id="uf-sp" />
        {/if}
      </div>
    {/if}

    {#if !orig}
      <div class="field">
        <label for="uf-p">Geçici şifre</label>
        <div class="pw">
          <input
            id="uf-p"
            class="input mono"
            type={showPw ? 'text' : 'password'}
            bind:value={password}
            autocomplete="new-password"
            spellcheck="false"
          />
          <button type="button" class="btn" onclick={generate}><Icon name="refresh" size={15} /> Rastgele oluştur</button>
          <CopyButton class="btn icon" iconOnly ariaLabel="Şifreyi kopyala" text={password} disabled={!password} size={15} />
        </div>
        <span class="help">En az 8 karakter. Kullanıcı ilk girişte bu şifreyi değiştirmek zorunda kalır; şifreyi ona güvenli bir yoldan iletin.</span>
      </div>
    {:else if !isSelf}
      <label class="check">
        <input type="checkbox" bind:checked={disabled} />
        <span>Hesap devre dışı<small>Devre dışı hesap giriş yapamaz ve API anahtarları çalışmaz.</small></span>
      </label>
    {/if}

    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
    <button type="submit" form="uf" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {orig ? 'Kaydet' : 'Kullanıcıyı ekle'}
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
