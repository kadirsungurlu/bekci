<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import ScriptsShell from '../components/ScriptsShell.svelte';
  import CodeModal from '../components/CodeModal.svelte';
  import CopyButton from '../components/CopyButton.svelte';
  import Icon from '../components/Icon.svelte';
  import { scriptsApi, type ScriptRunDetail } from '../lib/api';
  import { approvalText, codeText, eventDetail, eventText, isLive, isPostgresRequired, parseJson, scriptError, stateClass, stateText } from '../lib/scripts';
  import { fmtDate, fmtDateSec } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { t } from '../lib/i18n';

  let { id }: { id: string } = $props();

  const POLL_MS = 3000;

  let data = $state.raw<ScriptRunDetail | null>(null);
  let loading = $state(true);
  let error = $state('');
  let pg = $state(false);

  const canWrite = $derived(!!session.user?.two_factor_enabled);
  const run = $derived(data?.run);
  const runState = $derived(run?.state ?? '');
  const awaiting = $derived(runState === 'awaiting_approval' || runState === 'awaiting_ticket');
  const cancellable = $derived(runState === 'queued' || runState === 'accepted');
  const ticketJson = $derived(data?.ticket_request ? JSON.stringify(data.ticket_request, null, 2) : '');

  let timer: ReturnType<typeof setInterval> | undefined;
  function stopPoll() {
    clearInterval(timer);
    timer = undefined;
  }

  async function load() {
    try {
      const res = await scriptsApi.run(id);
      data = res;
      error = '';
      if (!isLive(res.run.state)) stopPoll();
      else if (!timer) timer = setInterval(() => !document.hidden && load(), POLL_MS);
    } catch (e) {
      if (isPostgresRequired(e)) pg = true;
      else error = scriptError(e);
      if (!data) stopPoll();
    } finally {
      loading = false;
    }
  }
  onMount(load);
  onDestroy(stopPoll);

  let approveOpen = $state(false);
  let ticketOpen = $state(false);
  let ticket = $state('');

  async function approve(code: string) {
    await scriptsApi.approve(id, code);
    toast.success(t('scripts.detail.approved'));
    load();
  }

  async function sendTicket(code: string) {
    await scriptsApi.ticket(id, parseJson(ticket.trim()), code);
    toast.success(t('scripts.detail.ticketSent'));
    ticket = '';
    load();
  }

  async function act(fn: () => Promise<unknown>, title: string, message: string, confirmText: string, done: string) {
    const ok = await confirmDialog({ title, message, confirmText, cancelText: t('common.cancel'), danger: true });
    if (!ok) return;
    try {
      await fn();
      toast.success(done);
    } catch (e) {
      toast.error(scriptError(e));
    }
    load();
  }
  const reject = () => act(() => scriptsApi.reject(id), t('scripts.detail.reject'), t('scripts.detail.rejectMsg'), t('scripts.detail.reject'), t('scripts.detail.rejected'));
  const cancel = () => act(() => scriptsApi.cancel(id), t('scripts.detail.cancel'), t('scripts.detail.cancelMsg'), t('scripts.detail.cancel'), t('scripts.detail.canceled'));
</script>

<ScriptsShell active="runs" {pg}>
  <a class="back text-2 small" href="#/scripts/runs"><Icon name="chevron-left" size={14} /> {t('scripts.detail.back')}</a>

  {#if loading}
    <div class="skeleton" style="height:220px"></div>
  {:else if !run}
    <div class="empty">
      <h3>{t('scripts.loadFailed')}</h3>
      <p>{error}</p>
      <button class="btn primary" onclick={load}>{t('common.retry')}</button>
    </div>
  {:else}
    <div class="stack">
      {#if error}<div class="alert error">{error}</div>{/if}
      <section class="card">
        <div class="head">
          <h2 class="card-title"><code>{run.script_id}</code> v{run.version}</h2>
          <span class="badge {stateClass(runState)}" data-testid="run-state">{stateText(runState)}</span>
          {#if isLive(runState)}<span class="text-2 small">{t('scripts.detail.live')}</span>{/if}
          <div class="btns">
            {#if cancellable}<button class="btn danger" onclick={cancel}>{t('scripts.detail.cancel')}</button>{/if}
            {#if awaiting}<button class="btn danger" onclick={reject}>{t('scripts.detail.reject')}</button>{/if}
            {#if data?.can_approve}<button class="btn primary" disabled={!canWrite} onclick={() => (approveOpen = true)}>{t('scripts.detail.approve')}</button>{/if}
          </div>
        </div>
        {#if runState === 'awaiting_approval' && !data?.can_approve}
          <div class="alert info top">{t('scripts.detail.otherAdmin')}</div>
        {/if}
        {#if run.code}<div class="alert error top"><strong>{t('scripts.detail.agentCode')}:</strong> {codeText(run.code)}</div>{/if}
        <dl class="kv">
          <dt>{t('scripts.col.host')}</dt><dd>{run.probe_name}</dd>
          <dt>{t('scripts.col.by')}</dt><dd>{run.requested_by}</dd>
          <dt>{t('scripts.col.time')}</dt><dd>{fmtDate(run.requested_at)}</dd>
          <dt>{t('scripts.col.approval')}</dt><dd>{approvalText(run.approval)}</dd>
          <dt>{t('scripts.detail.needs')}</dt><dd>{approvalText(run.needs)}</dd>
          {#if run.exit_code !== undefined}<dt>{t('scripts.detail.exit')}</dt><dd><code>{run.exit_code}</code></dd>{/if}
          <dt>{t('scripts.detail.params')}</dt><dd><code class="brk">{run.params}</code></dd>
          <dt>{t('scripts.detail.runId')}</dt><dd><code class="brk">{run.run_id}</code></dd>
        </dl>
      </section>

      {#if ticketJson}
        <section class="card">
          <div class="head">
            <h2 class="card-title">{t('scripts.detail.ticketTitle')}</h2>
            <div class="btns">
              <CopyButton text={ticketJson} class="btn" />
              <button class="btn primary" disabled={!canWrite} onclick={() => (ticketOpen = true)}>{t('scripts.detail.ticketPaste')}</button>
            </div>
          </div>
          <p class="text-2 small sub">{t('scripts.detail.ticketHelp')}</p>
          <pre class="out">{ticketJson}</pre>
        </section>
      {/if}

      <section class="card">
        <h2 class="card-title">{t('scripts.detail.timeline')}</h2>
        <ol class="tl">
          {#each data?.events ?? [] as e (e.seq)}
            {@const extra = eventDetail(e.data)}
            <li>
              <span class="when text-2 small">{fmtDateSec(e.time)}</span>
              <span class="what">{eventText(e.kind)}{#if extra}<span class="text-2 small">{' · '}{extra}</span>{/if}</span>
            </li>
          {/each}
        </ol>
      </section>

      {#if data?.output}
        <section class="card">
          <div class="head">
            <h2 class="card-title">{t('scripts.detail.output')}</h2>
            <span class="badge">{t('scripts.detail.agentClaim')}</span>
            {#if data.output.truncated}<span class="badge pending">{t('scripts.detail.truncated')}</span>{/if}
            <div class="btns"><CopyButton text={data.output.text} class="btn sm" /></div>
          </div>
          <pre class="out">{data.output.text}</pre>
          <p class="text-2 small sub end"><code class="brk">SHA-256 {data.output.sha256}</code></p>
        </section>
      {/if}
    </div>
  {/if}
</ScriptsShell>

<CodeModal bind:open={approveOpen} title={t('scripts.detail.approve')} confirmText={t('scripts.detail.approve')} onconfirm={approve}>
  <div class="alert warning">{t('scripts.run.warning')}</div>
</CodeModal>

<CodeModal bind:open={ticketOpen} title={t('scripts.detail.ticketPaste')} confirmText={t('scripts.detail.ticketSend')} ready={!!ticket.trim()} onconfirm={sendTicket}>
  <div class="field">
    <label for="sr-ticket">{t('scripts.detail.ticketField')}</label>
    <textarea id="sr-ticket" class="input plain mono" rows="7" spellcheck="false" bind:value={ticket}></textarea>
  </div>
</CodeModal>

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 32px;
    margin-bottom: 8px;
    text-decoration: none;
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
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .btns {
    margin-left: auto;
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .top {
    margin-bottom: 12px;
  }
  .sub {
    margin: 0 0 10px;
  }
  .end {
    margin: 10px 0 0;
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
  .brk {
    word-break: break-all;
  }
  .out {
    margin: 0;
    padding: 12px;
    max-height: 480px;
    overflow: auto;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    font-size: 0.82rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .tl {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }
  .tl li {
    display: grid;
    grid-template-columns: 170px 1fr;
    gap: 12px;
    padding-left: 12px;
    border-left: 2px solid var(--border);
  }
  @media (max-width: 640px) {
    .tl li {
      grid-template-columns: 1fr;
      gap: 0;
    }
    .kv {
      grid-template-columns: 1fr;
      gap: 2px;
    }
    .kv dd {
      margin-bottom: 8px;
    }
    .btns {
      margin-left: 0;
      width: 100%;
    }
    .btns .btn {
      flex: 1;
      min-height: 44px;
    }
  }
</style>
