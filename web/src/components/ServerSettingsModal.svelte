<script lang="ts">
  // Sunucu ayarları (yönetici): ad, metrik toplamayı duraklatma ve silme.
  // Sunucular kontrol noktalarından ayrı kayıtlardır; ayarları burada yapılır.
  import { api, errorMessage, type ServerView } from '../lib/api';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { navigate } from '../lib/router.svelte';
  import Modal from './Modal.svelte';

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
  let error = $state('');
  let busy = $state(false);

  // Pencere her açıldığında güncel değerlerle başlar.
  $effect(() => {
    if (open) {
      name = server.name;
      metrics = server.metrics;
      ipLock = server.ip_lock;
      lockedIp = server.locked_ip;
      error = '';
    }
  });

  async function save(e: SubmitEvent) {
    e.preventDefault();
    const n = name.trim();
    if (!n) return (error = 'Ad gerekli.');
    busy = true;
    error = '';
    try {
      await api.updateProbe(server.id, n, server.active, metrics, { ipLock });
      toast.success('Sunucu kaydedildi');
      open = false;
      onsaved();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  // Kilidi sıfırla: ajan bir sonraki bağlantıda yeni IP'ye kilitlenir.
  async function resetIp() {
    busy = true;
    error = '';
    try {
      await api.updateProbe(server.id, server.name, server.active, undefined, { resetIp: true });
      lockedIp = '';
      toast.success('IP kilidi sıfırlandı');
      onsaved();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    const ok = await confirmDialog({
      title: 'Sunucuyu sil',
      message: `“${server.name}” ve tüm metrik geçmişi, uyarı kuralları ve uyarı geçmişi silinecek. Sunucudaki ajan bundan sonra reddedilir; kaldırmak için ajan konteynerini veya hizmetini de silin.`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteProbe(server.id);
      toast.success('Sunucu silindi');
      open = false;
      navigate('/servers');
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }
</script>

<Modal bind:open title="Sunucu ayarları" width={460}>
  <form id="srv-settings" class="stack" onsubmit={save} novalidate>
    <div class="field">
      <label for="srv-name">Ad</label>
      <input id="srv-name" class="input" maxlength="100" bind:value={name} />
      <span class="help">Listede ve bildirimlerde görünür.</span>
    </div>
    <label class="check">
      <input type="checkbox" bind:checked={metrics} />
      <span>
        Metrik topla
        <small>Kapatılırsa ajan ölçüm göndermeyi bırakır, çevrimdışı uyarısı da gitmez. Geçmiş silinmez.</small>
      </span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={ipLock} />
      <span>
        IP'ye kilitle
        <small>Ajan yalnızca ilk bağlandığı IP'den veri gönderebilir; sunucu taşınırsa kilidi sıfırlayın.</small>
      </span>
    </label>
    {#if ipLock && lockedIp}
      <div class="field">
        <span class="help">Kilitli IP: <strong>{lockedIp}</strong></span>
        <button type="button" class="btn sm" onclick={resetIp} disabled={busy}>Kilidi sıfırla</button>
      </div>
    {/if}
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <button type="button" class="btn danger" onclick={remove}>Sunucuyu sil</button>
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
    <button type="submit" form="srv-settings" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if} Kaydet
    </button>
  {/snippet}
</Modal>
