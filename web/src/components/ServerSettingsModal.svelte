<script lang="ts">
  // Sunucu ayarları (yönetici): ad, metrik toplamayı duraklatma, ajanın
  // otomatik güncellemesi ve silme.
  // Sunucular kontrol noktalarından ayrı kayıtlardır; ayarları burada yapılır.
  import { untrack } from 'svelte';
  import { api, ApiError, errorMessage, type ServerView } from '../lib/api';
  import { servers } from '../lib/servers.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { navigate } from '../lib/router.svelte';
  import { t, tParts } from '../lib/i18n';
  import Modal from './Modal.svelte';
  import OobFields from './OobFields.svelte';
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
  let role = $state('');
  let region = $state('');
  let metrics = $state(true);
  let ipLock = $state(true);
  let lockedIp = $state('');
  let auto = $state<AutoChoice>('inherit');
  let error = $state('');
  let field = $state('');
  // OOB ikinci hat: görünümde ad yalnızca etiket olarak gelir ("OOB" = varsayılan).
  let oobIp = $state('');
  let oobName = $state('');
  let oobIp0 = '';
  let oobName0 = '';
  let busy = $state(false);

  // Pencere her açıldığında güncel değerlerle başlar. Yalnızca `open` izlenir:
  // canlı akışla gelen sunucu güncellemeleri yazılanları silmesin.
  $effect(() => {
    if (open)
      untrack(() => {
        name = server.name;
        role = server.role ?? '';
        region = server.region ?? '';
        metrics = server.metrics;
        ipLock = server.ip_lock;
        lockedIp = server.locked_ip;
        auto = autoChoice(server.update);
        oobIp = oobIp0 = server.oob_ip ?? '';
        oobName = oobName0 = server.oob_ip && server.uplink_name !== 'OOB' ? (server.uplink_name ?? '') : '';
        error = '';
        field = '';
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
    field = '';
    // OOB alanları yalnızca değiştiyse gönderilir (kayıtlı ad görünümde yok).
    const oobChanged = oobIp.trim() !== oobIp0 || oobName.trim() !== oobName0;
    try {
      await api.updateProbe(server.id, n, server.active, metrics, {
        ipLock,
        labels: { role: role.trim(), region: region.trim() },
        ...(server.update ? { autoUpdate: autoValue(auto) } : {}),
        ...(oobChanged ? { oob: { oob_ip: oobIp.trim(), oob_name: oobName.trim() } } : {}),
      });
      toast.success(t('servers.settings.saved'));
      open = false;
      onsaved();
    } catch (err) {
      field = err instanceof ApiError ? ((err.data as { field?: string } | null)?.field ?? '') : '';
      error =
        field === 'oob_ip' ? t('probes.oob.errIp') : field === 'oob_name' ? t('probes.oob.errName') : errorMessage(err);
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
    <div class="field">
      <label for="srv-role">{t('servers.settings.role')}</label>
      <input id="srv-role" class="input" maxlength="64" placeholder={t('servers.settings.rolePlaceholder')} bind:value={role} />
    </div>
    <div class="field">
      <label for="srv-region">{t('servers.settings.region')}</label>
      <input id="srv-region" class="input" maxlength="64" placeholder={t('servers.settings.regionPlaceholder')} bind:value={region} />
      <span class="help">{t('servers.settings.labelsHelp')}</span>
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
    <OobFields id="srv-oob" bind:ip={oobIp} bind:name={oobName} {field} {error} />
    {#if server.update}<AgentAutoUpdate id="srv-auto" bind:value={auto} update={server.update} />{/if}
    {#if error && field !== 'oob_ip' && field !== 'oob_name'}<div class="alert error" role="alert">{error}</div>{/if}
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
