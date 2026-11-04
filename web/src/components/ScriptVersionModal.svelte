<script lang="ts">
  // Sürüm ayrıntısı: bildirim alanları ve gövde (salt okunur düz metin).
  import Modal from './Modal.svelte';
  import { scriptsApi, type ScriptVersion } from '../lib/api';
  import { approvalText, scriptError, shortId } from '../lib/scripts';
  import { fmtDate } from '../lib/format';
  import { t } from '../lib/i18n';

  let {
    open = $bindable(false),
    versions,
    initial,
  }: { open?: boolean; versions: ScriptVersion[]; initial: number } = $props();

  let version = $state(0);
  let full = $state.raw<ScriptVersion | null>(null);
  let error = $state('');

  let req = 0;
  async function load(v: number) {
    const my = ++req;
    full = null;
    error = '';
    const id = versions[0]?.script_id;
    if (!id) return;
    try {
      const res = await scriptsApi.version(id, v);
      if (my === req) full = res;
    } catch (e) {
      if (my === req) error = scriptError(e);
    }
  }

  $effect(() => {
    if (open) {
      version = initial;
      load(initial);
    }
  });

  const m = $derived(full?.manifest);
  const joined = (l: string[] | null | undefined) => (l?.length ? l.join(', ') : '-');
</script>

<Modal bind:open title={t('scripts.version.title', { id: versions[0]?.script_id ?? '' })} width={760}>
  <div class="stack">
    {#if versions.length > 1}
      <div class="field">
        <label for="sv-v">{t('scripts.version.pick')}</label>
        <select id="sv-v" class="input" bind:value={version} onchange={() => load(version)}>
          {#each versions as v (v.version)}<option value={v.version}>v{v.version}</option>{/each}
        </select>
      </div>
    {/if}
    {#if error}
      <div class="alert error">{error}</div>
    {:else if !full || !m}
      <div class="skeleton" style="height:200px"></div>
    {:else}
      <dl class="kv">
        <dt>{t('scripts.col.approval')}</dt><dd>{approvalText(full.approval)}</dd>
        <dt>{t('scripts.version.interpreter')}</dt><dd><code>{m.interpreter}</code></dd>
        <dt>{t('scripts.version.runAs')}</dt><dd><code>{m.run_as}</code></dd>
        <dt>{t('scripts.version.platforms')}</dt><dd>{joined(m.platforms)}</dd>
        <dt>{t('scripts.version.groups')}</dt><dd>{joined(m.groups)}</dd>
        <dt>{t('scripts.version.timeout')}</dt><dd>{m.timeout_sec} s</dd>
        <dt>{t('scripts.version.outputMax')}</dt><dd>{m.output_max}</dd>
        <dt>{t('scripts.version.minInterval')}</dt><dd>{m.min_interval_sec} s</dd>
        <dt>{t('scripts.version.maxPerDay')}</dt><dd>{m.max_per_day}</dd>
        <dt>{t('scripts.version.jitter')}</dt><dd>{m.jitter_sec} s</dd>
        <dt>{t('scripts.col.validity')}</dt><dd>{fmtDate(full.not_before)} - {fmtDate(full.not_after)}</dd>
        <dt>{t('scripts.col.key')}</dt><dd><code>{shortId(full.key_id)}</code></dd>
        <dt>SHA-256</dt><dd><code class="brk">{full.sha256}</code></dd>
        <dt>{t('scripts.version.uploaded')}</dt><dd>{full.uploaded_by_name} · {fmtDate(full.uploaded_at)}</dd>
      </dl>
      {#if m.params?.length}
        <div>
          <div class="label">{t('scripts.version.params')}</div>
          <ul class="params">
            {#each m.params as p (p.name)}
              <li>
                <code>{p.name}</code>
                <span class="text-2 small">
                  {t(`scripts.paramType.${p.type}`)}{#if p.type === 'enum'}: {joined(p.values)}{/if}{#if p.type === 'int'}: {p.min} - {p.max}{/if}
                </span>
              </li>
            {/each}
          </ul>
        </div>
      {/if}
      <div>
        <div class="label">{t('scripts.version.body')}</div>
        <pre class="out">{full.body ?? ''}</pre>
      </div>
    {/if}
  </div>
</Modal>

<style>
  .kv {
    display: grid;
    grid-template-columns: minmax(120px, max-content) 1fr;
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
  .params {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .params li {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .out {
    margin: 0;
    padding: 12px;
    max-height: 340px;
    overflow: auto;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    font-size: 0.82rem;
    white-space: pre;
  }
  @media (max-width: 640px) {
    .kv {
      grid-template-columns: 1fr;
      gap: 2px;
    }
    .kv dd {
      margin-bottom: 8px;
    }
  }
</style>
