<script lang="ts">
  // Ajan sürümü ve güncelleme durumu (sunucu ajanı / kontrol noktası): sürüm,
  // durum rozeti ve yöneticiye "Şimdi güncelle". Durum panelde hesaplanır
  // (internal/agentupdate.Status); eski panelde update gelmez, yalnızca sürüm görünür.
  import { api, errorMessage, type AgentUpdate } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { toast } from '../lib/ui.svelte';
  import { t } from '../lib/i18n';

  let {
    id,
    version,
    update,
    onchange,
  }: {
    id: number;
    version?: string;
    update?: AgentUpdate;
    onchange?: () => void;
  } = $props();

  let busy = $state(false);

  type Badge = { cls: string; label: string; title: string };
  const badge = $derived.by((): Badge | null => {
    const u = update;
    if (!u) return null;
    const vars = { v: version || '—', panel: u.panel || '—', note: u.note || '' };
    switch (u.state) {
      case 'current':
        return { cls: 'up', label: t('probes.update.current'), title: t('probes.update.currentTitle', vars) };
      case 'outdated':
        return {
          cls: 'pending',
          label: u.requested ? t('probes.update.requested') : t('probes.update.outdated'),
          title: u.auto_effective || u.requested ? t('probes.update.outdatedAuto', vars) : t('probes.update.outdatedManual', vars),
        };
      case 'updating':
        return { cls: 'accent', label: t('probes.update.updating'), title: t('probes.update.updatingTitle', vars) };
      case 'failed':
        return { cls: 'down', label: t('probes.update.failed'), title: t('probes.update.failedTitle', vars) };
      case 'unsupported':
        return { cls: 'pending', label: t('probes.update.unsupported'), title: t('probes.update.unsupportedTitle', vars) };
      case 'unsigned':
      case 'unversioned':
      case 'newer':
      case 'off':
      case 'readonly':
        return { cls: 'paused', label: t(`probes.update.${u.state}`), title: t(`probes.update.${u.state}Title`, vars) };
    }
    return null;
  });

  // Teklif edilebilir durumda yönetici elle tetikleyebilir (başarısızsa yeniden).
  const canUpdate = $derived(
    session.isAdmin && !!update && (update.state === 'failed' || (update.state === 'outdated' && !update.requested)),
  );

  async function run() {
    busy = true;
    try {
      await api.updateAgent(id);
      toast.success(t('probes.update.requestedToast'));
      onchange?.();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      busy = false;
    }
  }
</script>

<span class="av">
  <span class="ver mono">{version || '—'}</span>
  {#if badge}<span class="badge {badge.cls}" title={badge.title}>{badge.label}</span>{/if}
  {#if canUpdate}
    <button type="button" class="btn sm" onclick={run} disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {t('probes.update.now')}
    </button>
  {/if}
</span>

<style>
  .av {
    display: inline-flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }
  .ver {
    overflow-wrap: anywhere;
  }
</style>
