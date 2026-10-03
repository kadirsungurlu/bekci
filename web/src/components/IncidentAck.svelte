<script lang="ts">
  // Süren olayda onaylama ve susturma düğmeleri (editör+). Onay: hatırlatma ve
  // eskalasyon olay kapanana kadar gitmez. Susturma: seçilen süre boyunca.
  import { api, errorMessage, type Incident } from '../lib/api';
  import { t } from '../lib/i18n';
  import { toast } from '../lib/ui.svelte';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  let { incident, onchanged }: { incident: Incident; onchanged: (inc: Incident) => void } = $props();

  const acked = $derived((incident.acked_at ?? 0) > 0);
  const snoozed = $derived((incident.snoozed_until ?? 0) * 1000 > Date.now());

  let ackOpen = $state(false);
  let note = $state('');
  let snoozeOpen = $state(false);
  let minutes = $state(60);
  let busy = $state(false);
  let error = $state('');

  const DURATIONS: { key: 'm30' | 'h1' | 'h4' | 'h24' | 'd7'; min: number }[] = [
    { key: 'm30', min: 30 },
    { key: 'h1', min: 60 },
    { key: 'h4', min: 240 },
    { key: 'h24', min: 1440 },
    { key: 'd7', min: 10080 },
  ];

  async function run(fn: () => Promise<Incident>, msg: string) {
    busy = true;
    error = '';
    try {
      const inc = await fn();
      onchanged(inc);
      ackOpen = snoozeOpen = false;
      toast.success(msg);
    } catch (e) {
      error = errorMessage(e);
      if (!ackOpen && !snoozeOpen) toast.error(error);
    } finally {
      busy = false;
    }
  }
</script>

{#if acked}
  <button type="button" class="btn" onclick={() => run(() => api.unackIncident(incident.id), t('incidents.ack.undone'))} disabled={busy}>
    <Icon name="eye-off" size={15} /> {t('incidents.ack.unack')}
  </button>
{:else}
  <button type="button" class="btn" onclick={() => { note = ''; error = ''; ackOpen = true; }} disabled={busy}>
    <Icon name="eye" size={15} /> {t('incidents.ack.ack')}
  </button>
{/if}
{#if snoozed}
  <button type="button" class="btn" onclick={() => run(() => api.unsnoozeIncident(incident.id), t('incidents.ack.unsnoozeDone'))} disabled={busy}>
    <Icon name="bell" size={15} /> {t('incidents.ack.unsnooze')}
  </button>
{:else}
  <button type="button" class="btn" onclick={() => { error = ''; snoozeOpen = true; }} disabled={busy}>
    <Icon name="bell-off" size={15} /> {t('incidents.ack.snooze')}
  </button>
{/if}

<Modal bind:open={ackOpen} title={t('incidents.ack.title')} width={520}>
  <form
    id="ack-form"
    class="stack"
    onsubmit={(e) => {
      e.preventDefault();
      run(() => api.ackIncident(incident.id, note.trim()), t('incidents.ack.done'));
    }}
  >
    <p class="help nomargin">{t('incidents.ack.intro')}</p>
    <div class="field">
      <label for="ack-note">{t('incidents.ack.note')}</label>
      <textarea id="ack-note" class="input" rows="3" maxlength="500" bind:value={note} placeholder={t('incidents.ack.notePlaceholder')}></textarea>
    </div>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (ackOpen = false)}>{t('common.cancel')}</button>
    <button type="submit" form="ack-form" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {t('incidents.ack.ack')}
    </button>
  {/snippet}
</Modal>

<Modal bind:open={snoozeOpen} title={t('incidents.ack.snoozeTitle')} width={480}>
  <form
    id="snooze-form"
    class="stack"
    onsubmit={(e) => {
      e.preventDefault();
      run(() => api.snoozeIncident(incident.id, minutes), t('incidents.ack.snoozeDone'));
    }}
  >
    <p class="help nomargin">{t('incidents.ack.snoozeIntro')}</p>
    <fieldset class="durs">
      <legend class="label">{t('incidents.ack.duration')}</legend>
      <div class="dur-list" role="radiogroup" aria-label={t('incidents.ack.duration')}>
        {#each DURATIONS as d (d.key)}
          <label class="dur" class:on={minutes === d.min}>
            <input type="radio" name="snooze-min" value={d.min} bind:group={minutes} />
            <span>{t(`incidents.ack.durations.${d.key}`)}</span>
          </label>
        {/each}
      </div>
    </fieldset>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <button type="button" class="btn" onclick={() => (snoozeOpen = false)}>{t('common.cancel')}</button>
    <button type="submit" form="snooze-form" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {t('incidents.ack.snooze')}
    </button>
  {/snippet}
</Modal>

<style>
  textarea.input {
    height: auto;
    padding: 10px 12px;
    resize: vertical;
  }
  .durs {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
  }
  .durs legend {
    padding: 0;
    margin-bottom: 8px;
  }
  .dur-list {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .dur {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    background: var(--input);
    cursor: pointer;
    font-size: 0.9rem;
    min-height: 40px;
  }
  .dur.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .dur input {
    margin: 0;
    accent-color: var(--accent);
  }
</style>
