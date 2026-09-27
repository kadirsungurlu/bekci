<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type AppSettings } from '../lib/api';
  import { toast } from '../lib/ui.svelte';

  let { version, username }: { version: string; username: string } = $props();

  // Şifre
  let current = $state('');
  let next = $state('');
  let next2 = $state('');
  let pwError = $state('');
  let pwBusy = $state(false);

  async function changePassword(e: SubmitEvent) {
    e.preventDefault();
    pwError = '';
    if (!current) return (pwError = 'Mevcut şifrenizi girin.');
    if (next.length < 8) return (pwError = 'Yeni şifre en az 8 karakter olmalı.');
    if (next !== next2) return (pwError = 'Yeni şifreler birbiriyle aynı değil.');
    pwBusy = true;
    try {
      await api.changePassword(current, next);
      current = next = next2 = '';
      toast.success('Şifreniz değiştirildi. Diğer cihazlardaki oturumlar kapatıldı.');
    } catch (err) {
      pwError = errorMessage(err);
    } finally {
      pwBusy = false;
    }
  }

  // Uygulama ayarları
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
    if (!isInt(hourlyDays, 7, 3650)) return (stError = 'Saatlik özet süresi 7-3650 gün arasında olmalı.');
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
</script>

<div class="page-head">
  <h1>Ayarlar<span class="dot">.</span></h1>
</div>

<div class="cols">
  <form class="card stack" onsubmit={saveSettings} novalidate>
    <h2 class="card-title">Veri saklama</h2>
    {#if !loaded}
      {#if loadError}
        <div class="alert error">{loadError} <button type="button" class="btn sm" onclick={load}>Tekrar dene</button></div>
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
          <input id="hourly" class="input" type="number" min="7" max="3650" bind:value={hourlyDays} />
          <span class="help">7-3650 gün. Günlük özetler süresiz saklanır.</span>
        </div>
      </div>
      <div class="field">
        <label for="cert">SSL uyarı günleri</label>
        <input id="cert" class="input" bind:value={certDays} placeholder="21, 14, 7, 3, 1" />
        <span class="help">Sertifikanın bitmesine bu kadar gün kala bildirim gönderilir. Virgülle ayırın (en fazla 10).</span>
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
    <form class="card stack" onsubmit={changePassword} novalidate>
      <h2 class="card-title">Şifre değiştir</h2>
      <input type="text" name="username" autocomplete="username" value={username} hidden readonly />
      <div class="field">
        <label for="cur">Mevcut şifre</label>
        <input id="cur" class="input" type="password" autocomplete="current-password" bind:value={current} />
      </div>
      <div class="field">
        <label for="new">Yeni şifre</label>
        <input id="new" class="input" type="password" autocomplete="new-password" bind:value={next} />
        <span class="help">En az 8 karakter.</span>
      </div>
      <div class="field">
        <label for="new2">Yeni şifre (tekrar)</label>
        <input id="new2" class="input" type="password" autocomplete="new-password" bind:value={next2} />
      </div>
      {#if pwError}<div class="alert error" role="alert">{pwError}</div>{/if}
      <div class="actions">
        <button class="btn primary" type="submit" disabled={pwBusy}>
          {#if pwBusy}<span class="spinner"></span>{/if}
          Şifreyi değiştir
        </button>
      </div>
    </form>

    <div class="card">
      <h2 class="card-title">Hakkında</h2>
      <dl>
        <dt>Kullanıcı</dt>
        <dd>{username}</dd>
        <dt>Sürüm</dt>
        <dd class="mono">{version || '—'}</dd>
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
    max-width: 1100px;
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
  @media (max-width: 900px) {
    .cols {
      grid-template-columns: minmax(0, 1fr);
    }
    .narrow {
      max-width: none;
    }
  }
</style>
