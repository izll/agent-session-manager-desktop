<script lang="ts">
  /**
   * How to set up reaching the app from a phone, step by step, for this platform.
   * It only explains: the commands are for the user to run in a terminal of their
   * own, as the ones that need it ask for a password.
   */
  import { BrowserOpenURL, ClipboardSetText } from '../../../../wailsjs/runtime/runtime';
  import { t } from '../../i18n';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { portal } from '../../utils/portal';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';

  export let show = false;
  export let current = '';
  export let platform = 'linux';
  export let commands: Record<string, string> = {};

  const STEPS = ['install', 'login', 'enable', 'serve', 'phone'];
  let copied = '';

  $: inApp = platform !== 'linux';

  // Reactive, so a change of language reaches what is already on screen.
  $: text = (step: string): string => (step === 'install' || step === 'login') && inApp
    ? $t(`mobile.guide.${step}App`)
    : $t(`mobile.guide.${step}`);

  async function copy(command: string) {
    try {
      await ClipboardSetText(command);
      copied = command;
    } catch {
      copied = '';
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    claimKeyForDialog();
    e.stopPropagation();
    show = false;
  }
</script>

{#if show}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="dialog-overlay" use:portal use:autoFocusDialog tabindex="-1" role="dialog" aria-modal="true"
    data-dialog="mobile-setup-guide" on:keydown={handleKeydown}>
    <div class="dialog-content guide" role="presentation">
      <div class="dialog-header">
        <h2>{$t('mobile.guide.title')}</h2>
        <DialogCloseButton on:click={() => (show = false)} />
      </div>
      <ol class="dialog-body steps">
        {#each STEPS as step, index (step)}
          <li class:current={step === current} data-guide-step={step}>
            <span class="number">{index + 1}</span>
            <div class="body">
              <strong>{$t(`mobile.setup.${step}`)}</strong>
              <p>{text(step)}</p>
              {#if commands[step]}
                <div class="command-row">
                  <code data-guide-command>{commands[step]}</code>
                  <button class="btn-secondary small" data-guide-copy on:click={() => copy(commands[step])}>
                    {copied === commands[step] ? $t('mobile.guide.copied') : $t('mobile.guide.copy')}
                  </button>
                </div>
              {:else if step === 'install' && inApp}
                <button class="btn-secondary small" data-guide-download on:click={() => BrowserOpenURL('https://tailscale.com/download')}>
                  {$t('mobile.setup.download')}
                </button>
              {/if}
            </div>
          </li>
        {/each}
      </ol>
    </div>
  </div>
{/if}

<style>
  .guide { width: min(640px, 94vw); max-width: min(640px, 94vw); max-height: 84vh; display: flex; flex-direction: column; }
  .steps { list-style: none; margin: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; }
  li { display: flex; gap: 12px; padding: 8px 10px; border-radius: 8px; }
  li.current { background: rgba(var(--accent-rgb), 0.08); outline: 1px solid rgba(var(--accent-rgb), 0.35); }
  .number {
    flex-shrink: 0; width: 22px; height: 22px; border-radius: 50%;
    display: grid; place-items: center; font-size: 12px;
    background: rgba(255, 255, 255, 0.08); color: #e4e4e7;
  }
  .body { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  strong { font-size: 14px; color: #e4e4e7; font-weight: 500; }
  p { margin: 0; font-size: 13px; color: #9ca3af; line-height: 1.45; }
  .command-row { display: flex; gap: 8px; align-items: flex-start; }
  code {
    flex: 1; min-width: 0; padding: 6px 8px; border-radius: 6px;
    background: rgba(0, 0, 0, 0.3); font-family: var(--font-mono, monospace);
    font-size: 12px; color: #e4e4e7; user-select: all; overflow-wrap: anywhere;
  }
  .small { padding: 3px 8px; font-size: 12px; flex-shrink: 0; }
  .body > .small { align-self: flex-start; }
</style>
