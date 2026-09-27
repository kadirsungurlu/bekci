<script lang="ts" module>
  import { MASK } from '../lib/api';
  import type { Field } from '../lib/notifyTypes';

  /** Alanların varsayılan değerleri (metin olarak). */
  export function fieldDefaults(fields: Field[]): Record<string, string> {
    const v: Record<string, string> = {};
    for (const f of fields) v[f.key] = f.def !== undefined ? String(f.def) : f.kind === 'bool' ? 'false' : '';
    return v;
  }

  /** Kayıtlı ayardan form değerleri. */
  export function fieldValues(fields: Field[], cfg: Record<string, unknown>): Record<string, string> {
    const v = fieldDefaults(fields);
    for (const f of fields) {
      const raw = cfg?.[f.key];
      if (raw !== undefined && raw !== null) v[f.key] = String(raw);
    }
    return v;
  }

  /** Form değerlerinden sunucuya gidecek ayar nesnesi. */
  export function fieldConfig(fields: Field[], values: Record<string, string>): Record<string, unknown> {
    const cfg: Record<string, unknown> = {};
    for (const f of fields) {
      const raw = values[f.key] ?? '';
      if (f.kind === 'bool') cfg[f.key] = raw === 'true';
      else if (f.numeric || f.kind === 'number') cfg[f.key] = raw.trim() === '' ? (typeof f.def === 'number' ? f.def : 0) : Number(raw);
      else if (f.kind === 'textarea' || f.kind === 'secret') cfg[f.key] = raw;
      else cfg[f.key] = raw.trim();
    }
    return cfg;
  }

  /** İlk hatanın metni; hata yoksa boş. */
  export function fieldError(fields: Field[], values: Record<string, string>): string {
    for (const f of fields) {
      const v = (values[f.key] ?? '').trim();
      if (f.required && !v) return `${f.label} gerekli.`;
      if (!v || v === MASK) continue;
      if (f.kind === 'url' && !/^https?:\/\/\S+$/i.test(v)) return `${f.label} geçerli bir http(s) adresi olmalı.`;
      if (f.pattern && !f.pattern.test(v)) return f.patternMsg ?? `${f.label} geçersiz.`;
      if (f.kind === 'number') {
        const n = Number(v);
        if (!Number.isFinite(n)) return `${f.label} bir sayı olmalı.`;
        if ((f.min !== undefined && n < f.min) || (f.max !== undefined && n > f.max)) return `${f.label} ${f.min}-${f.max} arasında olmalı.`;
      }
    }
    return '';
  }
</script>

<script lang="ts">
  // Şemadan çizilen genel ayar alanları (kayıt defterindeki yeni monitör tipleri için).
  let { fields, values = $bindable(), idPrefix = 'cf' }: { fields: Field[]; values: Record<string, string>; idPrefix?: string } = $props();
</script>

<div class="grid-2">
  {#each fields as f (f.key)}
    {#if f.kind === 'bool'}
      <label class="check wide">
        <input
          type="checkbox"
          checked={values[f.key] === 'true'}
          onchange={(e) => (values[f.key] = e.currentTarget.checked ? 'true' : 'false')}
        />
        <span>{f.label}{#if f.help}<small>{f.help}</small>{/if}</span>
      </label>
    {:else}
      <div class="field" class:wide={f.wide || f.kind === 'textarea'}>
        <label for="{idPrefix}-{f.key}">
          {f.label}
          {#if f.optional}<span class="muted">(isteğe bağlı)</span>{/if}
        </label>
        {#if f.kind === 'select'}
          <select id="{idPrefix}-{f.key}" class="input" bind:value={values[f.key]}>
            {#each f.options ?? [] as o (o.v)}<option value={o.v}>{o.l}</option>{/each}
          </select>
        {:else if f.kind === 'textarea'}
          <textarea id="{idPrefix}-{f.key}" class="input" rows="3" bind:value={values[f.key]} placeholder={f.placeholder}></textarea>
        {:else if f.kind === 'secret'}
          <input id="{idPrefix}-{f.key}" class="input" type="password" autocomplete="new-password" bind:value={values[f.key]} placeholder={f.placeholder} />
        {:else}
          <input
            id="{idPrefix}-{f.key}"
            class="input"
            type="text"
            inputmode={f.kind === 'number' ? 'numeric' : f.kind === 'url' ? 'url' : 'text'}
            autocapitalize="none"
            spellcheck="false"
            bind:value={values[f.key]}
            placeholder={f.placeholder}
          />
        {/if}
        {#if values[f.key] === MASK}
          <span class="help">Kayıtlı değer korunur; değiştirmek için yenisini yazın.</span>
        {:else if f.help}
          <span class="help">{f.help}</span>
        {/if}
      </div>
    {/if}
  {/each}
</div>

<style>
  .wide {
    grid-column: 1 / -1;
  }
</style>
