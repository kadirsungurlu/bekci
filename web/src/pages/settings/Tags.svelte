<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type Tag } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { confirmDialog, toast } from '../../lib/ui.svelte';
  import { collator } from '../../lib/format';
  import RowMenu, { type MenuItem } from '../../components/RowMenu.svelte';
  import TagChip from '../../components/TagChip.svelte';
  import TagDialog from '../../components/TagDialog.svelte';
  import Icon from '../../components/Icon.svelte';

  let tags = $state.raw<Tag[]>([]);
  let loading = $state(true);
  let loadError = $state('');

  async function load() {
    try {
      tags = await api.tags();
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  const sorted = $derived(tags.slice().sort((a, b) => collator.compare(a.name, b.name)));

  let formOpen = $state(false);
  let editing = $state<Tag | null>(null);
  let formKey = $state(0);

  function openForm(t: Tag | null) {
    editing = t;
    formKey++;
    formOpen = true;
  }

  function saved(t: Tag, created: boolean) {
    tags = created ? [...tags, { ...t, monitor_count: 0 }] : tags.map((x) => (x.id === t.id ? { ...t, monitor_count: x.monitor_count } : x));
    toast.success(created ? `“${t.name}” etiketi eklendi` : 'Etiket kaydedildi');
    // Monitör listesindeki etiket adları/renkleri güncellensin.
    if (!created) live.refresh();
  }

  async function remove(t: Tag) {
    const ok = await confirmDialog({
      title: 'Etiketi sil',
      message: `“${t.name}” etiketi silinsin mi? ${t.monitor_count} monitörden kaldırılacak.`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteTag(t.id);
      tags = tags.filter((x) => x.id !== t.id);
      toast.success('Etiket silindi');
      live.refresh();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const menu = (t: Tag): MenuItem[] => [
    { label: 'Düzenle', icon: 'edit', onclick: () => openForm(t) },
    { label: 'Sil', icon: 'trash', danger: true, onclick: () => remove(t) },
  ];
</script>

<section class="card">
  <div class="head">
    <div>
      <h2 class="card-title">Etiketler</h2>
      <p class="text-2 small sub">
        Monitörleri ortam, müşteri veya ekip gibi başlıklarla gruplayın. Bir etiketi monitöre eklerken isteğe bağlı bir değer de
        verebilirsiniz (ör. <b>ortam: canlı</b>). Monitör listesinde etikete göre filtreleyebilirsiniz.
      </p>
    </div>
    <button class="btn primary" onclick={() => openForm(null)}><Icon name="plus" size={16} /> Yeni etiket</button>
  </div>

  {#if loading}
    <div class="skeleton" style="height:120px"></div>
  {:else if loadError}
    <div class="alert error">{loadError} <button class="linkbtn" onclick={load}>Tekrar dene</button></div>
  {:else if tags.length === 0}
    <div class="none">
      <Icon name="tag" size={22} />
      <span>Henüz etiket yok. İlk etiketinizi ekleyin veya monitör formundan oluşturun.</span>
    </div>
  {:else}
    <ul class="tags">
      {#each sorted as t (t.id)}
        <li>
          <span class="swatch" style="--sw:{t.color}" aria-hidden="true"></span>
          <span class="tinfo">
            <TagChip name={t.name} color={t.color} />
            <span class="muted small">{t.monitor_count} monitör</span>
          </span>
          <code class="hex">{t.color}</code>
          <RowMenu items={menu(t)} label="{t.name} için işlemler" />
        </li>
      {/each}
    </ul>
  {/if}
</section>

{#key formKey}
  {#if formKey > 0}
    <TagDialog bind:open={formOpen} tag={editing} onsaved={saved} />
  {/if}
{/key}

<style>
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .head > div {
    flex: 1 1 280px;
    min-width: 0;
  }
  .card-title {
    margin-bottom: 4px;
  }
  .sub {
    margin: 0;
    max-width: 720px;
  }
  .none {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--muted);
    padding: 14px 0 4px;
  }
  .tags {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .tags li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
  }
  .tags li:last-child {
    border-bottom: none;
  }
  .swatch {
    width: 14px;
    height: 14px;
    border-radius: 4px;
    background: var(--sw);
    box-shadow: 0 0 0 1px var(--border-strong);
    flex-shrink: 0;
  }
  .tinfo {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .hex {
    color: var(--muted);
  }
  @media (max-width: 640px) {
    .hex {
      display: none;
    }
    .tinfo {
      gap: 4px 10px;
    }
  }
</style>
