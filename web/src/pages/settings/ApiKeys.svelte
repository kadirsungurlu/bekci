<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type ApiKey, type ApiKeyStatus, type Role } from '../../lib/api';
  import { ROLE_LABELS, roleRank, session } from '../../lib/session.svelte';
  import { clock, confirmDialog, toast } from '../../lib/ui.svelte';
  import { fmtDate, fmtDay, fmtRelative, isoDay, nowSec, parseLocal } from '../../lib/format';
  import Modal from '../../components/Modal.svelte';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';

  let keys = $state.raw<ApiKey[]>([]);
  let loading = $state(true);
  let loadError = $state('');
  let showAll = $state(false);

  async function load() {
    try {
      keys = await api.apiKeys(showAll && session.isAdmin);
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function toggleAll(v: boolean) {
    showAll = v;
    loading = true;
    load();
  }

  const STATUS: Record<ApiKeyStatus, { l: string; c: string }> = {
    active: { l: 'Etkin', c: 'up' },
    expired: { l: 'Süresi doldu', c: 'paused' },
    revoked: { l: 'İptal edildi', c: 'down' },
  };

  async function revoke(k: ApiKey) {
    const ok = await confirmDialog({
      title: 'Anahtarı iptal et',
      message: `“${k.name}” — Bu anahtar iptal edilsin mi? Onu kullanan betikler çalışmayı durdurur.`,
      confirmText: 'İptal et',
      cancelText: 'Vazgeç',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.revokeApiKey(k.id);
      toast.success('Anahtar iptal edildi');
      load();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  // Yeni anahtar ----------------------------------------------------------------------
  let open = $state(false);
  let name = $state('');
  let role = $state<Role>('viewer');
  let expires = $state('');
  let secret = $state('');
  let error = $state('');
  let busy = $state(false);

  const roles = $derived((['viewer', 'editor', 'admin'] as Role[]).filter((r) => roleRank(r) <= roleRank(session.role)));
  const minDay = $derived(isoDay(clock.now + 86400));

  function openNew() {
    name = '';
    role = 'viewer';
    expires = '';
    secret = '';
    error = '';
    open = true;
  }

  async function create(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const n = name.trim();
    if (!n) return (error = 'Anahtara bir ad verin (ör. “Grafana” veya “yedek betiği”).');
    let exp = 0;
    if (expires) {
      // Seçilen günün sonuna kadar geçerli (İstanbul saati).
      exp = parseLocal(`${expires}T23:59`) + 59;
      if (exp <= nowSec()) return (error = 'Son kullanma tarihi gelecekte olmalı.');
    }
    busy = true;
    try {
      const res = await api.createApiKey(n, role, exp);
      secret = res.secret;
      load();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }


  const origin = location.origin;
  const curl = $derived(`curl -H "Authorization: Bearer ${secret || 'upk_…'}" ${origin}/api/monitors`);
</script>

<section class="card">
  <div class="head">
    <div>
      <h2 class="card-title">API anahtarları</h2>
      <p class="text-2 small sub">Betiklerinizden ve Prometheus gibi araçlardan API’ye erişmek için. Örnek:</p>
      <pre class="ex">curl -H "Authorization: Bearer upk_…" {origin}/api/monitors</pre>
    </div>
    <button class="btn primary" onclick={openNew}><Icon name="plus" size={16} /> Yeni anahtar</button>
  </div>

  {#if session.isAdmin}
    <label class="check all">
      <input type="checkbox" checked={showAll} onchange={(e) => toggleAll(e.currentTarget.checked)} />
      <span>Tüm kullanıcıların anahtarları</span>
    </label>
  {/if}

  {#if loading}
    <div class="skeleton" style="height:90px"></div>
  {:else if loadError}
    <div class="alert error">{loadError} <button class="linkbtn" onclick={load}>Tekrar dene</button></div>
  {:else if keys.length === 0}
    <div class="none">
      <Icon name="key" size={22} />
      <span>Henüz API anahtarı yok.</span>
    </div>
  {:else}
    <table class="table responsive keys">
      <thead>
        <tr>
          <th>Ad</th>
          {#if showAll}<th>Sahibi</th>{/if}
          <th>Yetki</th>
          <th>Önek</th>
          <th>Son kullanım</th>
          <th>Son kullanma</th>
          <th>Durum</th>
          <th><span class="sr">İşlem</span></th>
        </tr>
      </thead>
      <tbody>
        {#each keys as k (k.id)}
          <tr class:dim={k.status !== 'active'}>
            <td data-label="Ad" class="nm">{k.name}</td>
            {#if showAll}<td data-label="Sahibi">{k.username}</td>{/if}
            <td data-label="Yetki">{ROLE_LABELS[k.role] ?? k.role}</td>
            <td data-label="Önek"><code>{k.prefix}…</code></td>
            <td data-label="Son kullanım" class="nowrap" title={k.last_used_at ? fmtDate(k.last_used_at) : ''}>
              {k.last_used_at ? fmtRelative(k.last_used_at, clock.now) : 'Hiç'}
            </td>
            <td data-label="Son kullanma" class="nowrap">{k.expires_at ? fmtDay(k.expires_at) : 'Süresiz'}</td>
            <td data-label="Durum"><span class="badge {STATUS[k.status].c}">{STATUS[k.status].l}</span></td>
            <td class="act">
              {#if k.status === 'active'}
                <button class="btn sm danger" onclick={() => revoke(k)}>İptal et</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>

<Modal bind:open title={secret ? 'Anahtarınız hazır' : 'Yeni API anahtarı'} width={520}>
  {#if secret}
    <div class="stack">
      <div class="alert warning">Bu anahtar bir daha gösterilmeyecek; şimdi kopyalayıp güvenli bir yerde saklayın.</div>
      <div>
        <div class="label">Anahtarınız:</div>
        <div class="copybox">
          <code>{secret}</code>
          <CopyButton text={secret} class="btn sm primary" />
        </div>
      </div>
      <div>
        <div class="label">Kullanım örneği</div>
        <div class="copybox"><code>{curl}</code></div>
      </div>
    </div>
  {:else}
    <form id="akf" class="stack" onsubmit={create} novalidate>
      <div class="field">
        <label for="ak-name">Ad</label>
        <input id="ak-name" class="input" maxlength="100" bind:value={name} placeholder="Ör. Grafana, yedek betiği" />
      </div>
      <div class="grid-2">
        <div class="field">
          <label for="ak-role">Yetki</label>
          <select id="ak-role" class="input" bind:value={role}>
            {#each roles as r (r)}<option value={r}>{ROLE_LABELS[r]}</option>{/each}
          </select>
        </div>
        <div class="field">
          <label for="ak-exp">Son kullanma <span class="muted">(isteğe bağlı)</span></label>
          <input id="ak-exp" class="input" type="date" min={minDay} bind:value={expires} />
        </div>
      </div>
      <span class="help">
        Anahtar, sizin rolünüzü aşamaz; rolünüz düşerse anahtarın yetkisi de düşer. Son kullanma boş bırakılırsa süresiz.
        Yalnızca izleme ve Prometheus için “İzleyici” yeterlidir.
      </span>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {/if}
  {#snippet footer()}
    <div class="spacer"></div>
    {#if secret}
      <button type="button" class="btn primary" onclick={() => (open = false)}>Kopyaladım, kapat</button>
    {:else}
      <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
      <button type="submit" form="akf" class="btn primary" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if} Oluştur
      </button>
    {/if}
  {/snippet}
</Modal>

<style>
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .card-title {
    margin-bottom: 4px;
  }
  .head > div {
    flex: 1 1 280px;
    min-width: 0;
  }
  .sub {
    margin: 0;
    max-width: 720px;
  }
  .ex {
    margin: 6px 0 0;
    padding: 8px 10px;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-2);
    overflow-x: auto;
    max-width: 720px;
  }
  .keys td {
    vertical-align: middle;
  }
  .all {
    margin-bottom: 12px;
    font-size: 0.88rem;
  }
  .none {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--muted);
    padding: 14px 0 4px;
  }
  .keys .nm {
    font-weight: 600;
    word-break: break-word;
  }
  .keys code {
    color: var(--text-2);
  }
  .keys tr.dim td:not(.act) {
    opacity: 0.6;
  }
  .act {
    text-align: right;
    width: 1%;
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
  .spacer {
    flex: 1;
  }
  @media (max-width: 720px) {
    .act {
      text-align: left;
      width: auto;
    }
    .table.responsive td.act::before {
      content: '';
    }
  }
</style>
