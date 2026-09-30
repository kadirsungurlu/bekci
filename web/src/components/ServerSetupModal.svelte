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
  import { t, tParts, type TKey } from '../lib/i18n';
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
    if (!n) return (error = t('servers.setup.errName'));
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
    setup ? t('servers.setup.titleFor', { name: setup.probe.name }) : renew ? t('servers.setup.command') : t('servers.setup.titleAdd'),
  );
</script>

<!-- Biçimli cümle: {ad} yer tutucuları code'da ise <code>, bold'da ise <b> olarak çizilir. -->
{#snippet rich(key: TKey, code: Record<string, string>, bold: Record<string, string>)}
  {#each tParts(key) as p, i (i)}{#if p.slot !== undefined && p.slot in code}<code>{code[p.slot]}</code>{:else if p.slot !== undefined && p.slot in bold}<b>{bold[p.slot]}</b>{:else}{p.text}{/if}{/each}
{/snippet}

<Modal bind:open {title} width={640}>
  {#if setup}
    <div class="stack">
      <div class="alert warning warn-row">
        <Icon name="key" size={16} />
        <span>{t('servers.setup.tokenOnce')}</span>
      </div>

      {#if systemdCmd || windowsCmd}
        <div class="seg" role="tablist" aria-label={t('servers.setup.method')}>
          <button type="button" role="tab" aria-selected={tab === 'docker'} class:active={tab === 'docker'} onclick={() => (tab = 'docker')}>Docker</button>
          {#if systemdCmd}
            <button type="button" role="tab" aria-selected={tab === 'systemd'} class:active={tab === 'systemd'} onclick={() => (tab = 'systemd')}>{t('servers.setup.systemd')}</button>
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
              {t('servers.setup.leadDocker')}
            {:else if tab === 'windows'}
              {@render rich('servers.setup.leadWindows', {}, { b: t('servers.setup.leadWindowsB') })}
            {:else}
              {t('servers.setup.leadSystemd')}
            {/if}
          </p>
          <CopyButton text={cmd} class="btn sm primary" />
        </div>
        <!-- Çok satırlı komut: uzun satırlar kutuya sığacak şekilde alta kayar (yatay
             kaydırma çubuğu görünmediği için komutun devamı fark edilmiyordu); alta
             kayan kısım içeriden başlar, yeni satırla karışmaz. Kopyala düğmesi komutu
             olduğu gibi kopyalar. -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <pre tabindex="0" aria-label={t('servers.setup.command')}>{#each cmd.split('\n') as line, i (i)}<span class="ln">{line || ' '}</span>{/each}</pre>
      </div>
      {#if tab !== 'windows'}
        <p class="help nomargin">
          {@render rich('servers.setup.rootHint', { sudo: "sudo sh <<'UPTIME_KURULUM'" }, { root: 'root' })}
        </p>
      {/if}

      {#if tab === 'docker'}
        <details class="explain">
          <summary>{t('servers.setup.explain')}</summary>
          <ul>
            <li>{@render rich('servers.setup.docker.host', { a: '--network host', b: '--pid host' }, {})}</li>
            <li>
              {@render rich(
                'servers.setup.docker.mount',
                { a: '-v /:/host:ro,rslave', proc: '/proc', sys: '/sys' },
                { ro: t('servers.setup.docker.readOnly') },
              )}
            </li>
            <li>{@render rich('servers.setup.docker.sock', { a: 'docker.sock:ro' }, {})}</li>
            <li>{@render rich('servers.setup.docker.caps', { a: '--cap-drop ALL', b: '--security-opt no-new-privileges', c: '--memory' }, {})}</li>
            <li>{@render rich('servers.setup.docker.token', { file: '/etc/uptime-agent.env' }, {})}</li>
            <li>
              {@render rich(
                'servers.setup.docker.bin',
                { vol: 'uptime-agent-bin', cmd: 'docker rm -f uptime-agent; docker volume rm uptime-agent-bin' },
                { b: t('servers.setup.docker.binB') },
              )}
            </li>
          </ul>
        </details>
      {:else if tab === 'windows'}
        <details class="explain">
          <summary>{t('servers.setup.explain')}</summary>
          <ul>
            <li>{@render rich('servers.setup.windows.download', { path: 'C:\\Program Files\\Uptime\\uptime.exe' }, {})}</li>
            <li>{@render rich('servers.setup.windows.service', { svc: 'uptime-agent' }, {})}</li>
            <li>{t('servers.setup.windows.verify')}</li>
            <li>{@render rich('servers.setup.windows.token', { env: 'C:\\Program Files\\Uptime\\agent.env', log: 'agent.log' }, {})}</li>
            <li>{@render rich('servers.setup.updateUninstall', { cmd: "& 'C:\\Program Files\\Uptime\\uptime.exe' service uninstall" }, {})}</li>
            <li>{t('servers.setup.windows.history')}</li>
            <li>{t('servers.setup.windows.load')}</li>
          </ul>
        </details>
      {:else}
        <details class="explain">
          <summary>{t('servers.setup.explain')}</summary>
          <ul>
            <li>{@render rich('servers.setup.systemdInfo.download', { path: '/usr/local/bin/uptime' }, {})}</li>
            <li>{@render rich('servers.setup.systemdInfo.service', { svc: 'uptime-agent' }, {})}</li>
            <li>{@render rich('servers.setup.systemdInfo.token', { file: '/etc/uptime-agent.env' }, {})}</li>
            <li>{@render rich('servers.setup.updateUninstall', { cmd: 'systemctl disable --now uptime-agent' }, {})}</li>
          </ul>
        </details>
      {/if}

      <div class="conn {conn}" role="status" aria-live="polite">
        {#if conn === 'ok'}
          <span><b>{t('servers.setup.conn.okTitle')}</b> {t('servers.setup.conn.okText')}</span>
        {:else if conn === 'unavailable'}
          <span class="cic"><Icon name="alert" size={16} /></span>
          <span><b>{t('servers.setup.conn.unavailableTitle')}</b> {current?.note || t('servers.setup.conn.unavailableHint')}</span>
        {:else}
          <span class="spinner"></span>
          <span><b>{t('servers.setup.conn.waitTitle')}</b> {t('servers.setup.conn.waitText')}</span>
        {/if}
      </div>
    </div>
  {:else if renew}
    <div class="stack">
      <p class="nomargin">{@render rich('servers.setup.renewText', {}, { name: renew.name })}</p>
      <div class="alert warning">{t('servers.setup.renewWarn')}</div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </div>
  {:else}
    <form id="srv-add" class="stack" onsubmit={create} novalidate>
      <p class="help nomargin intro">{t('servers.setup.intro')}</p>
      <div class="field">
        <label for="srv-name">{t('servers.setup.nameLabel')}</label>
        <input id="srv-name" class="input" maxlength="100" bind:value={name} placeholder={t('servers.setup.namePlaceholder')} />
        <span class="help">{t('servers.setup.nameHelp')}</span>
      </div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {/if}

  {#snippet footer()}
    <div class="spacer"></div>
    {#if setup}
      <button type="button" class="btn" onclick={() => (open = false)}>{t('common.close')}</button>
      <button type="button" class="btn primary" onclick={goto}>{t('servers.setup.goto')} <Icon name="arrow-right" size={15} /></button>
    {:else if renew}
      <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
      <button type="button" class="btn primary" onclick={doRenew} disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if} {t('servers.setup.renewBtn')}
      </button>
    {:else}
      <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
      <button type="submit" form="srv-add" class="btn primary" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if} {t('common.create')}
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
    overflow: auto;
    max-height: 260px;
    tab-size: 2;
    color: var(--text);
  }
  .cmdbox .ln {
    display: block;
    padding-left: 2ch;
    text-indent: -2ch;
    overflow-wrap: anywhere;
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
