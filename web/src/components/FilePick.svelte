<script lang="ts">
  // Tema uyumlu dosya seçici: gizli <input type=file> + düğme + seçilen dosyanın adı/boyutu.
  // Sürükle-bırak da desteklenir.
  import { fmtSize } from '../lib/format';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    file = $bindable(null),
    accept,
    id,
    label: labelProp,
    disabled = false,
  }: {
    file?: File | null;
    accept: string;
    id: string;
    label?: string;
    disabled?: boolean;
  } = $props();

  const label = $derived(labelProp ?? t('backup.filePick.pick'));

  let input: HTMLInputElement | undefined = $state();
  let over = $state(false);

  function pick(e: Event & { currentTarget: HTMLInputElement }) {
    file = e.currentTarget.files?.[0] ?? null;
  }

  function drop(e: DragEvent) {
    e.preventDefault();
    over = false;
    if (disabled) return;
    const f = e.dataTransfer?.files?.[0];
    if (f) file = f;
  }

  function clear() {
    file = null;
    if (input) input.value = '';
  }
</script>

<div
  class="fp"
  class:over
  class:has={!!file}
  role="group"
  aria-label={label}
  ondragover={(e) => {
    e.preventDefault();
    if (!disabled) over = true;
  }}
  ondragleave={() => (over = false)}
  ondrop={drop}
>
  <input bind:this={input} {id} class="sr" type="file" {accept} {disabled} onchange={pick} />
  <span class="fic"><Icon name={file ? 'file' : 'upload'} size={18} /></span>
  <span class="ft">
    {#if file}
      <span class="fn">{file.name}</span>
      <span class="muted small">{fmtSize(file.size)}</span>
    {:else}
      <span class="text-2">{t('backup.filePick.drop')}</span>
    {/if}
  </span>
  {#if file}
    <button type="button" class="btn ghost sm icon" aria-label={t('backup.filePick.remove')} onclick={clear} {disabled}><Icon name="x" size={15} /></button>
  {/if}
  <label for={id} class="btn sm" class:disabled>{file ? t('backup.filePick.change') : label}</label>
</div>

<style>
  .fp {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 10px 10px 12px;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    min-width: 0;
    transition:
      border-color 0.15s,
      background 0.15s;
  }
  .fp.over {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .fp.has {
    border-style: solid;
  }
  .fp:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
  .fic {
    display: inline-flex;
    color: var(--accent-text);
    flex-shrink: 0;
  }
  .ft {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    line-height: 1.3;
    font-size: 0.9rem;
  }
  .fn {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  label.btn {
    cursor: pointer;
  }
  label.btn.disabled {
    opacity: 0.55;
    pointer-events: none;
  }
</style>
