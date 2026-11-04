<script lang="ts">
  // Çalıştır penceresi: tek sunucu, bildirim şemasına göre alanlar, istenirse bilet.
  import CodeModal from './CodeModal.svelte';
  import { scriptsApi, type ScriptHost, type ScriptVersion } from '../lib/api';
  import { codeText, paramInitial, paramValue, reasonText } from '../lib/scripts';
  import { navigate } from '../lib/router.svelte';
  import { t } from '../lib/i18n';

  let {
    open = $bindable(false),
    version,
    hosts,
  }: { open?: boolean; version: ScriptVersion | null; hosts: ScriptHost[] } = $props();

  let probe = $state(0);
  let values = $state<Record<string, string | boolean>>({});
  let useTicket = $state(false);

  const params = $derived(version?.manifest.params ?? []);

  $effect(() => {
    if (open) {
      probe = 0;
      useTicket = false;
      values = Object.fromEntries((version?.manifest.params ?? []).map((p) => [p.name, paramInitial(p)]));
    }
  });

  /** Panelin bildiği engel; yoksa ''. Son karar yine ajanındır. */
  function blocked(h: ScriptHost): string {
    if (h.support === 'unsupported') return codeText('host_unsupported');
    if (h.support !== 'on') return h.reason ? reasonText(h.reason) : codeText('host_off');
    if (!h.host_key) return codeText('host_unapproved');
    if (h.pending_key) return codeText('host_key_pending');
    return '';
  }

  const ready = $derived(probe !== 0 && params.every((p) => paramValue(p, values[p.name] ?? '') !== undefined));

  async function confirm(code: string) {
    if (!version) return;
    const out: Record<string, unknown> = {};
    for (const p of params) out[p.name] = paramValue(p, values[p.name] ?? '');
    const run = await scriptsApi.start({
      script_id: version.script_id,
      version: version.version,
      probe_id: probe,
      params: out,
      code,
      use_ticket: useTicket,
    });
    navigate(`/scripts/runs/${run.run_id}`);
  }
</script>

<CodeModal bind:open title={t('scripts.run.title', { id: version?.script_id ?? '', version: version?.version ?? 0 })} confirmText={t('scripts.run.submit')} {ready} onconfirm={confirm}>
  <div class="alert warning">{t('scripts.run.warning')}</div>
  <div class="field">
    <label for="sr-host">{t('scripts.run.host')}</label>
    <select id="sr-host" class="input" bind:value={probe}>
      <option value={0}>{t('scripts.run.hostPick')}</option>
      {#each hosts as h (h.probe_id)}
        {@const why = blocked(h)}
        <option value={h.probe_id} disabled={!!why}>{h.name}{why ? ` (${why})` : ''}</option>
      {/each}
    </select>
  </div>
  {#each params as p (p.name)}
    <div class="field">
      {#if p.type === 'bool'}
        <label class="check">
          <input type="checkbox" checked={values[p.name] === true} onchange={(e) => (values[p.name] = e.currentTarget.checked)} />
          <span><code>{p.name}</code></span>
        </label>
      {:else}
        <label for="sr-p-{p.name}"><code>{p.name}</code></label>
        {#if p.type === 'enum'}
          <select id="sr-p-{p.name}" class="input" bind:value={values[p.name]}>
            {#each p.values ?? [] as v (v)}<option value={v}>{v}</option>{/each}
          </select>
        {:else if p.type === 'int'}
          <input id="sr-p-{p.name}" class="input" type="number" min={p.min} max={p.max} step="1" value={values[p.name]} oninput={(e) => (values[p.name] = e.currentTarget.value)} />
          <span class="help">{t('scripts.run.intRange', { min: p.min ?? 0, max: p.max ?? 0 })}</span>
        {:else}
          <input id="sr-p-{p.name}" class="input" bind:value={values[p.name]} autocapitalize="none" spellcheck="false" />
        {/if}
      {/if}
    </div>
  {/each}
  <label class="check">
    <input type="checkbox" bind:checked={useTicket} />
    <span>{t('scripts.run.useTicket')}<small>{t('scripts.run.useTicketHelp')}</small></span>
  </label>
</CodeModal>
