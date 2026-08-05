<script lang="ts">
  import { t } from '../../i18n';
  import {
    SHORTCUTS,
    FAVOURITE_SHORTCUT,
    type Binding,
    type Shortcut,
  } from '../../utils/shortcuts';
  import {
    effectiveBindings,
    formatBinding,
    bindingFromEvent,
    conflictsWith,
    setBinding,
    disableBinding,
    restoreDefaultBinding,
    isDisabled,
    resetAllBindings,
    capturingShortcut,
  } from '../../stores/shortcuts';
  import { settings } from '../../stores/settings';

  const isMac = navigator.platform.toLowerCase().includes('mac');

  const CATEGORY_LABELS: Record<string, string> = {
    navigation: 'help.navigation',
    session: 'help.sessionActions',
    search: 'help.searchCategory',
    other: 'help.other',
  };

  /** Which row is recording, and what it has captured so far. */
  let recordingId: string | null = null;
  let pending: Binding | null = null;
  let conflictNames: string[] = [];
  let error = '';

  $: overrides = ($settings.shortcutOverrides || {}) as Record<string, Binding[]>;

  $: groups = (['navigation', 'session', 'search', 'other'] as const).map((category) => ({
    key: category,
    label: $t(CATEGORY_LABELS[category]),
    items: [
      ...(category === 'navigation' ? [FAVOURITE_SHORTCUT] : []),
      ...SHORTCUTS.filter((s) => s.category === category),
    ],
  }));

  /**
   * What to print for one shortcut.
   *
   * Takes the binding map as an argument rather than reading the store inside.
   * Called as keysFor(shortcut) from the markup, Svelte re-runs it only when
   * `shortcut` changes — and that object never does — so a rebind or a reset
   * updated the store while every row went on showing its old keys. Passing
   * the map in makes the dependency one Svelte can see.
   */
  function keysFor(shortcut: Shortcut, bindings: Map<string, Binding[]>): string {
    if (shortcut.displayKey) {
      return shortcut.displayKey === 'wheel'
        ? 'Ctrl+' + $t('help.wheel')
        : shortcut.displayKey;
    }
    const current = bindings.get(shortcut.id) || shortcut.defaults;
    // An empty list is a shortcut the user switched off, not one with no
    // default — saying so beats an empty button that looks like a glitch.
    if (!current.length) return $t('shortcuts.disabled');
    return current.map((b) => formatBinding(b, isMac)).join(' / ');
  }

  async function turnOff(shortcut: Shortcut) {
    try {
      await disableBinding(shortcut.id);
    } catch (e: any) {
      error = e?.message || String(e);
    }
  }

  function startRecording(shortcut: Shortcut) {
    if (shortcut.fixed) return;
    recordingId = shortcut.id;
    pending = null;
    conflictNames = [];
    error = '';
    // The global handler stands aside while this is true, so the combination
    // being recorded does not also run whatever it is currently bound to.
    capturingShortcut.set(true);
  }

  function stopRecording() {
    recordingId = null;
    pending = null;
    conflictNames = [];
    capturingShortcut.set(false);
  }

  function onRecordKeydown(e: KeyboardEvent) {
    if (!recordingId) return;
    e.preventDefault();
    e.stopPropagation();

    if (e.key === 'Escape') {
      stopRecording();
      return;
    }

    const binding = bindingFromEvent(e);
    if (!binding) return; // still reaching for the combination

    // A shortcut with no modifier would fire while typing into the terminal,
    // and the global handler leaves on plain keys before it does any work —
    // deliberately, because that check is what keeps typing cheap.
    if (!binding.ctrl && !binding.alt) {
      error = $t('shortcuts.needsModifier');
      pending = null;
      conflictNames = [];
      return;
    }

    error = '';
    pending = binding;
    conflictNames = conflictsWith(binding, recordingId).map((s) => $t(s.descKey));
  }

  async function apply() {
    if (!recordingId || !pending) return;
    const id = recordingId;
    const binding = pending;
    try {
      await setBinding(id, [binding]);
      stopRecording();
    } catch (e: any) {
      error = e?.message || String(e);
    }
  }

  async function restoreDefault(shortcut: Shortcut) {
    try {
      await restoreDefaultBinding(shortcut.id);
    } catch (e: any) {
      error = e?.message || String(e);
    }
  }

  async function restoreAll() {
    await resetAllBindings();
    stopRecording();
  }
</script>

<div class="shortcut-editor">
  <div class="editor-header">
    <p class="editor-note">{$t('shortcuts.note')}</p>
    <button class="reset-all" on:click={restoreAll} disabled={!Object.keys(overrides).length}>
      {$t('shortcuts.resetAll')}
    </button>
  </div>

  {#if error}
    <div class="editor-error">{error}</div>
  {/if}

  {#each groups as group (group.key)}
    <div class="group">
      <h4>{group.label}</h4>
      {#each group.items as shortcut (shortcut.id)}
        <div class="row" class:not-reboundable={shortcut.fixed}>
          <span class="desc">{$t(shortcut.descKey)}</span>

          {#if recordingId === shortcut.id}
            <!-- svelte-ignore a11y-autofocus -->
            <input
              class="capture"
              class:conflict={conflictNames.length > 0}
              readonly
              autofocus
              value={pending ? formatBinding(pending, isMac) : $t('shortcuts.pressKeys')}
              on:keydown={onRecordKeydown}
              on:blur={stopRecording}
            />
            <!-- Same slot as the ✕/↺ buttons, so the key column does not move
                 when a row switches into recording. -->
            <span class="row-actions">
              <button class="icon-btn apply" title={$t('shortcuts.apply')}
                      on:mousedown|preventDefault={apply} disabled={!pending}>✓</button>
            </span>
          {:else}
            <button
              class="keys"
              class:off={isDisabled(shortcut.id, overrides)}
              disabled={shortcut.fixed}
              title={shortcut.fixed ? $t('shortcuts.fixedHint') : $t('shortcuts.clickToChange')}
              on:click={() => startRecording(shortcut)}
            >
              {keysFor(shortcut, $effectiveBindings)}
            </button>
            <!-- Both slots are always present, even when empty. Showing the
                 buttons only where they apply left every row a different width,
                 so the key column did not line up down the list. -->
            <span class="row-actions">
              {#if !shortcut.fixed && !isDisabled(shortcut.id, overrides)}
                <button class="icon-btn" title={$t('shortcuts.disable')}
                        on:click={() => turnOff(shortcut)}>✕</button>
              {/if}
              {#if overrides[shortcut.id]}
                <button class="icon-btn" title={$t('shortcuts.restoreDefault')}
                        on:click={() => restoreDefault(shortcut)}>↺</button>
              {/if}
            </span>
          {/if}
        </div>

        {#if recordingId === shortcut.id && conflictNames.length}
          <div class="conflict-note">
            {$t('shortcuts.conflict', { others: conflictNames.join(', ') })}
          </div>
        {/if}
      {/each}
    </div>
  {/each}
</div>

<style>
  .shortcut-editor { display: flex; flex-direction: column; gap: 18px; }
  .editor-header {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
  }
  .editor-note { margin: 0; font-size: 12px; opacity: 0.75; }
  .reset-all {
    flex-shrink: 0; padding: 4px 10px; font-size: 12px; cursor: pointer;
    border-radius: 4px; border: 1px solid rgba(255,255,255,0.18);
    background: rgba(255,255,255,0.06); color: inherit;
  }
  .reset-all:disabled { opacity: 0.4; cursor: default; }
  .editor-error {
    padding: 6px 10px; border-radius: 4px; font-size: 12px;
    background: rgba(239,68,68,0.14); color: #fca5a5;
  }
  .group h4 {
    margin: 0 0 6px; font-size: 12px; text-transform: uppercase;
    letter-spacing: 0.04em; opacity: 0.6;
  }
  .row {
    display: flex; align-items: center; gap: 6px;
    padding: 5px 0; font-size: 13px;
  }
  /* The description takes the slack, so the key column and the buttons after
     it stay hard right and line up down the list.
     min-width:0 and the wrap are both needed: a flex item's floor is its
     content width, so a long label — several of these are a full sentence in
     German or Hungarian — pushed the row wider than its container instead of
     wrapping, and the text ran over the row above and below it. */
  .row .desc {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
    line-height: 1.35;
  }
  /* Rows for shortcuts that cannot be rebound stay visible rather than being
     hidden: someone looking for Esc should find it and see that it cannot be
     changed, not wonder whether the list is incomplete.
     NOT called "fixed" — Tailwind ships a global .fixed utility meaning
     position:fixed, and Svelte's scoping does not protect against it, so these
     rows were being lifted out of the flow and drawn over their neighbours. */
  .row.not-reboundable .desc { opacity: 0.55; }
  .row-actions {
    /* Fixed width so the key column lines up whether or not the buttons are
       there — a row with no buttons must still reserve their space. Sized for
       two icon buttons and the gap between them, and left-aligned so a single
       button sits against the keys rather than drifting away from them. */
    flex-shrink: 0;
    width: 50px;
    display: flex;
    gap: 3px;
    justify-content: flex-start;
  }
  .keys, .capture {
    /* One width for both, so replacing the button with the capture field does
       not move the column. box-sizing because the input's padding and border
       would otherwise add to the width where the button's do not. */
    flex-shrink: 0;
    box-sizing: border-box;
    width: 190px;
    text-align: center; font-family: inherit; font-size: 12px;
    padding: 4px 8px; border-radius: 4px;
    border: 1px solid rgba(255,255,255,0.18);
    background: rgba(255,255,255,0.06); color: inherit;
  }
  .keys { cursor: pointer; }
  .keys:hover:not(:disabled) { background: rgba(255,255,255,0.12); }
  .keys:disabled { cursor: default; opacity: 0.5; }
  .keys.off { opacity: 0.5; font-style: italic; }
  .capture { outline: 2px solid rgba(96,165,250,0.7); }
  .capture.conflict { outline-color: rgba(251,146,60,0.8); }
  .icon-btn {
    width: 24px; padding: 3px 0; font-size: 12px; line-height: 1;
    cursor: pointer; border-radius: 4px;
    border: 1px solid rgba(255,255,255,0.18);
    background: rgba(255,255,255,0.06); color: inherit;
  }
  .icon-btn:hover { background: rgba(255,255,255,0.14); }

  /* Green, and only once there is something to apply: the colour is the
     signal that the combination was accepted, so it must not be inviting
     while the field is still waiting for a key. */
  .apply {
    border-color: rgba(74, 222, 128, 0.5);
    background: rgba(74, 222, 128, 0.16);
    color: #86efac;
    font-weight: 600;
  }
  .apply:hover:not(:disabled) { background: rgba(74, 222, 128, 0.3); }
  .apply:disabled {
    opacity: 0.4;
    cursor: default;
    /* Back to neutral while nothing has been captured — a green tick over an
       empty field reads as "done", which it is not. */
    border-color: rgba(255,255,255,0.18);
    background: rgba(255,255,255,0.06);
    color: inherit;
  }
  .conflict-note {
    padding: 2px 0 6px; font-size: 12px; color: #fdba74;
  }
</style>
