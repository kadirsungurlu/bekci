<script lang="ts" module>
  import { MASK } from '../lib/api';
  import type { CfgField } from '../lib/monitorTypes';

  const defText = (f: CfgField) => (f.def !== undefined ? String(f.def) : f.kind === 'bool' ? 'false' : '');

  /** Alan şu anki değerlere göre görünüyor mu. */
  export const fieldVisible = (f: CfgField, values: Record<string, string>) => !f.showIf || f.showIf(values);

  /** Alanların varsayılan değerleri (metin olarak). */
  export function fieldDefaults(fields: CfgField[]): Record<string, string> {
    const v: Record<string, string> = {};
    for (const f of fields) v[f.key] = defText(f);
    return v;
  }

  /** Kayıtlı ayardan form değerleri. Seçenek listesinde olmayan değer varsayılana döner. */
  export function fieldValues(fields: CfgField[], cfg: Record<string, unknown>): Record<string, string> {
    const v = fieldDefaults(fields);
    for (const f of fields) {
      const raw = cfg?.[f.key];
      if (raw === undefined || raw === null) continue;
      const s = String(raw);
      if (f.kind === 'select' && !(f.options ?? []).some((o) => o.v === s)) continue;
      v[f.key] = s;
    }
    return v;
  }

  /** Form değerlerinden sunucuya gidecek ayar nesnesi. Gizli (koşulu sağlanmayan) alanlar varsayılanla gider. */
  export function fieldConfig(fields: CfgField[], values: Record<string, string>): Record<string, unknown> {
    const cfg: Record<string, unknown> = {};
    for (const f of fields) {
      const raw = fieldVisible(f, values) ? (values[f.key] ?? '') : defText(f);
      if (f.kind === 'bool') cfg[f.key] = raw === 'true';
      else if (f.numeric || f.kind === 'number') cfg[f.key] = raw.trim() === '' ? (typeof f.def === 'number' ? f.def : 0) : Number(raw);
      else if (f.kind === 'textarea' || f.kind === 'secret' || f.kind === 'pem') cfg[f.key] = raw;
      else cfg[f.key] = raw.trim();
    }
    return cfg;
  }

  /** İlk hata ve hatalı alanın gelişmiş bölümde olup olmadığı; hata yoksa null. */
  export function fieldError(fields: CfgField[], values: Record<string, string>): { msg: string; advanced: boolean } | null {
    for (const f of fields) {
      if (!fieldVisible(f, values)) continue;
      const v = (values[f.key] ?? '').trim();
      const err = (msg: string) => ({ msg, advanced: !!f.advanced });
      if (f.required && !v) return err(`${f.label} gerekli.`);
      if (!v || v === MASK) continue;
      if (f.kind === 'url' && !/^https?:\/\/\S+$/i.test(v)) return err(`${f.label} geçerli bir http(s) adresi olmalı.`);
      if (f.pattern && !f.pattern.test(v)) return err(f.patternMsg ?? `${f.label} geçersiz.`);
      if (f.kind === 'number') {
        const n = Number(v);
        if (!Number.isFinite(n)) return err(`${f.label} bir sayı olmalı.`);
        if ((f.min !== undefined && n < f.min) || (f.max !== undefined && n > f.max)) return err(`${f.label} ${f.min}-${f.max} arasında olmalı.`);
      }
      if (f.kind === 'pem' && !/-----BEGIN [A-Z0-9 ]+-----/.test(v)) return err(`${f.label} PEM biçiminde olmalı (-----BEGIN … ile başlar).`);
    }
    return null;
  }
</script>

<script lang="ts">
  import Icon from './Icon.svelte';

  // Şemadan çizilen genel ayar alanları (kayıt defterindeki monitör tipleri ve HTTP ekleri için).
  let {
    fields,
    values = $bindable(),
    idPrefix = 'cf',
    part = 'all',
    onsuggest,
  }: {
    fields: CfgField[];
    values: Record<string, string>;
    idPrefix?: string;
    /** Hangi alanlar: ana bölüm, gelişmiş bölüm veya hepsi. */
    part?: 'basic' | 'advanced' | 'all';
    /** Ad önerilen alandan çıkıldığında değeriyle çağrılır. */
    onsuggest?: (value: string) => void;
  } = $props();

  const shown = $derived(
    fields.filter((f) => (part === 'all' || (part === 'advanced') === !!f.advanced) && fieldVisible(f, values)),
  );

  // Kayıtlı (maskeli) PEM anahtarını değiştirmek için açılan alanlar.
  let replacing = $state<Record<string, boolean>>({});

  function blur(f: CfgField) {
    if (f.suggestName && onsuggest) {
      const v = (values[f.key] ?? '').trim();
      if (v && v !== MASK) onsuggest(v);
    }
  }
</script>

<div class="grid-2">
  {#each shown as f (f.key)}
    {#if f.section}
      <div class="sec wide">
        <h4>{f.section}</h4>
        {#if f.sectionHelp}<p class="help">{f.sectionHelp}</p>{/if}
      </div>
    {/if}
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
      <div class="field" class:wide={f.wide || f.kind === 'textarea' || f.kind === 'pem'}>
        <label for="{idPrefix}-{f.key}">
          {f.label}
          {#if f.optional}<span class="muted">(isteğe bağlı)</span>{/if}
        </label>
        {#if f.kind === 'select'}
          <select id="{idPrefix}-{f.key}" class="input" bind:value={values[f.key]}>
            {#each f.options ?? [] as o (o.v)}<option value={o.v}>{o.l}</option>{/each}
          </select>
        {:else if (f.kind === 'pem' || f.kind === 'textarea') && f.secret && values[f.key] === MASK && !replacing[f.key]}
          <div class="kept">
            <span class="kept-t"
              ><span class="kept-ic"><Icon name="lock" size={14} /></span>
              {f.kind === 'pem' ? 'Kayıtlı anahtar korunuyor' : 'Kayıtlı değer korunuyor (gizli)'}</span
            >
            <button
              type="button"
              id="{idPrefix}-{f.key}"
              class="btn sm"
              onclick={() => {
                replacing[f.key] = true;
                values[f.key] = '';
              }}>Değiştir</button
            >
          </div>
        {:else if f.kind === 'textarea' || f.kind === 'pem'}
          <textarea
            id="{idPrefix}-{f.key}"
            class="input"
            class:pem={f.kind === 'pem'}
            rows={f.kind === 'pem' ? 5 : 3}
            autocapitalize="none"
            spellcheck="false"
            bind:value={values[f.key]}
            placeholder={f.placeholder}
          ></textarea>
          {#if f.secret && replacing[f.key]}
            <button
              type="button"
              class="linkbtn keep"
              onclick={() => {
                replacing[f.key] = false;
                values[f.key] = MASK;
              }}>{f.kind === 'pem' ? 'Vazgeç, kayıtlı anahtarı koru' : 'Vazgeç, kayıtlı değeri koru'}</button
            >
          {/if}
        {:else if f.kind === 'secret'}
          <input
            id="{idPrefix}-{f.key}"
            class="input"
            type="password"
            autocomplete="new-password"
            bind:value={values[f.key]}
            placeholder={f.placeholder}
            onblur={() => blur(f)}
          />
        {:else}
          <input
            id="{idPrefix}-{f.key}"
            class="input"
            class:mono={f.mono}
            type="text"
            inputmode={f.kind === 'number' ? 'numeric' : f.kind === 'url' ? 'url' : 'text'}
            autocapitalize="none"
            spellcheck="false"
            bind:value={values[f.key]}
            placeholder={f.placeholder}
            onblur={() => blur(f)}
          />
        {/if}
        {#if values[f.key] === MASK && f.kind === 'secret'}
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
  .sec {
    margin: 6px 0 -6px;
  }
  .sec:first-child {
    margin-top: 0;
  }
  .sec h4 {
    margin: 0;
    font-size: 0.9rem;
    font-weight: 700;
    color: var(--text);
  }
  .sec .help {
    display: block;
    margin: 2px 0 0;
  }
  .input.mono {
    font-family: var(--mono);
    font-size: 0.86rem;
  }
  textarea.pem {
    font-size: 0.78rem;
    line-height: 1.45;
    min-height: 110px;
  }
  .kept {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    min-height: 40px;
    padding: 4px 4px 4px 12px;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    color: var(--text-2);
    font-size: 0.88rem;
  }
  .kept-t {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .kept-ic {
    display: inline-flex;
    color: var(--accent-text);
  }
  .keep {
    align-self: flex-start;
    font-size: 0.82rem;
  }
</style>
