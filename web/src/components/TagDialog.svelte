<script lang="ts">
  // Etiket ekleme/düzenleme penceresi (Ayarlar → Etiketler ve monitör formu).
  import { api, errorMessage, type Tag } from '../lib/api';
  import { DEFAULT_TAG_COLOR, TAG_COLORS, isHexColor } from '../lib/tags';
  import Modal from './Modal.svelte';
  import TagChip from './TagChip.svelte';

  let {
    open = $bindable(false),
    tag = null,
    onsaved,
  }: {
    open?: boolean;
    tag?: Tag | null;
    onsaved: (t: Tag, created: boolean) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let name = $state(tag?.name ?? '');
  // svelte-ignore state_referenced_locally
  let color = $state(tag?.color ?? DEFAULT_TAG_COLOR);
  let error = $state('');
  let busy = $state(false);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const n = name.trim();
    if (!n) return (error = 'Etiket adı gerekli.');
    if ([...n].length > 50) return (error = 'Etiket adı en fazla 50 karakter olabilir.');
    const c = color.trim().toLowerCase();
    if (!isHexColor(c)) return (error = 'Renk #rrggbb biçiminde olmalı (ör. #2563eb).');
    busy = true;
    try {
      const res = tag ? await api.updateTag(tag.id, n, c) : await api.createTag(n, c);
      onsaved(res, !tag);
      open = false;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open title={tag ? 'Etiketi düzenle' : 'Yeni etiket'} width={460}>
  <form id="tagf" class="stack" onsubmit={submit} novalidate>
    <div class="field">
      <label for="tg-name">Etiket adı</label>
      <input id="tg-name" class="input" maxlength="50" bind:value={name} placeholder="Ör. ortam, müşteri, ekip" />
    </div>
    <div class="field">
      <span class="label" id="tg-color-l">Renk</span>
      <div class="swatches" role="radiogroup" aria-labelledby="tg-color-l">
        {#each TAG_COLORS as c (c)}
          <button
            type="button"
            role="radio"
            aria-checked={color.toLowerCase() === c}
            aria-label={c}
            class="sw"
            class:on={color.toLowerCase() === c}
            style="--sw:{c}"
            onclick={() => (color = c)}
          ></button>
        {/each}
      </div>
      <div class="custom">
        <input type="color" class="picker" aria-label="Özel renk seç" value={isHexColor(color) ? color : DEFAULT_TAG_COLOR} oninput={(e) => (color = e.currentTarget.value)} />
        <input id="tg-color" class="input mono hex" maxlength="7" bind:value={color} aria-label="Renk kodu" spellcheck="false" autocapitalize="none" />
      </div>
    </div>
    <div class="preview">
      <span class="muted small">Önizleme</span>
      <TagChip name={name.trim() || 'etiket'} color={isHexColor(color) ? color : DEFAULT_TAG_COLOR} value="değer" />
    </div>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>Vazgeç</button>
    <button type="submit" form="tagf" class="btn primary" disabled={busy}>
      {#if busy}<span class="spinner"></span>{/if}
      {tag ? 'Kaydet' : 'Etiketi ekle'}
    </button>
  {/snippet}
</Modal>

<style>
  .swatches {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .sw {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 2px solid transparent;
    background: var(--sw);
    cursor: pointer;
    padding: 0;
    box-shadow: 0 0 0 1px var(--border-strong);
  }
  .sw.on {
    border-color: var(--bg-elev);
    box-shadow: 0 0 0 2px var(--tag-swatch-ring);
  }
  .custom {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 4px;
  }
  .picker {
    width: 40px;
    height: 40px;
    padding: 0;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    cursor: pointer;
    flex-shrink: 0;
  }
  .picker::-webkit-color-swatch-wrapper {
    padding: 4px;
  }
  .picker::-webkit-color-swatch {
    border: none;
    border-radius: 5px;
  }
  .hex {
    max-width: 130px;
  }
  .preview {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .spacer {
    flex: 1;
  }
</style>
