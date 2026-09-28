<script lang="ts">
  // Monitör rozeti oluşturucu: README veya web sitesine eklenecek SVG rozet adresi.
  import CopyButton from './CopyButton.svelte';

  let { id, https = false }: { id: number; https?: boolean } = $props();

  type Kind = 'status' | 'uptime' | 'ping' | 'cert-exp';
  const KINDS: { v: Kind; l: string }[] = [
    { v: 'status', l: 'Durum' },
    { v: 'uptime', l: 'Uptime' },
    { v: 'ping', l: 'Yanıt süresi' },
    { v: 'cert-exp', l: 'Sertifika' },
  ];
  const DURATIONS = [
    { v: '24h', l: '24 saat' },
    { v: '7d', l: '7 gün' },
    { v: '30d', l: '30 gün' },
    { v: '90d', l: '90 gün' },
  ];
  const STYLES = [
    { v: 'flat', l: 'Düz' },
    { v: 'flat-square', l: 'Düz köşeli' },
    { v: 'for-the-badge', l: 'Büyük' },
  ];

  let kind = $state<Kind>('status');
  let duration = $state('30d');
  let style = $state('flat');
  let label = $state('');
  let labelColor = $state('');
  let color = $state('');
  let upColor = $state('');
  let downColor = $state('');

  const COLOR_RE = /^(#?[0-9a-fA-F]{3}|#?[0-9a-fA-F]{6}|[a-zA-Z]{3,20})$/;
  const shownColors = $derived(kind === 'status' ? [labelColor, upColor, downColor] : kind === 'ping' ? [labelColor, color] : [labelColor]);
  const bad = $derived(shownColors.some((c) => c.trim() && !COLOR_RE.test(c.trim())));

  const query = $derived.by(() => {
    const q = new URLSearchParams();
    if ((kind === 'uptime' || kind === 'ping') && duration !== '24h') q.set('duration', duration);
    if (style !== 'flat') q.set('style', style);
    if (label.trim()) q.set('label', label.trim());
    const add = (k: string, v: string) => {
      const t = v.trim();
      if (t && COLOR_RE.test(t)) q.set(k, t.replace(/^#/, ''));
    };
    add('labelColor', labelColor);
    if (kind === 'status') {
      add('upColor', upColor);
      add('downColor', downColor);
    } else if (kind === 'ping') add('color', color);
    // Uptime ve sertifika rozetlerinin değer rengi eşiğe göre otomatik seçilir.
    const s = q.toString();
    return s ? `?${s}` : '';
  });

  const path = $derived(`/api/badge/${id}/${kind}.svg${query}`);
  const url = $derived(`${location.origin}${path}`);
  const alt = $derived(label.trim() || KINDS.find((k) => k.v === kind)!.l.toLocaleLowerCase('tr'));
  const md = $derived(`![${alt}](${url})`);
  const html = $derived(`<img src="${url}" alt="${alt.replace(/"/g, '&quot;')}">`);

  // Önizleme yazarken her tuşta istek atmasın.
  let preview = $state('');
  let failed = $state(false);
  $effect(() => {
    const p = path;
    const t = setTimeout(() => {
      failed = false;
      preview = p;
    }, 350);
    return () => clearTimeout(t);
  });

</script>

<div class="bb">
  <p class="help nomargin">
    Rozet yalnızca monitör yayında ve şifresiz bir durum sayfasındaysa herkese açıktır; aksi halde yalnızca oturum açmış kullanıcılar ve
    API anahtarları görebilir. Rozetler 60 saniye önbelleğe alınır. Monitörün adı rozette gösterilmez.
  </p>
  <div class="grid">
    <div class="field">
      <label for="bb-kind">Tür</label>
      <select id="bb-kind" class="input" bind:value={kind}>
        {#each KINDS as k (k.v)}<option value={k.v}>{k.l}{k.v === 'cert-exp' && !https ? ' (yalnızca HTTPS)' : ''}</option>{/each}
      </select>
    </div>
    {#if kind === 'uptime' || kind === 'ping'}
      <div class="field">
        <label for="bb-dur">Süre</label>
        <select id="bb-dur" class="input" bind:value={duration}>
          {#each DURATIONS as d (d.v)}<option value={d.v}>{d.l}</option>{/each}
        </select>
      </div>
    {/if}
    <div class="field">
      <label for="bb-style">Stil</label>
      <select id="bb-style" class="input" bind:value={style}>
        {#each STYLES as s (s.v)}<option value={s.v}>{s.l}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="bb-label">Etiket <span class="muted">(boşsa varsayılan)</span></label>
      <input id="bb-label" class="input" maxlength="64" bind:value={label} placeholder={alt} />
    </div>
  </div>
  <fieldset>
    <legend class="label">Renkler <span class="muted">(#rrggbb veya renk adı, ör. green)</span></legend>
    <div class="grid">
      <div class="field">
        <label for="bb-lc">Etiket rengi</label>
        <input id="bb-lc" class="input mono" bind:value={labelColor} placeholder="#555" autocapitalize="none" spellcheck="false" />
      </div>
      {#if kind === 'status'}
        <div class="field">
          <label for="bb-uc">Çalışıyor rengi</label>
          <input id="bb-uc" class="input mono" bind:value={upColor} placeholder="#4c1" autocapitalize="none" spellcheck="false" />
        </div>
        <div class="field">
          <label for="bb-dc">Çalışmıyor rengi</label>
          <input id="bb-dc" class="input mono" bind:value={downColor} placeholder="#e05d44" autocapitalize="none" spellcheck="false" />
        </div>
      {:else if kind === 'ping'}
        <div class="field">
          <label for="bb-c">Değer rengi</label>
          <input id="bb-c" class="input mono" bind:value={color} placeholder="otomatik" autocapitalize="none" spellcheck="false" />
        </div>
      {/if}
    </div>
    {#if kind === 'uptime' || kind === 'cert-exp'}
      <div class="help">Değer rengi orana / kalan güne göre otomatik seçilir (yeşil, turuncu, kırmızı).</div>
    {/if}
    {#if bad}<div class="help c-down">Geçersiz renk yok sayıldı. Örnek: #2dd4bf, 0a0, brightgreen.</div>{/if}
  </fieldset>

  <div class="prev">
    <span class="label">Önizleme</span>
    <div class="prev-box">
      {#if preview && !failed}
        <img src={preview} alt="Rozet önizlemesi" onerror={() => (failed = true)} />
      {:else if failed}
        <span class="muted small">Rozet yüklenemedi.</span>
      {/if}
    </div>
  </div>

  <div class="out">
    <div class="label">Markdown</div>
    <div class="copybox">
      <code>{md}</code>
      <CopyButton text={md} />
    </div>
    <div class="label">HTML</div>
    <div class="copybox">
      <code>{html}</code>
      <CopyButton text={html} />
    </div>
  </div>
</div>

<style>
  .bb {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .nomargin {
    margin: 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
    gap: 12px;
  }
  fieldset {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  legend {
    padding: 0;
    margin-bottom: 8px;
  }
  .prev {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .prev-box {
    flex: 1;
    min-height: 48px;
    display: flex;
    align-items: center;
    padding: 10px 14px;
    border-radius: var(--radius-sm);
    border: 1px dashed var(--border-strong);
    background: var(--input);
  }
  .prev-box img {
    max-width: 100%;
  }
  .out {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .out .label {
    margin-top: 4px;
  }
  .out code {
    font-size: 0.8rem;
  }
</style>
