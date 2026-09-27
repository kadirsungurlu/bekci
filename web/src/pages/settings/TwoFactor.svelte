<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, errorMessage, type TwoFactorSetup, type TwoFactorStatus } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/ui.svelte';
  import Modal from '../../components/Modal.svelte';
  import RecoveryCodes from '../../components/RecoveryCodes.svelte';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';

  let status = $state<TwoFactorStatus | null>(null);
  let loadError = $state('');

  async function load() {
    try {
      status = await api.twoFactor();
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    }
  }
  onMount(load);

  // Kurulum sihirbazı ---------------------------------------------------------------
  type Step = 1 | 2 | 3 | 4;
  let wizOpen = $state(false);
  let step = $state<Step>(1);
  let password = $state('');
  let setup = $state<TwoFactorSetup | null>(null);
  let code = $state('');
  let codes = $state<string[]>([]);
  let saved = $state(false);
  let busy = $state(false);
  let error = $state('');
  let codeEl: HTMLInputElement | undefined = $state();

  function openWizard() {
    step = 1;
    password = code = error = '';
    setup = null;
    codes = [];
    saved = false;
    wizOpen = true;
  }

  async function doSetup(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (!password) return (error = 'Şifrenizi girin.');
    busy = true;
    try {
      setup = await api.twoFactorSetup(password);
      password = '';
      step = 2;
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function toCode() {
    step = 3;
    error = '';
    await tick();
    codeEl?.focus();
  }

  async function doEnable(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const c = code.replace(/\s+/g, '');
    if (!/^\d{6}$/.test(c)) return (error = 'Uygulamadaki 6 haneli kodu girin.');
    busy = true;
    try {
      const res = await api.twoFactorEnable(c);
      codes = res.recovery_codes;
      step = 4;
      if (session.user) session.set({ ...session.user, two_factor_enabled: true });
      load();
    } catch (err) {
      error = errorMessage(err);
      code = '';
    } finally {
      busy = false;
    }
  }

  function finish() {
    wizOpen = false;
    toast.success('İki adımlı doğrulama açıldı. Diğer cihazlardaki oturumlar kapatıldı.');
  }


  const secretGroups = $derived(setup ? setup.secret.replace(/(.{4})/g, '$1 ').trim() : '');

  // Kapatma ve kurtarma kodu yenileme (şifre + kod ister) ------------------------------
  let actOpen = $state(false);
  let act = $state<'disable' | 'recovery'>('disable');
  let actPassword = $state('');
  let actCode = $state('');
  let newCodes = $state<string[]>([]);
  let newSaved = $state(false);

  function openAct(a: 'disable' | 'recovery') {
    act = a;
    actPassword = actCode = error = '';
    newCodes = [];
    newSaved = false;
    actOpen = true;
  }

  async function doAct(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    if (!actPassword) return (error = 'Şifrenizi girin.');
    if (!actCode.trim()) return (error = 'Doğrulama kodunu girin.');
    busy = true;
    try {
      if (act === 'disable') {
        await api.twoFactorDisable(actPassword, actCode.trim());
        if (session.user) session.set({ ...session.user, two_factor_enabled: false });
        actOpen = false;
        toast.success('İki adımlı doğrulama kapatıldı');
      } else {
        const res = await api.twoFactorRecovery(actPassword, actCode.trim());
        newCodes = res.recovery_codes;
      }
      load();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  const STEPS = ['Şifre', 'QR kodu', 'Doğrulama', 'Kurtarma kodları'];
</script>

<div class="card tf">
  <div class="head">
    <span class="ic" class:on={status?.enabled}><Icon name={status?.enabled ? 'shield-check' : 'shield'} size={20} /></span>
    <div class="t">
      <h2 class="card-title">İki adımlı doğrulama</h2>
      {#if status}
        {#if status.enabled}
          <div class="state on">Açık — kalan kurtarma kodu: {status.recovery_codes_left}</div>
        {:else}
          <div class="state">Kapalı</div>
        {/if}
      {:else if loadError}
        <div class="state c-down">{loadError}</div>
      {/if}
    </div>
  </div>
  <p class="text-2 small desc">
    Girişte şifrenize ek olarak telefonunuzdaki doğrulama uygulamasının ürettiği kodu ister. Şifreniz ele geçse bile hesabınız korunur.
  </p>
  {#if status?.enabled && status.recovery_codes_left <= 3}
    <div class="alert warning small">Kurtarma kodlarınız azaldı. Yenilerini oluşturup güvenli bir yere kaydedin.</div>
  {/if}
  {#if !status}
    {#if loadError}
      <button class="btn sm" onclick={load}>Tekrar dene</button>
    {:else}
      <div class="skeleton" style="height:38px"></div>
    {/if}
  {:else if status.enabled}
    <div class="row">
      <button class="btn" onclick={() => openAct('recovery')}><Icon name="refresh" size={15} /> Kurtarma kodlarını yenile</button>
      <button class="btn danger" onclick={() => openAct('disable')}>Kapat</button>
    </div>
  {:else}
    <div class="row">
      <button class="btn primary" onclick={openWizard}><Icon name="shield-check" size={16} /> Etkinleştir</button>
    </div>
  {/if}
</div>

<!-- Kurtarma kodları adımı "Kodları kaydettim" onaylanmadan kapatılamaz (Esc, arka plan ve × devre dışı). -->
<Modal bind:open={wizOpen} title="İki adımlı doğrulama" width={520} dismissable={step !== 4}>
  <ol class="steps" aria-label="Adımlar">
    {#each STEPS as s, i (s)}
      <li class:done={step > i + 1} class:cur={step === i + 1} aria-current={step === i + 1 ? 'step' : undefined}>
        <span class="n">{#if step > i + 1}<Icon name="check" size={12} stroke={3} />{:else}{i + 1}{/if}</span>
        <span class="l">{s}</span>
      </li>
    {/each}
  </ol>

  {#if step === 1}
    <form id="tf1" class="stack" onsubmit={doSetup} novalidate>
      <p class="text-2 nomargin">Devam etmek için şifrenizi girin.</p>
      <input type="text" name="username" autocomplete="username" value={session.user?.username ?? ''} hidden readonly />
      <div class="field">
        <label for="tf-pw">Şifre</label>
        <input id="tf-pw" class="input" type="password" autocomplete="current-password" bind:value={password} />
      </div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {:else if step === 2 && setup}
    <div class="stack">
      <p class="text-2 nomargin">Google Authenticator, Authy, 1Password gibi bir uygulamayla QR kodunu tarayın.</p>
      {#if setup.qr_png}
        <div class="qr"><img src={setup.qr_png} alt="Doğrulama uygulaması için QR kodu" width="200" height="200" /></div>
      {/if}
      <div>
        <div class="help">QR tarayamıyor musunuz? Bu anahtarı elle girin:</div>
        <div class="copybox secret">
          <code>{secretGroups}</code>
          <CopyButton text={setup.secret} />
        </div>
      </div>
    </div>
  {:else if step === 3}
    <form id="tf3" class="stack" onsubmit={doEnable} novalidate>
      <p class="text-2 nomargin">Uygulamadaki 6 haneli kodu girin.</p>
      <div class="field">
        <label for="tf-code">Doğrulama kodu</label>
        <input
          id="tf-code"
          class="input code"
          bind:this={codeEl}
          bind:value={code}
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="7"
          placeholder="123456"
        />
      </div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {:else if step === 4}
    <RecoveryCodes {codes} bind:saved />
  {/if}

  {#snippet footer()}
    <div class="spacer"></div>
    {#if step === 1}
      <button type="button" class="btn" onclick={() => (wizOpen = false)}>Vazgeç</button>
      <button type="submit" form="tf1" class="btn primary" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if} Devam
      </button>
    {:else if step === 2}
      <button type="button" class="btn primary" onclick={toCode}>QR kodunu taradım</button>
    {:else if step === 3}
      <button type="button" class="btn" onclick={() => (step = 2)}>Geri</button>
      <button type="submit" form="tf3" class="btn primary" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if} Doğrula ve aç
      </button>
    {:else}
      <button type="button" class="btn primary" disabled={!saved} onclick={finish}>Bitti</button>
    {/if}
  {/snippet}
</Modal>

<Modal
  bind:open={actOpen}
  title={act === 'disable' ? 'İki adımlı doğrulamayı kapat' : 'Kurtarma kodlarını yenile'}
  width={480}
  dismissable={!newCodes.length}
>
  {#if newCodes.length}
    <RecoveryCodes codes={newCodes} bind:saved={newSaved} />
  {:else}
    <form id="tfa" class="stack" onsubmit={doAct} novalidate>
      <p class="text-2 nomargin">
        {act === 'disable'
          ? 'Kapatınca girişte yalnızca şifreniz istenir.'
          : 'Eski kurtarma kodlarınız geçersiz olur ve 10 yeni kod oluşturulur.'}
        Onaylamak için şifrenizi ve uygulamanızdaki kodu girin.
      </p>
      <input type="text" name="username" autocomplete="username" value={session.user?.username ?? ''} hidden readonly />
      <div class="field">
        <label for="tfa-pw">Şifre</label>
        <input id="tfa-pw" class="input" type="password" autocomplete="current-password" bind:value={actPassword} />
      </div>
      <div class="field">
        <label for="tfa-code">Doğrulama kodu</label>
        <input
          id="tfa-code"
          class="input code"
          bind:value={actCode}
          autocomplete="one-time-code"
          autocapitalize="none"
          autocorrect="off"
          spellcheck="false"
          maxlength="32"
          placeholder="123456"
        />
        <span class="help">6 haneli kod veya bir kurtarma kodu.</span>
      </div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {/if}
  {#snippet footer()}
    <div class="spacer"></div>
    {#if newCodes.length}
      <button type="button" class="btn primary" disabled={!newSaved} onclick={() => (actOpen = false)}>Bitti</button>
    {:else}
      <button type="button" class="btn" onclick={() => (actOpen = false)}>Vazgeç</button>
      <button type="submit" form="tfa" class="btn {act === 'disable' ? 'danger solid' : 'primary'}" disabled={busy}>
        {#if busy}<span class="spinner"></span>{/if}
        {act === 'disable' ? 'Kapat' : 'Yeni kodlar oluştur'}
      </button>
    {/if}
  {/snippet}
</Modal>

<style>
  .tf {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .card-title {
    margin: 0;
  }
  .ic {
    width: 40px;
    height: 40px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--card-2);
    color: var(--muted);
    flex-shrink: 0;
  }
  .ic.on {
    background: var(--up-soft);
    color: var(--up);
  }
  .state {
    font-size: 0.86rem;
    color: var(--muted);
    font-weight: 600;
  }
  .state.on {
    color: var(--up);
  }
  .desc {
    margin: 0;
  }
  .nomargin {
    margin: 0;
  }
  .spacer {
    flex: 1;
  }
  .steps {
    list-style: none;
    margin: 0 0 18px;
    padding: 0;
    display: flex;
    gap: 6px;
  }
  .steps li {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    font-size: 0.72rem;
    color: var(--muted);
    text-align: center;
    position: relative;
  }
  .steps li::before {
    content: '';
    position: absolute;
    top: 12px;
    left: calc(-50% + 16px);
    right: calc(50% + 16px);
    height: 2px;
    background: var(--border-strong);
  }
  .steps li:first-child::before {
    display: none;
  }
  .steps li.done::before,
  .steps li.cur::before {
    background: var(--accent);
  }
  .n {
    width: 24px;
    height: 24px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-weight: 700;
    font-size: 0.78rem;
    background: var(--card-2);
    border: 1px solid var(--border-strong);
    color: var(--text-2);
  }
  .cur .n,
  .done .n {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-contrast);
  }
  .cur .l {
    color: var(--text);
    font-weight: 600;
  }
  .qr {
    display: flex;
    justify-content: center;
  }
  .qr img {
    /* QR okunabilsin diye her temada beyaz zemin. */
    background: var(--qr-bg);
    padding: 10px;
    border-radius: 12px;
    width: 200px;
    height: 200px;
    image-rendering: pixelated;
  }
  .secret {
    margin-top: 6px;
  }
  .secret code {
    letter-spacing: 0.06em;
  }
  .code {
    font-family: var(--mono);
    font-size: 1.1rem;
    letter-spacing: 0.12em;
  }
  @media (max-width: 420px) {
    .steps .l {
      display: none;
    }
  }
</style>
