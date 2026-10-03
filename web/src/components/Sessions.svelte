<script lang="ts">
  // Hesabım › Açık oturumlar: cihaz, IP, son görülme; tekil ve toplu kapatma.
  import { onMount } from 'svelte';
  import { api, errorMessage, type SessionInfo } from '../lib/api';
  import { fmtRelative } from '../lib/format';
  import { clock, confirmDialog, toast } from '../lib/ui.svelte';
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  let list = $state.raw<SessionInfo[]>([]);
  let loaded = $state(false);
  let error = $state('');
  let busy = $state(false);

  async function load() {
    try {
      list = await api.sessions();
      loaded = true;
      error = '';
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(load);

  const others = $derived(list.filter((s) => !s.current));

  function device(s: SessionInfo): string {
    const ua = s.user_agent;
    const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : '';
    const os = /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Windows/.test(ua) ? 'Windows' : /Mac OS/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : '';
    return [browser, os].filter(Boolean).join(' · ') || t('account.sessions.unknownDevice');
  }

  async function revoke(s: SessionInfo) {
    busy = true;
    try {
      await api.revokeSession(s.id);
      list = list.filter((x) => x.id !== s.id);
      toast.success(t('account.sessions.revoked'));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      busy = false;
    }
  }
  async function revokeOthers() {
    const ok = await confirmDialog({
      title: t('account.sessions.revokeOthers'),
      message: t('account.sessions.intro'),
      confirmText: t('account.sessions.revokeOthers'),
      danger: true,
    });
    if (!ok) return;
    busy = true;
    try {
      const r = await api.revokeOtherSessions();
      toast.success(t('account.sessions.revokedOthers', { n: r.revoked }));
      await load();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      busy = false;
    }
  }
</script>

<div class="card stack">
  <div class="head">
    <h2 class="card-title"><Icon name="monitor" size={17} /> {t('account.sessions.title')}</h2>
    {#if others.length}
      <button type="button" class="btn sm danger" onclick={revokeOthers} disabled={busy}>{t('account.sessions.revokeOthers')}</button>
    {/if}
  </div>
  <p class="help nomargin">{t('account.sessions.intro')}</p>
  {#if error}
    <div class="alert error small"><span>{error}</span> <button type="button" class="btn sm" onclick={load}>{t('common.retry')}</button></div>
  {:else if !loaded}
    <div class="skeleton" style="height:60px"></div>
  {:else}
    <ul class="list">
      {#each list as s (s.id)}
        <li class:cur={s.current}>
          <Icon name={/iPhone|Android/.test(s.user_agent) ? 'smartphone' : 'monitor'} size={16} />
          <div class="info">
            <div class="row1">
              <span class="dev">{device(s)}</span>
              {#if s.current}<span class="badge up">{t('account.sessions.current')}</span>{/if}
              <span class="badge">{t(`account.sessions.via.${s.via}`)}</span>
            </div>
            <div class="row2 muted small">
              {#if s.ip}<span class="mono">{s.ip}</span> ·{/if}
              {t('account.sessions.lastSeen', { when: fmtRelative(s.last_seen_at, clock.now) })} · {t('account.sessions.signedIn', { when: fmtRelative(s.created_at, clock.now) })}
            </div>
          </div>
          {#if !s.current}
            <button type="button" class="btn ghost icon sm" aria-label={t('account.sessions.revoke')} title={t('account.sessions.revoke')} onclick={() => revoke(s)} disabled={busy}>
              <Icon name="logout" size={15} />
            </button>
          {/if}
        </li>
      {/each}
    </ul>
    {#if others.length === 0}<p class="muted small nomargin">{t('account.sessions.none')}</p>{/if}
  {/if}
</div>

<style>
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
  }
  .card-title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .list li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
  }
  .list li.cur {
    border-color: var(--accent-border);
  }
  .info {
    flex: 1;
    min-width: 0;
  }
  .row1 {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .dev {
    font-weight: 600;
    font-size: 0.92rem;
  }
  .row2 {
    margin-top: 2px;
    word-break: break-word;
  }
</style>
