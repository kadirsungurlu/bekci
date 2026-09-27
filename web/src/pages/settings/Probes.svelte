<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, errorMessage, type Probe, type ProbeSetup } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { clock, confirmDialog, toast } from '../../lib/ui.svelte';
  import { collator, fmtDate, fmtRelative } from '../../lib/format';
  import Modal from '../../components/Modal.svelte';
  import RowMenu, { type MenuItem } from '../../components/RowMenu.svelte';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';

  let probes = $state.raw<Probe[]>([]);
  let loading = $state(true);
  let loadError = $state('');

  async function load() {
    try {
      probes = await api.probes();
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loading = false;
    }
  }

  // Çevrimiçi/çevrimdışı değişimi canlı akıştan gelir; son görülme 30 sn'de bir tazelenir.
  let unsub: (() => void) | undefined;
  let timer: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    load();
    unsub = live.onProbe((ev) => {
      probes = probes.map((p) => (p.id === ev.probe_id ? { ...p, online: ev.online, last_seen_at: ev.last_seen_at || p.last_seen_at } : p));
    });
    timer = setInterval(load, 30_000);
  });
  onDestroy(() => {
    unsub?.();
    clearInterval(timer);
  });

  const sorted = $derived(probes.slice().sort((a, b) => collator.compare(a.name, b.name)));

  function probeState(p: Probe): { l: string; c: string } {
    if (!p.active) return { l: 'Devre dışı', c: 'paused' };
    return p.online ? { l: 'Çevrimiçi', c: 'up' } : { l: 'Çevrimdışı', c: 'down' };
  }

  // Yeni / token ----------------------------------------------------------------------
  let newOpen = $state(false);
  let newName = $state('');
  let newError = $state('');
  let newBusy = $state(false);
  let setup = $state<ProbeSetup | null>(null);
  let setupTitle = $state('');

  function openNew() {
    newName = '';
    newError = '';
    setup = null;
    setupTitle = '';
    newOpen = true;
  }

  async function create(e: SubmitEvent) {
    e.preventDefault();
    newError = '';
    const n = newName.trim();
    if (!n) return (newError = 'Kontrol noktasına bir ad verin (ör. Frankfurt).');
    newBusy = true;
    try {
      setup = await api.createProbe(n);
      setupTitle = `“${setup.probe.name}” eklendi`;
      load();
    } catch (err) {
      newError = errorMessage(err);
    } finally {
      newBusy = false;
    }
  }

  async function regenerate(p: Probe) {
    const ok = await confirmDialog({
      title: 'Token’ı yenile',
      message: `“${p.name}” — Eski token hemen geçersiz olur; kontrol noktasını yeni token ile yeniden başlatın.`,
      confirmText: 'Token’ı yenile',
      danger: true,
    });
    if (!ok) return;
    try {
      setup = await api.regenerateProbeToken(p.id);
      setupTitle = `“${p.name}” için yeni token`;
      newError = '';
      newOpen = true;
      load();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }


  // Düzenle ----------------------------------------------------------------------------
  let editOpen = $state(false);
  let editing = $state<Probe | null>(null);
  let editName = $state('');
  let editIpLock = $state(true);
  let editLockedIp = $state('');
  let editError = $state('');
  let editBusy = $state(false);

  function openEdit(p: Probe) {
    editing = p;
    editName = p.name;
    editIpLock = p.ip_lock ?? false;
    editLockedIp = p.locked_ip ?? '';
    editError = '';
    editOpen = true;
  }

  async function saveEdit(e: SubmitEvent) {
    e.preventDefault();
    if (!editing) return;
    editError = '';
    const n = editName.trim();
    if (!n) return (editError = 'Ad gerekli.');
    editBusy = true;
    try {
      await api.updateProbe(editing.id, n, editing.active, undefined, { ipLock: editIpLock });
      toast.success('Kontrol noktası kaydedildi');
      editOpen = false;
      load();
    } catch (err) {
      editError = errorMessage(err);
    } finally {
      editBusy = false;
    }
  }

  // Kilidi sıfırla: kontrol noktası bir sonraki bağlantıda yeni IP'ye kilitlenir.
  async function resetEditIp() {
    if (!editing) return;
    editError = '';
    editBusy = true;
    try {
      await api.updateProbe(editing.id, editing.name, editing.active, undefined, { resetIp: true });
      editLockedIp = '';
      toast.success('IP kilidi sıfırlandı');
      load();
    } catch (err) {
      editError = errorMessage(err);
    } finally {
      editBusy = false;
    }
  }

  async function toggle(p: Probe) {
    if (p.active) {
      const ok = await confirmDialog({
        title: 'Devre dışı bırak',
        message: `“${p.name}” sonuç gönderemeyecek ve konum hesaplarına katılmayacak. Daha sonra yeniden etkinleştirebilirsiniz.`,
        confirmText: 'Devre dışı bırak',
        danger: true,
      });
      if (!ok) return;
    }
    try {
      await api.updateProbe(p.id, p.name, !p.active);
      toast.success(p.active ? 'Kontrol noktası devre dışı bırakıldı' : 'Kontrol noktası etkinleştirildi');
      load();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function remove(p: Probe) {
    const ok = await confirmDialog({
      title: 'Kontrol noktasını sil',
      message: `“${p.name}” — Bu kontrol noktası tüm monitörlerden çıkarılacak.`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteProbe(p.id);
      probes = probes.filter((x) => x.id !== p.id);
      toast.success('Kontrol noktası silindi');
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const menu = (p: Probe): MenuItem[] => [
    { label: 'Düzenle', icon: 'edit', onclick: () => openEdit(p) },
    p.active
      ? { label: 'Devre dışı bırak', icon: 'ban', onclick: () => toggle(p) }
      : { label: 'Etkinleştir', icon: 'check-circle', onclick: () => toggle(p) },
    { label: 'Token’ı yenile', icon: 'key', onclick: () => regenerate(p) },
    { label: 'Sil', icon: 'trash', danger: true, onclick: () => remove(p) },
  ];
</script>

<section class="card">
  <div class="head">
    <div>
      <h2 class="card-title">Kontrol noktaları</h2>
      <p class="text-2 small sub">
        Monitörlerinizi farklı şehir veya ağlardan da kontrol edin. Kontrol noktası, bu uygulamanın başka bir sunucuda
        <code>probe</code> modunda çalışan bir kopyasıdır; kendisine atanan monitörleri kontrol edip sonuçları buraya bildirir. Monitör
        formundaki <b>Konumlar</b> bölümünden atanır.
      </p>
    </div>
    <button class="btn primary" onclick={openNew}><Icon name="plus" size={16} /> Yeni kontrol noktası</button>
  </div>

  {#if loading}
    <div class="skeleton" style="height:120px"></div>
  {:else if loadError}
    <div class="alert error">{loadError} <button class="linkbtn" onclick={load}>Tekrar dene</button></div>
  {:else if probes.length === 0}
    <div class="none">
      <Icon name="map-pin" size={22} />
      <span>Henüz kontrol noktası yok. Tüm kontroller şu an yalnızca bu sunucudan yapılıyor.</span>
    </div>
  {:else}
    <table class="table responsive probes">
      <thead>
        <tr>
          <th>Ad</th>
          <th>Durum</th>
          <th>Son görülme</th>
          <th>Adres</th>
          <th>Sürüm</th>
          <th>Monitör</th>
          <th><span class="sr">İşlemler</span></th>
        </tr>
      </thead>
      <tbody>
        {#each sorted as p (p.id)}
          {@const st = probeState(p)}
          <tr class:dim={!p.active}>
            <td data-label="Ad" class="nm">
              <span class="nmw">
                <span class="odot {st.c}" aria-hidden="true"></span>
                <span>
                  {p.name}
                  {#if p.token_prefix}<code class="pfx">{p.token_prefix}…</code>{/if}
                </span>
              </span>
            </td>
            <td data-label="Durum"><span class="badge {st.c}">{st.l}</span></td>
            <td data-label="Son görülme" class="nowrap" title={p.last_seen_at ? fmtDate(p.last_seen_at) : ''}>
              {p.last_seen_at ? fmtRelative(p.last_seen_at, clock.now) : 'Hiç bağlanmadı'}
            </td>
            <td data-label="Adres" class="mono small">
              {p.last_ip || '—'}
              {#if p.ip_lock}<span class="iplock" title={p.locked_ip ? `${p.locked_ip} IP'sine kilitli` : 'IP kilidi açık; ilk bağlantıda sabitlenir'}><Icon name="lock" size={12} /></span>{/if}
            </td>
            <td data-label="Sürüm" class="small">{p.version || '—'}</td>
            <td data-label="Monitör">{p.monitor_count ?? 0}</td>
            <td class="act"><RowMenu items={menu(p)} label="{p.name} için işlemler" /></td>
          </tr>
        {/each}
      </tbody>
    </table>
    <p class="help foot">Son 90 saniyede sonuç gönderen kontrol noktası çevrimiçi sayılır.</p>
  {/if}
</section>

<Modal bind:open={newOpen} title={setup ? setupTitle : 'Yeni kontrol noktası'} width={600}>
  {#if setup}
    <div class="stack">
      <div class="alert warning">Bu token yalnızca bir kez gösterilir; kopyalayıp saklayın.</div>
      <div>
        <div class="label">Token</div>
        <div class="copybox">
          <code>{setup.token}</code>
          <CopyButton text={setup.token} class="btn sm primary" />
        </div>
      </div>
      <div>
        <div class="label"><Icon name="terminal" size={14} /> Kurulum komutu</div>
        <p class="help cmd-help">Kontrol noktası olacak sunucuda bu komutu çalıştırın.</p>
        <div class="copybox">
          <code class="cmd">{setup.docker_command}</code>
          <CopyButton text={setup.docker_command} />
        </div>
      </div>
      <p class="help nomargin">
        Sunucu adresi: <code>{setup.server_url}</code>. Kontrol noktası bu adrese dışarıdan erişebilmeli; birkaç saniye içinde listede
        <b>Çevrimiçi</b> görünür.
      </p>
    </div>
  {:else}
    <form id="prf" class="stack" onsubmit={create} novalidate>
      <div class="field">
        <label for="pr-name">Ad</label>
        <input id="pr-name" class="input" maxlength="100" bind:value={newName} placeholder="ör. Frankfurt" />
        <span class="help">Konumu anlatan kısa bir ad; monitör detayında ve bildirimlerde görünür.</span>
      </div>
      {#if newError}<div class="alert error" role="alert">{newError}</div>{/if}
    </form>
  {/if}
  {#snippet footer()}
    <div class="spacer"></div>
    {#if setup}
      <button type="button" class="btn primary" onclick={() => (newOpen = false)}>Kopyaladım, kapat</button>
    {:else}
      <button type="button" class="btn" onclick={() => (newOpen = false)}>Vazgeç</button>
      <button type="submit" form="prf" class="btn primary" disabled={newBusy}>
        {#if newBusy}<span class="spinner"></span>{/if} Oluştur
      </button>
    {/if}
  {/snippet}
</Modal>

<Modal bind:open={editOpen} title="Kontrol noktasını düzenle" width={440}>
  <form id="pref" class="stack" onsubmit={saveEdit} novalidate>
    <div class="field">
      <label for="pre-name">Ad</label>
      <input id="pre-name" class="input" maxlength="100" bind:value={editName} />
    </div>
    <label class="check">
      <input type="checkbox" bind:checked={editIpLock} />
      <span>
        IP'ye kilitle
        <small>Ajan yalnızca ilk bağlandığı IP'den veri gönderebilir; sunucu taşınırsa kilidi sıfırlayın.</small>
      </span>
    </label>
    {#if editIpLock && editLockedIp}
      <div class="field">
        <span class="help">Kilitli IP: <strong>{editLockedIp}</strong></span>
        <button type="button" class="btn sm" onclick={resetEditIp} disabled={editBusy}>Kilidi sıfırla</button>
      </div>
    {/if}
    {#if editError}<div class="alert error" role="alert">{editError}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (editOpen = false)}>Vazgeç</button>
    <button type="submit" form="pref" class="btn primary" disabled={editBusy}>
      {#if editBusy}<span class="spinner"></span>{/if} Kaydet
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
  .head > div {
    flex: 1 1 280px;
    min-width: 0;
  }
  .card-title {
    margin-bottom: 4px;
  }
  .sub {
    margin: 0;
    max-width: 760px;
  }
  .none {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--muted);
    padding: 14px 0 4px;
  }
  .probes td {
    vertical-align: middle;
  }
  .probes tr.dim td:not(.act) {
    opacity: 0.6;
  }
  .nm {
    font-weight: 600;
    word-break: break-word;
  }
  .nmw {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .pfx {
    display: block;
    font-weight: 400;
    color: var(--muted);
    font-size: 0.75rem;
  }
  .odot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--paused);
  }
  .odot.up {
    background: var(--up);
    box-shadow: 0 0 0 3px var(--up-ring);
  }
  .odot.down {
    background: var(--down);
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
  .foot {
    margin: 10px 0 0;
  }
  .label {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
  }
  .cmd-help {
    margin: -2px 0 6px;
  }
  .cmd {
    font-size: 0.78rem;
  }
  .nomargin {
    margin: 0;
  }
  .iplock {
    display: inline-flex;
    vertical-align: middle;
    margin-left: 4px;
    color: var(--muted);
  }
  .spacer {
    flex: 1;
  }
  @media (max-width: 720px) {
    .probes tr {
      position: relative;
    }
    .probes td.act {
      position: absolute;
      top: 8px;
      right: 0;
      width: auto;
      padding: 0;
    }
    .table.responsive.probes td.act::before,
    .table.responsive.probes td.nm::before {
      display: none;
    }
    .nm {
      padding-right: 44px !important;
      margin-bottom: 4px;
    }
  }
</style>
