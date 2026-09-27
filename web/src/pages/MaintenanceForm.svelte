<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, ApiError, errorMessage, type Maintenance, type MaintenanceInput, type MaintStrategy } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { isoLocal, nowSec } from '../lib/format';
  import { DEFAULT_TZ, MAINT_STATUS, STRATEGY_DESCS, STRATEGY_LABELS, WEEKDAYS, nextText } from '../lib/maintenance';
  import MonitorPicker from '../components/MonitorPicker.svelte';
  import Icon from '../components/Icon.svelte';

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
    if (!isEdit) return;
    try {
      fill(await api.maintenanceItem(id!));
    } catch (e) {
      loadError = e instanceof ApiError && e.status === 404 ? 'Bakım penceresi bulunamadı.' : errorMessage(e);
    } finally {
      loading = false;
    }
  });

  function toggleDay(v: number) {
    weekdays = weekdays.includes(v) ? weekdays.filter((d) => d !== v) : [...weekdays, v];
  }

  function validate(): string {
    if (!title.trim()) return 'Başlık gerekli.';
    if (strategy === 'once') {
      if (!start || !end) return 'Başlangıç ve bitiş zamanını girin.';
      if (end <= start) return 'Bitiş zamanı başlangıçtan sonra olmalı.';
    }
    if (strategy === 'recurring_weekly' && weekdays.length === 0) return 'En az bir gün seçin.';
    if ((strategy === 'recurring_weekly' || strategy === 'recurring_daily') && startTime === endTime)
      return 'Başlangıç ve bitiş saati farklı olmalı.';
    if (strategy === 'cron') {
      if (!cron.trim()) return 'Cron ifadesini girin.';
      if (duration === null || !Number.isInteger(duration) || duration < 1 || duration > 10080)
        return 'Süre 1-10080 dakika arasında olmalı.';
    }
    if (dateFrom && dateTo && dateTo < dateFrom) return 'Bitiş tarihi başlangıç tarihinden önce olamaz.';
    if (!allMonitors && monitorIds.length === 0) return 'En az bir monitör seçin veya tüm monitörleri seçin.';
    return '';
  }

  async function showError(msg: string) {
    error = msg;
    await tick();
    errorEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const v = validate();
    if (v) return showError(v);
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
      toast.success(isEdit ? 'Bakım penceresi kaydedildi' : `“${res.title}” eklendi`);
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
      title: 'Bakımı sil',
      message: `“${current.title}” bakım penceresi silinecek.`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteMaintenance(current.id);
      toast.success('Bakım penceresi silindi');
      navigate('/maintenance');
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const overnight = $derived(
    (strategy === 'recurring_weekly' || strategy === 'recurring_daily') && endTime && startTime && endTime < startTime,
  );
</script>

<a class="back" href="#/maintenance"><Icon name="chevron-left" size={16} /> Bakım pencereleri</a>
<div class="page-head">
  <h1>{isEdit ? 'Bakımı düzenle' : 'Bakım ekle'}<span class="dot">.</span></h1>
  {#if current}
    <span class="badge {MAINT_STATUS[current.status].c} st">{MAINT_STATUS[current.status].l}</span>
  {/if}
</div>

{#if loading}
  <div class="skeleton" style="height:420px;max-width:820px"></div>
{:else if loadError}
  <div class="card empty">
    <h3>Bakım penceresi yüklenemedi</h3>
    <p>{loadError}</p>
    <a class="btn primary" href="#/maintenance">Listeye dön</a>
  </div>
{:else}
  <form class="form" onsubmit={submit} novalidate>
    {#if current && nextText(current)}
      <div class="alert {current.status === 'active' ? 'maint' : 'info'} small">{nextText(current)}</div>
    {/if}
    <section class="card stack">
      <div class="field">
        <label for="mt-title">Başlık</label>
        <input id="mt-title" class="input" maxlength="200" bind:value={title} placeholder="Ör. Sunucu güncellemesi" />
      </div>
      <div class="field">
        <label for="mt-desc">Açıklama <span class="muted">(isteğe bağlı)</span></label>
        <textarea id="mt-desc" class="input plain" rows="2" maxlength="2000" bind:value={description} placeholder="Bakımın nedeni, ekip için notlar"></textarea>
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={active} />
        <span>Aktif<small>Kapalıyken zamanlama çalışmaz; pencere “Durduruldu” görünür.</small></span>
      </label>
    </section>

    <section class="card stack">
      <h2 class="card-title">Zamanlama</h2>
      <div class="strats" role="radiogroup" aria-label="Strateji">
        {#each STRATEGIES as s (s)}
          <button type="button" role="radio" aria-checked={strategy === s} class="strat" class:active={strategy === s} onclick={() => (strategy = s)}>
            <span class="sl">{STRATEGY_LABELS[s]}</span>
            <span class="sd">{STRATEGY_DESCS[s]}</span>
          </button>
        {/each}
      </div>

      {#if strategy === 'manual'}
        <div class="alert info small">
          “Aktif” açık olduğu sürece etkilenen monitörler bakımda sayılır. Bakım bitince listeden <b>Durdur</b>’a basın.
        </div>
      {:else if strategy === 'once'}
        <div class="grid-2">
          <div class="field">
            <label for="mt-start">Başlangıç</label>
            <input id="mt-start" class="input" type="datetime-local" bind:value={start} />
          </div>
          <div class="field">
            <label for="mt-end">Bitiş</label>
            <input id="mt-end" class="input" type="datetime-local" min={start} bind:value={end} />
          </div>
        </div>
      {:else}
        {#if strategy === 'recurring_weekly'}
          <fieldset class="days">
            <legend class="label">Günler</legend>
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
              <label for="mt-st">Başlangıç saati</label>
              <input id="mt-st" class="input" type="time" bind:value={startTime} />
            </div>
            <div class="field">
              <label for="mt-et">Bitiş saati</label>
              <input id="mt-et" class="input" type="time" bind:value={endTime} />
              <span class="help">Bitiş başlangıçtan önceyse ertesi gün biter.{overnight ? ' (Bu pencere gece yarısını geçiyor.)' : ''}</span>
            </div>
          </div>
        {:else if strategy === 'cron'}
          <div class="grid-cron">
            <div class="field">
              <label for="mt-cron">Cron ifadesi</label>
              <input id="mt-cron" class="input mono" bind:value={cron} placeholder="0 3 * * 1" autocapitalize="none" spellcheck="false" />
              <span class="help">dakika saat gün ay haftanın-günü · ör. <code>0 3 * * 1</code> = her pazartesi 03:00, <code>@daily</code></span>
            </div>
            <div class="field">
              <label for="mt-dur">Süre (dakika)</label>
              <input id="mt-dur" class="input" type="number" min="1" max="10080" bind:value={duration} />
            </div>
          </div>
        {/if}
        <fieldset class="range">
          <legend class="label">Tarih aralığı <span class="muted">(isteğe bağlı)</span></legend>
          <div class="grid-2">
            <div class="field">
              <label for="mt-df">Başlangıç tarihi</label>
              <input id="mt-df" class="input" type="date" bind:value={dateFrom} />
            </div>
            <div class="field">
              <label for="mt-dt">Bitiş tarihi</label>
              <input id="mt-dt" class="input" type="date" min={dateFrom || undefined} bind:value={dateTo} />
            </div>
          </div>
        </fieldset>
      {/if}

      {#if strategy !== 'manual'}
        <div class="field tz">
          <label for="mt-tz">Saat dilimi</label>
          <input id="mt-tz" class="input" list="mt-tzs" bind:value={timezone} placeholder="Europe/Istanbul" autocapitalize="none" spellcheck="false" />
          {#if timezones.length}
            <datalist id="mt-tzs">
              {#each timezones as z (z)}<option value={z}></option>{/each}
            </datalist>
          {/if}
          <span class="help">Saatler bu dilime göre yorumlanır.</span>
        </div>
      {/if}
    </section>

    <section class="card stack">
      <h2 class="card-title">Etkilenen monitörler</h2>
      <label class="check">
        <input type="checkbox" bind:checked={allMonitors} />
        <span>Tüm monitörler<small>Sonradan eklenen monitörler de dahil.</small></span>
      </label>
      {#if !allMonitors}
        <MonitorPicker bind:selected={monitorIds} label="Etkilenen monitörler" id="mt-mp" />
      {/if}
    </section>

    {#if error}
      <div class="alert error" role="alert" bind:this={errorEl}>{error}</div>
    {/if}

    <div class="actions">
      {#if isEdit}
        <button type="button" class="btn danger" onclick={remove}><Icon name="trash" size={15} /> Sil</button>
        <div class="spacer"></div>
      {/if}
      <a class="btn" href="#/maintenance">Vazgeç</a>
      <button class="btn primary" type="submit" disabled={saving}>
        {#if saving}<span class="spinner"></span>{/if}
        {isEdit ? 'Kaydet' : 'Bakımı ekle'}
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
