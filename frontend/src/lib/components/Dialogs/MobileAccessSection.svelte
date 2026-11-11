<script lang="ts">
  /**
   * Reaching the app from a phone: the switch, the port, pairing and the devices
   * let in. The server listens on this computer only; Tailscale carries it to the
   * phone. Whoever pairs types into the terminals, which is why the warning stays.
   */
  import { onDestroy, onMount } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { describeBackendError } from '../../utils/backendError';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import MobileSetupGuide from './MobileSetupGuide.svelte';

  type Device = { id: string; name: string; createdAt: string; lastSeen: string };
  type Setup = { step: string; installed: boolean; loggedIn: boolean; serving: boolean; tailnetName: string; platform: string; commands: Record<string, string> };
  const STEPS = ['install', 'login', 'enable', 'serve', 'phone'];
  type Pairing = { code: string; url: string; localUrl: string; qrPng: string; expiresAt: string };

  let running = false;
  let port = 9760;
  let busy = false;
  let error = '';
  let pairing: Pairing | null = null;
  let devices: Device[] = [];
  let revokeTarget: Device | null = null;
  let showRevoke = false;
  let setup: Setup | null = null;
  let setupError = '';

  let showGuide = false;
  // Commands run in a terminal of the user's own, so the list catches up by itself,
  // until there is nothing left to catch up on.
  const RECHECK_MS = 5000;
  let recheckTimer: ReturnType<typeof setInterval> | undefined;
  $: {
    clearInterval(recheckTimer);
    recheckTimer = setup && setup.step !== 'phone' ? setInterval(() => void loadSetup(), RECHECK_MS) : undefined;
  }
  onDestroy(() => clearInterval(recheckTimer));

  async function loadSetup() {
    try {
      setup = (await App.GetMobileSetup()) as Setup;
      setupError = '';
    } catch (e) {
      setupError = describeBackendError(e);
    }
  }

  function stepState(step: string): string {
    if (!setup) return 'pending';
    const at = STEPS.indexOf(setup.step);
    const index = STEPS.indexOf(step);
    return index < at ? 'done' : index === at ? 'current' : 'pending';
  }

  onMount(() => { void load(); void loadSetup(); });

  async function load() {
    try {
      const status = await App.GetMobileStatus();
      running = !!status?.running;
      port = status?.port || 9760;
      devices = ((await App.ListMobileDevices()) || []) as Device[];
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  async function apply(enabled: boolean) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const status = await App.SetMobileAccess(enabled, Number(port));
      running = !!status?.running;
      if (!running) pairing = null;
      void loadSetup();
    } catch (e) {
      error = describeBackendError(e);
      // What is true now: a port that was refused leaves the old server running.
      try {
        const status = await App.GetMobileStatus();
        running = !!status?.running;
        if (running) port = status.port;
      } catch {
        running = false;
      }
      if (!running) pairing = null;
    } finally {
      busy = false;
    }
  }

  async function pair() {
    error = '';
    try {
      pairing = (await App.CreateMobilePairing()) as Pairing;
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  async function revoke() {
    const target = revokeTarget;
    revokeTarget = null;
    if (!target) return;
    try {
      await App.RevokeMobileDevice(target.id);
      devices = ((await App.ListMobileDevices()) || []) as Device[];
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  const when = (iso: string) => new Date(iso).toLocaleString();
  const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  async function enableFromSetup() {
    setupError = '';
    await apply(true);
    await loadSetup();
  }
</script>

<div class="mobile-access" data-section="mobile">
  <p class="warning" data-mobile-warning>{$t('mobile.warning')}</p>

  {#if setup}
    {#if setup.step === 'phone'}
      <p class="ready" data-setup-done>✓ {$t('mobile.setup.ready', { address: `https://${setup.tailnetName}` })}</p>
    {:else}
      <ol class="setup">
        {#each STEPS as step (step)}
          <li class={stepState(step)} data-setup-step={step}>
            <span class="mark">{stepState(step) === 'done' ? '✓' : stepState(step) === 'current' ? '●' : '○'}</span>
            <span class="step-body">
              <span>{$t(`mobile.setup.${step}`)}</span>
              {#if stepState(step) === 'current' && step !== 'phone'}
                <span class="step-actions">
                  {#if step === 'enable'}
                    <button class="btn-primary small" data-setup-enable on:click={enableFromSetup}>{$t('mobile.setup.turnOn')}</button>
                  {:else}
                    <button class="btn-secondary small" data-setup-guide on:click={() => (showGuide = true)}>{$t('mobile.setup.guide')}</button>
                  {/if}
                </span>
              {/if}
            </span>
          </li>
        {/each}
      </ol>
      <button class="link" data-setup-recheck on:click={loadSetup}>{$t('mobile.setup.recheck')}</button>
    {/if}
    {#if setupError}<p class="error" role="alert">{setupError}</p>{/if}
  {/if}

  <div class="row">
    <span class="info">
      <span class="label">{$t('mobile.enable')}</span>
      <span class="desc">{$t('mobile.enableDesc')}</span>
    </span>
    <button class="toggle-btn" class:active={running} data-mobile-toggle disabled={busy} aria-label={$t('mobile.enable')} on:click={() => apply(!running)}>
      <span class="toggle-track"><span class="toggle-thumb"></span></span>
    </button>
  </div>

  <div class="row">
    <span class="info"><span class="label">{$t('mobile.port')}</span></span>
    <input
      class="port"
      type="number"
      min="1024"
      max="65535"
      data-mobile-port
      bind:value={port}
      on:change={() => { if (running) void apply(true); }}
    />
  </div>

  {#if error}<p class="error" data-mobile-error role="alert">{error}</p>{/if}

  {#if running}
    <p class="desc">{$t('mobile.tailscale')}</p>
    <code class="command" data-mobile-tailscale>tailscale serve --bg http://127.0.0.1:{port}</code>

    <button class="btn-secondary" data-mobile-pair on:click={pair}>{$t('mobile.pair')}</button>
    {#if pairing}
      <div class="qr" data-mobile-qr>
        <img src={`data:image/png;base64,${pairing.qrPng}`} alt={$t('mobile.pair')} width="200" height="200" />
        <span class="desc">{$t('mobile.pairHint', { time: clock(pairing.expiresAt) })}</span>
        <code class="command">{pairing.url}</code>
      </div>
    {/if}
  {/if}

  <h4>{$t('mobile.devices')}</h4>
  {#each devices as device (device.id)}
    <div class="device" data-mobile-device={device.name}>
      <span class="info">
        <span class="label">{device.name}</span>
        <span class="desc">{$t('mobile.lastSeen', { time: when(device.lastSeen) })}</span>
      </span>
      <button class="btn-secondary small" data-action="revoke" on:click={() => { revokeTarget = device; showRevoke = true; }}>
        {$t('mobile.revoke')}
      </button>
    </div>
  {:else}
    <p class="desc">{$t('mobile.noDevices')}</p>
  {/each}
</div>

{#if setup}
  <MobileSetupGuide bind:show={showGuide} current={setup.step} platform={setup.platform} commands={setup.commands} />
{/if}

<ConfirmDialog
  bind:show={showRevoke}
  title={$t('mobile.revoke')}
  message={$t('mobile.revokeConfirm', { name: revokeTarget?.name ?? '' })}
  confirmText={$t('mobile.revoke')}
  variant="danger"
  on:confirm={revoke}
  on:cancel={() => { revokeTarget = null; }}
/>

<style>
  .mobile-access { display: flex; flex-direction: column; gap: 10px; }
  .row, .device {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    padding: 8px 0;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }
  .info { display: flex; flex-direction: column; gap: 2px; flex: 1; min-width: 0; }
  .label { font-size: 14px; font-weight: 500; color: #e4e4e7; }
  .desc { font-size: 13px; color: #6b7280; margin: 0; }
  .warning {
    margin: 0;
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 13px;
    color: #fbbf24;
    background: rgba(251, 191, 36, 0.08);
    border: 1px solid rgba(251, 191, 36, 0.25);
  }
  .error { margin: 0; color: #fb7185; font-size: 13px; }
  .command {
    display: block;
    padding: 6px 8px;
    border-radius: 6px;
    background: rgba(0, 0, 0, 0.3);
    font-family: var(--font-mono, monospace);
    font-size: 12px;
    color: #e4e4e7;
    user-select: all;
    overflow-wrap: anywhere;
  }
  .port {
    width: 110px;
    padding: 6px 9px;
    font-size: 13px;
    color: #e4e4e7;
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
  }
  .setup { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .setup li { display: flex; gap: 8px; font-size: 13px; color: #6b7280; }
  .setup li.done { color: #a1a1aa; }
  .setup li.current { color: #e4e4e7; }
  .setup .mark { width: 14px; flex-shrink: 0; text-align: center; }
  .setup li.done .mark { color: #4ade80; }
  .setup li.current .mark { color: rgb(var(--accent-rgb)); }
  .step-body { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .step-actions { display: flex; align-items: center; gap: 10px; }
  .link { background: none; border: none; padding: 0; color: #9ca3af; font-size: 12px; text-decoration: underline; cursor: pointer; align-self: flex-start; }
  .ready { margin: 0; color: #4ade80; font-size: 13px; overflow-wrap: anywhere; }
  .qr { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; }
  .qr img { background: #fff; padding: 8px; border-radius: 8px; image-rendering: pixelated; }
  h4 { margin: 8px 0 0; font-size: 13px; color: #e4e4e7; }
  .small { padding: 3px 8px; font-size: 12px; }
  .mobile-access > .btn-secondary { align-self: flex-start; }
  .toggle-btn { background: none; border: none; cursor: pointer; padding: 0; flex-shrink: 0; }
  .toggle-btn:disabled { cursor: wait; }
  .toggle-track {
    display: block;
    width: 44px;
    height: 24px;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 12px;
    position: relative;
    transition: background 0.2s ease;
  }
  .toggle-btn.active .toggle-track { background: rgba(var(--accent-rgb), 0.6); }
  .toggle-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 20px;
    height: 20px;
    background: #4b5563;
    border-radius: 50%;
    transition: all 0.2s ease;
  }
  .toggle-btn.active .toggle-thumb { left: 22px; background: var(--accent-light); }
</style>
