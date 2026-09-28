<script lang="ts">
  // Listeden hızlı işlem: monitörü bir durum sayfasının grubuna (veya yeni bir gruba) ekler.
  import { onMount } from 'svelte';
  import { api, errorMessage, type MonitorView, type StatusPage } from '../lib/api';
  import { collator } from '../lib/format';
  import { toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';

  let { open = $bindable(false), m }: { open?: boolean; m: MonitorView } = $props();

  let pages = $state.raw<StatusPage[]>([]);
  let loaded = $state(false);
  let pageId = $state('');
  let section = $state('');
  let sectionTitle = $state('');
  let name = $state('');
  let busy = $state(false);
  let error = $state('');
  let loadError = $state('');

  const has = (p: StatusPage) => p.sections.some((s) => s.monitors.some((x) => x.id === m.id));
  const page = $derived(pages.find((p) => String(p.id) === pageId) ?? null);
  const already = $derived(pages.filter(has));
  const available = $derived(pages.filter((p) => !has(p)));

  async function load() {
    loaded = false;
    loadError = '';
    try {
      pages = (await api.pages()).slice().sort((a, b) => collator.compare(a.title, b.title));
      const first = pages.find((p) => !has(p));
      if (first) choosePage(String(first.id));
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loaded = true;
    }
  }
  onMount(load);

  function choosePage(id: string) {
    pageId = id;
    const p = pages.find((x) => String(x.id) === id);
    // Varsayılan: son grup; grup yoksa yeni grup.
    section = p && p.sections.length ? String(p.sections.length - 1) : 'new';
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (!page) return (error = 'Bir durum sayfası seçin.');
    busy = true;
    try {
      await api.addPageMonitor(page.id, {
        monitor_id: m.id,
        section: section === 'new' ? -1 : Number(section),
        section_title: section === 'new' ? sectionTitle.trim() : '',
        name: name.trim(),
      });
      toast.success(`“${m.name}”, “${page.title}” sayfasına eklendi`);
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open title="Durum sayfasına ekle" width={500}>
  {#if !loaded}
    <div class="skeleton" style="height:140px"></div>
  {:else if loadError}
    <div class="alert error" role="alert">Durum sayfaları yüklenemedi: {loadError}</div>
    <button type="button" class="btn sm retry" onclick={load}><Icon name="refresh" size={14} /> Tekrar dene</button>
  {:else if pages.length === 0}
    <div class="none">
      <Icon name="layout" size={22} />
      <p>Henüz durum sayfası yok. Müşterilerinize hizmetlerinizin durumunu gösteren herkese açık bir sayfa oluşturun.</p>
      <a class="btn sm" href="#/status-pages/new" onclick={() => (open = false)}><Icon name="plus" size={14} /> Durum sayfası oluştur</a>
    </div>
  {:else}
    <form id="qpage" class="stack" onsubmit={submit} novalidate>
      {#if available.length === 0}
        <div class="alert info small">“{m.name}” tüm durum sayfalarında zaten var.</div>
      {:else}
        <div class="field">
          <label for="qp-page">Durum sayfası</label>
          <select id="qp-page" class="input" value={pageId} onchange={(e) => choosePage(e.currentTarget.value)}>
            {#each available as p (p.id)}
              <option value={String(p.id)}>{p.title} · /durum/{p.slug}</option>
            {/each}
          </select>
        </div>
        {#if page}
          <div class="field">
            <label for="qp-sec">Grup</label>
            <select id="qp-sec" class="input" bind:value={section}>
              {#each page.sections as s, i (i)}
                <option value={String(i)}>{s.title || `Grup ${i + 1}`} ({s.monitors.length} monitör)</option>
              {/each}
              <option value="new">+ Yeni grup</option>
            </select>
          </div>
          {#if section === 'new'}
            <div class="field">
              <label for="qp-st">Grup adı <span class="muted">(isteğe bağlı)</span></label>
              <input id="qp-st" class="input" maxlength="100" bind:value={sectionTitle} placeholder="Ör. Web siteleri" />
            </div>
          {/if}
          <div class="field">
            <label for="qp-name">Sayfada görünen ad <span class="muted">(isteğe bağlı)</span></label>
            <input id="qp-name" class="input" maxlength="100" bind:value={name} placeholder={m.name} />
          </div>
        {/if}
      {/if}
      {#if already.length}
        <p class="help nomargin">
          Zaten ekli: {#each already as p, i (p.id)}{i ? ', ' : ''}<a href="#/status-pages/{p.id}" onclick={() => (open = false)}>{p.title}</a>{/each}
        </p>
      {/if}
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {/if}
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
    {#if pages.length && available.length}
      <button type="submit" form="qpage" class="btn primary" disabled={busy || !page}>
        {#if busy}<span class="spinner"></span>{/if}
        Ekle
      </button>
    {/if}
  {/snippet}
</Modal>

<style>
  .retry {
    margin-top: 12px;
  }
  .none {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-align: center;
    padding: 12px 8px 4px;
    color: var(--muted);
  }
  .none p {
    margin: 0 0 6px;
    font-size: 0.9rem;
  }
  .nomargin {
    margin: 0;
  }
  .spacer {
    flex: 1;
  }
</style>
