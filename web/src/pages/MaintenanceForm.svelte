<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, ApiError, errorMessage, type Maintenance, type MaintenanceInput, type MaintStrategy } from '../lib/api';
  import { navigate, router } from '../lib/router.svelte';
  import { live } from '../lib/live.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { fmtDuration, isoLocal, nowSec } from '../lib/format';
  import { guardUnsaved, markInvalid, snapshot } from '../lib/forms';
  import { DEFAULT_TZ, MAINT_STATUS, STRATEGY_DESCS, STRATEGY_LABELS, WEEKDAYS, nextText } from '../lib/maintenance';
  import MonitorPicker from '../components/MonitorPicker.svelte';
  import Icon from '../components/Icon.svelte';
  import { t, tParts } from '../lib/i18n';

  let { id }: { id?: number } = $props();
  // svelte-ignore state_referenced_locally
  const isEdit = id !== undefined;

  const STRATEGIES: MaintStrategy[] = ['once', 'recurring_weekly', 'recurring_daily', 'cron', 'manual'];

  let loading = $state(isEdit);
  let loadError = $state('');
  let saving = $state(false);
  let error = $state('');
  let errorEl: HTMLDivElement | undefined = $state();
  let current = $state<Maintenance | null>(null);

  // Varsayılan tek seferlik pencere: bir sonraki tam saatten itibaren 2 saat.
  const nextHour = Math.ceil(nowSec() / 3600) * 3600;

  let title = $state('');
  let description = $state('');
  let active = $state(true);
  let strategy = $state<MaintStrategy>('once');
  let timezone = $state(DEFAULT_TZ);
  let start = $state(isoLocal(nextHour));
  let end = $state(isoLocal(nextHour + 7200));
  let weekdays = $state<number[]>([]);
  let startTime = $state('02:00');
  let endTime = $state('04:00');
  let dateFrom = $state('');
  let dateTo = $state('');
  let cron = $state('0 3 * * 1');
  let duration = $state<number | null>(60);
  let allMonitors = $state(false);
  let monitorIds = $state<number[]>([]);

  const timezones: string[] = (() => {
    try {
      return typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [];
    } catch {
      return [];
    }
  })();

  function fill(m: Maintenance) {
    current = m;
    title = m.title;
    description = m.description;
    active = m.active;
    strategy = m.strategy;
    timezone = m.timezone || DEFAULT_TZ;
    if (m.start) start = m.start;
    if (m.end) end = m.end;
    weekdays = [...m.weekdays];
    if (m.start_time) startTime = m.start_time;
    if (m.end_time) endTime = m.end_time;
    dateFrom = m.date_from;
    dateTo = m.date_to;
    if (m.cron) cron = m.cron;
    if (m.duration_minutes) duration = m.duration_minutes;
    allMonitors = m.all_monitors;
    monitorIds = [...m.monitor_ids];
  }

  onMount(async () => {
    if (!isEdit) {
      // Monitör listesindeki "Bakım planla" bağlantısı: #/maintenance/new?monitor=<id>
      const pre = Number(router.path.match(/[?&]monitor=(\d+)/)?.[1] ?? 0);
      if (pre > 0) {
        monitorIds = [pre];
        const m = live.byId(pre);
        if (m) title = t('maintenance.defaultTitle', { name: m.name }).slice(0, 200);
      }
    } else {
      try {
        fill(await api.maintenanceItem(id!));
      } catch (e) {
        loadError = e instanceof ApiError && e.status === 404 ? t('maintenance.form.notFound') : errorMessage(e);
      } finally {
        loading = false;
      }
    }
    await tick();
    baseline = snapshot(formState());
  });

  // Kaydedilmemiş değişiklikler.
  let baseline = '';
  let saved = false;
  const formState = () => ({
    title: title.trim(),
    description: description.trim(),
    active,
    strategy,
    timezone,
    start,
    end,
    weekdays: [...weekdays].sort(),
    startTime,
    endTime,
    dateFrom,
    dateTo,
    cron: cron.trim(),
    duration,
    allMonitors,
    monitorIds: [...monitorIds].sort((a, b) => a - b),
  });
  onMount(() => guardUnsaved(() => !saved && !!baseline && !loading && !loadError && snapshot(formState()) !== baseline));

  function toggleDay(v: number) {
    weekdays = weekdays.includes(v) ? weekdays.filter((d) => d !== v) : [...weekdays, v];
  }

  function validate(): { msg: string; field?: string } | null {
    if (!title.trim()) return { msg: t('maintenance.form.errTitle'), field: 'mt-title' };
    if (strategy === 'once') {
      if (!start || !end) return { msg: t('maintenance.form.errStartEnd'), field: start ? 'mt-end' : 'mt-start' };
      if (end <= start) return { msg: t('maintenance.form.errEndAfter'), field: 'mt-end' };
    }
    if (strategy === 'recurring_weekly' && weekdays.length === 0) return { msg: t('maintenance.form.errDays') };
    if ((strategy === 'recurring_weekly' || strategy === 'recurring_daily') && startTime === endTime)
      return { msg: t('maintenance.form.errSameTime'), field: 'mt-et' };
    if (strategy === 'cron') {
      if (!cron.trim()) return { msg: t('maintenance.form.errCron'), field: 'mt-cron' };
      if (duration === null || !Number.isInteger(duration) || duration < 1 || duration > 10080)
        return { msg: t('maintenance.form.errDuration'), field: 'mt-dur' };
    }
    if (dateFrom && dateTo && dateTo < dateFrom) return { msg: t('maintenance.form.errDateRange'), field: 'mt-dt' };
    if (!allMonitors && monitorIds.length === 0) return { msg: t('maintenance.form.errMonitors') };
    return null;
  }

  async function showError(msg: string, field?: string) {
    error = msg;
    await tick();
    markInvalid(field, 'mt-error');
    errorEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    markInvalid(null, 'mt-error');
    const v = validate();
    if (v) return showError(v.msg, v.field);
    // Stratejiye ait olmayan alanlar sunucuda temizlenir; hepsi gönderilebilir.
    const body: MaintenanceInput = {
      title: title.trim(),
      description: description.trim(),
      active,
      strategy,
      timezone: timezone.trim() || DEFAULT_TZ,
      start,
      end,
      weekdays: [...weekdays].sort(),
      start_time: startTime,
      end_time: endTime,
      date_from: dateFrom,
      date_to: dateTo,
      cron: cron.trim(),
      duration_minutes: duration ?? 0,
      all_monitors: allMonitors,
      monitor_ids: allMonitors ? [] : monitorIds,
    };
    saving = true;
    try {
      const res = isEdit ? await api.updateMaintenance(id!, body) : await api.createMaintenance(body);
      saved = true;
      toast.success(isEdit ? t('maintenance.form.saved') : t('maintenance.form.added', { name: res.title }));
      navigate('/maintenance');
    } catch (err) {
      showError(errorMessage(err));
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!current) return;
    const ok = await confirmDialog({
      title: t('maintenance.list.deleteTitle'),
      message: t('maintenance.list.deleteMsg', { name: current.title }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteMaintenance(current.id);
      saved = true;
      toast.success(t('maintenance.form.deleted'));
      navigate('/maintenance');
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const overnight = $derived(
    (strategy === 'recurring_weekly' || strategy === 'recurring_daily') && endTime && startTime && endTime < startTime,
  );
  // Tekrarlayan pencerenin uzunluğu (dakika): bitiş başlangıçtan önceyse ertesi gün.
  // Ör. 02:00–01:00 23 saatlik bir penceredir; uzun pencerede uyarı gösterilir.
  const hm = (v: string) => {
    const m = v.match(/^(\d{2}):(\d{2})/);
    return m ? +m[1] * 60 + +m[2] : null;
  };
  const spanMin = $derived.by(() => {
    const a = hm(startTime);
    const b = hm(endTime);
    if (a === null || b === null || a === b) return 0;
    return (b - a + 1440) % 1440;
  });
</script>

<a class="back" href="#/maintenance"><Icon name="chevron-left" size={16} /> {t('maintenance.form.back')}</a>
<div class="page-head">
  <h1>{isEdit ? t('maintenance.form.editTitle') : t('maintenance.form.addTitle')}<span class="dot">.</span></h1>
  {#if current}
    <span class="badge {MAINT_STATUS[current.status].c} st">{MAINT_STATUS[current.status].l}</span>
  {/if}
</div>

{#if loading}
  <div class="skeleton" style="height:420px;max-width:820px"></div>
{:else if loadError}
  <div class="card empty">
    <h3>{t('maintenance.form.loadFailed')}</h3>
    <p>{loadError}</p>
    <a class="btn primary" href="#/maintenance">{t('maintenance.form.backToList')}</a>
  </div>
{:else}
  <form class="form" onsubmit={submit} novalidate>
    {#if current && nextText(current)}
      <div class="alert {current.status === 'active' ? 'maint' : 'info'} small">{nextText(current)}</div>
    {/if}
    <section class="card stack">
      <div class="field">
        <label for="mt-title">{t('maintenance.form.title')}</label>
        <input id="mt-title" class="input" maxlength="200" bind:value={title} placeholder={t('maintenance.form.titlePlaceholder')} />
      </div>
      <div class="field">
        <label for="mt-desc">{t('maintenance.form.description')} <span class="muted">{t('maintenance.form.optional')}</span></label>
        <textarea id="mt-desc" class="input plain" rows="2" maxlength="2000" bind:value={description} placeholder={t('maintenance.form.descPlaceholder')}></textarea>
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={active} />
        <span>{t('maintenance.form.enabled')}<small>{t('maintenance.form.enabledHelp')}</small></span>
      </label>
    </section>

    <section class="card stack">
      <h2 class="card-title">{t('maintenance.form.schedule')}</h2>
      <div class="strats" role="radiogroup" aria-label={t('maintenance.form.strategyLabel')}>
        {#each STRATEGIES as s (s)}
          <button type="button" role="radio" aria-checked={strategy === s} class="strat" class:active={strategy === s} onclick={() => (strategy = s)}>
            <span class="sl">{STRATEGY_LABELS[s]}</span>
            <span class="sd">{STRATEGY_DESCS[s]}</span>
          </button>
        {/each}
      </div>

      {#if strategy === 'manual'}
        <div class="alert info small">
          {#each tParts('maintenance.form.manualInfo') as p, i (i)}
            {#if p.slot === 'stop'}<b>{t('maintenance.list.stop')}</b>{:else}{p.text}{/if}
          {/each}
        </div>
      {:else if strategy === 'once'}
        <div class="grid-2">
          <div class="field">
            <label for="mt-start">{t('maintenance.form.start')}</label>
            <input id="mt-start" class="input" type="datetime-local" bind:value={start} />
          </div>
          <div class="field">
            <label for="mt-end">{t('maintenance.form.end')}</label>
            <input id="mt-end" class="input" type="datetime-local" min={start} bind:value={end} />
          </div>
        </div>
      {:else}
        {#if strategy === 'recurring_weekly'}
          <fieldset class="days">
            <legend class="label">{t('maintenance.form.days')}</legend>
            <div class="day-list">
              {#each WEEKDAYS as d (d.v)}
                <button
                  type="button"
                  class="day"
                  class:active={weekdays.includes(d.v)}
                  aria-pressed={weekdays.includes(d.v)}
                  aria-label={d.long}
                  onclick={() => toggleDay(d.v)}>{d.short}</button
                >
              {/each}
            </div>
          </fieldset>
        {/if}
        {#if strategy === 'recurring_weekly' || strategy === 'recurring_daily'}
          <div class="grid-2">
            <div class="field">
              <label for="mt-st">{t('maintenance.form.startTime')}</label>
              <input id="mt-st" class="input" type="time" bind:value={startTime} />
            </div>
            <div class="field">
              <label for="mt-et">{t('maintenance.form.endTime')}</label>
              <input id="mt-et" class="input" type="time" bind:value={endTime} />
              <span class="help">{t('maintenance.form.endTimeHelp')}</span>
            </div>
          </div>
          {#if spanMin > 0}
            <div class="alert small {spanMin >= 12 * 60 ? 'warning' : 'info'}" role="status">
              {overnight
                ? t('maintenance.form.spanOvernight', { start: startTime, end: endTime, d: fmtDuration(spanMin * 60) })
                : t('maintenance.form.span', { start: startTime, end: endTime, d: fmtDuration(spanMin * 60) })}{#if spanMin >= 12 * 60}{t(
                  'maintenance.form.spanLong',
                )}{/if}
            </div>
          {/if}
        {:else if strategy === 'cron'}
          <div class="grid-cron">
            <div class="field">
              <label for="mt-cron">{t('maintenance.form.cron')}</label>
              <input id="mt-cron" class="input mono" bind:value={cron} placeholder="0 3 * * 1" autocapitalize="none" spellcheck="false" />
              <span class="help">
                {#each tParts('maintenance.form.cronHelp') as p, i (i)}
                  {#if p.slot === 'a'}<code>0 3 * * 1</code>{:else if p.slot === 'b'}<code>@daily</code>{:else}{p.text}{/if}
                {/each}
              </span>
            </div>
            <div class="field">
              <label for="mt-dur">{t('maintenance.form.durationMin')}</label>
              <input id="mt-dur" class="input" type="number" min="1" max="10080" bind:value={duration} />
            </div>
          </div>
        {/if}
        <fieldset class="range">
          <legend class="label">{t('maintenance.form.dateRange')} <span class="muted">{t('maintenance.form.optional')}</span></legend>
          <div class="grid-2">
            <div class="field">
              <label for="mt-df">{t('maintenance.form.dateFrom')}</label>
              <input id="mt-df" class="input" type="date" bind:value={dateFrom} />
            </div>
            <div class="field">
              <label for="mt-dt">{t('maintenance.form.dateTo')}</label>
              <input id="mt-dt" class="input" type="date" min={dateFrom || undefined} bind:value={dateTo} />
            </div>
          </div>
        </fieldset>
      {/if}

      {#if strategy !== 'manual'}
        <div class="field tz">
          <label for="mt-tz">{t('maintenance.form.timezone')}</label>
          <input id="mt-tz" class="input" list="mt-tzs" bind:value={timezone} placeholder="Europe/Istanbul" autocapitalize="none" spellcheck="false" />
          {#if timezones.length}
            <datalist id="mt-tzs">
              {#each timezones as z (z)}<option value={z}></option>{/each}
            </datalist>
          {/if}
          <span class="help">{t('maintenance.form.timezoneHelp')}</span>
        </div>
      {/if}
    </section>

    <section class="card stack">
      <h2 class="card-title">{t('maintenance.form.affected')}</h2>
      <label class="check">
        <input type="checkbox" bind:checked={allMonitors} />
        <span>{t('maintenance.form.allMonitors')}<small>{t('maintenance.form.allMonitorsHelp')}</small></span>
      </label>
      {#if !allMonitors}
        <MonitorPicker bind:selected={monitorIds} label={t('maintenance.form.affected')} id="mt-mp" />
      {/if}
    </section>

    {#if error}
      <div class="alert error" role="alert" id="mt-error" bind:this={errorEl}>{error}</div>
    {/if}

    <div class="actions">
      {#if isEdit}
        <button type="button" class="btn danger" onclick={remove}><Icon name="trash" size={15} /> {t('common.delete')}</button>
        <div class="spacer"></div>
      {/if}
      <a class="btn" href="#/maintenance">{t('common.cancel')}</a>
      <button class="btn primary" type="submit" disabled={saving}>
        {#if saving}<span class="spinner"></span>{/if}
        {isEdit ? t('common.save') : t('maintenance.form.submitAdd')}
      </button>
    </div>
  </form>
{/if}

<style>
  .form {
    max-width: 820px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .st {
    height: 24px;
    font-size: 0.75rem;
  }
  .card-title {
    margin: 0;
  }
  textarea.plain {
    font-family: var(--font);
    font-size: 0.92rem;
  }
  .strats {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 8px;
  }
  .strat {
    display: flex;
    flex-direction: column;
    gap: 3px;
    align-items: flex-start;
    text-align: left;
    padding: 11px 12px;
    border-radius: 10px;
    border: 1px solid var(--border-strong);
    background: var(--input);
    color: var(--text);
    font: inherit;
    cursor: pointer;
  }
  @media (hover: hover) {
    .strat:hover {
      border-color: var(--border-hover);
    }
    .day:hover {
      border-color: var(--border-hover);
    }
  }
  .strat.active {
    border-color: var(--accent);
    background: var(--accent-soft);
    box-shadow: 0 0 0 1px var(--accent) inset;
  }
  .strat.active .sd {
    color: var(--text-2);
  }
  .sl {
    font-weight: 700;
    font-size: 0.9rem;
  }
  .sd {
    font-size: 0.75rem;
    color: var(--muted);
    line-height: 1.3;
  }
  fieldset {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
  }
  legend {
    padding: 0;
    margin-bottom: 8px;
  }
  .day-list {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .day {
    min-width: 50px;
    height: 38px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--border-strong);
    background: var(--input);
    color: var(--text-2);
    font: inherit;
    font-weight: 600;
    font-size: 0.88rem;
    cursor: pointer;
  }
  .day.active {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-contrast);
  }
  .grid-cron {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 160px;
    gap: 16px;
  }
  .tz {
    max-width: 360px;
  }
  .help code {
    color: var(--text-2);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    flex-wrap: wrap;
  }
  .spacer {
    flex: 1;
  }
  @media (max-width: 900px) {
    .strats {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }
  @media (max-width: 640px) {
    .strats {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .grid-cron {
      grid-template-columns: minmax(0, 1fr);
    }
    .tz {
      max-width: none;
    }
    .day-list {
      display: grid;
      grid-template-columns: repeat(7, minmax(0, 1fr));
      gap: 4px;
    }
    .day {
      min-width: 0;
      padding: 0;
      font-size: 0.8rem;
    }
    .actions > :global(*:not(.spacer)) {
      flex: 1;
    }
    .spacer {
      display: none;
    }
  }
</style>
