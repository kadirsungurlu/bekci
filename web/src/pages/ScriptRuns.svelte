<script lang="ts">
  import { onMount } from 'svelte';
  import ScriptsShell from '../components/ScriptsShell.svelte';
  import ScriptFilters from '../components/ScriptFilters.svelte';
  import Icon from '../components/Icon.svelte';
  import { scriptsApi, type ScriptRun } from '../lib/api';
  import { codeText, isLive, isPostgresRequired, scriptError, stateClass, stateText } from '../lib/scripts';
  import { fmtDate } from '../lib/format';
  import { t } from '../lib/i18n';

  let runs = $state.raw<ScriptRun[]>([]);
  let next = $state(0);
  let loading = $state(true);
  let more = $state(false);
  let error = $state('');
  let pg = $state(false);

  // Süzgeç yüklenmiş sayfalara uygulanır; eski kayıtlar için "Daha fazla" açık kalır.
  let q = $state('');
  let group = $state('all');
  const GROUPS = $derived([
    { k: 'all', label: t('scripts.filter.all') },
    { k: 'live', label: t('scripts.filter.live') },
    { k: 'ok', label: t('scripts.filter.ok') },
    { k: 'problem', label: t('scripts.filter.problem') },
  ]);
  const shown = $derived.by(() => {
    const k = q.trim().toLocaleLowerCase();
    return runs.filter((r) => {
      if (group === 'live' && !isLive(r.state)) return false;
      if (group === 'ok' && r.state !== 'ok') return false;
      if (group === 'problem' && stateClass(r.state) !== 'down') return false;
      return !k || r.script_id.includes(k) || r.probe_name.toLocaleLowerCase().includes(k) || r.requested_by.toLocaleLowerCase().includes(k);
    });
  });

  async function load(before = 0) {
    try {
      const res = await scriptsApi.runs(before);
      runs = before ? [...runs, ...res.runs] : res.runs;
      next = res.next;
      error = '';
    } catch (e) {
      if (isPostgresRequired(e)) pg = true;
      else error = scriptError(e);
    } finally {
      loading = false;
      more = false;
    }
  }
  onMount(() => load());

  function loadMore() {
    more = true;
    load(next);
  }
</script>

<ScriptsShell active="runs" {pg}>
  {#if loading}
    <div class="skeleton" style="height:180px"></div>
  {:else if error && runs.length === 0}
    <div class="empty">
      <h3>{t('scripts.loadFailed')}</h3>
      <p>{error}</p>
      <button class="btn primary" onclick={() => load()}>{t('common.retry')}</button>
    </div>
  {:else if runs.length === 0}
    <div class="empty">
      <Icon name="list" size={28} />
      <h3>{t('scripts.runs.emptyTitle')}</h3>
      <p>{t('scripts.runs.emptyText')}</p>
      <a class="btn primary" href="#/scripts">{t('scripts.tabs.library')}</a>
    </div>
  {:else}
    <ScriptFilters bind:q bind:value={group} options={GROUPS} placeholder={t('scripts.filter.runsPlaceholder')} />
    <section class="card">
      {#if shown.length === 0}
        <div class="empty inner">
          <Icon name="search" size={24} />
          <h3>{t('scripts.filter.emptyTitle')}</h3>
          <p>{t('scripts.filter.emptyText')}</p>
          <button class="btn" onclick={() => { q = ''; group = 'all'; }}>{t('scripts.filter.clear')}</button>
        </div>
      {:else}
      <table class="table responsive">
        <thead>
          <tr>
            <th>{t('scripts.col.state')}</th>
            <th>{t('scripts.col.script')}</th>
            <th>{t('scripts.col.host')}</th>
            <th>{t('scripts.col.by')}</th>
            <th>{t('scripts.col.time')}</th>
            <th><span class="sr">{t('scripts.col.action')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each shown as r (r.run_id)}
            <tr>
              <td data-label={t('scripts.col.state')}>
                <span class="badge {stateClass(r.state)}">{stateText(r.state)}</span>
                {#if r.code}<span class="text-2 small">{codeText(r.code)}</span>{/if}
              </td>
              <td data-label={t('scripts.col.script')}><code>{r.script_id}</code> v{r.version}</td>
              <td data-label={t('scripts.col.host')}>{r.probe_name}</td>
              <td data-label={t('scripts.col.by')}>{r.requested_by}</td>
              <td data-label={t('scripts.col.time')} class="nowrap">{fmtDate(r.requested_at)}</td>
              <td class="act"><a class="btn sm" href="#/scripts/runs/{r.run_id}">{t('scripts.detailBtn')}</a></td>
            </tr>
          {/each}
        </tbody>
      </table>
      {/if}
      {#if error}<div class="alert error top">{error}</div>{/if}
      {#if next}
        <div class="morebar"><button class="btn" disabled={more} onclick={loadMore}>{t('scripts.runs.more')}</button></div>
      {/if}
    </section>
  {/if}
</ScriptsShell>

<style>
  .act {
    text-align: right;
  }
  .inner {
    padding-block: 18px;
  }
  .morebar {
    display: flex;
    justify-content: center;
    margin-top: 14px;
  }
  .top {
    margin-top: 12px;
  }
</style>
