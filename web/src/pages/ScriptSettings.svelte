<script lang="ts">
  import { onMount } from 'svelte';
  import ScriptsShell from '../components/ScriptsShell.svelte';
  import CodeModal from '../components/CodeModal.svelte';
  import Icon from '../components/Icon.svelte';
  import { api, SCRIPT_EXPORT_URL, scriptsApi, type NotificationChannel, type ScriptChain, type ScriptControl, type ScriptHost, type ScriptKey } from '../lib/api';
  import { isPostgresRequired, reasonText, scriptError, shortId } from '../lib/scripts';
  import { fmtDate } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { t } from '../lib/i18n';

  let keys = $state.raw<ScriptKey[]>([]);
  let hosts = $state.raw<ScriptHost[]>([]);
  let control = $state.raw<ScriptControl | null>(null);
  let chain = $state.raw<ScriptChain | null>(null);
  let channels = $state.raw<NotificationChannel[]>([]);
  let picked = $state<number[]>([]);
  let loading = $state(true);
  let loadError = $state('');
  let pg = $state(false);

  const canWrite = $derived(!!session.user?.two_factor_enabled);

  async function load() {
    try {
      const [k, h, c, ch, n] = await Promise.all([scriptsApi.keys(), scriptsApi.hosts(), scriptsApi.list(), scriptsApi.chain(), api.notifications()]);
      keys = k ?? [];
      hosts = h;
      control = c.control;
      chain = ch;
      channels = n;
      picked = [...(c.control.channel_ids ?? [])];
      loadError = '';
    } catch (e) {
      if (isPostgresRequired(e)) pg = true;
      else loadError = scriptError(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  // Acil durdurma: yetki vermez, kod istemez (2FA'sız oturum da durdurabilir).
  async function stop() {
    const ok = await confirmDialog({
      title: t('scripts.settings.stopTitle'),
      message: t('scripts.settings.stopMsg'),
      confirmText: t('scripts.settings.stopBtn'),
      cancelText: t('common.cancel'),
      danger: true,
    });
    if (!ok) return;
    try {
      await scriptsApi.stop();
      toast.success(t('scripts.settings.stopped'));
    } catch (e) {
      toast.error(scriptError(e));
    }
    load();
  }

  let resumeOpen = $state(false);
  async function resume(code: string) {
    await scriptsApi.resume(code);
    toast.success(t('scripts.settings.resumed'));
    load();
  }

  // Güven anahtarları ----------------------------------------------------------------
  let keyOpen = $state(false);
  let slot = $state<'yayin' | 'onay'>('yayin');
  let keyName = $state('');
  let keyPub = $state('');
  let delOpen = $state(false);
  let delKey = $state.raw<ScriptKey | null>(null);

  function openKey() {
    slot = 'yayin';
    keyName = '';
    keyPub = '';
    keyOpen = true;
  }
  async function addKey(code: string) {
    await scriptsApi.addKey(slot, keyName.trim(), keyPub.trim(), code);
    toast.success(t('scripts.settings.keyAdded'));
    load();
  }
  async function removeKey(code: string) {
    if (!delKey) return;
    await scriptsApi.deleteKey(delKey.key_id, code);
    toast.success(t('scripts.settings.keyDeleted'));
    load();
  }

  // Sunucu anahtarı onayı --------------------------------------------------------------
  let hostOpen = $state(false);
  let host = $state.raw<ScriptHost | null>(null);
  async function approveHost(code: string) {
    if (!host) return;
    await scriptsApi.approveHost(host.probe_id, host.pending_key, code);
    toast.success(t('scripts.settings.hostApproved'));
    load();
  }

  // Bildirim kanalları ----------------------------------------------------------------
  let chanOpen = $state(false);
  function toggle(id: number, on: boolean) {
    picked = on ? [...picked, id] : picked.filter((x) => x !== id);
  }
  async function saveChannels(code: string) {
    await scriptsApi.channels(picked, code);
    toast.success(t('scripts.settings.channelsSaved'));
    load();
  }

  const supportClass = (s: string) => (s === 'on' ? 'up' : s === 'off' ? 'pending' : 'paused');
  const slotText = (s: string) => (s === 'onay' ? t('scripts.settings.slotOnay') : t('scripts.settings.slotYayin'));
</script>

<ScriptsShell active="settings" {pg}>
  {#if loading}
    <div class="skeleton" style="height:220px"></div>
  {:else if loadError}
    <div class="empty">
      <h3>{t('scripts.loadFailed')}</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={load}>{t('common.retry')}</button>
    </div>
  {:else}
    <div class="stack">
      <section class="card estop" class:on={control?.stopped}>
        <div>
          <h2 class="card-title">{t('scripts.settings.stopTitle')}</h2>
          {#if control?.stopped}
            <p class="small">{t('scripts.settings.stoppedBy', { name: control.stopped_by_name, time: fmtDate(control.stopped_at) })}</p>
          {:else}
            <p class="text-2 small">{t('scripts.settings.stopHelp')}</p>
          {/if}
        </div>
        {#if control?.stopped}
          <button class="btn primary big" disabled={!canWrite} onclick={() => (resumeOpen = true)}><Icon name="play" size={18} /> {t('scripts.settings.resumeBtn')}</button>
        {:else}
          <button class="btn danger solid big" onclick={stop}><Icon name="siren" size={18} /> {t('scripts.settings.stopBtn')}</button>
        {/if}
      </section>

      <section class="card">
        <div class="head">
          <h2 class="card-title">{t('scripts.settings.keysTitle')}</h2>
          <button class="btn sm primary" disabled={!canWrite} onclick={openKey}><Icon name="plus" size={14} /> {t('scripts.settings.keyAdd')}</button>
        </div>
        {#if keys.length === 0}
          <div class="empty inner">
            <Icon name="key" size={24} />
            <h3>{t('scripts.settings.keysEmptyTitle')}</h3>
            <p>{t('scripts.settings.keysEmpty')}</p>
          </div>
        {:else}
          <table class="table responsive">
            <thead>
              <tr>
                <th>{t('common.name')}</th>
                <th>{t('scripts.settings.slot')}</th>
                <th>{t('scripts.col.key')}</th>
                <th>{t('scripts.settings.added')}</th>
                <th><span class="sr">{t('scripts.col.action')}</span></th>
              </tr>
            </thead>
            <tbody>
              {#each keys as k (k.key_id)}
                <tr>
                  <td data-label={t('common.name')}>{k.name}</td>
                  <td data-label={t('scripts.settings.slot')}>{slotText(k.slot)}</td>
                  <td data-label={t('scripts.col.key')}><code title={k.key_id}>{shortId(k.key_id)}</code></td>
                  <td data-label={t('scripts.settings.added')}>{k.added_by_name} · {fmtDate(k.added_at)}</td>
                  <td class="act"><button class="btn sm danger" disabled={!canWrite} onclick={() => { delKey = k; delOpen = true; }}>{t('common.delete')}</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>

      <section class="card">
        <h2 class="card-title">{t('scripts.settings.hostsTitle')}</h2>
        {#if hosts.length === 0}
          <div class="empty inner">
            <Icon name="server" size={24} />
            <h3>{t('scripts.settings.hostsEmptyTitle')}</h3>
            <p>{t('scripts.settings.hostsEmpty')}</p>
          </div>
        {:else}
          <table class="table responsive">
            <thead>
              <tr>
                <th>{t('scripts.col.host')}</th>
                <th>{t('scripts.settings.support')}</th>
                <th>{t('scripts.settings.fingerprint')}</th>
                <th>{t('scripts.settings.pending')}</th>
                <th><span class="sr">{t('scripts.col.action')}</span></th>
              </tr>
            </thead>
            <tbody>
              {#each hosts as h (h.probe_id)}
                <tr>
                  <td data-label={t('scripts.col.host')}>
                    {h.name}
                    <span class="badge {h.online ? 'up' : 'paused'}">{h.online ? t('scripts.settings.online') : t('scripts.settings.offline')}</span>
                  </td>
                  <td data-label={t('scripts.settings.support')}>
                    <span class="badge {supportClass(h.support)}">{t(`scripts.support.${h.support === 'on' || h.support === 'off' ? h.support : 'unsupported'}`)}</span>
                    {#if h.reason}<span class="text-2 small" title={h.reason}>{reasonText(h.reason)}</span>{/if}
                  </td>
                  <td data-label={t('scripts.settings.fingerprint')}>{#if h.host_key}<code title={h.host_key}>{shortId(h.host_key)}</code>{:else}<span class="text-2">-</span>{/if}</td>
                  <td data-label={t('scripts.settings.pending')}>{#if h.pending_key}<code class="brk">{h.pending_key}</code>{:else}<span class="text-2">-</span>{/if}</td>
                  <td class="act">
                    {#if h.pending_key}
                      <button class="btn sm primary" disabled={!canWrite} onclick={() => { host = h; hostOpen = true; }}>{t('scripts.settings.hostApprove')}</button>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>

      <section class="card">
        <div class="head">
          <h2 class="card-title">{t('scripts.settings.channelsTitle')}</h2>
          <button class="btn sm primary" disabled={!canWrite || channels.length === 0} onclick={() => (chanOpen = true)}>{t('common.save')}</button>
        </div>
        <p class="text-2 small sub">{t('scripts.settings.channelsHelp')}</p>
        {#if channels.length === 0}
          <div class="empty inner">
            <Icon name="bell" size={24} />
            <h3>{t('scripts.settings.channelsEmptyTitle')}</h3>
            <p>{t('scripts.settings.channelsEmpty')}</p>
            <a class="btn" href="#/notifications">{t('nav.notifications')}</a>
          </div>
        {:else}
          <div class="chans">
            {#each channels as c (c.id)}
              <label class="check">
                <input type="checkbox" checked={picked.includes(c.id)} onchange={(e) => toggle(c.id, e.currentTarget.checked)} />
                <span>{c.name}</span>
              </label>
            {/each}
          </div>
        {/if}
      </section>

      <section class="card">
        <div class="head">
          <h2 class="card-title">{t('scripts.settings.chainTitle')}</h2>
          <span class="badge">{t('scripts.settings.panelClaim')}</span>
          {#if chain}<span class="badge {chain.ok ? 'up' : 'down'}">{chain.ok ? t('scripts.settings.chainOk') : t('scripts.settings.chainBad')}</span>{/if}
          <a class="btn sm ex" href={SCRIPT_EXPORT_URL} download><Icon name="download" size={14} /> {t('scripts.settings.export')}</a>
        </div>
        {#if chain}
          {#if chain.error}<div class="alert error sub">{chain.error}</div>{/if}
          <dl class="kv">
            <dt>{t('scripts.settings.chainEvents')}</dt><dd>{chain.events}</dd>
            <dt>{t('scripts.settings.chainCheckpoints')}</dt><dd>{chain.checkpoints}</dd>
            <dt>{t('scripts.settings.chainHead')}</dt><dd><code class="brk">{chain.seq} {shortId(chain.hash)}</code></dd>
            <dt>{t('scripts.settings.chainAnchor')}</dt><dd>{chain.last_anchor_at ? fmtDate(chain.last_anchor_at) : '-'}</dd>
          </dl>
        {/if}
        <p class="text-2 small end">{t('scripts.settings.chainHelp')}</p>
      </section>
    </div>
  {/if}
</ScriptsShell>

<CodeModal bind:open={resumeOpen} title={t('scripts.settings.resumeBtn')} confirmText={t('scripts.settings.resumeBtn')} onconfirm={resume} />

<CodeModal bind:open={keyOpen} title={t('scripts.settings.keyAdd')} confirmText={t('scripts.settings.keyAdd')} ready={!!keyName.trim() && !!keyPub.trim()} onconfirm={addKey}>
  <div class="field">
    <label for="sk-slot">{t('scripts.settings.slot')}</label>
    <select id="sk-slot" class="input" bind:value={slot}>
      <option value="yayin">{t('scripts.settings.slotYayin')}</option>
      <option value="onay">{t('scripts.settings.slotOnay')}</option>
    </select>
  </div>
  <div class="field">
    <label for="sk-name">{t('common.name')}</label>
    <input id="sk-name" class="input" maxlength="64" bind:value={keyName} />
  </div>
  <div class="field">
    <label for="sk-pub">{t('scripts.settings.publicKey')}</label>
    <textarea id="sk-pub" class="input plain mono" rows="3" spellcheck="false" bind:value={keyPub}></textarea>
  </div>
</CodeModal>

<CodeModal bind:open={delOpen} danger title={t('scripts.settings.keyDeleteTitle')} confirmText={t('common.delete')} onconfirm={removeKey}>
  <div class="alert warning">{t('scripts.settings.keyDeleteMsg', { name: delKey?.name ?? '' })}</div>
</CodeModal>

<CodeModal bind:open={hostOpen} title={t('scripts.settings.hostApprove')} confirmText={t('scripts.settings.hostApprove')} onconfirm={approveHost}>
  <p class="small">{t('scripts.settings.hostApproveMsg', { name: host?.name ?? '' })}</p>
  <code class="brk">{host?.pending_key}</code>
</CodeModal>

<CodeModal bind:open={chanOpen} title={t('scripts.settings.channelsTitle')} confirmText={t('common.save')} onconfirm={saveChannels} />

<style>
  .estop {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }
  .estop.on {
    border-color: var(--down);
  }
  .estop p {
    margin: 4px 0 0;
  }
  .estop > div {
    flex: 1;
    min-width: 240px;
  }
  .big {
    min-height: 48px;
    padding: 0 22px;
    font-size: 1rem;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin-bottom: 10px;
  }
  .head .card-title {
    margin: 0;
    flex: 0 1 auto;
  }
  .head .btn:not(.ex) {
    margin-left: auto;
  }
  .ex {
    margin-left: auto;
  }
  .sub {
    margin: 0 0 10px;
  }
  .end {
    margin: 12px 0 0;
  }
  .inner {
    padding-block: 18px;
  }
  .act {
    text-align: right;
  }
  .brk {
    word-break: break-all;
  }
  .chans {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 8px;
  }
  .kv {
    display: grid;
    grid-template-columns: minmax(130px, max-content) 1fr;
    gap: 6px 16px;
    margin: 0;
  }
  .kv dt {
    color: var(--text-2);
  }
  .kv dd {
    margin: 0;
    min-width: 0;
  }
  @media (max-width: 640px) {
    .big {
      width: 100%;
    }
    .chans .check {
      min-height: 44px;
    }
  }
</style>
