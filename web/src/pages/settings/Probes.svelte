<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, ApiError, errorMessage, type AppSettings, type NotificationChannel, type Probe, type ProbeSetup } from '../../lib/api';
  import { NOTIFY_LABELS } from '../../lib/notifyTypes';
  import { live } from '../../lib/live.svelte';
  import { clock, confirmDialog, toast } from '../../lib/ui.svelte';
  import { collator, fmtDate, fmtRelative } from '../../lib/format';
  import Modal from '../../components/Modal.svelte';
  import RowMenu, { type MenuItem } from '../../components/RowMenu.svelte';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import AgentVersion from '../../components/AgentVersion.svelte';
  import AgentAutoUpdate, { autoChoice, autoValue, type AutoChoice } from '../../components/AgentAutoUpdate.svelte';
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
    loadMesh();
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

  // Panelin sürümüne güncellenebilecek ajanlar ("Tümünü güncelle").
  const updatable = $derived(probes.filter((p) => p.update?.state === 'outdated' && !p.update.requested).length);
  let allBusy = $state(false);
  async function updateAll() {
    allBusy = true;
    try {
      const r = await api.updateAllAgents('location');
      toast.success(t('probes.update.allToast', { n: r.requested }));
      load();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      allBusy = false;
    }
  }

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


  // Mesh genel ayarları ---------------------------------------------------------------
  const MESH_DEF = { ping: 5, loss: 3, panel: 5 };
  let settings = $state.raw<AppSettings | null>(null);
  let meshError = $state('');
  let meshField = $state('');
  let meshBusy = $state(false);
  let meshOn = $state(true);
  let meshPing = $state<number | null>(MESH_DEF.ping);
  let meshLoss = $state<number | null>(MESH_DEF.loss);
  let meshPanel = $state<number | null>(MESH_DEF.panel);
  // Kaydedilmiş genel değerler (probe penceresindeki "genel değer" etiketleri).
  const globalMesh = $derived({
    ping: settings?.mesh_ping_sec || MESH_DEF.ping,
    loss: settings?.mesh_loss_threshold || MESH_DEF.loss,
    panel: settings?.probe_panel_sec || MESH_DEF.panel,
  });

  function applyMesh(st: AppSettings) {
    settings = st;
    meshOn = st.mesh_enabled ?? true;
    meshPing = st.mesh_ping_sec || MESH_DEF.ping;
    meshLoss = st.mesh_loss_threshold || MESH_DEF.loss;
    meshPanel = st.probe_panel_sec || MESH_DEF.panel;
  }

  async function loadMesh() {
    try {
      applyMesh(await api.settings());
    } catch (e) {
      meshError = errorMessage(e);
    }
  }

  const isInt = (v: number | null, lo: number, hi: number) => v !== null && Number.isInteger(v) && v >= lo && v <= hi;

  /** Etkin zamanlamayı denetler (sunucudaki ValidateProbeTiming ile aynı kurallar); hata: [alan, metin]. */
  function timingError(ping: number | null, loss: number | null, panel: number | null): [string, string] | null {
    if (!isInt(ping, 1, 60)) return ['mesh_ping_sec', t('probes.mesh.errPing')];
    if (!isInt(loss, 1, 10)) return ['mesh_loss_threshold', t('probes.mesh.errLoss')];
    if (!isInt(panel, 1, 60)) return ['panel_sec', t('probes.mesh.errPanel')];
    if (loss! * ping! < 3 || loss! * panel! < 3) return ['mesh_loss_threshold', t('probes.mesh.errWindow')];
    return null;
  }

  /** Sunucunun alan hatası (400 {error, field}): alan + arayüz dilinde metin. */
  function fieldError(e: unknown): [string, string] {
    const field = e instanceof ApiError ? ((e.data as { field?: string } | null)?.field ?? '') : '';
    const msg: Record<string, string> = {
      mesh_ping_sec: t('probes.mesh.errPing'),
      panel_sec: t('probes.mesh.errPanel'),
      mesh_addr: t('probes.mesh.errAddr'),
      mesh_loss_threshold: / x /.test(errorMessage(e)) ? t('probes.mesh.errWindow') : t('probes.mesh.errLoss'),
    };
    return [field, msg[field] ?? errorMessage(e)];
  }

  const offlineAfter = $derived(isInt(meshLoss, 1, 10) && isInt(meshPanel, 1, 60) ? meshLoss! * meshPanel! : null);

  async function saveMesh(e: SubmitEvent) {
    e.preventDefault();
    if (!settings) return;
    const bad = timingError(meshPing, meshLoss, meshPanel);
    [meshField, meshError] = bad ?? ['', ''];
    if (bad) return;
    meshBusy = true;
    try {
      // Kayıtlı ayarlar korunur; yalnızca mesh alanları değişir.
      applyMesh(
        await api.saveSettings({
          ...settings,
          mesh_enabled: meshOn,
          mesh_ping_sec: meshPing!,
          mesh_loss_threshold: meshLoss!,
          probe_panel_sec: meshPanel!,
        }),
      );
      toast.success(t('probes.mesh.saved'));
      load();
    } catch (err) {
      [meshField, meshError] = fieldError(err);
    } finally {
      meshBusy = false;
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
  let editAuto = $state<AutoChoice>('inherit');
  // Mesh ezmeleri: *Own false ise genel değer kullanılır (null gönderilir).
  let editPingOwn = $state(false);
  let editPing = $state<number | null>(null);
  let editLossOwn = $state(false);
  let editLoss = $state<number | null>(null);
  let editPanelOwn = $state(false);
  let editPanel = $state<number | null>(null);
  let editAddr = $state('');
  let editFallback = $state(true);
  let editField = $state('');
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
    editAuto = autoChoice(p.update);
    editPingOwn = p.mesh_ping_sec != null;
    editPing = p.mesh_ping_sec ?? globalMesh.ping;
    editLossOwn = p.mesh_loss_threshold != null;
    editLoss = p.mesh_loss_threshold ?? globalMesh.loss;
    editPanelOwn = p.panel_sec != null;
    editPanel = p.panel_sec ?? globalMesh.panel;
    editAddr = p.mesh_addr ?? '';
    editFallback = p.fallback_notifier ?? true;
    editField = '';
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
    // Mesh alanları yalnızca sunucu destekliyorsa (yanıtta timing var) gönderilir.
    const hasMesh = !!editing.timing;
    if (hasMesh) {
      const bad = timingError(
        editPingOwn ? editPing : globalMesh.ping,
        editLossOwn ? editLoss : globalMesh.loss,
        editPanelOwn ? editPanel : globalMesh.panel,
      );
      editField = bad?.[0] ?? '';
      if (bad) return (editError = bad[1]);
    }
    editBusy = true;
    try {
      await api.updateProbe(editing.id, n, editing.active, undefined, {
        ipLock: editIpLock,
        notifyOffline: editNotify,
        notificationIds: editChannels,
        ...(editing.update ? { autoUpdate: autoValue(editAuto) } : {}),
        ...(hasMesh
          ? {
              mesh: {
                mesh_ping_sec: editPingOwn ? editPing : null,
                mesh_loss_threshold: editLossOwn ? editLoss : null,
                panel_sec: editPanelOwn ? editPanel : null,
                mesh_addr: editAddr.trim(),
                fallback_notifier: editFallback,
              },
            }
          : {}),
      });
      toast.success(t('probes.toast.saved'));
      editOpen = false;
      load();
    } catch (err) {
      [editField, editError] = fieldError(err);
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
    <div class="head-actions">
      {#if updatable > 0}
        <button class="btn" onclick={updateAll} disabled={allBusy} title={t('probes.update.allTitle', { n: updatable, panel: session.version })}>
          {#if allBusy}<span class="spinner"></span>{:else}<Icon name="refresh" size={16} />{/if}
          {t('probes.update.all')} ({updatable})
        </button>
      {/if}
      <button class="btn primary" onclick={openNew}><Icon name="plus" size={16} /> {t('probes.newProbe')}</button>
    </div>
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
              <AgentVersion id={p.id} version={p.version} update={p.update} onchange={load} />
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

<form class="card stack mesh" onsubmit={saveMesh} novalidate>
  <div>
    <h2 class="card-title">{t('probes.mesh.title')}</h2>
    <p class="text-2 small sub">{t('probes.mesh.intro')}</p>
  </div>
  {#if !settings}
    {#if meshError}
      <div class="alert error">{meshError} <button type="button" class="linkbtn" onclick={loadMesh}>{t('common.retry')}</button></div>
    {:else}
      <div class="skeleton" style="height:120px"></div>
    {/if}
  {:else}
    <label class="check">
      <input type="checkbox" bind:checked={meshOn} />
      <span>
        {t('probes.mesh.enabled')}
        <small>{t('probes.mesh.enabledHelp')}</small>
      </span>
    </label>
    <div class="grid-3">
      <div class="field">
        <label for="mesh-ping">{t('probes.mesh.pingSec')}</label>
        <input id="mesh-ping" class="input" type="number" min="1" max="60" bind:value={meshPing} aria-invalid={meshField === 'mesh_ping_sec'} />
        <span class="help">{t('probes.mesh.pingSecHelp')}</span>
      </div>
      <div class="field">
        <label for="mesh-loss">{t('probes.mesh.lossThreshold')}</label>
        <input id="mesh-loss" class="input" type="number" min="1" max="10" bind:value={meshLoss} aria-invalid={meshField === 'mesh_loss_threshold'} />
        <span class="help">{t('probes.mesh.lossThresholdHelp')}</span>
      </div>
      <div class="field">
        <label for="mesh-panel">{t('probes.mesh.panelSec')}</label>
        <input id="mesh-panel" class="input" type="number" min="1" max="60" bind:value={meshPanel} aria-invalid={meshField === 'panel_sec'} />
        <span class="help">{t('probes.mesh.panelSecHelp')}</span>
      </div>
    </div>
    {#if offlineAfter !== null}
      <p class="info"><Icon name="clock" size={14} /> {t('probes.mesh.offlineAfter', { loss: meshLoss!, panel: meshPanel!, n: offlineAfter })}</p>
    {/if}
    {#if meshError}<div class="alert error" role="alert">{meshError}</div>{/if}
    <div class="actions">
      <button class="btn primary" type="submit" disabled={meshBusy}>
        {#if meshBusy}<span class="spinner"></span>{/if}
        {t('common.save')}
      </button>
    </div>
  {/if}
</form>

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
    {#if editing?.timing}
      <fieldset class="meshbox">
        <legend>{t('probes.mesh.probeTitle')}</legend>
        {#snippet over(id: string, label: string, field: string, globalV: number, min: number, max: number)}
          <div class="field ov">
            <label for={id}>{label}</label>
            <div class="ovrow">
              {#if field === 'mesh_ping_sec'}
                <input id={id} class="input" type="number" {min} {max} bind:value={editPing} disabled={!editPingOwn} aria-invalid={editField === field} />
                <label class="check sm"><input type="checkbox" checked={!editPingOwn} onchange={(e) => (editPingOwn = !e.currentTarget.checked)} /><span>{t('probes.mesh.useGlobal', { v: globalV })}</span></label>
              {:else if field === 'mesh_loss_threshold'}
                <input id={id} class="input" type="number" {min} {max} bind:value={editLoss} disabled={!editLossOwn} aria-invalid={editField === field} />
                <label class="check sm"><input type="checkbox" checked={!editLossOwn} onchange={(e) => (editLossOwn = !e.currentTarget.checked)} /><span>{t('probes.mesh.useGlobal', { v: globalV })}</span></label>
              {:else}
                <input id={id} class="input" type="number" {min} {max} bind:value={editPanel} disabled={!editPanelOwn} aria-invalid={editField === field} />
                <label class="check sm"><input type="checkbox" checked={!editPanelOwn} onchange={(e) => (editPanelOwn = !e.currentTarget.checked)} /><span>{t('probes.mesh.useGlobal', { v: globalV })}</span></label>
              {/if}
            </div>
          </div>
        {/snippet}
        {@render over('pre-ping', t('probes.mesh.pingSec'), 'mesh_ping_sec', globalMesh.ping, 1, 60)}
        {@render over('pre-loss', t('probes.mesh.lossThreshold'), 'mesh_loss_threshold', globalMesh.loss, 1, 10)}
        {@render over('pre-panel', t('probes.mesh.panelSec'), 'panel_sec', globalMesh.panel, 1, 60)}
        <div class="field">
          <label for="pre-addr">{t('probes.mesh.addr')}</label>
          <input id="pre-addr" class="input mono" maxlength="255" bind:value={editAddr} placeholder={t('probes.mesh.addrPlaceholder')} spellcheck="false" autocomplete="off" aria-invalid={editField === 'mesh_addr'} />
          <span class="help">{t('probes.mesh.addrHelp')}</span>
        </div>
        <label class="check">
          <input type="checkbox" bind:checked={editFallback} />
          <span>
            {t('probes.mesh.fallback')}
            <small>{t('probes.mesh.fallbackHelp')}</small>
          </span>
        </label>
        <p class="help nomargin">
          {t('probes.mesh.effective', { ping: editing.timing.ping_sec, loss: editing.timing.loss_threshold, panel: editing.timing.panel_sec })}
        </p>
      </fieldset>
    {/if}
    {#if editing?.update}<AgentAutoUpdate id="pre-auto" bind:value={editAuto} update={editing.update} />{/if}
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
  .head > div:first-child {
    flex: 1 1 280px;
    min-width: 0;
  }
  .head-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
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
  .mesh {
    margin-top: 16px;
  }
  .info {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    color: var(--text-2);
    font-size: 0.85rem;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
  .meshbox {
    display: grid;
    gap: 12px;
    margin: 0;
    padding: 10px 12px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .meshbox legend {
    padding: 0 6px;
    font-weight: 600;
    font-size: 0.9rem;
  }
  .ovrow {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .ovrow .input {
    width: 96px;
    flex: 0 0 96px;
  }
  .check.sm {
    font-size: 0.85rem;
    align-items: center;
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
