<script lang="ts">
  import { tick } from 'svelte';
  import { get } from 'svelte/store';
  import {
    sessions, selectedSession, selectedSessionId, selectedWindowIdx,
    selectSession, selectWindow, restartTab
  } from '../../stores/sessions';
  import { projects, activeProjectId, otherInstancePID, selectProject } from '../../stores/projects';
  import { activities } from '../../stores/activities';
  import { tabStatuses } from '../../stores/statusLines';
  import { showDashboard, showSessionView } from '../../stores/navigation';
  import { t } from '../../i18n';

  export let show = false;

  interface PaletteItem {
    id: string;
    category: string;
    title: string;
    detail?: string;
    keywords?: string;
    icon: string;
    action: () => void | Promise<void>;
  }

  let query = '';
  let cursor = 0;
  let inputEl: HTMLInputElement | null = null;
  let busy = false;
  let actionError = '';

  function focusOnMount(node: HTMLInputElement) {
    inputEl = node;
    query = '';
    cursor = 0;
    busy = false;
    actionError = '';
    node.focus();
    const frame = requestAnimationFrame(() => node.focus());
    return {
      destroy() {
        cancelAnimationFrame(frame);
        if (inputEl === node) inputEl = null;
      }
    };
  }

  function close() {
    if (busy) return;
    show = false;
  }

  function closeAfterAction() {
    show = false;
  }

  function openView(view: 'terminal' | 'diff' | 'notes' | 'tasks') {
    if (get(selectedSessionId)) showSessionView();
    window.dispatchEvent(new CustomEvent('main-panel:set-view', { detail: { view } }));
  }

  function openNewTab() {
    showSessionView();
    window.dispatchEvent(new CustomEvent('command:new-tab'));
  }

  function requestStart() {
    window.dispatchEvent(new CustomEvent('command:start-selected'));
  }

  function requestStop() {
    window.dispatchEvent(new CustomEvent('command:stop-selected'));
  }

  function waitingTargets() {
    const result: { sessionId: string; windowIdx: number }[] = [];
    const statuses = get(tabStatuses);
    const currentActivities = get(activities);
    for (const session of get(sessions)) {
      if (session.status !== 'running') continue;
      const waitingTabs = (statuses[session.id] || []).filter(tab => tab.activity === 'waiting');
      if (waitingTabs.length > 0) {
        result.push(...waitingTabs.map(tab => ({ sessionId: session.id, windowIdx: tab.windowIdx })));
      } else if (currentActivities[session.id] === 'waiting') {
        result.push({ sessionId: session.id, windowIdx: 0 });
      }
    }
    return result;
  }

  function nextWaitingTarget() {
    const targets = waitingTargets();
    if (targets.length === 0) return null;
    const currentSession = get(selectedSessionId);
    const currentWindow = get(selectedWindowIdx);
    const currentIndex = targets.findIndex(target =>
      target.sessionId === currentSession && target.windowIdx === currentWindow
    );
    return targets[currentIndex >= 0 ? (currentIndex + 1) % targets.length : 0];
  }

  function jumpToNextWaiting() {
    const target = nextWaitingTarget();
    if (!target) return;
    selectSession(target.sessionId);
    selectWindow(target.windowIdx);
    openView('terminal');
  }

  function buildItems(_reactiveStores: unknown[]): PaletteItem[] {
    const result: PaletteItem[] = [];
    const current = get(selectedSession);
    const currentWindow = get(selectedWindowIdx);
    const waitingTarget = nextWaitingTarget();
    const projectWritable = get(otherInstancePID) === 0;
    const currentTabStopped = current
      ? currentWindow === 0
        ? current.mainWindowStopped
        : !!current.followedWindows?.find(tab => tab.index === currentWindow)?.stopped
      : false;

    if (current) {
      result.push(
        { id: 'view-terminal', category: $t('palette.actions'), title: $t('palette.openTerminal'), icon: '›_', keywords: 'terminal', action: () => openView('terminal') },
        { id: 'view-diff', category: $t('palette.actions'), title: $t('palette.openDiff'), icon: '±', keywords: 'git changes', action: () => openView('diff') },
        { id: 'view-notes', category: $t('palette.actions'), title: $t('palette.openNotes'), icon: '⌑', keywords: 'notes jegyzet', action: () => openView('notes') }
      );
    }

    if (projectWritable && current?.status === 'running') {
      result.push(
        { id: 'new-tab', category: $t('palette.actions'), title: $t('palette.newTab'), detail: current.name, icon: '+', action: openNewTab },
        { id: 'stop-session', category: $t('palette.actions'), title: $t('palette.stopSession'), detail: current.name, icon: '■',
          action: requestStop }
      );
      if (currentTabStopped) {
        result.push({
          id: 'restart-tab', category: $t('palette.actions'), title: $t('palette.restartTab'), detail: current.name, icon: '↻',
          action: () => restartTab(current.id, currentWindow)
        });
      }
    } else if (projectWritable && current) {
      result.push({
        id: 'start-session', category: $t('palette.actions'), title: $t('palette.startSession'), detail: current.name, icon: '▶',
        action: requestStart
      });
    }

    if (waitingTarget) {
      result.push({
        id: 'next-waiting', category: $t('palette.actions'), title: $t('palette.nextWaiting'),
        detail: get(sessions).find(s => s.id === waitingTarget.sessionId)?.name || '', icon: '⏳',
        action: jumpToNextWaiting
      });
    }

    const projectItems = [
      { id: '', name: $t('project.default') },
      ...get(projects)
    ];
    for (const project of projectItems) {
      result.push({
        id: `project:${project.id}`,
        category: $t('palette.projects'),
        title: project.name,
        detail: project.id === get(activeProjectId) ? $t('palette.current') : '',
        keywords: `project ${project.name}`,
        icon: '◆',
        action: async () => {
          await selectProject(project.id);
          showDashboard();
        }
      });
    }

    for (const session of get(sessions)) {
      result.push({
        id: `session:${session.id}`,
        category: $t('palette.sessions'),
        title: session.name,
        detail: `${session.agent} · ${session.status}`,
        keywords: `${session.path} ${session.agent} ${session.notes || ''}`,
        icon: session.status === 'running' ? '●' : '○',
        action: () => { selectSession(session.id); openView('terminal'); }
      });
      result.push({
        id: `tab:${session.id}:0`,
        category: $t('palette.tabs'),
        title: `${session.name} › ${session.name}`,
        detail: session.agent,
        keywords: `tab ${session.path}`,
        icon: '▤',
        action: () => { selectSession(session.id); selectWindow(0); openView('terminal'); }
      });
      for (const tab of session.followedWindows || []) {
        result.push({
          id: `tab:${session.id}:${tab.index}`,
          category: $t('palette.tabs'),
          title: `${session.name} › ${tab.name || tab.agent}`,
          detail: tab.agent || '',
          keywords: `tab ${tab.notes || ''} ${tab.work_dir || ''}`,
          icon: '▤',
          action: () => { selectSession(session.id); selectWindow(tab.index); openView('terminal'); }
        });
      }
    }
    return result;
  }

  $: allItems = show ? buildItems([
    $sessions, $projects, $activities, $tabStatuses,
    $selectedSessionId, $selectedWindowIdx, $activeProjectId, $otherInstancePID
  ]) : [];
  $: normalizedQuery = query.trim().toLocaleLowerCase();
  $: filteredItems = normalizedQuery
    ? allItems.filter(item => `${item.title} ${item.detail || ''} ${item.category} ${item.keywords || ''}`.toLocaleLowerCase().includes(normalizedQuery))
    : allItems;
  $: if (cursor >= filteredItems.length) cursor = Math.max(0, filteredItems.length - 1);
  $: if (show && filteredItems.length > 0) {
    cursor;
    void tick().then(() => document.getElementById(`palette-item-${cursor}`)?.scrollIntoView({ block: 'nearest' }));
  }

  async function execute(item: PaletteItem | undefined) {
    if (!item || busy) return;
    busy = true;
    actionError = '';
    try {
      await item.action();
      closeAfterAction();
    } catch (e) {
      console.error('Command palette action failed:', e);
      actionError = String(e);
      void tick().then(() => inputEl?.focus());
    } finally {
      busy = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.isComposing) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      if (!busy) close();
    } else if (e.key === 'Tab') {
      e.preventDefault();
      inputEl?.focus();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (busy) return;
      cursor = filteredItems.length ? (cursor + 1) % filteredItems.length : 0;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (busy) return;
      cursor = filteredItems.length ? (cursor - 1 + filteredItems.length) % filteredItems.length : 0;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      void execute(filteredItems[cursor]);
    }
    e.stopPropagation();
  }
</script>

{#if show}
  <div class="dialog-overlay palette-overlay" on:click|self={close} role="presentation">
    <div class="command-palette" role="dialog" aria-modal="true" aria-label={$t('palette.title')} on:keydown={handleKeydown}>
      <div class="palette-search">
        <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/>
        </svg>
        <input
          bind:this={inputEl}
          use:focusOnMount
          bind:value={query}
          on:input={() => cursor = 0}
          placeholder={$t('palette.placeholder')}
          aria-label={$t('palette.placeholder')}
          disabled={busy}
        />
        <kbd class="shortcut-hint">Ctrl K</kbd>
        <kbd>Esc</kbd>
      </div>

      {#if actionError}
        <div class="palette-error" role="alert">{actionError}</div>
      {/if}

      <div class="palette-results">
        {#if filteredItems.length === 0}
          <div class="palette-empty">{$t('palette.noResults')}</div>
        {:else}
          {#each filteredItems as item, index (item.id)}
            <button
              id="palette-item-{index}"
              class:active={index === cursor}
              on:mouseenter={() => cursor = index}
              on:click={() => execute(item)}
              disabled={busy}
            >
              <span class="item-icon">{item.icon}</span>
              <span class="item-main">
                <strong>{item.title}</strong>
                {#if item.detail}<small>{item.detail}</small>{/if}
              </span>
              <span class="item-category">{item.category}</span>
            </button>
          {/each}
        {/if}
      </div>

      <div class="palette-footer">
        <span><kbd>↑</kbd><kbd>↓</kbd> {$t('palette.navigate')}</span>
        <span><kbd>Enter</kbd> {$t('palette.run')}</span>
      </div>
    </div>
  </div>
{/if}

<style>
  .palette-overlay { position:fixed; inset:0; z-index:12000; display:flex; align-items:flex-start; justify-content:center; padding-top:min(14vh,120px); background:rgba(0,0,0,.42); backdrop-filter:blur(3px); }
  .command-palette { width:min(680px,calc(100vw - 32px)); max-height:min(620px,calc(100vh - 80px)); display:flex; flex-direction:column; overflow:hidden; border:1px solid rgba(139,92,246,.32); border-radius:12px; background:rgba(17,17,27,.985); box-shadow:0 24px 80px rgba(0,0,0,.55),0 0 35px rgba(109,40,217,.12); }
  .palette-search { display:flex; align-items:center; gap:10px; padding:14px 15px; border-bottom:1px solid rgba(255,255,255,.07); color:#8b5cf6; }
  .palette-search input { flex:1; border:0; background:transparent; color:#f4f4f5; font-size:15px; }
  .palette-search input:disabled { opacity:.65; }
  .palette-search input::placeholder { color:#52525b; }
  .shortcut-hint { color:#a78bfa; }
  kbd { display:inline-grid; place-items:center; min-width:22px; height:20px; padding:0 5px; border:1px solid rgba(255,255,255,.1); border-radius:4px; background:rgba(255,255,255,.045); color:#71717a; font:10px ui-monospace,monospace; }
  .palette-results { flex:1; min-height:100px; overflow:auto; padding:6px; }
  .palette-results button { width:100%; display:flex; align-items:center; gap:10px; padding:9px 10px; border:0; border-radius:7px; background:transparent; color:#a1a1aa; text-align:left; cursor:pointer; }
  .palette-results button:disabled { cursor:wait; opacity:.65; }
  .palette-results button.active { background:linear-gradient(90deg,rgba(139,92,246,.17),rgba(99,102,241,.08)); color:#e4e4e7; }
  .item-icon { width:24px; color:#a78bfa; text-align:center; font:13px ui-monospace,monospace; }
  .item-main { min-width:0; flex:1; display:flex; align-items:baseline; gap:8px; }
  .item-main strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:12px; font-weight:500; }
  .item-main small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; color:#60606b; font-size:10px; }
  .item-category { color:#52525b; font-size:9px; text-transform:uppercase; letter-spacing:.06em; }
  .palette-empty { display:grid; place-items:center; min-height:150px; color:#52525b; font-size:12px; }
  .palette-error { padding:8px 15px; border-bottom:1px solid rgba(248,113,113,.18); background:rgba(127,29,29,.2); color:#fca5a5; font-size:11px; }
  .palette-footer { display:flex; justify-content:flex-end; gap:16px; padding:7px 11px; border-top:1px solid rgba(255,255,255,.06); color:#52525b; font-size:9px; }
  .palette-footer span { display:flex; align-items:center; gap:4px; }
</style>
