<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type AppSettings } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/ui.svelte';
  import CopyButton from '../../components/CopyButton.svelte';

  let loaded = $state(false);
  let loadError = $state('');
  let rawDays = $state<number | null>(14);
  let hourlyDays = $state<number | null>(365);
  let certDays = $state('21, 14, 7, 3, 1');
  let backupKeep = $state<number | null>(7);
  let stError = $state('');
  let stBusy = $state(false);

  function apply(s: AppSettings) {
    rawDays = s.retention_raw_days;
    hourlyDays = s.retention_hourly_days;
    certDays = (s.cert_days ?? []).join(', ');
    backupKeep = s.backup_keep;
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
    if (!isInt(rawDays, 1, 90)) return (stError = 'Ham kayıt süresi 1-90 gün arasında olmalı.');
    if (!isInt(hourlyDays, 90, 3650)) return (stError = 'Saatlik özet süresi 90-3650 gün arasında olmalı.');
    if (hourlyDays! < rawDays!) return (stError = 'Saatlik özet süresi ham kayıt süresinden kısa olamaz.');
    const parts = certDays
      .split(/[,\s]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    const days = parts.map(Number);
    if (days.some((d) => !Number.isInteger(d) || d < 0 || d > 90))
      return (stError = 'SSL uyarı günleri 0-90 arasında tam sayılar olmalı (virgülle ayırın).');
    if (days.length > 10) return (stError = 'En fazla 10 SSL uyarı günü girilebilir.');
    if (!isInt(backupKeep, 0, 60)) return (stError = 'Yedek sayısı 0-60 arasında olmalı.');
    stBusy = true;
    try {
      const res = await api.saveSettings({
        retention_raw_days: rawDays!,
        retention_hourly_days: hourlyDays!,
        cert_days: days,
        backup_keep: backupKeep!,
      });
      apply(res);
      toast.success('Ayarlar kaydedildi');
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
    <h2 class="card-title">Veri saklama ve uyarılar</h2>
    {#if !loaded}
      {#if loadError}
        <div class="alert error load-err"><span>{loadError}</span> <button type="button" class="btn sm" onclick={load}>Tekrar dene</button></div>
      {:else}
        <div class="skeleton" style="height:200px"></div>
      {/if}
    {:else}
      <div class="grid-2">
        <div class="field">
          <label for="raw">Ham kayıt süresi (gün)</label>
          <input id="raw" class="input" type="number" min="1" max="90" bind:value={rawDays} />
          <span class="help">Her kontrolün ayrı kaydı. 1-90 gün.</span>
        </div>
        <div class="field">
          <label for="hourly">Saatlik özet süresi (gün)</label>
          <input id="hourly" class="input" type="number" min="90" max="3650" bind:value={hourlyDays} />
          <span class="help">90-3650 gün. 30 ve 90 günlük grafikler bu özetlerden çizilir; günlük özetler süresiz saklanır.</span>
        </div>
      </div>
      <div class="field">
        <label for="cert">SSL uyarı günleri</label>
        <input id="cert" class="input" bind:value={certDays} placeholder="Ör. 21, 14, 7" />
        <span class="help">
          Sertifikanın bitmesine bu kadar gün kala bildirim gönderilir. Virgülle ayırın (en fazla 10); boş bırakırsanız SSL
          uyarısı gönderilmez.
        </span>
      </div>
      <div class="field narrow">
        <label for="bk">Gece yedeği sayısı</label>
        <input id="bk" class="input" type="number" min="0" max="60" bind:value={backupKeep} />
        <span class="help">Her gece veritabanı yedeklenir ve son N yedek tutulur. 0 = yedek alma.</span>
      </div>
      {#if stError}<div class="alert error" role="alert">{stError}</div>{/if}
      <div class="actions">
        <button class="btn primary" type="submit" disabled={stBusy}>
          {#if stBusy}<span class="spinner"></span>{/if}
          Kaydet
        </button>
      </div>
    {/if}
  </form>

  <div class="stack">
    <div class="card stack prom">
      <h2 class="card-title">Prometheus</h2>
      <p class="text-2 small nomargin">
        Metrikler <code>{metricsUrl}</code> adresindedir ve bir API anahtarı (izleyici yetkisi yeterli) gerektirir. Anahtarı
        <a href="#/settings">Hesabım › API anahtarları</a> bölümünden oluşturun.
      </p>
      <div class="copybox"><code>{metricsUrl}</code><CopyButton text={metricsUrl} /></div>
      <div>
        <div class="label">prometheus.yml</div>
        <pre class="yaml">{promYaml}</pre>
        <p class="help nomargin">
          <code>basic_auth</code> yerine <code>authorization: {'{'}credentials: upk_…{'}'}</code> da kullanılabilir.
        </p>
      </div>
    </div>
    <div class="card">
      <h2 class="card-title">Hakkında</h2>
      <dl>
        <dt>Sürüm</dt>
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
  .narrow {
    max-width: 260px;
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
    .narrow {
      max-width: none;
    }
  }
  @media (max-width: 640px) {
    .actions .btn {
      flex: 1;
    }
  }
</style>
