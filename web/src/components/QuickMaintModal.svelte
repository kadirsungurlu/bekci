<script lang="ts">
  // Listeden hızlı işlem: monitörü hemen başlayan tek seferlik bir bakım
  // penceresine alır (yalnızca bu monitör). Sürmekte olan bakımlar da gösterilir.
  import { onMount } from 'svelte';
  import { api, errorMessage, type Maintenance, type MonitorView } from '../lib/api';
  import { fmtDate, isoLocal, nowSec, timeLocative } from '../lib/format';
  import { live } from '../lib/live.svelte';
  import { DEFAULT_TZ, nextText } from '../lib/maintenance';
  import { clock, toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';

  let { open = $bindable(false), m }: { open?: boolean; m: MonitorView } = $props();

  type Choice = '30' | '60' | '240' | 'custom';
  const CHOICES: { v: Choice; l: string }[] = [
    { v: '30', l: '30 dk' },
    { v: '60', l: '1 saat' },
    { v: '240', l: '4 saat' },
    { v: 'custom', l: 'Özel' },
  ];
  const UNITS = { m: { l: 'dakika', s: 60 }, h: { l: 'saat', s: 3600 }, d: { l: 'gün', s: 86400 } } as const;
  type Unit = keyof typeof UNITS;

  let choice = $state<Choice>('60');
  let amount = $state<number | null>(2);
  let unit = $state<Unit>('h');
  let note = $state('');
  let busy = $state(false);
  let error = $state('');

  let current = $state.raw<Maintenance[]>([]);
  let stopping = $state<number | null>(null);

  const seconds = $derived.by(() => {
    if (choice !== 'custom') return Number(choice) * 60;
    if (amount === null || !Number.isFinite(amount) || amount <= 0) return 0;
    return Math.round(amount * UNITS[unit].s);
  });
  const MAX = 30 * 86400;
  // Pencere açık kaldıkça bitiş zamanı da ilerlesin (genel saat birkaç saniyede bir tıklar).
  const endAt = $derived(seconds > 0 ? clock.now + seconds : 0);

  onMount(async () => {
    try {
      const list = await api.maintenance();
      current = list.filter((w) => w.status === 'active' && (w.all_monitors || w.monitor_ids.includes(m.id)));
    } catch {
      /* bilgi amaçlı; yüklenemezse gösterilmez */
    }
  });

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (seconds < 60) return (error = 'Süre en az 1 dakika olmalı.');
    if (seconds > MAX) return (error = 'Süre en fazla 30 gün olabilir.');
    const now = nowSec();
    const title = `Bakım: ${m.name}`.slice(0, 200);
    busy = true;
    try {
      await api.createMaintenance({
        title,
        description: note.trim(),
        active: true,
        strategy: 'once',
        timezone: DEFAULT_TZ,
        start: isoLocal(now),
        end: isoLocal(now + seconds),
        weekdays: [],
        start_time: '',
        end_time: '',
        date_from: '',
        date_to: '',
        cron: '',
        duration_minutes: 0,
        all_monitors: false,
        monitor_ids: [m.id],
      });
      toast.success(`“${m.name}” ${fmtDate(now + seconds)} tarihine kadar bakımda`);
      live.refresh();
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function stop(w: Maintenance) {
    stopping = w.id;
    try {
      await api.pauseMaintenance(w.id);
      current = current.filter((x) => x.id !== w.id);
      toast.success(`“${w.title}” durduruldu`);
      live.refresh();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      stopping = null;
    }
  }
</script>

<Modal bind:open title="Bakıma al" width={500}>
  <form id="qmaint" class="stack" onsubmit={submit} novalidate>
    <p class="lead">
      <b>{m.name}</b> bakım süresince kontrol edilmeye devam eder, ama kesinti sayılmaz ve bildirim gönderilmez.
    </p>

    {#if current.length}
      <div class="current">
        {#each current as w (w.id)}
          <div class="cw">
            <span class="cw-ic"><Icon name="wrench" size={15} /></span>
            <span class="cw-t">
              <b>{w.all_monitors ? `${w.title} (tüm monitörler)` : w.title}</b>
              <small>{nextText(w)}</small>
            </span>
            {#if !w.all_monitors && w.monitor_ids.length === 1}
              <button type="button" class="btn sm" disabled={stopping !== null} onclick={() => stop(w)}>
                {#if stopping === w.id}<span class="spinner"></span>{/if} Bitir
              </button>
            {:else}
              <a class="btn sm" href="#/maintenance/{w.id}" onclick={() => (open = false)}>Aç</a>
            {/if}
          </div>
        {/each}
      </div>
    {/if}

    <div class="field">
      <span class="label" id="qm-dur">Süre</span>
      <div class="choices" role="radiogroup" aria-labelledby="qm-dur">
        {#each CHOICES as c (c.v)}
          <button type="button" role="radio" aria-checked={choice === c.v} class:active={choice === c.v} onclick={() => (choice = c.v)}>
            {c.l}
          </button>
        {/each}
      </div>
      {#if choice === 'custom'}
        <div class="custom">
          <input class="input" type="number" min="1" step="1" bind:value={amount} aria-label="Süre" />
          <select class="input" bind:value={unit} aria-label="Birim">
            {#each Object.entries(UNITS) as [k, u] (k)}<option value={k}>{u.l}</option>{/each}
          </select>
        </div>
      {/if}
      <span class="help">
        {#if endAt}Şimdi başlar, <b>{fmtDate(endAt)}</b>{timeLocative(endAt)} biter.{:else}Geçerli bir süre girin.{/if}
      </span>
    </div>

    <div class="field">
      <label for="qm-note">Not <span class="muted">(isteğe bağlı)</span></label>
      <input id="qm-note" class="input" maxlength="500" bind:value={note} placeholder="Ör. sunucu güncellemesi" />
    </div>

    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <a class="plan" href="#/maintenance/new?monitor={m.id}" onclick={() => (open = false)}>
      <Icon name="calendar" size={14} /> Bakım planla
    </a>
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
    <button type="submit" form="qmaint" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      Bakıma al
    </button>
  {/snippet}
</Modal>

<style>
  .lead {
    margin: 0;
    color: var(--text-2);
    font-size: 0.9rem;
  }
  .lead b {
    color: var(--text);
  }
  .choices {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 8px;
  }
  .choices button {
    height: 40px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: var(--input);
    color: var(--text-2);
    font: inherit;
    font-weight: 600;
    font-size: 0.9rem;
    cursor: pointer;
    transition:
      border-color 0.12s,
      background 0.12s;
  }
  @media (hover: hover) {
    .choices button:hover {
      border-color: var(--border-hover);
      color: var(--text);
    }
  }
  .choices button.active {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--accent-text);
  }
  .custom {
    display: grid;
    grid-template-columns: 110px 140px;
    gap: 8px;
    margin-top: 2px;
  }
  .help b {
    color: var(--text-2);
    font-weight: 600;
  }
  .current {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .cw {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 10px 9px 12px;
    border: 1px solid var(--maint-border);
    background: var(--maint-soft);
    border-radius: var(--radius-sm);
  }
  .cw-ic {
    display: inline-flex;
    color: var(--maint);
  }
  .cw-t {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    font-size: 0.88rem;
  }
  .cw-t b {
    color: var(--maint-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cw-t small {
    color: var(--text-2);
  }
  .plan {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.88rem;
  }
  .spacer {
    flex: 1;
  }
  @media (max-width: 640px) {
    .choices {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .custom {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
  }
</style>
