<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type StatusPage } from '../lib/api';
  import { confirmDialog, copyText, toast } from '../lib/ui.svelte';
  import { collator } from '../lib/format';
  import RowMenu, { type MenuItem } from '../components/RowMenu.svelte';
  import Icon from '../components/Icon.svelte';

  let list = $state.raw<StatusPage[]>([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    try {
      list = await api.pages();
      error = '';
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  const sorted = $derived(list.slice().sort((a, b) => collator.compare(a.title, b.title)));

  function publicUrl(p: StatusPage): string {
    return `${location.origin}/durum/${p.slug}`;
  }

  const count = (p: StatusPage) => p.sections.reduce((n, s) => n + s.monitors.length, 0);

  async function remove(p: StatusPage) {
    const ok = await confirmDialog({
      title: 'Durum sayfasını sil',
      message: `“${p.title}” sayfası, logosu ve duyuruları kalıcı olarak silinecek. Sayfanın adresi artık açılmayacak.`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deletePage(p.id);
      list = list.filter((x) => x.id !== p.id);
      toast.success(`“${p.title}” silindi`);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function copyUrl(p: StatusPage) {
    if (await copyText(publicUrl(p))) toast.success('Adres panoya kopyalandı');
  }

  function menu(p: StatusPage): MenuItem[] {
    return [
      { label: 'Düzenle', icon: 'edit', href: `#/status-pages/${p.id}` },
      { label: 'Önizle', icon: 'eye', href: `#/status-pages/${p.id}/preview` },
      { label: 'Adresi kopyala', icon: 'copy', onclick: () => copyUrl(p) },
      { label: 'Sil', icon: 'trash', danger: true, onclick: () => remove(p) },
    ];
  }
</script>

<div class="page-head">
  <h1>Durum sayfaları<span class="dot">.</span></h1>
  <a class="btn primary" href="#/status-pages/new"><Icon name="plus" size={16} /> Yeni sayfa</a>
</div>

{#if loading}
  <div class="skeleton" style="height:160px"></div>
{:else if error && list.length === 0}
  <div class="card empty">
    <h3>Durum sayfaları yüklenemedi</h3>
    <p>{error}</p>
    <button class="btn primary" onclick={load}>Tekrar dene</button>
  </div>
{:else if list.length === 0}
  <div class="card empty">
    <div class="big-ic"><Icon name="layout" size={30} /></div>
    <h3>Henüz durum sayfası yok</h3>
    <p>Müşterilerinize servislerinizin durumunu, planlı bakımları ve geçmiş olayları gösteren herkese açık bir sayfa oluşturun.</p>
    <a class="btn primary" href="#/status-pages/new"><Icon name="plus" size={16} /> Sayfa oluştur</a>
  </div>
{:else}
  <div class="grid">
    {#each sorted as p (p.id)}
      <article class="card page">
        <div class="top">
          <a class="logo" href="#/status-pages/{p.id}" aria-hidden="true" tabindex="-1">
            {#if p.has_logo}
              <img src="/api/status-pages/{p.id}/logo?v={p.updated_at}" alt="" />
            {:else}
              <Icon name="layout" size={20} />
            {/if}
          </a>
          <div class="tt">
            <h2><a href="#/status-pages/{p.id}">{p.title}</a></h2>
            <div class="badges">
              {#if p.published}
                <span class="badge up">Yayında</span>
              {:else}
                <span class="badge paused">Taslak</span>
              {/if}
              {#if p.has_password}<span class="badge"><Icon name="lock" size={11} /> Şifreli</span>{/if}
              <span class="muted small">{count(p)} monitör</span>
            </div>
          </div>
          <RowMenu items={menu(p)} label="{p.title} için işlemler" />
        </div>
        <div class="links">
          <a href={publicUrl(p)} target="_blank" rel="noopener noreferrer" class="url">
            <Icon name="link" size={14} /> <span>/durum/{p.slug}</span>
            <Icon name="external" size={12} />
          </a>
          {#if p.custom_domain}
            <a href="https://{p.custom_domain}" target="_blank" rel="noopener noreferrer" class="url">
              <Icon name="globe" size={14} /> <span>{p.custom_domain}</span>
              <Icon name="external" size={12} />
            </a>
          {/if}
        </div>
        {#if p.description}<p class="desc">{p.description}</p>{/if}
        <div class="foot">
          <a class="btn sm" href="#/status-pages/{p.id}"><Icon name="edit" size={14} /> Düzenle</a>
          <a class="btn sm ghost" href="#/status-pages/{p.id}/preview"><Icon name="eye" size={14} /> Önizle</a>
        </div>
      </article>
    {/each}
  </div>
{/if}

<style>
  .big-ic {
    display: flex;
    justify-content: center;
    color: var(--accent-text);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 16px;
  }
  .page {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  .top {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .logo {
    width: 44px;
    height: 44px;
    border-radius: 10px;
    background: var(--card-2);
    border: 1px solid var(--border);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--muted);
    flex-shrink: 0;
    overflow: hidden;
  }
  .logo img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }
  .tt {
    flex: 1;
    min-width: 0;
  }
  h2 {
    font-size: 1.02rem;
    margin-bottom: 5px;
    overflow-wrap: anywhere;
  }
  h2 a {
    color: var(--text);
  }
  .badges {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .badge :global(svg) {
    margin-right: 3px;
  }
  .links {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .url {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.86rem;
    min-width: 0;
    align-self: flex-start;
    max-width: 100%;
  }
  .url span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .url :global(svg) {
    flex-shrink: 0;
  }
  .desc {
    margin: 0;
    font-size: 0.86rem;
    color: var(--text-2);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .foot {
    display: flex;
    gap: 8px;
    margin-top: auto;
    padding-top: 4px;
  }
  @media (max-width: 640px) {
    .grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
