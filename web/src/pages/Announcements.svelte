<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type Announcement, type AnnouncementInput, type Severity } from '../lib/api';
  import { clock, confirmDialog, toast } from '../lib/ui.svelte';
  import { fmtDate, isoLocal, parseLocal } from '../lib/format';
  import Modal from '../components/Modal.svelte';
  import RowMenu, { type MenuItem } from '../components/RowMenu.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import { t } from '../lib/i18n';

  let { pageId }: { pageId: number } = $props();

  const SEVERITIES: { v: Severity; c: string; icon: IconName }[] = [
    { v: 'info', c: 'accent', icon: 'info' },
    { v: 'warning', c: 'pending', icon: 'alert' },
    { v: 'danger', c: 'down', icon: 'alert-circle' },
    { v: 'success', c: 'up', icon: 'check-circle' },
  ];
  const sev = (v: Severity) => SEVERITIES.find((s) => s.v === v) ?? SEVERITIES[0];

  let list = $state.raw<Announcement[]>([]);
  let loading = $state(true);
  let loadError = $state('');

  async function load() {
    try {
      list = await api.announcements(pageId);
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  type AnnState = 'scheduled' | 'ended' | 'live';
  function annState(a: Announcement, now: number): { k: AnnState; c: string } {
    if (a.starts_at > now) return { k: 'scheduled', c: 'accent' };
    if (a.ends_at && a.ends_at <= now) return { k: 'ended', c: '' };
    return { k: 'live', c: 'up' };
  }

  const sorted = $derived(list.slice().sort((a, b) => b.starts_at - a.starts_at));

  // Form
  let open = $state(false);
  let editing = $state<Announcement | null>(null);
  let title = $state('');
  let body = $state('');
  let severity = $state<Severity>('info');
  let startsAt = $state('');
  let endsAt = $state('');
  let error = $state('');
  let busy = $state(false);

  function openForm(a: Announcement | null) {
    editing = a;
    title = a?.title ?? '';
    body = a?.body ?? '';
    severity = a?.severity ?? 'info';
    startsAt = a ? isoLocal(a.starts_at) : '';
    endsAt = a?.ends_at ? isoLocal(a.ends_at) : '';
    error = '';
    open = true;
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (!title.trim()) return (error = t('pages.ann.errTitle'));
    const s = startsAt ? parseLocal(startsAt) : 0;
    const en = endsAt ? parseLocal(endsAt) : 0;
    if (en && en <= (s || clock.now)) return (error = t('pages.ann.errEnd'));
    const input: AnnouncementInput = { title: title.trim(), body: body.trim(), severity, starts_at: s, ends_at: en };
    busy = true;
    try {
      if (editing) {
        const res = await api.updateAnnouncement(editing.id, input);
        list = list.map((x) => (x.id === res.id ? res : x));
        toast.success(t('pages.ann.updated'));
      } else {
        const res = await api.createAnnouncement(pageId, input);
        list = [...list, res];
        toast.success(t('pages.ann.added'));
      }
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function endNow(a: Announcement) {
    try {
      const res = await api.updateAnnouncement(a.id, {
        title: a.title,
        body: a.body,
        severity: a.severity,
        starts_at: Math.min(a.starts_at, clock.now - 1),
        ends_at: clock.now,
      });
      list = list.map((x) => (x.id === res.id ? res : x));
      toast.success(t('pages.ann.ended'));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function remove(a: Announcement) {
    const ok = await confirmDialog({
      title: t('pages.ann.deleteTitle'),
      message: t('pages.ann.deleteMsg', { name: a.title }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteAnnouncement(a.id);
      list = list.filter((x) => x.id !== a.id);
      toast.success(t('pages.ann.deleted'));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  function menu(a: Announcement): MenuItem[] {
    const out: MenuItem[] = [{ label: t('common.edit'), icon: 'edit', onclick: () => openForm(a) }];
    if (annState(a, clock.now).k === 'live') out.push({ label: t('pages.ann.endNow'), icon: 'x', onclick: () => endNow(a) });
    out.push({ label: t('common.delete'), icon: 'trash', danger: true, onclick: () => remove(a) });
    return out;
  }
</script>

<section class="card">
  <div class="head">
    <div>
      <h2 class="card-title">{t('pages.ann.title')}</h2>
      <p class="help nomargin">{t('pages.ann.help')}</p>
    </div>
    <button class="btn" onclick={() => openForm(null)}><Icon name="megaphone" size={15} /> {t('pages.ann.add')}</button>
  </div>
  {#if loading}
    <div class="skeleton" style="height:70px"></div>
  {:else if loadError}
    <div class="alert error">{loadError}</div>
  {:else if list.length === 0}
    <div class="none muted small">{t('pages.ann.empty')}</div>
  {:else}
    <ul class="anns">
      {#each sorted as a (a.id)}
        {@const s = sev(a.severity)}
        {@const st = annState(a, clock.now)}
        <li class="ann sev-{a.severity}" class:ended={st.k === 'ended'}>
          <span class="ic"><Icon name={s.icon} size={16} /></span>
          <div class="body">
            <div class="t">
              <b>{a.title}</b>
              <span class="badge {s.c}">{t(`pages.ann.sev.${s.v}`)}</span>
              <span class="badge {st.c}">{t(`pages.ann.state.${st.k}`)}</span>
            </div>
            {#if a.body}<p class="txt">{a.body}</p>{/if}
            <div class="when muted small">
              {fmtDate(a.starts_at)} – {a.ends_at ? fmtDate(a.ends_at) : t('pages.ann.indefinite')}
            </div>
          </div>
          <RowMenu items={menu(a)} label={t('pages.ann.actionsFor', { name: a.title })} />
        </li>
      {/each}
    </ul>
  {/if}
</section>

<Modal bind:open title={editing ? t('pages.ann.edit') : t('pages.ann.add')} width={560}>
  <form id="anf" class="stack" onsubmit={save} novalidate>
    <div class="field">
      <label for="an-t">{t('pages.ann.fTitle')}</label>
      <input id="an-t" class="input" maxlength="200" bind:value={title} placeholder={t('pages.ann.titlePlaceholder')} />
    </div>
    <div class="field">
      <span class="label" id="an-sev">{t('pages.ann.severity')}</span>
      <div class="seg" role="radiogroup" aria-labelledby="an-sev">
        {#each SEVERITIES as s (s.v)}
          <button type="button" role="radio" aria-checked={severity === s.v} class:active={severity === s.v} onclick={() => (severity = s.v)}>{t(`pages.ann.sev.${s.v}`)}</button>
        {/each}
      </div>
    </div>
    <div class="field">
      <label for="an-b">{t('pages.ann.body')} <span class="muted">{t('pages.ann.optional')}</span></label>
      <textarea id="an-b" class="input plain" rows="4" maxlength="5000" bind:value={body} placeholder={t('pages.ann.bodyPlaceholder')}></textarea>
    </div>
    <div class="grid-2">
      <div class="field">
        <label for="an-s">{t('pages.ann.start')}</label>
        <input id="an-s" class="input" type="datetime-local" bind:value={startsAt} />
        <span class="help">{t('pages.ann.startHelp')}</span>
      </div>
      <div class="field">
        <label for="an-e">{t('pages.ann.end')}</label>
        <input id="an-e" class="input" type="datetime-local" bind:value={endsAt} />
      </div>
    </div>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button type="submit" form="anf" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if} {t('common.save')}
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
    margin-bottom: 14px;
  }
  .card-title {
    margin: 0 0 4px;
  }
  .nomargin {
    margin: 0;
  }
  .none {
    padding: 6px 0;
  }
  .anns {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .ann {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 12px 8px 12px 12px;
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: 8px;
    background: var(--bg-elev);
  }
  .ann.sev-warning {
    border-left-color: var(--pending);
  }
  .ann.sev-danger {
    border-left-color: var(--down);
  }
  .ann.sev-success {
    border-left-color: var(--up);
  }
  .ann.ended {
    opacity: 0.6;
  }
  .ic {
    display: inline-flex;
    margin-top: 2px;
    color: var(--accent-text);
  }
  .sev-warning .ic {
    color: var(--pending);
  }
  .sev-danger .ic {
    color: var(--down-text-2);
  }
  .sev-success .ic {
    color: var(--up);
  }
  .body {
    flex: 1;
    min-width: 0;
  }
  .t {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .t b {
    overflow-wrap: anywhere;
  }
  .txt {
    margin: 4px 0;
    font-size: 0.88rem;
    color: var(--text-2);
    white-space: pre-line;
    overflow-wrap: anywhere;
  }
  .when {
    margin-top: 2px;
  }
  textarea.plain {
    font-family: var(--font);
    font-size: 0.92rem;
  }
  .spacer {
    flex: 1;
  }
</style>
