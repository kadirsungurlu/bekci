<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, ApiError, errorMessage, type ImportSummary } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { router } from '../../lib/router.svelte';
  import { confirmDialog, toast } from '../../lib/ui.svelte';
  import { fmtSize } from '../../lib/format';
  import FilePick from '../../components/FilePick.svelte';
  import ImportResult from '../../components/ImportResult.svelte';
  import Icon from '../../components/Icon.svelte';

  const MB = 1024 * 1024;

  // Aynı anda tek içe aktarma (sunucu da 409 ile reddeder).
  let busy = $state<'' | 'restore' | 'kuma' | 'ur'>('');

  /** Yükleme ilerlemesi: yüzde; 100'e ulaşınca sunucunun işlemesi beklenir. */
  interface Progress {
    loaded: number;
    total: number;
  }

  // Geri yükle ----------------------------------------------------------------------
  let rFile = $state<File | null>(null);
  let rMode = $state<'merge' | 'replace'>('merge');
  let rResult = $state.raw<ImportSummary | null>(null);
  let rError = $state('');
  let rProg = $state<Progress | null>(null);

  // Dosya veya mod değişince eski önizleme geçersiz.
  $effect(() => {
    void rFile;
    void rMode;
    rResult = null;
    rError = '';
  });

  function tooBig(f: File, limitMB: number, what: string): string {
    return f.size > limitMB * MB ? `${what} en fazla ${limitMB} MB olabilir (seçilen: ${fmtSize(f.size)}).` : '';
  }

  async function restore(dry: boolean) {
    if (!rFile) return (rError = 'Önce yedek dosyasını seçin.');
    const big = tooBig(rFile, 20, 'Yedek dosyası');
    if (big) return (rError = big);
    rError = '';
    const file = rFile;
    const mode = rMode;
    busy = 'restore';
    try {
      if (!dry && mode === 'replace') {
        // Silinecek kayıtların sayısı için önce önizleme (yoksa) alınır.
        let preview = rResult?.dry_run ? rResult : null;
        if (!preview) {
          rProg = { loaded: 0, total: file.size };
          preview = await api.importBackup(file, mode, true, (loaded, total) => (rProg = { loaded, total }));
          rResult = preview;
        }
        rProg = null;
        const d = preview.deleted ?? { monitors: 0, notifications: 0, tags: 0, status_pages: 0 };
        const ok = await confirmDialog({
          title: 'Mevcut kayıtlar silinecek',
          message: `Mevcut ${d.monitors} monitör, ${d.notifications} bildirim kanalı, ${d.tags} etiket ve ${d.status_pages} durum sayfası kalıcı olarak silinecek; kontrol geçmişleri de silinir. Kısıtlı izleyicilerin monitör atamaları da kalkar.`,
          confirmText: 'Sil ve geri yükle',
          danger: true,
        });
        if (!ok) return;
      }
      rProg = { loaded: 0, total: file.size };
      const res = await api.importBackup(file, mode, dry, (loaded, total) => (rProg = { loaded, total }));
      rResult = res;
      if (!dry) done(res, 'Yedek geri yüklendi');
    } catch (e) {
      rError = errorMessage(e);
    } finally {
      busy = '';
      rProg = null;
    }
  }

  // Uptime Kuma --------------------------------------------------------------------------
  let kFile = $state<File | null>(null);
  let kResult = $state.raw<ImportSummary | null>(null);
  let kError = $state('');
  let kProg = $state<Progress | null>(null);

  $effect(() => {
    void kFile;
    kResult = null;
    kError = '';
  });

  async function kuma(dry: boolean) {
    if (!kFile) return (kError = 'Önce Uptime Kuma yedeğini (JSON) veya kuma.db dosyasını seçin.');
    const big = tooBig(kFile, 200, 'Dosya');
    if (big) return (kError = big);
    kError = '';
    busy = 'kuma';
    kProg = { loaded: 0, total: kFile.size };
    try {
      const res = await api.importKuma(kFile, dry, (loaded, total) => (kProg = { loaded, total }));
      kResult = res;
      if (!dry) done(res, 'Uptime Kuma verileri içe aktarıldı');
    } catch (e) {
      kError = errorMessage(e);
    } finally {
      busy = '';
      kProg = null;
    }
  }

  // UptimeRobot ------------------------------------------------------------------------------
  let urKey = $state('');
  let urResult = $state.raw<ImportSummary | null>(null);
  let urError = $state('');

  $effect(() => {
    void urKey;
    urResult = null;
    urError = '';
  });

  async function uptimeRobot(dry: boolean) {
    const key = urKey.trim();
    if (!key) return (urError = 'UptimeRobot salt okunur API anahtarını girin.');
    if (/\s/.test(key)) return (urError = 'API anahtarı boşluk içeremez.');
    urError = '';
    busy = 'ur';
    try {
      const res = await api.importUptimeRobot(key, dry);
      urResult = res;
      if (!dry) done(res, 'UptimeRobot monitörleri içe aktarıldı');
    } catch (e) {
      urError =
        e instanceof ApiError && e.status === 502
          ? `UptimeRobot’a bağlanılamadı: ${e.message}`
          : errorMessage(e);
    } finally {
      busy = '';
    }
  }

  function done(res: ImportSummary, msg: string) {
    const n = res.created.monitors;
    toast.success(n > 0 ? `${msg}: ${n} monitör eklendi` : msg);
    live.refresh();
  }

  const pct = (p: Progress) => (p.total > 0 ? Math.min(100, Math.round((p.loaded / p.total) * 100)) : 0);

  // "Taşıyın" bağlantısıyla gelindiyse taşıma bölümüne kaydır.
  let migrateEl: HTMLElement | undefined = $state();
  onMount(async () => {
    if (/[?&]tasi\b/.test(router.path)) {
      await tick();
      migrateEl?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  });
</script>

{#snippet progress(p: Progress, what: string)}
  {@const v = pct(p)}
  <div class="prog" role="status">
    <div class="prog-t small">
      <span class="spinner sm-spin"></span>
      {#if v < 100}
        {what} yükleniyor… %{v} <span class="muted">({fmtSize(p.loaded)} / {fmtSize(p.total)})</span>
      {:else}
        Yüklendi; sunucu dosyayı işliyor…
      {/if}
    </div>
    <div class="bar" aria-hidden="true"><span style="width:{v}%"></span></div>
  </div>
{/snippet}

<div class="stack">
  <section class="card">
    <div class="sec-head">
      <span class="sic"><Icon name="download" size={18} /></span>
      <div>
        <h2 class="card-title">Yedeği indir</h2>
        <p class="text-2 small sub">
          Monitörler, bildirim kanalları, etiketler, durum sayfaları ve ayarlar tek bir JSON dosyasına aktarılır. Dosya şifreleri,
          token’ları ve API anahtarlarını açık halde içerir; güvenli bir yerde saklayın. Kullanıcılar, kontrol geçmişi ve işlem kaydı
          dahil değildir.
        </p>
      </div>
    </div>
    <a class="btn primary" href="/api/export" download><Icon name="download" size={16} /> Yedeği indir</a>
  </section>

  <section class="card">
    <div class="sec-head">
      <span class="sic"><Icon name="upload" size={18} /></span>
      <div>
        <h2 class="card-title">Geri yükle</h2>
        <p class="text-2 small sub">Bu uygulamadan indirdiğiniz yedek dosyasını (JSON, en fazla 20 MB) yükleyin. Önce önizleyip neyin değişeceğini görebilirsiniz.</p>
      </div>
    </div>
    <div class="stack">
      <FilePick bind:file={rFile} accept=".json,application/json" id="rs-file" label="Yedek seç" disabled={busy !== ''} />
      <div class="modes" role="radiogroup" aria-label="Geri yükleme modu">
        <label class="mode" class:on={rMode === 'merge'}>
          <input type="radio" name="rmode" value="merge" bind:group={rMode} />
          <span><b>Birleştir (önerilen)</b><small>Mevcut kayıtlar korunur, aynı ad ve hedefli monitörler tekrar eklenmez.</small></span>
        </label>
        <label class="mode danger" class:on={rMode === 'replace'}>
          <input type="radio" name="rmode" value="replace" bind:group={rMode} />
          <span>
            <b>Değiştir</b>
            <small>Mevcut tüm monitörler, bildirim kanalları, etiketler ve durum sayfaları silinir; ayarlar da geri yüklenir.</small>
          </span>
        </label>
      </div>
      {#if rProg}{@render progress(rProg, 'Yedek')}{/if}
      {#if rError}<div class="alert error" role="alert">{rError}</div>{/if}
      <div class="acts">
        <button class="btn" onclick={() => restore(true)} disabled={busy !== '' || !rFile}>
          {#if busy === 'restore' && !rResult}<span class="spinner"></span>{:else}<Icon name="eye" size={15} />{/if} Önizle
        </button>
        <button class="btn {rMode === 'replace' ? 'danger solid' : 'primary'}" onclick={() => restore(false)} disabled={busy !== '' || !rFile}>
          {#if busy === 'restore' && rResult}<span class="spinner"></span>{/if}
          Geri yükle
        </button>
      </div>
      {#if rResult}<ImportResult result={rResult} />{/if}
    </div>
  </section>

  <div class="migrate" bind:this={migrateEl}>
    <h2 class="mig-title">Başka bir servisten taşıyın<span class="dot">.</span></h2>
    <p class="text-2 small mig-sub">Monitörlerinizi tek tek yeniden eklemenize gerek yok. İçe aktarma her zaman birleştirme modunda çalışır; mevcut kayıtlarınız silinmez.</p>
  </div>

  <div class="mig-grid">
    <section class="card">
      <div class="sec-head">
        <span class="sic brand"><Icon name="log-in" size={18} /></span>
        <div>
          <h2 class="card-title">UptimeRobot’tan içe aktar</h2>
          <p class="text-2 small sub">
            UptimeRobot’ta <b>Integrations &amp; API → API</b> bölümünden “Read-only API key” oluşturup buraya yapıştırın. Anahtar kaydedilmez.
            HTTP, anahtar kelime, ping, port ve heartbeat monitörleri aktarılır; duraklatılmış olanlar durdurulmuş eklenir. Uyarı kişileri
            aktarılmaz. Heartbeat monitörleri için yeni push adresleri oluşturulur; cron işlerinizi güncelleyin.
          </p>
        </div>
      </div>
      <div class="stack">
        <div class="field">
          <label for="ur-key">Salt okunur API anahtarı</label>
          <input
            id="ur-key"
            class="input mono"
            type="password"
            autocomplete="off"
            spellcheck="false"
            bind:value={urKey}
            placeholder="ur1234567-…"
            disabled={busy !== ''}
          />
        </div>
        {#if urError}<div class="alert error" role="alert">{urError}</div>{/if}
        <div class="acts">
          <button class="btn" onclick={() => uptimeRobot(true)} disabled={busy !== '' || !urKey.trim()}>
            {#if busy === 'ur' && !urResult}<span class="spinner"></span>{:else}<Icon name="eye" size={15} />{/if} Önizle
          </button>
          <button class="btn primary" onclick={() => uptimeRobot(false)} disabled={busy !== '' || !urResult?.dry_run}>
            {#if busy === 'ur' && urResult}<span class="spinner"></span>{/if} İçe aktar
          </button>
        </div>
        {#if !urResult}<p class="help nomargin">Önce önizleyin; ne aktarılacağını gördükten sonra “İçe aktar” açılır.</p>{/if}
        {#if urResult}<ImportResult result={urResult} />{/if}
      </div>
    </section>

    <section class="card">
      <div class="sec-head">
        <span class="sic brand"><Icon name="archive" size={18} /></span>
        <div>
          <h2 class="card-title">Uptime Kuma’dan içe aktar</h2>
          <p class="text-2 small sub">
            Uptime Kuma’da <b>Ayarlar → Yedekle → Dışa aktar</b> ile aldığınız JSON dosyasını (Kuma 1.x) veya veri klasöründeki
            <code>kuma.db</code> dosyasını yükleyin (en fazla 200 MB; kuma.db’yi Kuma durdurulmuşken kopyalayın). Monitörler, bildirim
            kanalları ve etiketler aktarılır; geçmiş, durum sayfaları ve bakım pencereleri aktarılmaz. Push adresleri korunur: cron
            işlerinde yalnızca sunucu adını değiştirin.
          </p>
        </div>
      </div>
      <div class="stack">
        <FilePick
          bind:file={kFile}
          accept=".json,.db,.sqlite,application/json,application/vnd.sqlite3,application/x-sqlite3"
          id="kuma-file"
          label="Dosya seç"
          disabled={busy !== ''}
        />
        {#if kProg}{@render progress(kProg, 'Dosya')}{/if}
        {#if kError}<div class="alert error" role="alert">{kError}</div>{/if}
        <div class="acts">
          <button class="btn" onclick={() => kuma(true)} disabled={busy !== '' || !kFile}>
            {#if busy === 'kuma' && !kResult}<span class="spinner"></span>{:else}<Icon name="eye" size={15} />{/if} Önizle
          </button>
          <button class="btn primary" onclick={() => kuma(false)} disabled={busy !== '' || !kResult?.dry_run}>
            {#if busy === 'kuma' && kResult}<span class="spinner"></span>{/if} İçe aktar
          </button>
        </div>
        {#if !kResult}<p class="help nomargin">Önce önizleyin; ne aktarılacağını gördükten sonra “İçe aktar” açılır.</p>{/if}
        {#if kResult}<ImportResult result={kResult} />{/if}
      </div>
    </section>
  </div>
</div>

<style>
  .sec-head {
    display: flex;
    align-items: flex-start;
    gap: 14px;
    margin-bottom: 16px;
  }
  .sec-head > div {
    min-width: 0;
    flex: 1;
  }
  .sic {
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent-text);
    flex-shrink: 0;
  }
  .card-title {
    margin-bottom: 4px;
  }
  .sub {
    margin: 0;
    max-width: 780px;
  }
  .modes {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }
  .mode {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 12px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    cursor: pointer;
    font-size: 0.9rem;
  }
  .mode.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent) inset;
  }
  .mode.danger.on {
    border-color: var(--down);
    box-shadow: 0 0 0 1px var(--down) inset;
  }
  .mode input {
    appearance: none;
    -webkit-appearance: none;
    width: 18px;
    height: 18px;
    margin: 1px 0 0;
    border-radius: 50%;
    border: 1.5px solid var(--border-hover);
    background: var(--input);
    flex-shrink: 0;
    cursor: pointer;
  }
  .mode input:checked {
    border: 5px solid var(--accent);
  }
  .mode.danger input:checked {
    border-color: var(--down);
  }
  .mode input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .mode small {
    display: block;
    color: var(--muted);
    font-size: 0.8rem;
    line-height: 1.4;
    margin-top: 2px;
  }
  .acts {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .nomargin {
    margin: 0;
  }
  .prog {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .prog-t {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-2);
  }
  .sm-spin {
    width: 14px;
    height: 14px;
  }
  .bar {
    height: 6px;
    border-radius: 999px;
    background: var(--progress-track);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.2s;
  }
  .migrate {
    margin-top: 10px;
    scroll-margin-top: 70px;
  }
  .mig-title {
    font-size: 1.15rem;
  }
  .mig-sub {
    margin: 4px 0 0;
  }
  .mig-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
    align-items: start;
  }
  @media (max-width: 1100px) {
    .mig-grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 640px) {
    .modes {
      grid-template-columns: minmax(0, 1fr);
    }
    .sec-head {
      gap: 12px;
    }
    .acts > :global(*) {
      flex: 1;
    }
  }
</style>
