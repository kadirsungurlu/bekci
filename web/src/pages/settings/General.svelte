<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type AppSettings } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/ui.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import { LOCALES, t, tParts, type Locale } from '../../lib/i18n';

  let loaded = $state(false);
  let loadError = $state('');
  let rawDays = $state<number | null>(14);
  let hourlyDays = $state<number | null>(365);
  let certDays = $state('21, 14, 7, 3, 1');
  let backupKeep = $state<number | null>(7);
  let notifyLang = $state<Locale>('tr');
  let stError = $state('');
  let stBusy = $state(false);

  function apply(s: AppSettings) {
    rawDays = s.retention_raw_days;
    hourlyDays = s.retention_hourly_days;
    certDays = (s.cert_days ?? []).join(', ');
    backupKeep = s.backup_keep;
    notifyLang = s.notify_lang ?? 'tr';
  }

  async function load() {
    try {
      apply(await api.settings());
      loaded = true;
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    }
  }
  onMount(load);

  const isInt = (v: number | null, lo: number, hi: number) => v !== null && Number.isInteger(v) && v >= lo && v <= hi;

  async function saveSettings(e: SubmitEvent) {
    e.preventDefault();
    stError = '';
    if (!isInt(rawDays, 1, 90)) return (stError = t('settings.general.errRaw'));
    if (!isInt(hourlyDays, 90, 3650)) return (stError = t('settings.general.errHourly'));
    if (hourlyDays! < rawDays!) return (stError = t('settings.general.errHourlyShort'));
    const parts = certDays
      .split(/[,\s]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    const days = parts.map(Number);
    if (days.some((d) => !Number.isInteger(d) || d < 0 || d > 90))
      return (stError = t('settings.general.errCertDays'));
    if (days.length > 10) return (stError = t('settings.general.errCertCount'));
    if (!isInt(backupKeep, 0, 60)) return (stError = t('settings.general.errBackup'));
    stBusy = true;
    try {
      const res = await api.saveSettings({
        retention_raw_days: rawDays!,
        retention_hourly_days: hourlyDays!,
        cert_days: days,
        backup_keep: backupKeep!,
        notify_lang: notifyLang,
      });
      apply(res);
      toast.success(t('settings.general.saved'));
    } catch (err) {
      stError = errorMessage(err);
    } finally {
      stBusy = false;
    }
  }

  const metricsUrl = `${location.origin}/metrics`;
  const promYaml = `scrape_configs:
  - job_name: uptime
    scheme: ${location.protocol.replace(':', '')}
    metrics_path: /metrics
    static_configs:
      - targets: ['${location.host}']
    basic_auth:
      username: metrics
      password: upk_…`;

</script>

<div class="cols">
  <form class="card stack" onsubmit={saveSettings} novalidate>
    <h2 class="card-title">{t('settings.general.retentionTitle')}</h2>
    {#if !loaded}
      {#if loadError}
        <div class="alert error load-err"><span>{loadError}</span> <button type="button" class="btn sm" onclick={load}>{t('common.retry')}</button></div>
      {:else}
        <div class="skeleton" style="height:200px"></div>
      {/if}
    {:else}
      <div class="grid-2">
        <div class="field">
          <label for="raw">{t('settings.general.rawDays')}</label>
          <input id="raw" class="input" type="number" min="1" max="90" bind:value={rawDays} />
          <span class="help">{t('settings.general.rawDaysHelp')}</span>
        </div>
        <div class="field">
          <label for="hourly">{t('settings.general.hourlyDays')}</label>
          <input id="hourly" class="input" type="number" min="90" max="3650" bind:value={hourlyDays} />
          <span class="help">{t('settings.general.hourlyDaysHelp')}</span>
        </div>
      </div>
      <div class="field">
        <label for="cert">{t('settings.general.certDays')}</label>
        <input id="cert" class="input" bind:value={certDays} placeholder={t('settings.general.certDaysPlaceholder')} />
        <span class="help">{t('settings.general.certDaysHelp')}</span>
      </div>
      <div class="grid-2">
        <div class="field">
          <label for="bk">{t('settings.general.backupKeep')}</label>
          <input id="bk" class="input" type="number" min="0" max="60" bind:value={backupKeep} />
          <span class="help">{t('settings.general.backupKeepHelp')}</span>
        </div>
        <div class="field">
          <label for="nlang">{t('settings.general.notifyLang')}</label>
          <select id="nlang" class="input" bind:value={notifyLang}>
            {#each LOCALES as l (l)}
              <option value={l} lang={l}>{t(`common.languages.${l}`)}</option>
            {/each}
          </select>
          <span class="help">{t('settings.general.notifyLangHelp')}</span>
        </div>
      </div>
      {#if stError}<div class="alert error" role="alert">{stError}</div>{/if}
      <div class="actions">
        <button class="btn primary" type="submit" disabled={stBusy}>
          {#if stBusy}<span class="spinner"></span>{/if}
          {t('common.save')}
        </button>
      </div>
    {/if}
  </form>

  <div class="stack">
    <div class="card stack prom">
      <h2 class="card-title">Prometheus</h2>
      <p class="text-2 small nomargin">
        {#each tParts('settings.general.promText') as p, i (i)}
          {#if p.slot === 'url'}<code>{metricsUrl}</code>{:else if p.slot === 'link'}<a href="#/settings"
              >{t('settings.general.promLink')}</a
            >{:else}{p.text}{/if}
        {/each}
      </p>
      <div class="copybox"><code>{metricsUrl}</code><CopyButton text={metricsUrl} /></div>
      <div>
        <div class="label">prometheus.yml</div>
        <pre class="yaml">{promYaml}</pre>
        <p class="help nomargin">
          {#each tParts('settings.general.promAlt') as p, i (i)}
            {#if p.slot === 'a'}<code>basic_auth</code>{:else if p.slot === 'b'}<code
                >authorization: {'{'}credentials: upk_…{'}'}</code
              >{:else}{p.text}{/if}
          {/each}
        </p>
      </div>
    </div>
    <div class="card">
      <h2 class="card-title">{t('settings.general.about')}</h2>
      <dl>
        <dt>{t('common.version')}</dt>
        <dd class="mono">{session.version || '—'}</dd>
      </dl>
    </div>
  </div>
</div>

<style>
  .cols {
    display: grid;
    grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr);
    gap: 16px;
    align-items: start;
    max-width: 1200px;
  }
  .card-title {
    margin-bottom: 0;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
  .load-err {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .nomargin {
    margin: 0;
  }
  .prom code {
    color: var(--text-2);
    word-break: break-all;
  }
  .prom .copybox code {
    color: var(--text);
  }
  .label {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--muted);
    margin-bottom: 6px;
  }
  .yaml {
    margin: 0 0 8px;
    padding: 10px 12px;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-2);
    overflow-x: auto;
  }
  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 8px 16px;
    margin: 14px 0 0;
    font-size: 0.92rem;
  }
  dt {
    color: var(--muted);
  }
  dd {
    margin: 0;
    word-break: break-all;
  }
  @media (max-width: 1000px) {
    .cols {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 640px) {
    .actions .btn {
      flex: 1;
    }
  }
</style>
