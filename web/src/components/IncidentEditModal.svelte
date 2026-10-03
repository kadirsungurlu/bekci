<script lang="ts">
  // Elle olay açma / düzenleme: durum sayfası, başlık, önem, etkilenen
  // monitörler ve (eklemede) ilk güncelleme.
  import { onMount } from 'svelte';
  import {
    api,
    errorMessage,
    INCIDENT_SEVERITIES,
    INCIDENT_STATES,
    type Incident,
    type IncidentSeverity,
    type IncidentState,
    type StatusPage,
  } from '../lib/api';
  import { t } from '../lib/i18n';
  import Modal from './Modal.svelte';
  import MonitorPicker from './MonitorPicker.svelte';

  let {
    open = $bindable(false),
    incident = null,
    affected = [],
    pageId = 0,
    onsaved,
  }: {
    open?: boolean;
    /** Düzenlenen olay (null: yeni). */
    incident?: Incident | null;
    /** Düzenlemede etkilenen monitörlerin kimlikleri. */
    affected?: number[];
    /** Yeni olayda önceden seçili sayfa. */
    pageId?: number;
    onsaved: (inc: Incident, created: boolean) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  const orig = incident;
  let pages = $state.raw<StatusPage[]>([]);
  // svelte-ignore state_referenced_locally
  let page = $state<number>(orig?.page_id ?? pageId);
  let title = $state(orig?.title ?? '');
  let severity = $state<IncidentSeverity>(orig?.severity ?? 'major');
  // svelte-ignore state_referenced_locally
  let monitorIds = $state<number[]>([...affected]);
  let phase = $state<IncidentState>('investigating');
  let body = $state('');
  let error = $state('');
  let busy = $state(false);

  onMount(async () => {
    if (orig) return;
    try {
      pages = await api.pages();
      if (!page && pages.length) page = pages[0].id;
    } catch (e) {
      error = errorMessage(e);
    }
  });

  async function save(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (!title.trim()) return (error = t('incidents.manual.errTitle'));
    if (!orig && !page) return (error = t('incidents.manual.errPage'));
    busy = true;
    try {
      const res = orig
        ? await api.updateIncident(orig.id, { title: title.trim(), severity, monitor_ids: monitorIds })
        : await api.createIncident({ page_id: page, title: title.trim(), severity, monitor_ids: monitorIds, state: phase, body: body.trim() });
      onsaved(res, !orig);
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open title={orig ? t('incidents.manual.editTitle') : t('incidents.manual.newTitle')} width={620}>
  <form id="incf" class="stack" onsubmit={save} novalidate>
    {#if !orig}
      <div class="field">
        <label for="inc-page">{t('incidents.manual.page')}</label>
        <select id="inc-page" class="input" bind:value={page}>
          {#each pages as p (p.id)}
            <option value={p.id}>{p.title} <span>/durum/{p.slug}</span></option>
          {/each}
        </select>
        <span class="help">{t('incidents.manual.pageHelp')}</span>
      </div>
    {/if}
    <div class="field">
      <label for="inc-title">{t('incidents.manual.title')}</label>
      <input id="inc-title" class="input" maxlength="200" bind:value={title} placeholder={t('incidents.manual.titlePlaceholder')} />
    </div>
    <div class="field">
      <span class="label" id="inc-sev">{t('incidents.manual.severity')}</span>
      <div class="seg" role="radiogroup" aria-labelledby="inc-sev">
        {#each INCIDENT_SEVERITIES as s (s)}
          <button type="button" role="radio" aria-checked={severity === s} class:active={severity === s} onclick={() => (severity = s)}>
            {t(`incidents.severity.${s}`)}
          </button>
        {/each}
      </div>
    </div>
    <div class="field">
      <span class="label" id="inc-aff">{t('incidents.manual.affected')}</span>
      <MonitorPicker bind:selected={monitorIds} label={t('incidents.manual.affected')} id="inc-mp" />
      <span class="help">{t('incidents.manual.affectedHelp')}</span>
    </div>
    {#if !orig}
      <div class="field">
        <span class="label" id="inc-st">{t('incidents.manual.state')}</span>
        <div class="seg" role="radiogroup" aria-labelledby="inc-st">
          {#each INCIDENT_STATES as s (s)}
            <button type="button" role="radio" aria-checked={phase === s} class:active={phase === s} onclick={() => (phase = s)}>
              {t(`incidents.state.${s}`)}
            </button>
          {/each}
        </div>
      </div>
      <div class="field">
        <label for="inc-body">{t('incidents.manual.body')} <span class="muted">{t('monitors.optionalParen')}</span></label>
        <textarea id="inc-body" class="input plain" rows="3" maxlength="5000" bind:value={body} placeholder={t('incidents.manual.bodyPlaceholder')}></textarea>
      </div>
    {/if}
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button type="submit" form="incf" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {orig ? t('common.save') : t('incidents.manual.create')}
    </button>
  {/snippet}
</Modal>

<style>
  .spacer {
    flex: 1;
  }
  textarea.plain {
    font-family: var(--font);
    font-size: 0.92rem;
  }
  .label {
    display: block;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-2);
    margin-bottom: 6px;
  }
</style>
