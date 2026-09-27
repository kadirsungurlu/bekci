<script lang="ts">
  // Kurtarma kodlarının tek seferlik gösterimi: kopyala, indir, "kaydettim" onayı.
  import { copyText, toast } from '../lib/ui.svelte';
  import { session } from '../lib/session.svelte';
  import Icon from './Icon.svelte';

  let { codes, saved = $bindable(false) }: { codes: string[]; saved?: boolean } = $props();

  const text = $derived(
    `Uptime kurtarma kodları (${session.user?.username ?? ''})\n` +
      `Her kod yalnızca bir kez kullanılabilir.\n\n${codes.join('\n')}\n`,
  );
  const href = $derived('data:text/plain;charset=utf-8,' + encodeURIComponent(text));

  async function copy() {
    if (await copyText(codes.join('\n'))) toast.success('Kodlar panoya kopyalandı');
    else toast.error('Kopyalanamadı; kodları elle seçip kopyalayın');
  }
</script>

<p class="text-2 small intro">
  <b>Kurtarma kodları</b> — telefonunuzu kaybederseniz bu kodlarla giriş yapabilirsiniz. Her kod bir kez kullanılabilir. Bunlar
  bir daha gösterilmeyecek.
</p>
<ol class="codes">
  {#each codes as c (c)}<li><code>{c}</code></li>{/each}
</ol>
<div class="row btns">
  <a class="btn sm" {href} download="uptime-kurtarma-kodlari.txt"><Icon name="download" size={15} /> İndir</a>
  <button type="button" class="btn sm" onclick={copy}><Icon name="copy" size={15} /> Kopyala</button>
</div>
<label class="check ack">
  <input type="checkbox" bind:checked={saved} />
  <span>Kodları güvenli bir yere kaydettim</span>
</label>

<style>
  .intro {
    margin: 0 0 12px;
  }
  .codes {
    list-style: none;
    margin: 0;
    padding: 14px;
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px 16px;
    background: var(--input);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
  }
  .codes code {
    font-size: 0.98rem;
    letter-spacing: 0.04em;
    color: var(--text);
  }
  .btns {
    margin: 12px 0 14px;
  }
  .ack {
    font-weight: 600;
  }
  @media (max-width: 380px) {
    .codes {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
