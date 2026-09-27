<script lang="ts">
  // Sunucu ekleme ve kurulum komutu penceresi (yönetici).
  //  - create: ad sorar, ajan (kontrol noktası) oluşturur, komutu gösterir.
  //  - renew: mevcut ajanın token'ını yeniler ve güncel komutu gösterir (token bir
  //    kez gösterildiği için eski komut yeniden gösterilemez).
  // Komut gösterilirken ajanın bağlanması 5 sn'de bir sorgulanır; canlı akıştan
  // gelen güncelleme de anında yansır.
  import { api, errorMessage, type ProbeSetup, type ServerView } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import { servers } from '../lib/servers.svelte';
  import { copyText, toast } from '../lib/ui.svelte';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  let {
    open = $bindable(false),
    renew = null,
  }: {
    open?: boolean;
    /** Verilirse bu ajanın token'ı yenilenir; verilmezse yeni sunucu eklenir. */
    renew?: Pick<ServerView, 'id' | 'name'> | null;
  } = $props();

  let name = $state('');
  let error = $state('');
  let busy = $state(false);
  let setup = $state.raw<ProbeSetup | null>(null);
  let tab = $state<'docker' | 'systemd'>('docker');
  // Bağlantı: komut gösterildiği andaki son örnek/son görülme; bunlar ilerleyince yeni ajan bağlanmıştır.
  let base = { metrics: 0, seen: 0 };
  let view = $state.raw<ServerView | null>(null);

  // Pencere her açıldığında baştan başla.
  $effect(() => {
    if (open) {
      name = '';
      error = '';
      setup = null;
      view = null;
      tab = 'docker';
    }
  });

  async function create(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const n = name.trim();
    if (!n) return (error = 'Sunucuya bir ad verin (ör. CP Server İstanbul).');
    busy = true;
    try {
      base = { metrics: 0, seen: 0 };
      setup = await api.createProbe(n);
      servers.load();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function doRenew() {
    if (!renew) return;
    error = '';
    busy = true;
    try {
      const cur = servers.byId(renew.id);
      base = { metrics: cur?.metrics_at ?? 0, seen: cur?.last_seen_at ?? 0 };
      setup = await api.regenerateProbeToken(renew.id);
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  const probeId = $derived(setup?.probe.id ?? 0);
  // Canlı akıştaki en yeni bilgi ile sorgu sonucunun daha yenisi.
  const current = $derived.by(() => {
    const a = probeId ? servers.byId(probeId) : undefined;
    const b = view;
    if (!a) return b;
    if (!b) return a;
    return (a.last_seen_at ?? 0) >= (b.last_seen_at ?? 0) ? a : b;
  });
  const conn = $derived.by<'wait' | 'ok' | 'unavailable'>(() => {
    const c = current;
    if (!c) return 'wait';
    if (c.state === 'online' && c.metrics_at > base.metrics) return 'ok';
    if (c.state === 'unavailable' && c.last_seen_at > base.seen) return 'unavailable';
    return 'wait';
  });

  $effect(() => {
    if (!open || !probeId || conn === 'ok') return;
    const id = probeId;
    const poll = async () => {
      try {
        view = await api.getServer(id);
      } catch {
        /* sunucu henüz listede değil veya ağ hatası; tekrar denenir */
      }
    };
    const t = setInterval(poll, 5000);
    return () => clearInterval(t);
  });

  const dockerCmd = $derived(setup ? setup.docker_agent || setup.docker_command : '');
  const systemdCmd = $derived(setup?.systemd ?? '');
  const cmd = $derived(tab === 'systemd' && systemdCmd ? systemdCmd : dockerCmd);

  async function copy(text: string, what: string) {
    if (await copyText(text)) toast.success(`${what} panoya kopyalandı`);
    else toast.error('Kopyalanamadı; metni elle seçip kopyalayın');
  }

  function goto() {
    const id = probeId;
    open = false;
    navigate(`/servers/${id}`);
  }

  const title = $derived(
    setup ? `“${setup.probe.name}” için kurulum` : renew ? 'Kurulum komutu' : 'Sunucu ekle',
  );
</script>

<Modal bind:open {title} width={640}>
  {#if setup}
    <div class="stack">
      <div class="alert warning warn-row">
        <Icon name="key" size={16} />
        <span>Komuttaki token yalnızca şimdi gösterilir. Kaybederseniz sunucu sayfasından yeni komut alabilirsiniz (eski token geçersiz olur).</span>
      </div>

      {#if systemdCmd}
        <div class="seg" role="tablist" aria-label="Kurulum yöntemi">
          <button type="button" role="tab" aria-selected={tab === 'docker'} class:active={tab === 'docker'} onclick={() => (tab = 'docker')}>Docker</button>
          <button type="button" role="tab" aria-selected={tab === 'systemd'} class:active={tab === 'systemd'} onclick={() => (tab = 'systemd')}>Doğrudan (systemd)</button>
        </div>
      {/if}

      <div class="cmdbox">
        <div class="cmdbar">
          <p class="lead">
            {#if tab === 'docker'}
              İzlemek istediğiniz sunucuda, Docker kurulu bir kullanıcıyla çalıştırın.
            {:else}
              Docker kullanmayan sunucular için; <b>root</b> olarak çalıştırın.
            {/if}
          </p>
          <button type="button" class="btn sm primary" onclick={() => copy(cmd, 'Komut')}><Icon name="copy" size={14} /> Kopyala</button>
        </div>
        <pre>{cmd}</pre>
      </div>

      {#if tab === 'docker'}
        <details class="explain">
          <summary>Bu komut ne yapar?</summary>
          <ul>
            <li><code>--network host</code>, <code>--pid host</code>: ağ trafiği ve yük konteynerin değil, sunucunun kendisinden ölçülür.</li>
            <li><code>-v /:/host:ro</code>: sunucunun diskleri ve <code>/proc</code>, <code>/sys</code> bilgileri <b>salt okunur</b> bağlanır; hiçbir şey yazılmaz.</li>
            <li><code>docker.sock:ro</code>: konteyner listesi ve CPU/RAM kullanımları Docker’dan okunur. Docker yoksa bu kısmı silebilirsiniz.</li>
            <li><code>PROBE_TOKEN</code>: bu sunucuya özel anahtar; kimseyle paylaşmayın.</li>
            <li>Program açılışta bu panelden indirilir; her yeniden başlatmada en güncel sürüm gelir.</li>
          </ul>
        </details>
      {:else}
        <details class="explain">
          <summary>Bu komut ne yapar?</summary>
          <ul>
            <li>Programı bu panelden indirip <code>/usr/local/bin/uptime</code> olarak kaydeder.</li>
            <li><code>uptime-agent</code> adında bir systemd hizmeti oluşturur ve başlatır; sunucu yeniden başlasa da çalışır.</li>
            <li>Kaldırmak için: <code>systemctl disable --now uptime-agent</code></li>
          </ul>
        </details>
      {/if}

      <div class="conn {conn}" role="status" aria-live="polite">
        {#if conn === 'ok'}
          <span><b>Bağlandı ✓</b> İlk ölçümler geldi; sunucu listede görünüyor.</span>
        {:else if conn === 'unavailable'}
          <span class="cic"><Icon name="alert" size={16} /></span>
          <span><b>Ajan bağlandı ama metrik okuyamıyor.</b> {current?.note || 'Komutu eksiksiz (host bağlamalarıyla) çalıştırdığınızdan emin olun.'}</span>
        {:else}
          <span class="spinner"></span>
          <span><b>Bağlantı bekleniyor…</b> Komutu çalıştırdıktan sonra bir iki dakika içinde ilk ölçümler gelir.</span>
        {/if}
      </div>
    </div>
  {:else if renew}
    <div class="stack">
      <p class="nomargin">
        Güvenlik nedeniyle token yalnızca oluşturulduğunda gösterilir. Güncel kurulum komutunu almak için <b>{renew.name}</b> ajanının
        token’ı yenilenir.
      </p>
      <div class="alert warning">Eski token hemen geçersiz olur: ajanı sunucuda yeni komutla yeniden başlatmanız gerekir.</div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </div>
  {:else}
    <form id="srv-add" class="stack" onsubmit={create} novalidate>
      <p class="help nomargin intro">
        Sunucunuza küçük bir ajan kurarsınız; CPU, RAM, disk, ağ ve Docker konteynerlerini dakikada bir buraya gönderir. Eşik aşılınca
        monitörlerle aynı kanallardan bildirim alırsınız.
      </p>
      <div class="field">
        <label for="srv-name">Sunucu adı</label>
        <input id="srv-name" class="input" maxlength="100" bind:value={name} placeholder="ör. CP Server İstanbul" />
        <span class="help">Listede ve bildirimlerde görünür. Bu ajan istenirse monitörler için kontrol noktası olarak da kullanılabilir.</span>
      </div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {/if}

  {#snippet footer()}
    <div class="spacer"></div>
    {#if setup}
      <button type="button" class="btn" onclick={() => (open = false)}>Kapat</button>
      <button type="button" class="btn primary" onclick={goto}>Sunucuya git <Icon name="arrow-right" size={15} /></button>
    {:else if renew}
      <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
      <button type="button" class="btn primary" onclick={doRenew} disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if} Token’ı yenile ve komutu göster
      </button>
    {:else}
      <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
      <button type="submit" form="srv-add" class="btn primary" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if} Oluştur
      </button>
    {/if}
  {/snippet}
</Modal>

<style>
  .spacer {
    flex: 1;
  }
  .nomargin {
    margin: 0;
  }
  .intro {
    font-size: 0.88rem;
    color: var(--text-2);
  }
  .warn-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }
  .warn-row :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }
  .cmdbox {
    background: var(--input);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .cmdbar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 8px 8px 12px;
    border-bottom: 1px solid var(--border);
    background: var(--card-2);
  }
  .lead {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: 0.83rem;
    color: var(--text-2);
    line-height: 1.4;
  }
  .cmdbox pre {
    margin: 0;
    padding: 12px;
    font-size: 0.78rem;
    line-height: 1.55;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 230px;
    overflow-y: auto;
    color: var(--text);
  }
  .explain {
    font-size: 0.85rem;
    color: var(--text-2);
  }
  .explain summary {
    cursor: pointer;
    font-weight: 600;
    color: var(--accent-text);
    width: fit-content;
    border-radius: 4px;
  }
  .explain ul {
    margin: 8px 0 0;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .explain code {
    color: var(--text);
  }
  .conn {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 12px 14px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: var(--card-2);
    font-size: 0.88rem;
    color: var(--text-2);
  }
  .conn b {
    color: var(--text);
    margin-right: 4px;
  }
  .conn .spinner {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    margin-top: 2px;
    color: var(--accent);
  }
  .conn.ok {
    border-color: var(--up-border);
    background: var(--up-soft);
  }
  .conn.ok b {
    color: var(--up);
  }
  .conn.unavailable {
    border-color: var(--pending-border);
    background: var(--pending-soft);
  }
  .cic {
    display: inline-flex;
    flex-shrink: 0;
    margin-top: 2px;
  }
  .conn.ok .cic {
    color: var(--up);
  }
  .conn.unavailable .cic {
    color: var(--pending);
  }
</style>
