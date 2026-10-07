<script lang="ts">
  // Sunucu ayarları (yönetici): ad, metrik toplamayı duraklatma, ajanın
  // otomatik güncellemesi ve silme.
  // Sunucular kontrol noktalarından ayrı kayıtlardır; ayarları burada yapılır.
  import { untrack } from 'svelte';
  import { api, errorMessage, type ServerView } from '../lib/api';
  import { servers } from '../lib/servers.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { navigate } from '../lib/router.svelte';
  import { t, tParts } from '../lib/i18n';
  import Modal from './Modal.svelte';
  import AgentAutoUpdate, { autoChoice, autoValue, type AutoChoice } from './AgentAutoUpdate.svelte';

  let {
    open = $bindable(false),
    server,
    onsaved,
  }: {
    open?: boolean;
    server: ServerView;
    onsaved: () => void;
  } = $props();

  let name = $state('');
  let metrics = $state(true);
  let ipLock = $state(true);
  let lockedIp = $state('');
  let auto = $state<AutoChoice>('inherit');
  let error = $state('');
  let busy = $state(false);

  // Pencere her açıldığında güncel değerlerle başlar. Yalnızca `open` izlenir:
  // canlı akışla gelen sunucu güncellemeleri yazılanları silmesin.
  $effect(() => {
    if (open)
      untrack(() => {
        name = server.name;
        metrics = server.metrics;
        ipLock = server.ip_lock;
        lockedIp = server.locked_ip;
        auto = autoChoice(server.update);
        error = '';
      });
  });

  // Kilitli IP yalnızca bilgi amaçlı: canlı güncellemeyle yenilenebilir.
  $effect(() => {
    const ip = server.locked_ip;
    if (open) untrack(() => (lockedIp = ip));
  });

  async function save(e: SubmitEvent) {
    e.preventDefault();
    const n = name.trim();
    if (!n) return (error = t('servers.settings.errName'));
    busy = true;
    error = '';
    try {
      await api.updateProbe(server.id, n, server.active, metrics, {
        ipLock,
        ...(server.update ? { autoUpdate: autoValue(auto) } : {}),
      });
      toast.success(t('servers.settings.saved'));
      open = false;
      onsaved();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  // Kilidi sıfırla: ajan bir sonraki bağlantıda yeni IP’ye kilitlenir.
  async function resetIp() {
    busy = true;
    error = '';
    try {
      await api.updateProbe(server.id, server.name, server.active, undefined, { resetIp: true });
      lockedIp = '';
      toast.success(t('servers.settings.ipReset'));
      onsaved();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    const ok = await confirmDialog({
      title: t('servers.settings.deleteTitle'),
      message: t('servers.settings.deleteMessage', { name: server.name }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteProbe(server.id);
      servers.remove(server.id);
      toast.success(t('servers.settings.deleted'));
      open = false;
      navigate('/servers');
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }
</script>

<Modal bind:open title={t('servers.settings.title')} width={460}>
  <form id="srv-settings" class="stack" onsubmit={save} novalidate>
    <div class="field">
      <label for="srv-name">{t('common.name')}</label>
      <input id="srv-name" class="input" maxlength="100" bind:value={name} />
      <span class="help">{t('servers.setup.nameHelp')}</span>
    </div>
    <label class="check">
      <input type="checkbox" bind:checked={metrics} />
      <span>
        {t('servers.settings.metrics')}
        <small>{t('servers.settings.metricsHelp')}</small>
      </span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={ipLock} />
      <span>
        {t('servers.settings.ipLock')}
        <small>{t('servers.settings.ipLockHelp')}</small>
      </span>
    </label>
    {#if ipLock && lockedIp}
      <div class="field">
        <span class="help">
          {#each tParts('servers.settings.lockedIp') as p, i (i)}{#if p.slot === 'ip'}<strong>{lockedIp}</strong>{:else}{p.text}{/if}{/each}
        </span>
        <button type="button" class="btn sm" onclick={resetIp} disabled={busy}>{t('servers.settings.resetLock')}</button>
      </div>
    {/if}
    {#if server.update}<AgentAutoUpdate id="srv-auto" bind:value={auto} update={server.update} />{/if}
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <button type="button" class="btn danger" onclick={remove}>{t('servers.settings.deleteTitle')}</button>
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button type="submit" form="srv-settings" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if} {t('common.save')}
    </button>
  {/snippet}
</Modal>

<style>
  .spacer {
    flex: 1;
  }
</style>
