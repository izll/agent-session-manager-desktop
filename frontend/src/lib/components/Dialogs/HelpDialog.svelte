<script lang="ts">
  import { createEventDispatcher } from 'svelte';

  export let show = false;

  const dispatch = createEventDispatcher();

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' || e.key === '?') {
      close();
    }
  }

  const shortcuts = [
    { category: 'Navigation', items: [
      { key: '↑ / ↓', desc: 'Select previous/next session' },
      { key: 'Ctrl+↑ / Ctrl+↓', desc: 'Reorder session (move up/down)' },
      { key: 'Enter', desc: 'Attach to selected session' },
    ]},
    { category: 'Session Actions', items: [
      { key: 'n', desc: 'New session' },
      { key: 's', desc: 'Start session' },
      { key: 'x', desc: 'Stop session' },
      { key: 'd', desc: 'Delete session' },
      { key: '*', desc: 'Toggle favorite' },
    ]},
    { category: 'Search', items: [
      { key: 'Ctrl+F', desc: 'Global history search' },
      { key: '/', desc: 'Filter sessions' },
    ]},
    { category: 'Other', items: [
      { key: '?', desc: 'Show this help' },
      { key: 'Esc', desc: 'Close dialogs' },
    ]},
  ];
</script>

{#if show}
  <div
    class="dialog-overlay"
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>Keyboard Shortcuts</h2>
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
          <h3 class="section-title">About</h3>
          <p>Agent Session Manager Desktop</p>
          <p class="version">Version 0.1.0</p>
          <p class="link">
            <a href="https://github.com/anthropics/claude-code" target="_blank" rel="noopener">
              github.com/anthropics/claude-code
            </a>
          </p>
        </div>
      </div>

      <div class="dialog-footer">
        <button class="btn-close" on:click={close}>
          Close
          <kbd>?</kbd>
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
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #a78bfa;
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
    font-size: 12px;
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
    color: #a78bfa;
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
    background: rgba(139, 92, 246, 0.2);
    border-radius: 4px;
    font-size: 11px;
    color: #a78bfa;
  }
</style>
