// Liste ve detay sayfalarının ortak monitör işlemleri.

import { api, errorMessage, type MonitorView } from './api';
import { live } from './live.svelte';
import { confirmDialog, toast } from './ui.svelte';
import { navigate } from './router.svelte';
import { t } from './i18n';

export async function togglePause(m: MonitorView): Promise<MonitorView | null> {
  try {
    const res = m.active ? await api.pauseMonitor(m.id) : await api.resumeMonitor(m.id);
    live.upsert(res);
    toast.success(t(res.active ? 'monitors.actions.started' : 'monitors.actions.paused', { name: res.name }));
    return res;
  } catch (e) {
    toast.error(errorMessage(e));
    return null;
  }
}

/** Onay alıp monitörü siler; silindiyse true döner. */
export async function deleteMonitor(m: MonitorView): Promise<boolean> {
  const ok = await confirmDialog({
    title: t('monitors.actions.deleteTitle'),
    message: t('monitors.actions.deleteMessage', { name: m.name }),
    confirmText: t('common.delete'),
    danger: true,
  });
  if (!ok) return false;
  try {
    await api.deleteMonitor(m.id);
    live.remove(m.id);
    toast.success(t('monitors.actions.deleted', { name: m.name }));
    return true;
  } catch (e) {
    toast.error(errorMessage(e));
    return false;
  }
}

/** Monitörü kopyalar (durdurulmuş olarak) ve kopyanın düzenleme formunu açar. */
export async function cloneMonitor(m: MonitorView): Promise<MonitorView | null> {
  try {
    const res = await api.cloneMonitor(m.id);
    live.upsert(res);
    toast.success(t('monitors.actions.cloned', { name: m.name }));
    navigate(`/monitors/${res.id}/edit?kopya=1`);
    return res;
  } catch (e) {
    toast.error(errorMessage(e));
    return null;
  }
}

/** Onay alıp monitörün kontrol geçmişini, uptime özetlerini ve bitmiş olaylarını siler. */
export async function resetStats(m: MonitorView): Promise<MonitorView | null> {
  const ok = await confirmDialog({
    title: t('monitors.actions.resetTitle'),
    message: t('monitors.actions.resetMessage', { name: m.name }),
    confirmText: t('monitors.actions.resetConfirm'),
    danger: true,
  });
  if (!ok) return null;
  try {
    const res = await api.resetMonitorStats(m.id);
    live.upsert(res);
    toast.success(t('monitors.actions.resetDone', { name: m.name }));
    return res;
  } catch (e) {
    toast.error(errorMessage(e));
    return null;
  }
}
