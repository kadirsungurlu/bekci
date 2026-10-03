<script lang="ts">
  // Olay güncellemeleri kartı: liste (yeniden eskiye) ve editör için ekleme formu.
  // Manuel olayda "Çözüldü" aşaması olayı kapatır; otomatik olayda açık olaya
  // "çözüldü" yazılamaz (sunucu 400 döner).
  import { api, errorMessage, INCIDENT_STATES, type IncidentState, type IncidentUpdate } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { confirmDialog } from '../lib/ui.svelte';
  import { fmtDateSec } from '../lib/format';
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  let {
    incidentId,
    updates,
    manual,
    ongoing,
    onchanged,
  }: {
    incidentId: number;
    updates: IncidentUpdate[];
    manual: boolean;
    ongoing: boolean;
    onchanged: () => void;
  } = $props();

  const canEdit = $derived(session.canEdit);
  // Varsayılan aşama: son güncellemeden sonraki (ya da aynısı).
  const nextState = (): IncidentState => {
    const last = updates[0]?.state ?? 'investigating';
    const i = INCIDENT_STATES.indexOf(last);
    return INCIDENT_STATES[Math.min(i + 1, INCIDENT_STATES.length - 1)] ?? 'investigating';
  };
  let phase = $state<IncidentState>(nextState());
  let body = $state('');
  let error = $state('');
  let busy = $state(false);
  let formOpen = $state(false);

  const STATE_TONE: Record<IncidentState, string> = { investigating: 'down', identified: 'pending', monitoring: 'pending', resolved: 'up' };

  async function add(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    busy = true;
    try {
      await api.addIncidentUpdate(incidentId, { state: phase, body: body.trim() });
      body = '';
      formOpen = false;
      onchanged();
      phase = nextState();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function remove(u: IncidentUpdate) {
    const ok = await confirmDialog({
      title: t('incidents.updates.deleteTitle'),
      message: t('incidents.updates.deleteMsg'),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteIncidentUpdate(incidentId, u.id);
      onchanged();
    } catch (err) {
      error = errorMessage(err);
    }
  }
</script>

<div class="card upd">
  <div class="card-head">
    <h2 class="card-title">{t('incidents.updates.title')}<span class="dot">.</span></h2>
    {#if canEdit && !formOpen}
      <button class="btn sm" onclick={() => (formOpen = true)}><Icon name="plus" size={14} /> {t('incidents.updates.add')}</button>
    {/if}
  </div>
  <p class="help nomargin">{t(manual ? 'incidents.updates.helpManual' : 'incidents.updates.helpAuto')}</p>

  {#if formOpen}
    <form class="stack form" onsubmit={add} novalidate>
      <div class="field">
        <span class="label" id="up-st">{t('incidents.manual.state')}</span>
        <div class="seg" role="radiogroup" aria-labelledby="up-st">
          {#each INCIDENT_STATES as s (s)}
            {@const disabled = s === 'resolved' && !manual && ongoing}
            <button
              type="button"
              role="radio"
              aria-checked={phase === s}
              class:active={phase === s}
              {disabled}
              title={disabled ? t('incidents.updates.autoResolvedHint') : undefined}
              onclick={() => (phase = s)}
            >
              {t(`incidents.state.${s}`)}
            </button>
          {/each}
        </div>
      </div>
      <div class="field">
        <label for="up-body">{t('incidents.manual.body')} <span class="muted">{t('monitors.optionalParen')}</span></label>
        <textarea id="up-body" class="input plain" rows="3" maxlength="5000" bind:value={body} placeholder={t('incidents.manual.bodyPlaceholder')}></textarea>
      </div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
      <div class="actions">
        <button type="button" class="btn" onclick={() => (formOpen = false)}>{t('common.cancel')}</button>
        <button type="submit" class="btn primary" disabled={busy}>
          {#if busy}<span class="spinner"></span>{/if}
          {t('incidents.updates.post')}
        </button>
      </div>
    </form>
  {:else if error}
    <div class="alert error" role="alert">{error}</div>
  {/if}

  {#if updates.length === 0}
    <p class="muted small none">{t('incidents.updates.empty')}</p>
  {:else}
    <ol class="list">
      {#each updates as u (u.id)}
        <li>
          <span class="pill {STATE_TONE[u.state]}">{t(`incidents.state.${u.state}`)}</span>
          <div class="u-b">
            {#if u.body}<p class="u-body">{u.body}</p>{/if}
            <div class="u-meta muted small">
              <time datetime={new Date(u.time * 1000).toISOString()}>{fmtDateSec(u.time)}</time>
              {#if u.username}<span>· {u.username}</span>{/if}
            </div>
          </div>
          {#if canEdit}
            <button class="btn ghost icon sm" aria-label={t('common.delete')} title={t('common.delete')} onclick={() => remove(u)}><Icon name="trash" size={14} /></button>
          {/if}
        </li>
      {/each}
    </ol>
  {/if}
</div>

<style>
  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 6px;
  }
  .card-title {
    margin: 0;
  }
  .nomargin {
    margin: 0 0 10px;
  }
  .form {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 12px;
    margin-bottom: 12px;
  }
  .label {
    display: block;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-2);
    margin-bottom: 6px;
  }
  textarea.plain {
    font-family: var(--font);
    font-size: 0.92rem;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .none {
    margin: 4px 0 0;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .list li {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .list .pill {
    flex-shrink: 0;
    margin-top: 2px;
  }
  .u-b {
    flex: 1;
    min-width: 0;
  }
  .u-body {
    margin: 0 0 2px;
    white-space: pre-line;
    overflow-wrap: anywhere;
  }
  @media (max-width: 640px) {
    .list li {
      flex-wrap: wrap;
    }
  }
</style>
