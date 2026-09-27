<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type UserRecord } from '../../lib/api';
  import { ROLE_LABELS, session } from '../../lib/session.svelte';
  import { clock, confirmDialog, copyText, toast } from '../../lib/ui.svelte';
  import { collator, fmtDate, fmtRelative, randomPassword } from '../../lib/format';
  import Modal from '../../components/Modal.svelte';
  import RowMenu, { type MenuItem } from '../../components/RowMenu.svelte';
  import Icon from '../../components/Icon.svelte';
  import UserForm from './UserForm.svelte';

  let users = $state.raw<UserRecord[]>([]);
  let loading = $state(true);
  let loadError = $state('');

  async function load() {
    try {
      users = await api.users();
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  const sorted = $derived(
    users.slice().sort((a, b) => {
      const r = { admin: 0, editor: 1, viewer: 2 };
      return r[a.role] - r[b.role] || collator.compare(a.username, b.username);
    }),
  );

  // Form
  let formOpen = $state(false);
  let editing = $state<UserRecord | null>(null);
  let formKey = $state(0);

  function openForm(u: UserRecord | null) {
    editing = u;
    formKey++;
    formOpen = true;
  }

  function saved(u: UserRecord, created: boolean) {
    users = created ? [...users, u] : users.map((x) => (x.id === u.id ? u : x));
    toast.success(created ? `“${u.username}” eklendi. Geçici şifreyi kendisine iletin.` : 'Kullanıcı kaydedildi');
  }

  async function toggleDisabled(u: UserRecord) {
    if (!u.disabled) {
      const ok = await confirmDialog({
        title: 'Hesabı devre dışı bırak',
        message: `“${u.username}” giriş yapamayacak ve API anahtarları çalışmayacak. Daha sonra yeniden etkinleştirebilirsiniz.`,
        confirmText: 'Devre dışı bırak',
        danger: true,
      });
      if (!ok) return;
    }
    try {
      const res = await api.updateUser(u.id, {
        display_name: u.display_name,
        role: u.role,
        disabled: !u.disabled,
        all_monitors: u.all_monitors,
        monitor_ids: u.monitor_ids ?? [],
        server_ids: u.server_ids ?? [],
      });
      users = users.map((x) => (x.id === u.id ? res : x));
      toast.success(res.disabled ? 'Hesap devre dışı bırakıldı' : 'Hesap etkinleştirildi');
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function remove(u: UserRecord) {
    const ok = await confirmDialog({
      title: 'Kullanıcıyı sil',
      message: `“${u.username}” kalıcı olarak silinecek; oturumları ve API anahtarları da kapanır. İşlem kaydındaki geçmişi korunur.`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteUser(u.id);
      users = users.filter((x) => x.id !== u.id);
      toast.success(`“${u.username}” silindi`);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function reset2fa(u: UserRecord) {
    const ok = await confirmDialog({
      title: 'İki adımlı doğrulamayı sıfırla',
      message: 'Kullanıcının iki adımlı doğrulaması kapatılacak; bir sonraki girişte sadece şifre istenecek.',
      confirmText: '2FA’yı sıfırla',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.resetUser2fa(u.id);
      users = users.map((x) => (x.id === u.id ? { ...x, two_factor_enabled: false } : x));
      toast.success('İki adımlı doğrulama sıfırlandı');
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  // Şifre sıfırlama
  let pwOpen = $state(false);
  let pwUser = $state<UserRecord | null>(null);
  let pw = $state('');
  let pwError = $state('');
  let pwBusy = $state(false);

  function openPw(u: UserRecord) {
    pwUser = u;
    pw = randomPassword();
    pwError = '';
    pwOpen = true;
  }

  async function resetPw(e: SubmitEvent) {
    e.preventDefault();
    if (!pwUser) return;
    pwError = '';
    if (pw.length < 8) return (pwError = 'Geçici şifre en az 8 karakter olmalı.');
    pwBusy = true;
    try {
      await api.resetUserPassword(pwUser.id, pw);
      users = users.map((x) => (x.id === pwUser!.id ? { ...x, must_change_password: true } : x));
      pwOpen = false;
      toast.success('Geçici şifre ayarlandı. Kullanıcıya iletin.');
    } catch (err) {
      pwError = errorMessage(err);
    } finally {
      pwBusy = false;
    }
  }

  async function copyPw() {
    if (await copyText(pw)) toast.success('Şifre panoya kopyalandı');
  }

  function menu(u: UserRecord): MenuItem[] {
    const self = u.id === session.user?.id;
    const out: MenuItem[] = [{ label: 'Düzenle', icon: 'edit', onclick: () => openForm(u) }];
    if (!self) out.push({ label: 'Şifre sıfırla', icon: 'key', onclick: () => openPw(u) });
    if (!self && u.two_factor_enabled) out.push({ label: '2FA’yı sıfırla', icon: 'shield', onclick: () => reset2fa(u) });
    if (!self)
      out.push(
        u.disabled
          ? { label: 'Etkinleştir', icon: 'check-circle', onclick: () => toggleDisabled(u) }
          : { label: 'Devre dışı bırak', icon: 'ban', onclick: () => toggleDisabled(u) },
      );
    if (!self) out.push({ label: 'Sil', icon: 'trash', danger: true, onclick: () => remove(u) });
    return out;
  }
</script>

<div class="card">
  <div class="head">
    <div>
      <h2 class="card-title">Kullanıcılar</h2>
      <p class="text-2 small sub">
        <b>Yönetici</b> her şeyi yönetir, <b>Editör</b> monitörleri ve kanalları yönetir, <b>İzleyici</b> yalnızca görüntüler.
        Müşterilerinize yalnızca kendi monitörlerini gösteren izleyici hesapları açabilirsiniz.
      </p>
    </div>
    <button class="btn primary" onclick={() => openForm(null)}><Icon name="user-plus" size={16} /> Kullanıcı ekle</button>
  </div>

  {#if loading}
    <div class="skeleton" style="height:180px"></div>
  {:else if loadError}
    <div class="empty">
      <h3>Kullanıcılar yüklenemedi</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={load}>Tekrar dene</button>
    </div>
  {:else}
    <table class="table responsive users">
      <thead>
        <tr>
          <th>Kullanıcı</th>
          <th>Görünen ad</th>
          <th>Rol</th>
          <th>Durum</th>
          <th>2FA</th>
          <th>Son giriş</th>
          <th><span class="sr">İşlemler</span></th>
        </tr>
      </thead>
      <tbody>
        {#each sorted as u (u.id)}
          <tr class:dim={u.disabled}>
            <td data-label="Kullanıcı" class="un">
              <span class="unw">
                <span class="av" aria-hidden="true">{(u.display_name || u.username).slice(0, 1).toLocaleUpperCase('tr')}</span>
                <span class="uname">{u.username}{#if u.id === session.user?.id}<span class="you">(siz)</span>{/if}</span>
              </span>
            </td>
            <td data-label="Görünen ad" class="dn">{u.display_name || '—'}</td>
            <td data-label="Rol">
              <span class="badge {u.role === 'admin' ? 'accent' : ''}">{ROLE_LABELS[u.role]}</span>
              {#if u.role === 'viewer' && !u.all_monitors}
                <span class="scope" title="Yalnızca seçili monitörleri ve sunucuları görür"
                  >{u.monitor_ids?.length ?? 0} monitör{u.server_ids?.length ? ` · ${u.server_ids.length} sunucu` : ''}</span
                >
              {/if}
            </td>
            <td data-label="Durum">
              {#if u.disabled}
                <span class="badge down">Devre dışı</span>
              {:else if u.must_change_password}
                <span class="badge pending">Şifre değişimi bekliyor</span>
              {:else}
                <span class="badge up">Aktif</span>
              {/if}
            </td>
            <td data-label="2FA">
              {#if u.two_factor_enabled}
                <span class="badge up" title="İki adımlı doğrulama açık"><Icon name="shield-check" size={12} /> 2FA</span>
              {:else}
                <span class="muted small">Kapalı</span>
              {/if}
            </td>
            <td data-label="Son giriş" class="nowrap muted" title={u.last_login_at ? fmtDate(u.last_login_at) : ''}>
              {u.last_login_at ? fmtRelative(u.last_login_at, clock.now) : 'Hiç'}
            </td>
            <td class="act"><RowMenu items={menu(u)} label="{u.username} için işlemler" /></td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

{#key formKey}
  {#if formKey > 0}
    <UserForm bind:open={formOpen} user={editing} onsaved={saved} />
  {/if}
{/key}

<Modal bind:open={pwOpen} title="Şifre sıfırla" width={460}>
  <form id="pwf" class="stack" onsubmit={resetPw} novalidate>
    <p class="text-2 nomargin">
      <b>{pwUser?.username}</b> için geçici bir şifre belirlenecek. Kullanıcının açık oturumları kapanır ve bir sonraki girişte kendi
      şifresini belirlemesi istenir.
    </p>
    <div class="field">
      <label for="pw-new">Geçici şifre</label>
      <div class="pw">
        <input id="pw-new" class="input mono" bind:value={pw} autocomplete="new-password" spellcheck="false" />
        <button type="button" class="btn icon" aria-label="Yeni rastgele şifre" onclick={() => (pw = randomPassword())}><Icon name="refresh" size={15} /></button>
        <button type="button" class="btn icon" aria-label="Şifreyi kopyala" onclick={copyPw}><Icon name="copy" size={15} /></button>
      </div>
    </div>
    {#if pwError}<div class="alert error" role="alert">{pwError}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (pwOpen = false)}>Vazgeç</button>
    <button type="submit" form="pwf" class="btn primary" disabled={pwBusy}>
      {#if pwBusy}<span class="spinner"></span>{/if} Şifreyi sıfırla
    </button>
  {/snippet}
</Modal>

<style>
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .card-title {
    margin-bottom: 4px;
  }
  .sub {
    margin: 0;
    max-width: 680px;
  }
  .users td {
    vertical-align: middle;
  }
  .users tr.dim td:not(.act) {
    opacity: 0.55;
  }
  .unw {
    display: flex;
    align-items: center;
    gap: 10px;
    font-weight: 700;
    min-width: 0;
  }
  .uname {
    word-break: break-all;
  }
  .av {
    width: 30px;
    height: 30px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--card-2);
    border: 1px solid var(--border-strong);
    color: var(--text-2);
    font-size: 0.82rem;
    flex-shrink: 0;
  }
  .you {
    color: var(--muted);
    font-weight: 500;
    margin-left: 6px;
    font-size: 0.82rem;
  }
  .dn {
    color: var(--text-2);
  }
  .scope {
    display: inline-block;
    margin-left: 6px;
    font-size: 0.78rem;
    color: var(--muted);
  }
  .badge :global(svg) {
    margin-right: 3px;
  }
  .act {
    text-align: right;
    width: 1%;
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
  .spacer {
    flex: 1;
  }
  .nomargin {
    margin: 0;
  }
  .pw {
    display: flex;
    gap: 8px;
  }
  .pw .input {
    flex: 1;
  }
  @media (max-width: 720px) {
    /* Kart görünümünde işlem menüsü sağ üst köşede. */
    .users tr {
      position: relative;
    }
    .users td.act {
      position: absolute;
      top: 8px;
      right: 0;
      width: auto;
      padding: 0;
    }
    .table.responsive.users td.act::before {
      display: none;
    }
    .table.responsive.users td.un::before {
      display: none;
    }
    .un {
      padding-right: 44px !important;
      margin-bottom: 4px;
    }
  }
</style>
