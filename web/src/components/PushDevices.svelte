<script lang="ts">
  // Hesabım › Tarayıcı bildirimleri (Web Push): bu cihazda aç/kapat, test, diğer cihazlar.
  import { onMount } from 'svelte';
  import { api, errorMessage, type PushDevice } from '../lib/api';
  import { webpush } from '../lib/webpush.svelte';
  import { pwa } from '../lib/pwa.svelte';
  import { fmtRelative } from '../lib/format';
  import { clock, toast } from '../lib/ui.svelte';
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  let devices = $state.raw<PushDevice[]>([]);
  let loaded = $state(false);
  let error = $state('');
  let testing = $state(false);
  const support = $derived(webpush.support());
  const devMode = !import.meta.env.PROD;

  async function load() {
    try {
      devices = await api.webpushDevices();
      loaded = true;
    } catch (e) {
      error = errorMessage(e);
    }
  }
  onMount(async () => {
    await webpush.refresh();
    await load();
  });

  const thisDevice = $derived(devices.find((d) => d.endpoint === webpush.endpoint) ?? null);
  const others = $derived(devices.filter((d) => d.endpoint !== webpush.endpoint));

  async function enable() {
    error = '';
    try {
      await webpush.subscribe();
      await load();
      toast.success(t('account.push.enabled'));
    } catch (e) {
      const msg = e instanceof Error ? e.message : '';
      error = msg === 'denied' ? t('account.push.deniedNow') : msg === 'no-sw' ? t('account.push.noSw') : errorMessage(e);
    }
  }
  async function disable() {
    error = '';
    try {
      await webpush.unsubscribe(thisDevice?.id);
      await load();
      toast.success(t('account.push.disabled'));
    } catch (e) {
      error = errorMessage(e);
    }
  }
  async function remove(d: PushDevice) {
    try {
      await api.webpushUnsubscribe(d.id);
      devices = devices.filter((x) => x.id !== d.id);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }
  async function test() {
    testing = true;
    error = '';
    try {
      const r = await api.webpushTest();
      toast.success(t('account.push.testSent', { n: r.sent }));
    } catch (e) {
      error = errorMessage(e);
    } finally {
      testing = false;
    }
  }
  /** Tarayıcı adını kısaltır (UA'dan): Chrome / Firefox / Safari / Edge + işletim sistemi. */
  function label(d: PushDevice): string {
    const ua = d.user_agent;
    const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : '';
    const os = /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Windows/.test(ua) ? 'Windows' : /Mac OS/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : '';
    return [browser, os].filter(Boolean).join(' · ') || t('account.push.unknownDevice');
  }
</script>

<div class="card stack push">
  <h2 class="card-title"><Icon name="bell-ring" size={17} /> {t('account.push.title')}</h2>
  <p class="help nomargin">{t('account.push.intro')}</p>
  {#if devMode}
    <p class="muted small nomargin">{t('account.push.devMode')}</p>
  {:else if support === 'unsupported'}
    <div class="alert warning small">{t('account.push.unsupported')}</div>
  {:else if support === 'ios-browser'}
    <div class="alert info small">{t('account.push.iosBrowser')}</div>
  {:else if support === 'denied'}
    <div class="alert warning small">{t('account.push.denied')}</div>
  {:else}
    <div class="row">
      {#if webpush.subscribed}
        <span class="badge up"><Icon name="check" size={12} /> {t('account.push.onThisDevice')}</span>
        <button type="button" class="btn sm" onclick={disable} disabled={webpush.busy}>{t('account.push.turnOff')}</button>
        <button type="button" class="btn sm" onclick={test} disabled={testing || webpush.busy}>
          {#if testing}<span class="spinner"></span>{:else}<Icon name="send" size={14} />{/if}
          {t('account.push.test')}
        </button>
      {:else}
        <button type="button" class="btn sm primary" onclick={enable} disabled={webpush.busy}>
          {#if webpush.busy}<span class="spinner"></span>{:else}<Icon name="bell" size={14} />{/if}
          {t('account.push.turnOn')}
        </button>
        {#if !pwa.standalone}<span class="muted small">{t('account.push.installHint')}</span>{/if}
      {/if}
    </div>
  {/if}
  {#if error}<div class="alert error small" role="alert">{error}</div>{/if}
  {#if loaded && others.length}
    <div class="label">{t('account.push.otherDevices')}</div>
    <ul class="devs">
      {#each others as d (d.id)}
        <li>
          <Icon name={/iPhone|Android/.test(d.user_agent) ? 'smartphone' : 'monitor'} size={15} />
          <span class="dn">{label(d)}</span>
          <span class="muted small">{d.last_used_at ? t('account.push.lastUsed', { when: fmtRelative(d.last_used_at, clock.now) }) : t('account.push.neverUsed')}</span>
          <button type="button" class="btn ghost icon sm" aria-label={t('common.remove')} onclick={() => remove(d)}><Icon name="x" size={14} /></button>
        </li>
      {/each}
    </ul>
  {/if}
  <p class="help nomargin">{t('account.push.channelHint')}</p>
</div>

<style>
  .card-title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .devs {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .devs li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
  }
  .dn {
    font-weight: 600;
    font-size: 0.9rem;
    flex: 1;
  }
  .badge :global(svg) {
    margin-right: 3px;
  }
</style>
