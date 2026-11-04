<script lang="ts">
  import { onMount } from 'svelte';
  import ScriptsShell from '../components/ScriptsShell.svelte';
  import CodeModal from '../components/CodeModal.svelte';
  import FilePick from '../components/FilePick.svelte';
  import Icon from '../components/Icon.svelte';
  import ScriptRunModal from '../components/ScriptRunModal.svelte';
  import ScriptVersionModal from '../components/ScriptVersionModal.svelte';
  import { scriptsApi, type ScriptHost, type ScriptItem, type ScriptList, type ScriptVersion } from '../lib/api';
  import { approvalText, codeText, isPostgresRequired, parseBundle, parseRevoke, scriptError, shortId, stateClass, stateText, versionBlocked } from '../lib/scripts';
  import { fmtDate, fmtDay } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import { clock, toast } from '../lib/ui.svelte';
  import { t } from '../lib/i18n';

  let data = $state.raw<ScriptList | null>(null);
  let hosts = $state.raw<ScriptHost[]>([]);
  let loading = $state(true);
  let loadError = $state('');
  let pg = $state(false);

  const canWrite = $derived(!!session.user?.two_factor_enabled);
  const stopped = $derived(!!data?.control.stopped);

  async function load() {
    try {
      const [list, h] = await Promise.all([scriptsApi.list(), scriptsApi.hosts()]);
      data = list;
      hosts = h;
      loadError = '';
    } catch (e) {
      if (isPostgresRequired(e)) pg = true;
      else loadError = scriptError(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  // Yükleme ----------------------------------------------------------------------
  let uploadOpen = $state(false);
  let uploadFile = $state<File | null>(null);
  let revokeOpen = $state(false);
  let revokeFile = $state<File | null>(null);

  async function upload(code: string) {
    if (!uploadFile) return;
    const b = parseBundle(await uploadFile.text());
    const v = await scriptsApi.upload(b.manifest, b.body, code);
    toast.success(t('scripts.upload.done', { id: v.script_id, version: v.version }));
    uploadFile = null;
    load();
  }

  async function uploadRevoke(code: string) {
    if (!revokeFile) return;
    await scriptsApi.uploadRevoke(parseRevoke(await revokeFile.text()), code);
    toast.success(t('scripts.upload.revokeDone'));
    revokeFile = null;
    load();
  }

  // Ayrıntı ve çalıştırma ---------------------------------------------------------
  let detailOpen = $state(false);
  let detail = $state.raw<ScriptItem | null>(null);
  let runOpen = $state(false);
  let runVersion = $state.raw<ScriptVersion | null>(null);

  function showDetail(s: ScriptItem) {
    detail = s;
    detailOpen = true;
  }
  function startRun(s: ScriptItem) {
    runVersion = s.latest;
    runOpen = true;
  }
</script>

<ScriptsShell active="library" {pg}>
  {#snippet actions()}
    <button class="btn" disabled={!canWrite} onclick={() => (revokeOpen = true)}><Icon name="ban" size={16} /> {t('scripts.upload.revokeTitle')}</button>
    <button class="btn primary" disabled={!canWrite} onclick={() => (uploadOpen = true)}><Icon name="upload" size={16} /> {t('scripts.upload.title')}</button>
  {/snippet}

  {#if stopped}
    <div class="alert error gap">{t('scripts.stoppedBanner')} <a href="#/scripts/settings">{t('scripts.tabs.settings')}</a></div>
  {/if}

  {#if loading}
    <div class="skeleton" style="height:180px"></div>
  {:else if loadError}
    <div class="empty">
      <h3>{t('scripts.loadFailed')}</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={load}>{t('common.retry')}</button>
    </div>
  {:else if !data || data.scripts.length === 0}
    <div class="empty">
      <Icon name="file" size={28} />
      <h3>{t('scripts.emptyTitle')}</h3>
      <p>{t('scripts.emptyText')}</p>
      <button class="btn primary" disabled={!canWrite} onclick={() => (uploadOpen = true)}><Icon name="upload" size={16} /> {t('scripts.upload.title')}</button>
    </div>
  {:else}
    <section class="card">
      <table class="table responsive">
        <thead>
          <tr>
            <th>{t('scripts.col.script')}</th>
            <th>{t('scripts.col.version')}</th>
            <th>{t('scripts.col.approval')}</th>
            <th>{t('scripts.col.validity')}</th>
            <th>{t('scripts.col.key')}</th>
            <th>{t('scripts.col.hosts')}</th>
            <th><span class="sr">{t('scripts.col.action')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each data.scripts as s (s.id)}
            {@const blockedBy = versionBlocked(s.latest, clock.now)}
            <tr>
              <td data-label={t('scripts.col.script')}><code>{s.id}</code></td>
              <td data-label={t('scripts.col.version')} class="nowrap">
                v{s.latest.version}
                {#if s.latest.revoked}<span class="badge down">{t('scripts.badge.revoked')}</span>{/if}
                {#if blockedBy === 'expired'}<span class="badge paused">{t('scripts.badge.expired')}</span>{/if}
                {#if blockedBy === 'not_yet'}<span class="badge pending">{t('scripts.badge.notYet')}</span>{/if}
              </td>
              <td data-label={t('scripts.col.approval')}>{approvalText(s.latest.approval)}</td>
              <td data-label={t('scripts.col.validity')} class="nowrap" title="{fmtDate(s.latest.not_before)} - {fmtDate(s.latest.not_after)}">{fmtDay(s.latest.not_after)}</td>
              <td data-label={t('scripts.col.key')}><code>{shortId(s.latest.key_id)}</code></td>
              <td data-label={t('scripts.col.hosts')}>
                {#if s.hosts.length === 0}
                  <span class="text-2">{t('scripts.noResults')}</span>
                {:else}
                  <ul class="hosts">
                    {#each s.hosts as h (h.probe_id)}
                      <li title={fmtDate(h.at)}>
                        <span class="hn">{h.probe_name}</span>
                        <span class="badge {stateClass(h.state)}">{stateText(h.state)}</span>
                        {#if h.code}<span class="text-2 small">{codeText(h.code)}</span>{/if}
                      </li>
                    {/each}
                  </ul>
                {/if}
              </td>
              <td class="act">
                <button class="btn sm" onclick={() => showDetail(s)}>{t('scripts.detailBtn')}</button>
                <button class="btn sm primary" disabled={!canWrite || stopped || !!blockedBy} title={blockedBy ? codeText(blockedBy) : ''} onclick={() => startRun(s)}>{t('scripts.run.submit')}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
      <p class="text-2 small note">{t('scripts.hostsNote')}</p>
    </section>
  {/if}
</ScriptsShell>

<CodeModal bind:open={uploadOpen} title={t('scripts.upload.title')} confirmText={t('scripts.upload.submit')} ready={!!uploadFile} onconfirm={upload}>
  <p class="text-2 small">{t('scripts.upload.help')}</p>
  <FilePick bind:file={uploadFile} accept=".json,application/json" id="scr-file" />
</CodeModal>

<CodeModal bind:open={revokeOpen} title={t('scripts.upload.revokeTitle')} confirmText={t('scripts.upload.submit')} ready={!!revokeFile} onconfirm={uploadRevoke}>
  <p class="text-2 small">{t('scripts.upload.revokeHelp')}</p>
  <FilePick bind:file={revokeFile} accept=".json,application/json" id="scr-revoke" />
</CodeModal>

{#if detail}
  <ScriptVersionModal bind:open={detailOpen} versions={detail.versions} initial={detail.latest.version} />
{/if}
<ScriptRunModal bind:open={runOpen} version={runVersion} {hosts} />

<style>
  .gap {
    margin-bottom: 18px;
  }
  .hosts {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .hosts li {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .hn {
    overflow-wrap: anywhere;
  }
  .act {
    white-space: nowrap;
    text-align: right;
  }
  .note {
    margin: 12px 0 0;
  }
</style>
