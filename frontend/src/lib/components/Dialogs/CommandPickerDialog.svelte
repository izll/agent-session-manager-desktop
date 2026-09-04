<script lang="ts">
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { activeProjectId } from '../../stores/projects';
  import { get } from 'svelte/store';

  export let show = false;
  /** Session the picked command is sent to. Empty means "nothing selected". */
  export let sessionId = '';
  export let windowIdx = 0;

  /** Raised when the user asks for the editor from inside the picker. */
  export let onOpenManager: (() => void) | null = null;

  type Cmd = main.SavedCommandInfo;
  type Group = main.CommandGroupInfo;

  let commands: Cmd[] = [];
  let groups: Group[] = [];
  let loading = false;
  let error = '';
  let running = false;
  let operationGeneration = 0;
  let targetSessionId = '';
  let targetWindowIdx = 0;
  let targetProjectId = '';

  let query = '';
  let selectedIdx = 0;
  // The dialog often opens under the pointer. Without this, the row that
  // happens to be beneath it fires mouseenter and steals the selection, so
  // Enter runs a command the user never chose. The mouse only takes over
  // once it has actually moved.
  let mouseActive = false;
  let listEl: HTMLElement | null = null;

  // Second phase: a picked command that still needs placeholder values.
  let pending: Cmd | null = null;
  let values: Record<string, string> = {};
  let valueInputs: HTMLInputElement[] = [];

  // Load fresh on every open — the manager may have changed things.
  let lastShow = false;
  $: {
    if (show && !lastShow) void open();
    if (!show && lastShow) reset();
    lastShow = show;
  }

  // A command can be destructive. If keyboard navigation changes the active
  // tab behind the modal, close instead of silently offering the old target.
  $: if (show && targetSessionId &&
         (sessionId !== targetSessionId || windowIdx !== targetWindowIdx ||
          $activeProjectId !== targetProjectId)) close();

  async function open() {
    const generation = ++operationGeneration;
    reset();
    targetSessionId = sessionId;
    targetWindowIdx = windowIdx;
    targetProjectId = get(activeProjectId);
    loading = true;
    try {
      const lib = await App.GetCommands();
      if (!show || generation !== operationGeneration) return;
      commands = (lib?.commands || []) as Cmd[];
      groups = (lib?.groups || []) as Group[];
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) loading = false;
    }
  }

  function reset() {
    query = '';
    selectedIdx = 0;
    mouseActive = false;
    pending = null;
    values = {};
    valueInputs = [];
    error = '';
    running = false;
  }

  $: groupNames = new Map(groups.map(g => [g.id, g.name]));

  function matches(c: Cmd, q: string): boolean {
    return (
      c.name.toLowerCase().includes(q) ||
      c.command.toLowerCase().includes(q) ||
      (c.description || '').toLowerCase().includes(q)
    );
  }

  $: filtered = (() => {
    const q = query.trim().toLowerCase();
    return q ? commands.filter(c => matches(c, q)) : commands;
  })();

  // Grouped for display, but the keyboard walks a single flat list so ↑/↓
  // cross section boundaries the way the eye expects.
  interface Section {
    id: string;
    name: string;
    items: Cmd[];
  }

  $: sections = (() => {
    const out: Section[] = [];
    for (const g of groups) {
      const items = filtered.filter(c => c.groupId === g.id);
      if (items.length) out.push({ id: g.id, name: g.name, items });
    }
    const loose = filtered.filter(c => !c.groupId || !groupNames.has(c.groupId));
    if (loose.length) out.push({ id: '', name: $t('commands.ungrouped'), items: loose });
    return out;
  })();

  $: flat = sections.flatMap(s => s.items);

  // Keep the cursor inside the list as the filter narrows it.
  $: if (selectedIdx >= flat.length) selectedIdx = Math.max(0, flat.length - 1);

  function indexOfCmd(c: Cmd): number {
    return flat.indexOf(c);
  }

  function scrollSelectedIntoView() {
    if (!listEl) return;
    const row = listEl.querySelector<HTMLElement>(`[data-idx="${selectedIdx}"]`);
    row?.scrollIntoView({ block: 'nearest' });
  }

  $: if (selectedIdx >= 0 && listEl && !mouseActive) {
    // Keyboard navigation has to keep the selection visible. Skipped while
    // the mouse is driving: the row is already under the pointer, and
    // scrolling to it would fight the user's own scrolling.
    void selectedIdx;
    requestAnimationFrame(scrollSelectedIntoView);
  }

  function move(delta: number) {
    if (flat.length === 0) return;
    mouseActive = false;
    selectedIdx = (selectedIdx + delta + flat.length) % flat.length;
  }

  function pick(c: Cmd) {
    if (!targetSessionId || running) return;
    const ph = c.placeholders || [];
    if (ph.length === 0) {
      void run(c, {});
      return;
    }
    // Pre-fill the defaults so a command with sensible ones can be run by
    // pressing Enter, without retyping what was already written down.
    const next: Record<string, string> = {};
    for (const p of ph) next[p.name] = p.default || '';
    values = next;
    pending = c;
    valueInputs = [];
    requestAnimationFrame(() => valueInputs[0]?.focus());
  }

  async function run(c: Cmd, vals: Record<string, string>) {
    if (running || !targetSessionId) return;
    const generation = operationGeneration;
    const targetSession = targetSessionId;
    const targetWindow = targetWindowIdx;
    const targetProject = targetProjectId;
    const submittedValues = { ...vals };
    running = true;
    error = '';
    try {
      await App.RunCommand(c.id, targetSession, targetWindow, submittedValues, targetProject);
      if (!show || generation !== operationGeneration || targetProject !== get(activeProjectId)) return;
      close();
    } catch (e) {
      if (!show || generation !== operationGeneration || targetProject !== get(activeProjectId)) return;
      error = String(e);
      pending = null;
    } finally {
      if (generation === operationGeneration) running = false;
    }
  }

  function submitValues() {
    if (pending) void run(pending, values);
  }

  /** Enter walks to the next field, and submits from the last one. */
  function handleValueKey(e: KeyboardEvent, i: number) {
    if (running) {
      e.preventDefault();
      return;
    }
    if (e.key === 'Escape') {
      e.stopPropagation();
      pending = null;
      return;
    }
    if (e.key !== 'Enter') return;
    e.preventDefault();
    const ph = pending?.placeholders || [];
    if (i < ph.length - 1) {
      valueInputs[i + 1]?.focus();
    } else {
      submitValues();
    }
  }

  // Live preview of what will actually be sent, so a half-filled command is
  // obvious before it runs.
  $: previewText = pending
    ? pending.command.replace(
        /\{\{([\p{L}\p{N}_][\p{L}\p{N}_ -]*?)(?::([^}]*))?\}\}/gu,
        (whole, name: string, fallback: string) => {
          const v = values[name];
          if (v) return v;
          // Mirror the backend: an untouched field falls back to its default,
          // and only a placeholder with neither stays visible as unfilled.
          if (fallback) return fallback;
          return `{{${name}}}`;
        }
      )
    : '';

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      if (pending) {
        pending = null;
        return;
      }
      close();
      return;
    }
    if (pending) return; // the value form owns the keyboard while it's up
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const c = flat[selectedIdx];
      if (c) pick(c);
    }
  }

  function autoFocusSearch(node: HTMLInputElement) {
    requestAnimationFrame(() => node.focus());
  }

  function bindInput(node: HTMLInputElement, i: number) {
    valueInputs[i] = node;
    return {
      destroy() {
        valueInputs[i] = undefined as unknown as HTMLInputElement;
      },
    };
  }

  function openManager() {
    close();
    onOpenManager?.();
  }

  function close() {
    operationGeneration++;
    loading = false;
    running = false;
    show = false;
  }
</script>

{#if show}
  <div
    class="dialog-overlay picker-overlay"
    use:autoFocusDialog
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    on:keydown={handleKeydown}
  >
    <div class="dialog-content picker">
      <div class="dialog-header">
        <h2>{$t('commands.pickerTitle')}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      {#if !targetSessionId}
        <div class="dialog-body">
          <div class="state warn">{$t('commands.noSession')}</div>
        </div>
      {:else if pending}
        <div class="dialog-body">
          <div class="pending-head">
            <span class="pending-name">{pending.name}</span>
            <span class="pending-hint">{$t('commands.fillValues')}</span>
          </div>

          <div class="value-form">
            {#each pending.placeholders as p, i (p.name)}
              <label class="value-row">
                <span class="value-label">{p.name}</span>
                <input
                  use:bindInput={i}
                  bind:value={values[p.name]}
                  placeholder={p.default || p.name}
                  on:keydown={(e) => handleValueKey(e, i)}
                  disabled={running}
                />
              </label>
            {/each}
          </div>

          <div class="preview">
            <span class="preview-label">{$t('commands.preview')}</span>
            <code class="preview-text">{previewText}</code>
          </div>

          {#if error}<div class="error-line">{error}</div>{/if}
        </div>

        <div class="dialog-footer">
          <button class="btn-secondary" on:click={() => (pending = null)} disabled={running}>{$t('common.cancel')}</button>
          <button class="btn-primary" on:click={submitValues} disabled={running}>{$t('commands.run')}</button>
        </div>
      {:else}
        <div class="search-wrap">
          <input
            class="search"
            use:autoFocusSearch
            bind:value={query}
            on:input={() => { mouseActive = false; selectedIdx = 0; }}
            placeholder={$t('commands.searchPlaceholder')}
          />
        </div>

        <div class="dialog-body" bind:this={listEl}>
          {#if loading}
            <div class="state">{$t('common.loading')}</div>
          {:else if error}
            <div class="error-line">{error}</div>
          {:else if commands.length === 0}
            <div class="empty">
              <p class="empty-title">{$t('commands.emptyTitle')}</p>
              <p class="empty-hint">{$t('commands.emptyHint')}</p>
              <button class="btn-primary" on:click={openManager}>{$t('commands.openManager')}</button>
            </div>
          {:else if flat.length === 0}
            <div class="state">{$t('commands.noMatch')}</div>
          {:else}
            {#each sections as sec (sec.id)}
              <div class="section">
                <div class="section-head">{sec.name}</div>
                {#each sec.items as c (c.id)}
                  {@const idx = indexOfCmd(c)}
                  <div
                    class="cmd-row"
                    class:selected={idx === selectedIdx}
                    data-idx={idx}
                    role="button"
                    tabindex="-1"
                    on:click={() => pick(c)}
                    on:mousemove={() => { mouseActive = true; selectedIdx = idx; }}
                    on:keydown={() => {}}
                  >
                    <div class="cmd-main">
                      <span class="cmd-name">{c.name}</span>
                      <span class="cmd-text">{c.command}</span>
                    </div>
                    <span class="cmd-group">{sec.name}</span>
                  </div>
                {/each}
              </div>
            {/each}
          {/if}
        </div>

        <div class="dialog-footer picker-footer">
          <button class="link-btn" on:click={openManager}>{$t('commands.openManager')}</button>
          <span class="foot-hint">{$t('commands.navHint')}</span>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .picker-overlay:focus { outline: none; }

  .picker {
    width: min(640px, 94vw);
    max-width: min(640px, 94vw);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
  }
  .dialog-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 14px 18px; border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }
  .dialog-header h2 { margin: 0; font-size: 15px; color: #e4e4e7; }
  .close-btn { border: 0; background: none; color: #71717a; font-size: 20px; cursor: pointer; }
  .close-btn:hover { color: #e4e4e7; }

  .search-wrap { padding: 12px 18px 0; }
  .search {
    width: 100%; padding: 8px 11px; border-radius: 7px; font-size: 13px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.25); color: #e4e4e7;
  }
  .search:focus { outline: none; border-color: rgba(var(--accent-rgb), 0.6); }

  .dialog-body { padding: 12px 18px; overflow-y: auto; flex: 1; min-height: 0; }
  .state { padding: 26px 0; text-align: center; font-size: 13px; color: #71717a; }
  .state.warn { color: #fbbf24; }
  .error-line { color: #fb7185; font-size: 13px; }

  .empty { padding: 26px 0; text-align: center; }
  .empty-title { margin: 0 0 6px; font-size: 14px; color: #e4e4e7; }
  .empty-hint { margin: 0 0 16px; font-size: 13px; color: #71717a; line-height: 1.6; }

  .section { margin-bottom: 10px; }
  .section-head {
    font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em;
    color: #6b7280; padding: 4px 2px;
  }
  .cmd-row {
    display: flex; align-items: center; gap: 10px; padding: 7px 9px;
    border-radius: 7px; cursor: pointer; border: 1px solid transparent;
  }
  .cmd-row.selected { background: rgba(var(--accent-rgb), 0.14); border-color: rgba(var(--accent-rgb), 0.4); }
  .cmd-main { display: flex; flex-direction: column; gap: 2px; flex: 1; min-width: 0; }
  .cmd-name { font-size: 13px; color: #e4e4e7; }
  .cmd-text {
    font-family: 'JetBrains Mono', monospace; font-size: 11px; color: #6b7280;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .cmd-group { font-size: 11px; color: #71717a; flex-shrink: 0; }

  .pending-head { display: flex; flex-direction: column; gap: 2px; margin-bottom: 12px; }
  .pending-name { font-size: 14px; color: #e4e4e7; }
  .pending-hint { font-size: 12px; color: #71717a; }

  .value-form { display: flex; flex-direction: column; gap: 8px; }
  .value-row { display: flex; flex-direction: column; gap: 3px; }
  .value-label { font-size: 12px; color: #a1a1aa; font-family: 'JetBrains Mono', monospace; }
  .value-row input {
    padding: 7px 10px; border-radius: 7px; font-size: 13px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.25); color: #e4e4e7;
  }
  .value-row input:focus { outline: none; border-color: rgba(var(--accent-rgb), 0.6); }

  .preview { margin-top: 14px; display: flex; flex-direction: column; gap: 4px; }
  .preview-label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7280; }
  .preview-text {
    font-family: 'JetBrains Mono', monospace; font-size: 12px; color: #a5b4fc;
    background: rgba(0, 0, 0, 0.3); border-radius: 6px; padding: 8px 10px;
    word-break: break-all; white-space: pre-wrap;
  }

  .dialog-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 12px 18px; border-top: 1px solid rgba(255, 255, 255, 0.06);
  }
  .picker-footer { justify-content: space-between; align-items: center; }
  .foot-hint { font-size: 11px; color: #6b7280; }
  .link-btn {
    border: 0; background: none; padding: 0; cursor: pointer;
    font-size: 12px; color: var(--accent-light); text-decoration: underline;
  }
  .link-btn:hover { color: var(--accent-pale); }

  .btn-primary, .btn-secondary {
    padding: 7px 16px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer;
  }
  .btn-secondary {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05); color: #a1a1aa;
  }
  .btn-primary {
    border: 1px solid var(--accent);
    background: linear-gradient(135deg, var(--accent-dark), var(--accent)); color: var(--accent-ink);
  }
</style>
