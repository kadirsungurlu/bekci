<script lang="ts">
  // Tarih/saat girişi. Tarayıcının yerel biçimi sayfa dilinden bağımsızdır (TR
  // sayfada AA/GG/YYYY ve 12 saat çıkabilir); Türkçede metin girişiyle sabit
  // GG.AA.YYYY ve 24 saat gösterilir, diğer dillerde yerel giriş kullanılır.
  // Değer her durumda yerel giriş biçimindedir: YYYY-MM-DD, HH:mm, YYYY-MM-DDTHH:mm.
  import { i18n } from '../lib/i18n';

  let {
    id,
    type,
    value = $bindable(''),
    min,
    onchange,
  }: {
    id?: string;
    type: 'date' | 'time' | 'datetime-local';
    value?: string;
    min?: string;
    onchange?: () => void;
  } = $props();

  const tr = $derived(i18n.locale === 'tr');

  function show(v: string): string {
    const m = v.match(/^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}:\d{2}))?$|^(\d{2}:\d{2})$/);
    if (!m) return '';
    if (m[5]) return m[5];
    const d = `${m[3]}.${m[2]}.${m[1]}`;
    return m[4] ? `${d} ${m[4]}` : d;
  }

  function parse(s: string): string | null {
    s = s.trim();
    if (s === '') return '';
    const dm = '(\\d{1,2})[./-](\\d{1,2})[./-](\\d{4})';
    const tm = '(\\d{1,2})[:.](\\d{2})';
    const pad = (n: string) => n.padStart(2, '0');
    const okDate = (y: number, mo: number, d: number) => {
      const dt = new Date(y, mo - 1, d);
      return dt.getFullYear() === y && dt.getMonth() === mo - 1 && dt.getDate() === d;
    };
    const okTime = (h: number, mi: number) => h < 24 && mi < 60;
    let m: RegExpMatchArray | null;
    if (type === 'time') {
      m = s.match(new RegExp(`^${tm}$`));
      return m && okTime(+m[1], +m[2]) ? `${pad(m[1])}:${m[2]}` : null;
    }
    if (type === 'date') {
      m = s.match(new RegExp(`^${dm}$`));
      return m && okDate(+m[3], +m[2], +m[1]) ? `${m[3]}-${pad(m[2])}-${pad(m[1])}` : null;
    }
    m = s.match(new RegExp(`^${dm}\\s+${tm}$`));
    return m && okDate(+m[3], +m[2], +m[1]) && okTime(+m[4], +m[5]) ? `${m[3]}-${pad(m[2])}-${pad(m[1])}T${pad(m[4])}:${m[5]}` : null;
  }

  let text = $state(show(value));
  let bad = $state(false);
  // Dışarıdan değer değişince (yükleme, sıfırlama) metni yeniden çiz; yalnızca
  // kullanıcının kendi yazdığı değer için metne dokunulmaz (geçersiz metin korunur).
  let emitted = value;
  $effect(() => {
    const v = value;
    if (v !== emitted) {
      emitted = v;
      text = show(v);
      bad = false;
    }
  });

  function onInput() {
    const p = parse(text);
    bad = p === null;
    emitted = value = p ?? '';
    onchange?.();
  }
  function onBlur() {
    const p = parse(text);
    if (p !== null) text = show(p);
  }

  const placeholder = $derived(type === 'time' ? 'SS:DD' : type === 'date' ? 'GG.AA.YYYY' : 'GG.AA.YYYY SS:DD');
</script>

{#if tr}
  <input
    {id}
    class="input"
    class:invalid={bad}
    type="text"
    inputmode="numeric"
    autocomplete="off"
    {placeholder}
    aria-invalid={bad}
    maxlength={16}
    bind:value={text}
    oninput={onInput}
    onblur={onBlur}
  />
{:else}
  <input {id} class="input" {type} {min} bind:value {onchange} />
{/if}
