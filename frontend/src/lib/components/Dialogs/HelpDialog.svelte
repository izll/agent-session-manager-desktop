<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher, onMount } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { BrowserOpenURL } from '../../../../wailsjs/runtime/runtime';
  import { t } from '../../i18n';
  import { SHORTCUTS, FAVOURITE_SHORTCUT, type Shortcut, type Binding } from '../../utils/shortcuts';
  import { effectiveBindings, formatBinding } from '../../stores/shortcuts';

  // Modifier names differ on Apple keyboards; the bindings themselves do not.
  const isMac = navigator.platform.toLowerCase().includes('mac');

  export let show = false;

  const dispatch = createEventDispatcher();

  // Read the version from the binary rather than hard-coding it here, so it
  // can't drift out of date at the next release.
  const REPO_URL = 'https://github.com/izll/agent-session-manager-desktop';
  let version = '';

  onMount(async () => {
    try {
      version = await App.GetVersion();
    } catch {
      // Leave it blank rather than showing a stale or made-up number.
    }
  });

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
    }
  }

  // Rendered from the same list the key handlers match against, so a rebound
  // shortcut shows its new keys here without anyone remembering to update a
  // second copy. Before this, the keys lived in the handlers and the
  // descriptions lived here, and nothing kept the two in step.
  const CATEGORY_LABELS: Record<string, string> = {
    navigation: 'help.navigation',
    session: 'help.sessionActions',
    search: 'help.searchCategory',
    other: 'help.other',
  };

  $: shortcuts = (['navigation', 'session', 'search', 'other'] as const).map((category) => ({
    category: $t(CATEGORY_LABELS[category]),
    items: [
      ...(category === 'navigation' ? [FAVOURITE_SHORTCUT] : []),
      ...SHORTCUTS.filter((s) => s.category === category),
    ].map((shortcut) => ({
      key: displayKeys(shortcut, $effectiveBindings),
      desc: $t(shortcut.descKey),
    })).filter((row) => row.key !== ''),
  // A category whose shortcuts were all switched off would otherwise leave a
  // heading with nothing under it.
  })).filter((section) => section.items.length > 0);

  /** What to print for one shortcut: its bindings, or the fixed label for the
   *  entries that are not key presses (the mouse wheel, the favourite range). */
  function displayKeys(shortcut: Shortcut, bindings: Map<string, Binding[]>): string {
    if (shortcut.displayKey) {
      return shortcut.displayKey === 'wheel'
        ? 'Ctrl+' + $t('help.wheel')
        : shortcut.displayKey;
    }
    const current = bindings.get(shortcut.id) || shortcut.defaults;
    // A shortcut the user switched off is dropped from the help entirely (the
    // caller filters empty strings): listing it with no keys would read as a
    // shortcut that exists but cannot be pressed.
    if (!current.length) return '';
    return current.map((b) => formatBinding(b, isMac)).join(' / ');
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('help.title')}</h2>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="help-content">
        {#each shortcuts as section}
          <div class="section">
            <h3 class="section-title">{section.category}</h3>
            <div class="shortcuts-list">
              {#each section.items as item}
                <div class="shortcut-item">
                  <kbd class="key">{item.key}</kbd>
                  <span class="desc">{item.desc}</span>
                </div>
              {/each}
            </div>
          </div>
        {/each}

        <div class="section about">
          <h3 class="section-title">{$t('help.about')}</h3>
          <p>{$t('help.appName')}</p>
          {#if version}
            <p class="version">v{version}</p>
          {/if}
          <p class="link">
            <!-- The webview won't follow target="_blank"; hand the URL to the
                 host so it opens in the user's real browser. -->
            <a
              href={REPO_URL}
              on:click|preventDefault={() => BrowserOpenURL(REPO_URL)}
            >
              github.com/izll/agent-session-manager-desktop
            </a>
          </p>
        </div>
      </div>

      <div class="dialog-footer">
        <button class="btn-close" on:click={close}>
          {$t('help.close')}
          <kbd>Esc</kbd>
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Component-specific overrides for dialog */
  .dialog-content {
    max-width: 600px;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
  }

  .help-content {
    flex: 1;
    overflow-y: auto;
    padding: 24px;
  }

  .section {
    margin-bottom: 24px;
  }

  .section:last-child {
    margin-bottom: 0;
  }

  .section-title {
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--accent-light);
    margin: 0 0 12px 0;
  }

  .shortcuts-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .shortcut-item {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .key {
    min-width: 120px;
    padding: 6px 12px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    font-family: monospace;
    font-size: 13px;
    color: #e4e4e7;
    text-align: center;
  }

  .desc {
    font-size: 13px;
    color: #9ca3af;
  }

  .about {
    padding-top: 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  .about p {
    margin: 0 0 8px 0;
    font-size: 13px;
    color: #9ca3af;
  }

  .about .version {
    color: #6b7280;
  }

  .about .link a {
    color: var(--accent-light);
    text-decoration: none;
  }

  .about .link a:hover {
    text-decoration: underline;
  }

  .btn-close {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 20px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 10px;
    font-size: 14px;
    font-weight: 500;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-close:hover {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  .btn-close kbd {
    padding: 2px 6px;
    background: rgba(var(--accent-rgb), 0.2);
    border-radius: 4px;
    font-size: 12px;
    color: var(--accent-light);
  }
</style>
