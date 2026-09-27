// Liste ve detay sayfalarının ortak monitör işlemleri.

import { api, errorMessage, type MonitorView } from './api';
import { live } from './live.svelte';
import { confirmDialog, toast } from './ui.svelte';

export async function togglePause(m: MonitorView): Promise<MonitorView | null> {
  try {
    const res = m.active ? await api.pauseMonitor(m.id) : await api.resumeMonitor(m.id);
    live.upsert(res);
    toast.success(res.active ? `“${res.name}” başlatıldı` : `“${res.name}” durduruldu`);
    return res;
  } catch (e) {
    toast.error(errorMessage(e));
    return null;
  }
}

/** Onay alıp monitörü siler; silindiyse true döner. */
export async function deleteMonitor(m: MonitorView): Promise<boolean> {
  const ok = await confirmDialog({
    title: 'Monitörü sil',
    message: `“${m.name}” ve tüm geçmiş kayıtları (kontroller, olaylar) kalıcı olarak silinecek. Bu işlem geri alınamaz.`,
    confirmText: 'Sil',
    danger: true,
  });
  if (!ok) return false;
  try {
    await api.deleteMonitor(m.id);
    live.remove(m.id);
    toast.success(`“${m.name}” silindi`);
    return true;
  } catch (e) {
    toast.error(errorMessage(e));
    return false;
  }
}
