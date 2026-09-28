<script lang="ts">
  // Sunucu ekleme ve kurulum komutu penceresi (yönetici).
  //  - create: ad sorar, sunucu kaydı oluşturur, komutu gösterir.
  //  - renew: mevcut ajanın token'ını yeniler ve güncel komutu gösterir (token bir
  //    kez gösterildiği için eski komut yeniden gösterilemez).
  // Komut gösterilirken ajanın bağlanması 5 sn'de bir sorgulanır; canlı akıştan
  // gelen güncelleme de anında yansır.
  import { api, errorMessage, type ProbeSetup, type ServerView } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import { servers } from '../lib/servers.svelte';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import CopyButton from './CopyButton.svelte';

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
  let tab = $state<'docker' | 'systemd' | 'windows'>('docker');
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
      setup = await api.createServer(n);
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
  const windowsCmd = $derived(setup?.windows ?? '');
  const cmd = $derived(
    tab === 'systemd' && systemdCmd ? systemdCmd : tab === 'windows' && windowsCmd ? windowsCmd : dockerCmd,
  );

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

      {#if systemdCmd || windowsCmd}
        <div class="seg" role="tablist" aria-label="Kurulum yöntemi">
          <button type="button" role="tab" aria-selected={tab === 'docker'} class:active={tab === 'docker'} onclick={() => (tab = 'docker')}>Docker</button>
          {#if systemdCmd}
            <button type="button" role="tab" aria-selected={tab === 'systemd'} class:active={tab === 'systemd'} onclick={() => (tab = 'systemd')}>Doğrudan (systemd)</button>
          {/if}
          {#if windowsCmd}
            <button type="button" role="tab" aria-selected={tab === 'windows'} class:active={tab === 'windows'} onclick={() => (tab = 'windows')}>Windows</button>
          {/if}
        </div>
      {/if}

      <div class="cmdbox">
        <div class="cmdbar">
          <p class="lead">
            {#if tab === 'docker'}
              İzlemek istediğiniz sunucuda çalıştırın.
            {:else if tab === 'windows'}
              Windows Server’da <b>PowerShell’i “Yönetici olarak çalıştır”</b> ile açıp yapıştırın.
            {:else}
              Docker kullanmayan sunucular için.
            {/if}
          </p>
          <CopyButton text={cmd} class="btn sm primary" />
        </div>
        <!-- Çok satırlı komut: satır sonları korunur, uzun satırlar yatay kayar; klavyeyle kaydırılabilsin diye odaklanabilir. -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <pre tabindex="0" aria-label="Kurulum komutu">{cmd}</pre>
      </div>
      {#if tab !== 'windows'}
        <p class="help nomargin">
          Komutun tamamını <b>root</b> olarak yapıştırın (ya da ilk satırı <code>sudo sh &lt;&lt;'UPTIME_KURULUM'</code> yapın).
        </p>
      {/if}

      {#if tab === 'docker'}
        <details class="explain">
          <summary>Bu komut ne yapar?</summary>
          <ul>
            <li><code>--network host</code>, <code>--pid host</code>: ağ trafiği ve yük konteynerin değil, sunucunun kendisinden ölçülür.</li>
            <li><code>-v /:/host:ro,rslave</code>: sunucunun diskleri ve <code>/proc</code>, <code>/sys</code> bilgileri <b>salt okunur</b> bağlanır; hiçbir şey yazılmaz. Sonradan takılan diskler de görünür.</li>
            <li><code>docker.sock:ro</code>: konteyner listesi ve CPU/RAM kullanımları Docker’dan okunur. Docker yoksa bu kısmı silebilirsiniz.</li>
            <li><code>--cap-drop ALL</code>, <code>--security-opt no-new-privileges</code>, <code>--memory</code>: konteynerin yetkileri ve kaynakları kısıtlanır.</li>
            <li>Token bu sunucuya özel anahtardır; komut onu <code>/etc/uptime-agent.env</code> dosyasına yalnızca root’un okuyabileceği (600) izinle yazar. Kimseyle paylaşmayın.</li>
            <li>Program bir kez indirilip SHA-256 ile doğrulanır ve <code>uptime-agent-bin</code> biriminde saklanır; <b>yeniden başlatmada tekrar indirilmez</b> (sürüm sabit). Güncellemek için: <code>docker rm -f uptime-agent; docker volume rm uptime-agent-bin</code>, sonra komutu tekrar çalıştırın.</li>
          </ul>
        </details>
      {:else if tab === 'windows'}
        <details class="explain">
          <summary>Bu komut ne yapar?</summary>
          <ul>
            <li>Programı bu panelden indirip <code>C:\Program Files\Uptime\uptime.exe</code> olarak kaydeder.</li>
            <li><code>uptime-agent</code> adında bir Windows hizmeti kurar ve başlatır; sunucu yeniden başlasa da çalışır, hata olursa kendini yeniden başlatır.</li>
            <li>İndirilen program, kurulumdan önce SHA-256 ile doğrulanır (uyuşmazsa kurulum durur).</li>
            <li>Token, yalnızca yöneticilerin okuyabildiği <code>C:\Program Files\Uptime\agent.env</code> dosyasına yazılır; günlük aynı klasörde (<code>agent.log</code>).</li>
            <li>Güncellemek için aynı komutu tekrar çalıştırın. Kaldırmak için: <code>&amp; 'C:\Program Files\Uptime\uptime.exe' service uninstall</code></li>
            <li>Komut token içerir: PowerShell geçmişine yazılmaması için PSReadLine 2.2 veya üstü önerilir (eskilerde geçmiş dosyası kullanıcı klasöründe kalır).</li>
            <li>Windows’ta yük ortalaması yoktur; yük, işlemci kuyruğu uzunluğundan hesaplanan yaklaşık bir değerdir. Sıcaklık ve Docker konteynerleri toplanmaz.</li>
          </ul>
        </details>
      {:else}
        <details class="explain">
          <summary>Bu komut ne yapar?</summary>
          <ul>
            <li>Programı bu panelden indirir, SHA-256 ile doğrular ve <code>/usr/local/bin/uptime</code> olarak kaydeder; hizmet yeniden başlarken tekrar indirmez.</li>
            <li><code>uptime-agent</code> adında bir systemd hizmeti oluşturur ve başlatır; sunucu yeniden başlasa da çalışır. Yetki yükseltme kapalı, bellek sınırlı.</li>
            <li>Token <code>/etc/uptime-agent.env</code> dosyasında yalnızca root’un okuyabileceği (600) izinle tutulur.</li>
            <li>Güncellemek için aynı komutu tekrar çalıştırın. Kaldırmak için: <code>systemctl disable --now uptime-agent</code></li>
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
        Linux veya Windows sunucunuza küçük bir ajan kurarsınız; CPU, RAM, disk, ağ ve Docker konteynerlerini dakikada bir buraya gönderir. Eşik aşılınca
        monitörlerle aynı kanallardan bildirim alırsınız.
      </p>
      <div class="field">
        <label for="srv-name">Sunucu adı</label>
        <input id="srv-name" class="input" maxlength="100" bind:value={name} placeholder="ör. CP Server İstanbul" />
        <span class="help">Listede ve bildirimlerde görünür.</span>
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
    white-space: pre;
    overflow: auto;
    max-height: 260px;
    tab-size: 2;
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
