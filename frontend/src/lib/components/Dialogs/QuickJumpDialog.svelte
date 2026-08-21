<script lang="ts">
  /**
   * The quick-jump window: the handful of places you move between all day,
   * behind one keystroke.
   *
   * Distinct from the ⭐ favourite mark, which says "this one matters" and
   * shows it in the sidebar. This says "I keep going here", which is a
   * different question: the list is ordered by hand, holds tabs as well as
   * whole sessions, and its order is the whole point, because the first nine
   * entries answer to the number keys.
   *
   * Two ways to arrive somewhere, because they suit different moments. A
   * number is instant and needs no reading, which is what you want for the
   * three or four places you go constantly. The arrows read the list, which is
   * what you want for the rest of it — including the tenth entry and beyond,
   * where the numbers run out.
   */
  import { createEventDispatcher, tick } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { sessions } from '../../stores/sessions';
  import { activeProjectId } from '../../stores/projects';
  import { getGradientCSS, getNameStyle, getContrastColor, isGradient } from '../../utils/rowColors';
  import { activities } from '../../stores/activities';
  import { tabStatuses } from '../../stores/statusLines';
  import { autoFocusDialog } from '../../utils/dialogActions';

  export let show = false;

  const dispatch = createEventDispatcher();

  interface Entry {
    sessionId: string;
    windowIdx: number;
    label?: string;
  }

  let entries: Entry[] = [];
  let draggingIndex: number | null = null;
  let dragOverIndex: number | null = null;
  let cursor = 0;
  let listEl: HTMLDivElement | null = null;
  let loadGeneration = 0;
  let mutationPending = false;

  /** Only the first nine get a number: there is no key 10, and a badge that
   *  names a key nothing answers to is worse than no badge. */
  const NUMBERED = 9;

  // Both the open cycle and the active project own the list. A slow read from
  // an earlier project/open must not overwrite a newly reopened dialog.
  let loadedFor = '';
  $: {
    const key = `${show}|${$activeProjectId}`;
    if (key !== loadedFor) {
      loadedFor = key;
      loadGeneration++;
      if (show) void load();
      else {
        editingIndex = null;
      }
    }
  }

  async function load(options: { keepCursor?: number } = {}) {
    const generation = ++loadGeneration;
    const projectId = $activeProjectId;
    try {
      const result = (await App.GetQuickJump()) ?? [];
      if (!show || generation !== loadGeneration || projectId !== $activeProjectId) return;
      entries = result;
    } catch (e) {
      if (!show || generation !== loadGeneration || projectId !== $activeProjectId) return;
      console.error('Quick jump list failed to load:', e);
      entries = [];
    }
    // Reloading after an edit should leave the cursor where the eye is; only
    // opening the window starts at the top.
    cursor = Math.min(options.keepCursor ?? 0, Math.max(0, entries.length - 1));
    await tick();
    if (!show || generation !== loadGeneration || projectId !== $activeProjectId) return;
    listEl?.focus();
  }

  /**
   * What each entry points at, resolved against the live session list.
   *
   * An entry whose target has gone is kept and shown greyed rather than
   * dropped: a stopped session is not a deleted one, and renumbering the list
   * every time something stops would break the muscle memory the numbers exist
   * to build — if "3" is the build tab, it has to stay the build tab.
   */
  $: resolved = entries.map((entry, index) => {
    const session = $sessions.find((s) => s.id === entry.sessionId);
    // followedWindows holds the extra tabs and numbers them by `index`; the
    // session's own first tab is not in there, which is why index 0 resolves
    // to the session's name rather than to a missing entry.
    const tab =
      session && entry.windowIdx > 0
        ? (session.followedWindows ?? []).find((w: any) => w.index === entry.windowIdx)
        : null;

    const missing = !session || (entry.windowIdx > 0 && !tab);
    const isTab = entry.windowIdx >= 0;

    // Busy / waiting / idle, from the same source the sidebar reads. This is
    // the thing worth showing next to a name: which of these is asking for
    // you, and which is still working.
    const tabStatus = session
      ? ($tabStatuses[session.id] ?? []).find((t: any) => t.windowIdx === entry.windowIdx)
      : null;
    const activity = !session || session.status !== 'running'
      ? 'stopped'
      : (isTab ? (tabStatus?.activity ?? 'idle') : ($activities[session.id] ?? 'idle'));

    // "{session} — {tab}" for a tab: its own name says what it runs, the
    // session's says which project, and neither alone identifies it in a list.
    const tabName = tab?.name || tab?.agent ||
      (entry.windowIdx === 0 ? (session?.agent ?? '') : `#${entry.windowIdx}`);
    const sessionName = session?.name ?? entry.sessionId;
    const defaultName = isTab && tabName ? `${sessionName} - ${tabName}` : sessionName;

    return {
      ...entry,
      index,
      isTab,
      missing,
      activity,
      name: entry.label || defaultName,
      // What the entry would be called with no name of its own. Kept on the
      // row so editing can start from something even when the entry has no
      // label, and so "reset" has an original to go back to.
      defaultName,
      // A tab entry still says which session it is in; the name alone is
      // "shell" or "claude" in a dozen places at once.
      context: isTab ? (session?.name ?? entry.sessionId) : '',
      // Each kind is coloured the way it is coloured where it lives, so a
      // row here looks like the thing it points at rather than like a
      // generic list item.
      style: isTab ? tabStyle(tab, session) : sessionStyle(session),
      gradient: !isTab ? sessionGradientStyle(session) : '',
      number: index < NUMBERED ? index + 1 : null,
    };
  });

  /**
   * A session row, coloured as it is in the session list.
   *
   * getNameStyle handles what a raw `background` cannot: "auto" derives a
   * colour from the name, and "gradient-…" names a preset. Both are common in
   * a real list and both are silently ignored if used directly.
   */
  function sessionStyle(session: any): string {
    if (!session) return '';
    if (isGradient(session.color ?? '')) return '';
    return getNameStyle(session.color ?? '', session.bgColor ?? '', !!session.fullRowColor);
  }

  /**
   * A gradient session name, painted onto the letters.
   *
   * getNameStyle skips gradients deliberately — it produces a `color`, and a
   * gradient is not one — so the sidebar paints them itself and this list has
   * to match, or a gradient session renders plain. Kept separate from
   * sessionStyle because it goes on an element of its own.
   */
  function sessionGradientStyle(session: any): string {
    if (!session || !isGradient(session.color ?? '')) return '';
    return `background: ${getGradientCSS(session.color)};` +
      ' -webkit-background-clip: text; -webkit-text-fill-color: transparent;' +
      ' background-clip: text; font-weight: 600;';
  }

  /**
   * A tab row, coloured as its tab is in the tab bar.
   *
   * Same rule as TabBar's own tabStyle, including "auto" text meaning
   * "whichever of black or white stays readable on that background".
   */
  function tabStyle(tab: any, session: any): string {
    const background = tab?.background_color || session?.tabBackgroundColor || '';
    const text = tab?.text_color || session?.tabTextColor || '';
    if (!background && !text) return '';

    const styles: string[] = [];
    if (background) styles.push(`background: ${getGradientCSS(background)}`);
    // "auto" means "whichever of black or white stays readable", which needs a
    // single background colour to measure. A gradient has no such colour, so
    // white is the safe answer rather than a contrast reading of the name.
    if (text === 'auto' && background) {
      styles.push(`color: ${isGradient(background) ? '#FFFFFF' : getContrastColor(background)}`);
    } else if (text && !isGradient(text)) {
      styles.push(`color: ${text}`);
    }
    return styles.join('; ');
  }

  /**
   * Renaming an entry.
   *
   * In place rather than in a dialog of its own: it is one field on a row you
   * are already looking at, and a second window over the first would be more
   * ceremony than the edit deserves.
   */
  let editingIndex: number | null = null;
  let editingText = '';
  let editingProjectId = '';

  async function startEditing(index: number) {
    const target = entries[index];
    if (!target) return;
    editingIndex = index;
    editingProjectId = $activeProjectId;
    // Starts from what the row shows, which for an entry with no name of its
    // own is the name it is displayed under — an empty field would make the
    // user retype something already on screen.
    editingText = target.label || (resolved[index]?.defaultName ?? '');
    await tick();
    listEl?.querySelector<HTMLInputElement>('.note-input')?.focus();
  }

  async function commitEditing() {
    const index = editingIndex;
    if (index === null) return;
    const target = entries[index];
    editingIndex = null;
    if (!target || editingProjectId !== $activeProjectId) return;
    try {
      // A name that matches the suggestion is stored as no name at all, so the
      // entry keeps following its session and tab: pin it and renaming the
      // session later would leave this row showing the old one.
      const chosen = editingText.trim();
      const label = chosen === resolved[index]?.defaultName ? '' : chosen;
      await App.SetQuickJumpLabel(target.sessionId, target.windowIdx, label);
      await load({ keepCursor: index });
    } catch (e) {
      console.error('Renaming the entry failed:', e);
    }
    focusList();
  }

  function cancelEditing() {
    editingIndex = null;
    focusList();
  }

  /**
   * Put focus back on the list.
   *
   * The input that had it is gone by now, so without this focus lands on the
   * document body: the arrows stop moving the cursor and the number keys stop
   * jumping, which reads as the window having quietly stopped working.
   */
  async function focusList() {
    await tick();
    listEl?.focus();
  }

  /**
   * Put the suggested name back while editing.
   *
   * A rename is easy to regret — the original is derived from the session and
   * tab, so without this the only way back is to remember what it was and type
   * it again.
   */
  function resetEditingName() {
    if (editingIndex === null) return;
    editingText = resolved[editingIndex]?.defaultName ?? '';
  }

  function onNoteKeydown(e: KeyboardEvent) {
    // Handled here and stopped, so the list's own keys — digits jumping,
    // arrows moving, Delete removing — do not fire while typing a note.
    e.stopPropagation();
    if (e.key === 'Enter') {
      e.preventDefault();
      commitEditing();
    } else if (e.key === 'r' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      resetEditingName();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      cancelEditing();
    }
  }

  /** The sidebar's own wording, so the two do not describe the same state
   *  with different words.
   *
   *  The translator comes in as an argument rather than being read from the
   *  store inside: called from markup, a store read in here is invisible to
   *  Svelte, so the labels would keep the language they were first rendered in
   *  after a switch. */
  function activityLabel(translate: typeof $t, activity: string): string {
    switch (activity) {
      case 'busy': return translate('sidebar.statusBusy');
      case 'waiting': return translate('sidebar.statusWaiting');
      case 'stopped': return translate('sidebar.statusStopped');
      default: return translate('sidebar.statusIdle');
    }
  }

  function jump(index: number) {
    // A drag ends with a click on the row it started from, which would
    // otherwise jump away the moment a reorder finished.
    if (draggingIndex !== null) return;
    const target = resolved[index];
    if (!target || target.missing) return;
    dispatch('jump', { sessionId: target.sessionId, windowIdx: target.windowIdx });
    close();
  }

  function close() {
    show = false;
    dispatch('close');
  }

  async function remove(index: number) {
    if (mutationPending) return;
    const target = entries[index];
    if (!target) return;
    mutationPending = true;
    try {
      // Keep the cursor where the eye is rather than sending it home.
      await App.RemoveQuickJump(target.sessionId, target.windowIdx);
      await load({ keepCursor: index });
    } catch (e) {
      console.error('Removing the entry failed:', e);
    } finally {
      mutationPending = false;
    }
  }

  /** Moving is how a number gets assigned, so it is a first-class action here
   *  rather than something buried in a context menu. */
  async function move(from: number, to: number) {
    if (mutationPending || to < 0 || to >= entries.length) return;
    mutationPending = true;
    try {
      await App.MoveQuickJump(from, to);
      await load({ keepCursor: to });
    } catch (e) {
      console.error('Reordering failed:', e);
    } finally {
      mutationPending = false;
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      close();
      return;
    }

    // A number jumps straight there — the reason the order matters.
    const digit = /^Digit([1-9])$/.exec(e.code);
    if (digit && !e.ctrlKey && !e.altKey && !e.metaKey && !e.shiftKey) {
      e.preventDefault();
      jump(Number(digit[1]) - 1);
      return;
    }

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        // Alt+arrow reorders rather than navigates, so the list can be
        // arranged without leaving the keyboard.
        if (e.altKey) move(cursor, cursor + 1);
        else cursor = Math.min(cursor + 1, resolved.length - 1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        if (e.altKey) move(cursor, cursor - 1);
        else cursor = Math.max(cursor - 1, 0);
        break;
      case 'Home':
        e.preventDefault();
        cursor = 0;
        break;
      case 'End':
        e.preventDefault();
        cursor = Math.max(0, resolved.length - 1);
        break;
      case 'Enter':
        e.preventDefault();
        jump(cursor);
        break;
      case 'Delete':
        e.preventDefault();
        remove(cursor);
        break;
      case 'F2':
        e.preventDefault();
        startEditing(cursor);
        break;
      case 'Tab':
        // Tab walks the list too. The rows are not focusable — the list holds
        // focus and moves a cursor — so without this Tab left the list
        // entirely on the first press, and the arrows stopped working with no
        // sign of where focus had gone.
        e.preventDefault();
        cursor = e.shiftKey
          ? (cursor - 1 + resolved.length) % resolved.length
          : (cursor + 1) % resolved.length;
        break;
    }
  }

  /**
   * Dragging a row to reorder it.
   *
   * The same operation as Alt+↑↓, offered the way a list normally expects to
   * be rearranged. Reordering is what assigns the number keys, so it is worth
   * being reachable both ways: the keyboard for one step, the mouse for moving
   * something several places at once.
   */
  function onDragStart(e: DragEvent, index: number) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'move';
    // Firefox refuses to start a drag without payload, even unused.
    e.dataTransfer.setData('text/plain', String(index));
    draggingIndex = index;
  }

  function onDragOver(e: DragEvent, index: number) {
    if (draggingIndex === null) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    dragOverIndex = index;
  }

  async function onDrop(e: DragEvent, index: number) {
    e.preventDefault();
    const from = draggingIndex;
    draggingIndex = null;
    dragOverIndex = null;
    if (from === null || from === index) return;
    await move(from, index);
  }

  function onDragEnd() {
    draggingIndex = null;
    dragOverIndex = null;
  }

  // Keep the cursor in view when the arrows walk past the visible rows.
  $: if (listEl && cursor >= 0) scrollCursorIntoView(cursor);
  async function scrollCursorIntoView(index: number) {
    await tick();
    listEl?.querySelector(`[data-row="${index}"]`)?.scrollIntoView({ block: 'nearest' });
  }
</script>

{#if show}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div class="dialog-overlay" use:autoFocusDialog on:click|self={close} on:keydown={onKeydown} role="dialog" aria-modal="true">
    <div class="dialog-content jump-dialog">
      <div class="dialog-header">
        <h2>{$t('quickJump.title')}</h2>
        <button class="close-btn" on:click={close} aria-label={$t('common.close')}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
      <div
        class="jump-list"
        role="listbox"
        tabindex="0"
        bind:this={listEl}
      >
        {#if resolved.length === 0}
          <p class="empty">{$t('quickJump.empty')}</p>
        {:else}
          {#each resolved as row (row.sessionId + ':' + row.windowIdx)}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <div
              class="jump-row"
              class:selected={row.index === cursor}
              class:gone={row.missing}
              class:dragging={row.index === draggingIndex}
              class:drop-target={row.index === dragOverIndex && row.index !== draggingIndex}
              role="option"
              aria-selected={row.index === cursor}
              data-row={row.index}
              draggable="true"
              on:click={() => jump(row.index)}
              on:mouseenter={() => (cursor = row.index)}
              on:dragstart={(e) => onDragStart(e, row.index)}
              on:dragover={(e) => onDragOver(e, row.index)}
              on:drop={(e) => onDrop(e, row.index)}
              on:dragend={onDragEnd}
            >
              <span class="slot">{row.number ?? ''}</span>
              <!-- Busy / waiting / idle. Always rendered so every name starts
                   at the same column, and it says something a name cannot:
                   which of these is asking for you and which is still
                   working. -->
              <span
                class="activity {row.activity}"
                title={activityLabel($t, row.activity)}
              ></span>
              <!-- An icon rather than the word: "munkamenet" and "fül" are
                   different lengths, so spelling it out pushed every name to a
                   different column and the list stopped scanning as a list.
                   The distinction is still there, in fixed width. -->
              <span class="kind" title={row.isTab ? $t('quickJump.tab') : $t('quickJump.session')}>
                {#if row.isTab}
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M3 8h6l2-3h10v14H3z"/>
                  </svg>
                {:else}
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/>
                  </svg>
                {/if}
              </span>
              <!-- Hidden while this row is being edited: the field holds the
                   same name, and showing both put the old one right beside
                   what was being typed. -->
              {#if editingIndex !== row.index}
              <span class="name">
                {#if row.gradient}
                  <!-- A bare span, exactly as the sidebar does it. The
                       gradient has to clip to the text and nothing else:
                       padding, ellipsis and overflow belong on the element
                       around it, or they are painted over too. -->
                  <span style={row.gradient}>{row.name}</span>
                {:else}
                  <span style={row.style}>{row.name}</span>
                {/if}
              </span>
              {/if}
              {#if editingIndex === row.index}
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <input
                  class="note-input"
                  bind:value={editingText}
                  placeholder={$t('quickJump.namePlaceholder')}
                  on:keydown={onNoteKeydown}
                  on:blur={commitEditing}
                  on:click|stopPropagation
                />
                <!-- Mousedown, not click: blur fires first on a click and
                     would commit the edit before the reset ran. -->
                <button
                  class="reset-btn"
                  tabindex="-1"
                  title={$t('quickJump.resetName')}
                  on:mousedown|preventDefault|stopPropagation={resetEditingName}
                >
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M3 12a9 9 0 1 0 3-6.7"/><polyline points="3 4 3 9 8 9"/>
                  </svg>
                </button>
              {:else}
                <!-- Before the context, not after: the button only appears on
                     the row under the cursor, and behind the session name it
                     moved that name sideways as the cursor passed. -->
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <button
                  class="note-btn"
                  tabindex="-1"
                  title={$t('quickJump.rename')}
                  on:click|stopPropagation={() => startEditing(row.index)}
                >
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                  </svg>
                </button>
                {#if row.context}
                  <span class="context">{row.context}</span>
                {/if}
              {/if}
              {#if row.missing}
                <span class="gone-tag">{$t('quickJump.unavailable')}</span>
              {/if}
            </div>
          {/each}
        {/if}
      </div>

      <div class="dialog-footer">
        <span class="hint">{$t('quickJump.hint')}</span>
      </div>
    </div>
  </div>
{/if}

<style>
  .jump-dialog {
    /* The shared .dialog-content caps at 400px, which truncates session names
       that are mostly path. */
    width: min(680px, 92vw);
    max-width: min(680px, 92vw);
    display: flex;
    flex-direction: column;
    max-height: min(70vh, 640px);
  }


  /* Matching the settings dialog rather than inheriting whatever the shared
     overlay provides, which drew a circle here. */
  .close-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: rgba(255, 255, 255, 0.05);
    border: none;
    border-radius: 8px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
  }
  .close-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  .jump-list {
    overflow-y: auto;
    padding: 6px 0;
    outline: none;
  }

  .jump-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 18px;
    cursor: pointer;
    border-left: 3px solid transparent;
  }
  .jump-row.selected {
    background: rgba(255, 255, 255, 0.07);
    border-left-color: var(--accent, #61afef);
  }
  /* Dimmed rather than hidden: the row still holds its number, which is the
     point of keeping it. */
  .jump-row.gone {
    opacity: 0.45;
    cursor: default;
  }
  /* The row being carried, faded so the gap it will leave is readable. */
  .jump-row.dragging {
    opacity: 0.4;
  }
  /* Where it would land. A line rather than a fill: the row underneath keeps
     its own meaning, and the line says "between here" rather than "onto this". */
  .jump-row.drop-target {
    box-shadow: inset 0 2px 0 var(--accent, #61afef);
  }

  .slot {
    flex: 0 0 1.4em;
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: var(--accent, #61afef);
    font-weight: 600;
  }
  /* The activity dot, matching the sidebar's vocabulary so the two do not
     disagree about what a colour means. */
  .activity {
    flex: 0 0 auto;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #4b5263;
  }
  .activity.busy {
    background: #98c379;
  }
  .activity.waiting {
    background: #e5c07b;
  }
  .activity.idle {
    background: #4b5263;
  }
  .activity.stopped {
    background: transparent;
    border: 1px solid #4b5263;
  }

  /* The user's own words, after the name it belongs to. */
  .note {
    /* Sized to its text, like the name: the gap that separates them from the
       context is made by .context's auto margin, so neither has to grow and
       both stay together on the left. */
    flex: 0 1 auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.8;
  }

  .reset-btn {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    padding: 4px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: inherit;
    opacity: 0.6;
    cursor: pointer;
  }
  .reset-btn:hover {
    opacity: 1;
    background: rgba(255, 255, 255, 0.1);
  }

  .note-input {
    flex: 1 1 auto;
    min-width: 0;
    padding: 2px 8px;
    border: 1px solid var(--accent, #61afef);
    border-radius: 5px;
    background: rgba(0, 0, 0, 0.35);
    color: inherit;
    font: inherit;
    outline: none;
  }

  /* Only on the row under the cursor: an edit button on every row would turn a
     list meant for scanning into a column of controls. */
  .note-btn {
    flex: 0 0 auto;
    /* Takes the gap, so it sits at a fixed distance from the right edge
       whether or not the row has a context to show. */
    margin-left: auto;
    /* Kept in the layout at all times: revealed by visibility rather than
       display, so a row's contents do not shift as the cursor arrives. */
    display: flex;
    visibility: hidden;
    align-items: center;
    padding: 3px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: inherit;
    opacity: 0.55;
    cursor: pointer;
  }
  .jump-row.selected .note-btn {
    visibility: visible;
  }
  .note-btn:hover {
    opacity: 1;
    background: rgba(255, 255, 255, 0.1);
  }

  /* Which session a tab belongs to: "shell" or "claude" names a dozen tabs.
     Pushed to the right edge by the auto margin, so the name and its note stay
     together on the left however long either is. */
  .context {
    flex: 0 0 auto;
    margin-left: 8px;
    font-size: 0.78em;
    opacity: 0.5;
    max-width: 40%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Fixed width, so every name starts at the same column whatever the row is. */
  .kind {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    opacity: 0.5;
  }
  /* Layout only. The colours go on the span inside, the way the sidebar does
     it: `background-clip: text` clips to its own box, so padding and an
     ellipsis on the same element get painted over as well. */
  .name {
    flex: 0 1 auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* The chip a flat colour draws needs room to sit around the letters. A
     gradient sets no background on this span, so it costs it nothing. */
  .name > span {
    padding: 2px 7px;
    border-radius: 5px;
  }
  .gone-tag {
    flex: 0 0 auto;
    font-size: 0.72em;
    opacity: 0.7;
  }

  .empty {
    padding: 28px 18px;
    text-align: center;
    opacity: 0.6;
  }

  .dialog-footer {
    padding: 10px 18px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
  }
  .hint {
    font-size: 0.78em;
    opacity: 0.6;
  }
</style>
