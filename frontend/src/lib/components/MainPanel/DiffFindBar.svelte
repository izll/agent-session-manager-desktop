<script lang="ts">
  /**
   * The find bar over a diff.
   *
   * One component for every place a diff is shown — the working-tree diff in
   * all three of its renderers, and the commit-history dialog — so the keys,
   * the counter and the look cannot drift apart between them. It only reports
   * what was asked for; the owner decides what a search means for the renderer
   * it has on screen.
   */
  import { createEventDispatcher, tick } from 'svelte';
  import { t } from '../../i18n';
  import { findKeyAction, matchCounter } from '../../utils/diffFind';

  /** What is typed, bound by the owner. */
  export let query = '';
  /** How many matches, and which one is current (0-based, -1 for none). */
  export let hitCount = 0;
  export let hitAt = -1;
  /**
   * Keep the keys this bar handles from reaching anything around it.
   *
   * The history dialog answers Escape and the arrows itself — closing, and
   * walking the commit list — and a key meant for the search must not also do
   * that.
   */
  export let isolateKeys = false;

  const dispatch = createEventDispatcher<{ search: string; step: 1 | -1; close: void }>();

  let input: HTMLInputElement | undefined;

  export async function focus() {
    await tick();
    input?.focus();
    input?.select();
  }

  function handleKeydown(event: KeyboardEvent) {
    const action = findKeyAction(event);
    if (action === null) return;
    event.preventDefault();
    if (isolateKeys) event.stopPropagation();
    if (action === 'close') dispatch('close');
    else dispatch('step', action);
  }
</script>

<!-- Above the code rather than floating over it: a bar over code hides the
     very lines being searched. -->
<div class="diff-find">
  <input
    type="text"
    bind:this={input}
    bind:value={query}
    on:input={() => dispatch('search', query)}
    on:keydown={handleKeydown}
    placeholder={$t('diff.findPlaceholder')}
    title="{$t('notes.nextMatch')}: Enter · ↓ · F3 · Ctrl+G — {$t('notes.previousMatch')}: Shift+Enter · ↑"
  />
  <span class="find-count">{matchCounter(hitAt, hitCount, query, $t('notes.noMatches'))}</span>
  <button
    on:click={() => dispatch('step', -1)}
    disabled={!hitCount}
    title="{$t('notes.previousMatch')} (Shift+Enter · ↑)"
  >↑</button>
  <button
    on:click={() => dispatch('step', 1)}
    disabled={!hitCount}
    title="{$t('notes.nextMatch')} (Enter · ↓ · F3 · Ctrl+G)"
  >↓</button>
  <button on:click={() => dispatch('close')} title="{$t('common.close')} (Esc)">×</button>
</div>

<style>
  .diff-find {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    background: var(--bg-raised);
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    flex-shrink: 0;
  }

  .diff-find input {
    flex: 1;
    min-width: 0;
    padding: 5px 9px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 6px;
    color: #e5e7eb;
    font-size: 13px;
    font-family: inherit;
  }

  .diff-find input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  .find-count {
    font-size: 12px;
    color: #6b7280;
    font-variant-numeric: tabular-nums;
    min-width: 52px;
    text-align: center;
  }

  .diff-find button {
    padding: 4px 9px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 5px;
    color: #9ca3af;
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
  }

  .diff-find button:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.07);
    color: #e5e7eb;
  }

  .diff-find button:disabled {
    opacity: 0.4;
    cursor: default;
  }
</style>
