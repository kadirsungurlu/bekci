<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, errorMessage, type NotificationChannel, type Probe, type ProbeSetup } from '../../lib/api';
  import { NOTIFY_LABELS } from '../../lib/notifyTypes';
  import { live } from '../../lib/live.svelte';
  import { clock, confirmDialog, toast } from '../../lib/ui.svelte';
  import { collator, fmtDate, fmtRelative } from '../../lib/format';
  import Modal from '../../components/Modal.svelte';
  import RowMenu, { type MenuItem } from '../../components/RowMenu.svelte';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import { t, tParts } from '../../lib/i18n';
  import { session } from '../../lib/session.svelte';

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

  // Ajan sürümü panelden farklıysa güncelleme önerilir: eşzamanlı konum
  // kontrolü ve yeni User-Agent eski ajanlarda yoktur.
  const outdated = (p: Probe) => !!p.version && !!session.version && p.version !== session.version;

  function probeState(p: Probe): { l: string; c: string } {
    if (!p.active) return { l: t('probes.state.disabled'), c: 'paused' };
    return p.online ? { l: t('probes.state.online'), c: 'up' } : { l: t('probes.state.offline'), c: 'down' };
  }

  // Yeni / token ----------------------------------------------------------------------
  let newOpen = $state(false);
  let newName = $state('');
  let newError = $state('');
  let newBusy = $state(false);
  let setup = $state<ProbeSetup | null>(null);
  // Başlık şablonda çevrilir (dil değişince yenilensin): eklendi / yeni token.
  let setupFor = $state<{ kind: 'added' | 'newToken'; name: string } | null>(null);
  const setupTitle = $derived(
    setupFor
      ? setupFor.kind === 'added'
        ? t('probes.setup.addedTitle', { name: setupFor.name })
        : t('probes.setup.newTokenTitle', { name: setupFor.name })
      : '',
  );

  function openNew() {
    newName = '';
    newError = '';
    setup = null;
    setupFor = null;
    newOpen = true;
  }

  async function create(e: SubmitEvent) {
    e.preventDefault();
    newError = '';
    const n = newName.trim();
    if (!n) return (newError = t('probes.form.errName'));
    newBusy = true;
    try {
      setup = await api.createProbe(n);
      setupFor = { kind: 'added', name: setup.probe.name };
      load();
    } catch (err) {
      newError = errorMessage(err);
    } finally {
      newBusy = false;
    }
  }

  async function regenerate(p: Probe) {
    const ok = await confirmDialog({
      title: t('probes.confirm.regenerateTitle'),
      message: t('probes.confirm.regenerateMessage', { name: p.name }),
      confirmText: t('probes.confirm.regenerateConfirm'),
      danger: true,
    });
    if (!ok) return;
    try {
      setup = await api.regenerateProbeToken(p.id);
      setupFor = { kind: 'newToken', name: p.name };
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
  let editNotify = $state(false);
  let editChannels = $state<number[]>([]);
  let editError = $state('');
  let editBusy = $state(false);
  // Bildirim kanalları düzenleme penceresi ilk açıldığında bir kez yüklenir.
  let channels = $state.raw<NotificationChannel[] | null>(null);

  async function loadChannels() {
    if (channels !== null) return;
    try {
      channels = (await api.notifications()).slice().sort((a, b) => collator.compare(a.name, b.name));
    } catch {
      channels = [];
    }
  }

  function openEdit(p: Probe) {
    editing = p;
    editName = p.name;
    editIpLock = p.ip_lock ?? false;
    editLockedIp = p.locked_ip ?? '';
    editNotify = p.notify_offline ?? false;
    editChannels = (p.notification_ids ?? []).slice();
    editError = '';
    editOpen = true;
    loadChannels();
  }

  function toggleChannel(id: number, on: boolean) {
    editChannels = on ? [...editChannels.filter((x) => x !== id), id] : editChannels.filter((x) => x !== id);
  }

  async function saveEdit(e: SubmitEvent) {
    e.preventDefault();
    if (!editing) return;
    editError = '';
    const n = editName.trim();
    if (!n) return (editError = t('probes.form.errNameRequired'));
    editBusy = true;
    try {
      await api.updateProbe(editing.id, n, editing.active, undefined, {
        ipLock: editIpLock,
        notifyOffline: editNotify,
        notificationIds: editChannels,
      });
      toast.success(t('probes.toast.saved'));
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
      toast.success(t('probes.toast.ipReset'));
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
        title: t('probes.confirm.disableTitle'),
        message: t('probes.confirm.disableMessage', { name: p.name }),
        confirmText: t('probes.confirm.disableConfirm'),
        danger: true,
      });
      if (!ok) return;
    }
    try {
      await api.updateProbe(p.id, p.name, !p.active);
      toast.success(p.active ? t('probes.toast.disabled') : t('probes.toast.enabled'));
      load();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function remove(p: Probe) {
    const ok = await confirmDialog({
      title: t('probes.confirm.deleteTitle'),
      message: t('probes.confirm.deleteMessage', { name: p.name }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteProbe(p.id);
      probes = probes.filter((x) => x.id !== p.id);
      toast.success(t('probes.toast.deleted'));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const menu = (p: Probe): MenuItem[] => [
    { label: t('common.edit'), icon: 'edit', onclick: () => openEdit(p) },
    p.active
      ? { label: t('probes.menu.disable'), icon: 'ban', onclick: () => toggle(p) }
      : { label: t('probes.menu.enable'), icon: 'check-circle', onclick: () => toggle(p) },
    { label: t('probes.menu.regenerate'), icon: 'key', onclick: () => regenerate(p) },
    { label: t('common.delete'), icon: 'trash', danger: true, onclick: () => remove(p) },
  ];
</script>

<section class="card">
  <div class="head">
    <div>
      <h2 class="card-title">{t('probes.title')}</h2>
      <p class="text-2 small sub">
        {#each tParts('probes.intro') as part, i (i)}
          {#if part.slot === 'probe'}<code>probe</code>{:else if part.slot === 'locations'}<b>{t('probes.locations')}</b
            >{:else}{part.text}{/if}
        {/each}
      </p>
    </div>
    <button class="btn primary" onclick={openNew}><Icon name="plus" size={16} /> {t('probes.newProbe')}</button>
  </div>

  {#if loading}
    <div class="skeleton" style="height:120px"></div>
  {:else if loadError}
    <div class="alert error">{loadError} <button class="linkbtn" onclick={load}>{t('common.retry')}</button></div>
  {:else if probes.length === 0}
    <div class="none">
      <Icon name="map-pin" size={22} />
      <span>{t('probes.empty')}</span>
    </div>
  {:else}
    <table class="table responsive probes">
      <thead>
        <tr>
          <th>{t('common.name')}</th>
          <th>{t('probes.col.status')}</th>
          <th>{t('probes.col.lastSeen')}</th>
          <th>{t('probes.col.address')}</th>
          <th>{t('common.version')}</th>
          <th>{t('probes.col.monitors')}</th>
          <th><span class="sr">{t('common.actions')}</span></th>
        </tr>
      </thead>
      <tbody>
        {#each sorted as p (p.id)}
          {@const st = probeState(p)}
          <tr class:dim={!p.active}>
            <td data-label={t('common.name')} class="nm">
              <span class="nmw">
                <span class="odot {st.c}" aria-hidden="true"></span>
                <span>
                  {p.name}
                  {#if p.token_prefix}<code class="pfx">{p.token_prefix}…</code>{/if}
                </span>
              </span>
            </td>
            <td data-label={t('probes.col.status')}><span class="badge {st.c}">{st.l}</span></td>
            <td data-label={t('probes.col.lastSeen')} class="nowrap" title={p.last_seen_at ? fmtDate(p.last_seen_at) : ''}>
              {p.last_seen_at ? fmtRelative(p.last_seen_at, clock.now) : t('probes.neverConnected')}
            </td>
            <td data-label={t('probes.col.address')} class="mono small">
              {p.last_ip || '—'}
              {#if p.ip_lock}<span class="iplock" title={p.locked_ip ? t('probes.lockedTo', { ip: p.locked_ip }) : t('probes.lockPending')}><Icon name="lock" size={12} /></span>{/if}
            </td>
            <td data-label={t('common.version')} class="small">
              {p.version || '—'}
              {#if outdated(p)}
                <span class="badge pending" title={t('probes.outdatedTitle', { v: p.version ?? '', server: session.version })}>{t('probes.outdated')}</span>
              {/if}
            </td>
            <td data-label={t('probes.col.monitors')}>{p.monitor_count ?? 0}</td>
            <td class="act"><RowMenu items={menu(p)} label={t('probes.actionsFor', { name: p.name })} /></td>
          </tr>
        {/each}
      </tbody>
    </table>
    <p class="help foot">
      {#each tParts('probes.foot') as part, i (i)}
        {#if part.slot === 'cmd'}<code>docker rm -f uptime-probe; docker volume rm uptime-probe-bin</code>{:else}{part.text}{/if}
      {/each}
    </p>
  {/if}
</section>

<Modal bind:open={newOpen} title={setup ? setupTitle : t('probes.newProbe')} width={600}>
  {#if setup}
    <div class="stack">
      <div class="alert warning">{t('probes.setup.shownOnce')}</div>
      <div>
        <div class="label">{t('probes.setup.token')}</div>
        <div class="copybox">
          <code>{setup.token}</code>
          <CopyButton text={setup.token} class="btn sm primary" />
        </div>
      </div>
      <div>
        <div class="label"><Icon name="terminal" size={14} /> {t('probes.setup.command')}</div>
        <p class="help cmd-help">
          {#each tParts('probes.setup.commandHelp') as part, i (i)}
            {#if part.slot === 'root'}<b>root</b>{:else if part.slot === 'sudo'}<code>sudo sh &lt;&lt;'UPTIME_KURULUM'</code
              >{:else}{part.text}{/if}
          {/each}
        </p>
        <div class="cmdwrap">
          <!-- Çok satırlı komut: satır sonları korunur, uzun satırlar yatay kayar; klavyeyle kaydırılabilsin diye odaklanabilir. -->
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <pre class="cmd" tabindex="0" aria-label={t('probes.setup.command')}>{setup.docker_command}</pre>
          <div class="cmdcopy"><CopyButton text={setup.docker_command} /></div>
        </div>
        <p class="help cmd-help after">
          {#each tParts('probes.setup.commandAfter') as part, i (i)}
            {#if part.slot === 'path'}<code>/etc/uptime-probe.env</code>{:else if part.slot === 'cmd'}<code
                >docker rm -f uptime-probe; docker volume rm uptime-probe-bin</code
              >{:else}{part.text}{/if}
          {/each}
        </p>
      </div>
      <p class="help nomargin">
        {#each tParts('probes.setup.serverUrl') as part, i (i)}
          {#if part.slot === 'url'}<code>{setup.server_url}</code>{:else if part.slot === 'online'}<b
              >{t('probes.state.online')}</b
            >{:else}{part.text}{/if}
        {/each}
      </p>
    </div>
  {:else}
    <form id="prf" class="stack" onsubmit={create} novalidate>
      <div class="field">
        <label for="pr-name">{t('common.name')}</label>
        <input id="pr-name" class="input" maxlength="100" bind:value={newName} placeholder={t('probes.form.namePlaceholder')} />
        <span class="help">{t('probes.form.nameHelp')}</span>
      </div>
      {#if newError}<div class="alert error" role="alert">{newError}</div>{/if}
    </form>
  {/if}
  {#snippet footer()}
    <div class="spacer"></div>
    {#if setup}
      <button type="button" class="btn primary" onclick={() => (newOpen = false)}>{t('probes.setup.copiedClose')}</button>
    {:else}
      <button type="button" class="btn" onclick={() => (newOpen = false)}>{t('common.cancel')}</button>
      <button type="submit" form="prf" class="btn primary" disabled={newBusy}>
        {#if newBusy}<span class="spinner"></span>{/if} {t('common.create')}
      </button>
    {/if}
  {/snippet}
</Modal>

<Modal bind:open={editOpen} title={t('probes.form.editTitle')} width={440}>
  <form id="pref" class="stack" onsubmit={saveEdit} novalidate>
    <div class="field">
      <label for="pre-name">{t('common.name')}</label>
      <input id="pre-name" class="input" maxlength="100" bind:value={editName} />
    </div>
    <label class="check">
      <input type="checkbox" bind:checked={editIpLock} />
      <span>
        {t('probes.form.ipLock')}
        <small>{t('probes.form.ipLockHelp')}</small>
      </span>
    </label>
    {#if editIpLock && editLockedIp}
      <div class="field">
        <span class="help"
          >{#each tParts('probes.form.lockedIp') as part, i (i)}{#if part.slot === 'ip'}<strong>{editLockedIp}</strong
              >{:else}{part.text}{/if}{/each}</span
        >
        <button type="button" class="btn sm" onclick={resetEditIp} disabled={editBusy}>{t('probes.form.resetLock')}</button>
      </div>
    {/if}
    <label class="check">
      <input type="checkbox" bind:checked={editNotify} />
      <span>
        {t('probes.form.notifyOffline')}
        <small>{t('probes.form.notifyOfflineHelp')}</small>
      </span>
    </label>
    {#if editNotify}
      <div class="field">
        <span class="label">{t('probes.form.channels')}</span>
        {#if channels === null}
          <div class="skeleton" style="height:40px"></div>
        {:else if channels.length === 0}
          <p class="help nomargin">
            {#each tParts('probes.form.noChannels') as part, i (i)}{#if part.slot === 'link'}<a href="#/notifications">{t('probes.form.addChannel')}</a>{:else}{part.text}{/if}{/each}
          </p>
        {:else}
          <div class="chs">
            {#each channels as c (c.id)}
              <label class="check ch">
                <input type="checkbox" checked={editChannels.includes(c.id)} onchange={(e) => toggleChannel(c.id, (e.currentTarget as HTMLInputElement).checked)} />
                <span>
                  {c.name}
                  <small>{NOTIFY_LABELS[c.type] ?? c.type}{c.active ? '' : ` · ${t('probes.form.channelOff')}`}</small>
                </span>
              </label>
            {/each}
          </div>
          {#if editChannels.length === 0}<p class="help nomargin warn">{t('probes.form.noChannelSelected')}</p>{/if}
        {/if}
      </div>
    {/if}
    {#if editError}<div class="alert error" role="alert">{editError}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (editOpen = false)}>{t('common.cancel')}</button>
    <button type="submit" form="pref" class="btn primary" disabled={editBusy}>
      {#if editBusy}<span class="spinner"></span>{/if} {t('common.save')}
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
  .cmdwrap {
    background: var(--input);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .cmd {
    margin: 0;
    padding: 10px 12px;
    font-size: 0.78rem;
    line-height: 1.55;
    white-space: pre;
    overflow: auto;
    max-height: 240px;
    color: var(--text);
  }
  .cmdcopy {
    display: flex;
    justify-content: flex-end;
    padding: 6px 8px;
    border-top: 1px solid var(--border);
    background: var(--card-2);
  }
  .cmd-help.after {
    margin: 6px 0 0;
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
  .chs {
    display: grid;
    gap: 6px;
    max-height: 220px;
    overflow: auto;
    padding: 2px;
  }
  .warn {
    color: var(--pending-text, var(--text-2));
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
