<script lang="ts">
  import { api, errorMessage } from '../../lib/api';
  import { ROLE_LABELS, session } from '../../lib/session.svelte';
  import { toast } from '../../lib/ui.svelte';
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
    if (!current) return (pwError = 'Mevcut şifrenizi girin.');
    if (next.length < 8) return (pwError = 'Yeni şifre en az 8 karakter olmalı.');
    if (next !== next2) return (pwError = 'Yeni şifreler birbiriyle aynı değil.');
    pwBusy = true;
    try {
      await api.changePassword(current, next);
      current = next = next2 = '';
      toast.success('Şifreniz değiştirildi. Diğer cihazlardaki oturumlar kapatıldı.');
    } catch (err) {
      pwError = errorMessage(err);
    } finally {
      pwBusy = false;
    }
  }
</script>

<div class="cols">
  <form class="card stack" onsubmit={changePassword} novalidate>
    <h2 class="card-title">Şifre değiştir</h2>
    <input type="text" name="username" autocomplete="username" value={session.user?.username ?? ''} hidden readonly />
    <div class="field">
      <label for="cur">Mevcut şifre</label>
      <input id="cur" class="input" type="password" autocomplete="current-password" bind:value={current} />
    </div>
    <div class="grid-2">
      <div class="field">
        <label for="new">Yeni şifre</label>
        <input id="new" class="input" type="password" autocomplete="new-password" bind:value={next} />
        <span class="help">En az 8 karakter.</span>
      </div>
      <div class="field">
        <label for="new2">Yeni şifre (tekrar)</label>
        <input id="new2" class="input" type="password" autocomplete="new-password" bind:value={next2} />
      </div>
    </div>
    {#if pwError}<div class="alert error" role="alert">{pwError}</div>{/if}
    <div class="actions">
      <button class="btn primary" type="submit" disabled={pwBusy}>
        {#if pwBusy}<span class="spinner"></span>{/if}
        Şifreyi değiştir
      </button>
    </div>
  </form>

  <div class="stack">
    <TwoFactor />
    <div class="card">
      <h2 class="card-title">Hesap</h2>
      <dl>
        <dt>Kullanıcı adı</dt>
        <dd>{session.user?.username}</dd>
        {#if session.user?.display_name}
          <dt>Görünen ad</dt>
          <dd>{session.user.display_name}</dd>
        {/if}
        <dt>Rol</dt>
        <dd>{ROLE_LABELS[session.role]}{session.user && !session.user.all_monitors ? ' · yalnızca seçili monitörler' : ''}</dd>
        <dt>Sürüm</dt>
        <dd class="mono">{session.version || '—'}</dd>
      </dl>
    </div>
  </div>
</div>

<ApiKeys />

<style>
  .cols {
    display: grid;
    grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
    gap: 16px;
    align-items: start;
    margin-bottom: 16px;
  }
  .card-title {
    margin-bottom: 0;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
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
  @media (max-width: 1000px) {
    .cols {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 640px) {
    .actions .btn {
      flex: 1;
    }
  }
</style>
