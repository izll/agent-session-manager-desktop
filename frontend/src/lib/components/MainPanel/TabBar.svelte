<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher, tick } from 'svelte';
  import AgentIcon from '../common/AgentIcon.svelte';
  import NewTabDialog from '../Dialogs/NewTabDialog.svelte';
  import TabColorDialog from '../Dialogs/TabColorDialog.svelte';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import Toast from '../common/Toast.svelte';
  import { sessions, selectedSessionId, selectedWindowIdx, selectWindow, selectedSession, startSession, stopSession, stopTab, restartTab, deleteSession, deleteTab, toggleFavorite, renameTab, reorderTab } from '../../stores/sessions';
  import { agents } from '../../stores/agents';
  import { get } from 'svelte/store';
  import { t } from '../../i18n';
  import { focusTerminal } from '../../utils/focus';
  import { tabStatuses } from '../../stores/statusLines';
  import StatusIndicator from '../common/StatusIndicator.svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import * as DictationService from '../../../../wailsjs/go/main/DictationService';
  import { EventsOn, EventsOff } from '../../../../wailsjs/runtime/runtime';
  import type { session } from '../../../../wailsjs/go/models';

  interface TabColorTarget {
    Index: number;
    Name: string;
    TextColor?: string;
    BackgroundColor?: string;
  }

  // Dictation state
  export let dictationEnabled = false;
  export let dictationListening = false;
  let voiceLevel = 0;
  let interimText = '';
  let bufferMode = false;
  let streamingMode = false;
  let bufferCloseOnSend = true;
  let bufferText = '';
  let bufferEditor: HTMLDivElement;
  let bufferPanel: HTMLDivElement;
  let lastGoText = '';
  let syncTimeout: ReturnType<typeof setTimeout> | null = null;

  function onEditorInput() {
    // Read confirmed text (exclude interim span)
    if (!bufferEditor) return;
    const span = bufferEditor.querySelector('.interim-span');
    const savedInterim = span?.textContent || '';
    if (span) span.remove();
    bufferText = bufferEditor.textContent || '';
    // Re-add interim span
    if (savedInterim) appendInterimSpan(savedInterim);
    // Sync back to Go
    if (syncTimeout) clearTimeout(syncTimeout);
    syncTimeout = setTimeout(async () => {
      try {
        await DictationService.SetBufferText(bufferText);
        lastGoText = bufferText;
      } catch (_) {}
    }, 100);
  }

  function appendInterimSpan(text: string) {
    if (!bufferEditor) return;
    let span = bufferEditor.querySelector('.interim-span');
    if (text) {
      if (!span) {
        span = document.createElement('span');
        span.className = 'interim-span';
        bufferEditor.appendChild(span);
      }
      span.textContent = text;
    } else if (span) {
      span.remove();
    }
  }

  function updateEditorDisplay() {
    if (!bufferEditor) return;
    bufferEditor.textContent = bufferText;
    appendInterimSpan(interimText);
    // Place cursor at end of confirmed text (before interim span)
    const sel = window.getSelection();
    if (sel && bufferEditor.firstChild?.nodeType === Node.TEXT_NODE) {
      const range = document.createRange();
      range.setStart(bufferEditor.firstChild, bufferEditor.firstChild.textContent?.length || 0);
      range.collapse(true);
      sel.removeAllRanges();
      sel.addRange(range);
    }
  }

  // Buffer panel position & size (persists across show/hide while component alive)
  let bufferPanelX: number | null = null;
  let bufferPanelY: number | null = null;
  let bufferPanelW: number | null = null;
  let bufferPanelH: number | null = null;

  // Drag state
  let isDragging = false;
  let dragOffsetX = 0;
  let dragOffsetY = 0;

  function onHeaderMousedown(e: MouseEvent) {
    if ((e.target as HTMLElement).closest('.buffer-close')) return;
    isDragging = true;
    const rect = bufferPanel.getBoundingClientRect();
    dragOffsetX = e.clientX - rect.left;
    dragOffsetY = e.clientY - rect.top;
    if (bufferPanelX === null) {
      bufferPanelX = rect.left;
      bufferPanelY = rect.top;
      bufferPanelW = rect.width;
      bufferPanelH = rect.height;
    }
    document.addEventListener('mousemove', onDragMove);
    document.addEventListener('mouseup', onDragEnd);
    e.preventDefault();
  }

  function onDragMove(e: MouseEvent) {
    if (!isDragging) return;
    bufferPanelX = e.clientX - dragOffsetX;
    bufferPanelY = e.clientY - dragOffsetY;
  }

  function onDragEnd() {
    isDragging = false;
    document.removeEventListener('mousemove', onDragMove);
    document.removeEventListener('mouseup', onDragEnd);
  }

  // Resize state
  let isResizing = false;
  let resizeStartX = 0;
  let resizeStartY = 0;
  let resizeStartW = 0;
  let resizeStartH = 0;
  let resizeStartLeft = 0;
  let resizeStartTop = 0;
  let resizeDir = ''; // e.g. 'n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'

  function onEdgeMousedown(dir: string) {
    return (e: MouseEvent) => {
      isResizing = true;
      resizeDir = dir;
      const rect = bufferPanel.getBoundingClientRect();
      resizeStartX = e.clientX;
      resizeStartY = e.clientY;
      resizeStartW = rect.width;
      resizeStartH = rect.height;
      resizeStartLeft = rect.left;
      resizeStartTop = rect.top;
      if (bufferPanelX === null) {
        bufferPanelX = rect.left;
        bufferPanelY = rect.top;
      }
      bufferPanelW = rect.width;
      bufferPanelH = rect.height;
      document.addEventListener('mousemove', onResizeMove);
      document.addEventListener('mouseup', onResizeEnd);
      e.preventDefault();
    };
  }

  function onResizeMove(e: MouseEvent) {
    if (!isResizing) return;
    const dx = e.clientX - resizeStartX;
    const dy = e.clientY - resizeStartY;

    if (resizeDir.includes('e')) {
      bufferPanelW = Math.max(300, resizeStartW + dx);
    }
    if (resizeDir.includes('s')) {
      bufferPanelH = Math.max(150, resizeStartH + dy);
    }
    if (resizeDir.includes('w')) {
      const newW = Math.max(300, resizeStartW - dx);
      bufferPanelX = resizeStartLeft + (resizeStartW - newW);
      bufferPanelW = newW;
    }
    if (resizeDir.includes('n')) {
      const newH = Math.max(150, resizeStartH - dy);
      bufferPanelY = resizeStartTop + (resizeStartH - newH);
      bufferPanelH = newH;
    }
  }

  function onResizeEnd() {
    isResizing = false;
    resizeDir = '';
    document.removeEventListener('mousemove', onResizeMove);
    document.removeEventListener('mouseup', onResizeEnd);
  }

  $: bufferPanelStyle = bufferPanelX !== null
    ? `left: ${bufferPanelX}px; top: ${bufferPanelY}px; transform: none;` +
      (bufferPanelW ? ` width: ${bufferPanelW}px;` : '') +
      (bufferPanelH ? ` height: ${bufferPanelH}px;` : '')
    : '';

  const dispatch = createEventDispatcher();

  let windows: session.WindowInfo[] = [];
  let lastSessionId: string | null = null;
  let pollTimeout: ReturnType<typeof setTimeout> | null = null;
  let windowsLoadGeneration = 0;
  let showNewTabDialog = false;
  let showDeleteConfirm = false;
  let showDeleteTabConfirm = false;
  let deleteTabIndex: number | null = null;
  let showExtraArgsEditor = false;
  let showTabColorDialog = false;
  let tabColorTarget: TabColorTarget | null = null;
  let extraArgsValue = '';
  let extraArgsWindowIdx: number | null = null;
  let showErrorToast = false;
  let errorMessage = '';

  // Restore terminal focus when TabBar-local dialogs close
  let prevTabBarDialogOpen = false;
  $: {
    const open = showNewTabDialog || showDeleteConfirm || showDeleteTabConfirm || showExtraArgsEditor || showTabColorDialog;
    if (prevTabBarDialogOpen && !open) {
      focusTerminal();
    }
    prevTabBarDialogOpen = open;
  }

  // Dictation event listeners
  let dictationCleanup: (() => void) | null = null;

  // Ctrl+PageUp/PageDown to switch window tabs
  function handleWindowTabKeydown(e: KeyboardEvent) {
    if (!e.ctrlKey || e.shiftKey || e.altKey || e.metaKey) return;
    if (e.key !== 'PageUp' && e.key !== 'PageDown') return;
    if (windows.length <= 1) return;
    if (document.querySelector('.dialog-overlay')) return;

    e.preventDefault();
    e.stopPropagation();

    const currentIdx = windows.findIndex(w => w.Index === $selectedWindowIdx);
    if (currentIdx === -1) return;

    let newIdx: number;
    if (e.key === 'PageDown') {
      newIdx = (currentIdx + 1) % windows.length;
    } else {
      newIdx = (currentIdx - 1 + windows.length) % windows.length;
    }
    selectWindow(windows[newIdx].Index);
  }

  onMount(async () => {
    window.addEventListener('keydown', handleWindowTabKeydown, true);
    window.addEventListener('click', handleTabContextWindowClick);

    // Get initial dictation state
    try {
      const settings = await DictationService.GetDictationSettings();
      dictationEnabled = settings.enabled;
      bufferMode = settings.bufferMode && settings.mode === 'streaming';
      streamingMode = settings.mode === 'streaming';
      bufferCloseOnSend = settings.bufferCloseOnSend !== false;
    } catch (e) {
      console.error('[Dictation] Failed to get settings:', e);
    }

    // Listen for dictation state changes (App.svelte uses 'dictation:state')
    const unsubState = EventsOn('dictation:state', (listening: boolean) => {
      console.log('[Buffer] State change - listening:', listening, 'bufferMode:', bufferMode);
      dictationListening = listening;
      if (listening) {
        startVoiceLevelPoll();
        if (notesFieldCleanup) {
          // Notes field dictation active - focus notes textarea
          tick().then(() => {
            const notesTextarea = document.querySelector('.notes-textarea') as HTMLTextAreaElement;
            notesTextarea?.focus();
          });
        } else if (bufferMode) {
          console.log('[Buffer] Starting buffer text poll');
          startBufferTextPoll();
          tick().then(() => bufferEditor?.focus());
        } else if (streamingMode) {
          // Live preview mode: return focus to terminal
          tick().then(() => {
            const xtermTextarea = document.querySelector('.xterm-helper-textarea') as HTMLTextAreaElement;
            xtermTextarea?.focus();
          });
        }
      } else {
        stopVoiceLevelPoll();
        stopBufferTextPoll();
        cleanupNotesField();
      }
    });

    // Poll voice level via bound method (Wails events unreliable at high frequency)
    let voiceLevelPollId: ReturnType<typeof setInterval> | null = null;
    function startVoiceLevelPoll() {
      if (voiceLevelPollId) return;
      voiceLevelPollId = setInterval(async () => {
        if (!dictationListening) return;
        try {
          const level = await DictationService.GetVoiceLevel();
          voiceLevel = level;
        } catch (_) {}
      }, 80);
    }
    function stopVoiceLevelPoll() {
      if (voiceLevelPollId) {
        clearInterval(voiceLevelPollId);
        voiceLevelPollId = null;
      }
      voiceLevel = 0;
    }

    // Poll buffer text via bound method
    let bufferTextPollId: ReturnType<typeof setInterval> | null = null;

    function startBufferTextPoll() {
      if (bufferTextPollId) return;
      bufferTextPollId = setInterval(async () => {
        if (!dictationListening || !bufferMode) return;
        try {
          const text = await DictationService.GetBufferText();
          if (text !== lastGoText) {
            lastGoText = text;
            bufferText = text;
            updateEditorDisplay();
          }
        } catch (_) {}
      }, 150);
    }
    function stopBufferTextPoll() {
      if (bufferTextPollId) {
        clearInterval(bufferTextPollId);
        bufferTextPollId = null;
      }
    }

    // Listen for dictation enabled changes from settings dialog
    const unsubEnabled = EventsOn('dictation:enabledChange', (enabled: boolean) => {
      dictationEnabled = enabled;
    });

    // Listen for interim text from streaming recognizer
    const unsubInterim = EventsOn('dictation:interimText', (text: string) => {
      interimText = text || '';
      if (streamingMode && dictationListening) {
        appendInterimSpan(interimText);
      }
    });

    // Listen for settings changes (buffer mode toggle)
    const unsubSettings = EventsOn('dictation:settingsChanged', async () => {
      try {
        const settings = await DictationService.GetDictationSettings();
        bufferMode = settings.bufferMode && settings.mode === 'streaming';
        streamingMode = settings.mode === 'streaming';
        bufferCloseOnSend = settings.bufferCloseOnSend !== false;
      } catch (_) {}
    });

    // Window-level Ctrl+S / Ctrl+Enter handler as fallback for contenteditable issues in WebKit
    function windowKeydownHandler(e: KeyboardEvent) {
      if (!bufferMode || !dictationListening) return;
      const bufferVisible = document.querySelector('.dictation-buffer');
      if (!bufferVisible) return;
      // Ctrl+S always works reliably in WebKit
      if ((e.key === 's' || e.key === 'S') && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        sendBuffer();
        return;
      }
      // Ctrl+Enter as fallback
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        sendBuffer();
      }
    }
    window.addEventListener('keydown', windowKeydownHandler, true); // capture phase

    dictationCleanup = () => {
      unsubState();
      stopVoiceLevelPoll();
      stopBufferTextPoll();
      unsubEnabled();
      unsubInterim();
      unsubSettings();
      window.removeEventListener('keydown', windowKeydownHandler, true);
    };
  });

  onDestroy(() => {
    window.removeEventListener('keydown', handleWindowTabKeydown, true);
    window.removeEventListener('click', handleTabContextWindowClick);
    stopPolling();
    if (dictationCleanup) {
      dictationCleanup();
    }
  });

  // Notes field dictation routing
  let notesFieldCleanup: (() => void) | null = null;

  function setupNotesFieldListeners() {
    const unsubText = EventsOn('dictation:fieldText', (text: string) => {
      const el = document.querySelector('.notes-textarea') as HTMLTextAreaElement;
      if (el) {
        const start = el.selectionStart ?? el.value.length;
        const end = el.selectionEnd ?? el.value.length;
        el.value = el.value.substring(0, start) + text + el.value.substring(end);
        el.selectionStart = el.selectionEnd = start + text.length;
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
    });
    const unsubDelete = EventsOn('dictation:fieldDelete', (count: number) => {
      const el = document.querySelector('.notes-textarea') as HTMLTextAreaElement;
      if (el && count > 0) {
        const start = el.selectionStart ?? el.value.length;
        const deleteFrom = Math.max(0, start - count);
        el.value = el.value.substring(0, deleteFrom) + el.value.substring(start);
        el.selectionStart = el.selectionEnd = deleteFrom;
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
    });
    notesFieldCleanup = () => {
      unsubText();
      unsubDelete();
    };
  }

  function cleanupNotesField() {
    if (notesFieldCleanup) {
      notesFieldCleanup();
      notesFieldCleanup = null;
      DictationService.SetDictationTarget('terminal').catch(() => {});
    }
  }

  async function toggleDictation() {
    if (!dictationEnabled) return;
    try {
      // If starting dictation while notes view is active, target the notes field
      if (!dictationListening && activeView === 'notes') {
        await DictationService.SetDictationTarget('field');
        setupNotesFieldListeners();
      }
      await DictationService.ToggleDictation();
    } catch (e) {
      cleanupNotesField();
      console.error('[Dictation] Toggle failed:', e);
      errorMessage = `Dictation error: ${e}`;
      showErrorToast = true;
    }
  }

  async function sendBuffer() {
    if (!bufferText.trim()) return;
    try {
      // Use App.SendPrompt which mirrors the TUI approach:
      // 1. tmux send-keys -l (literal text)
      // 2. 50ms delay
      // 3. tmux send-keys Enter (separate key event)
      // Direct PTY write of text+\r doesn't trigger readline submit.
      const sid = get(selectedSessionId);
      if (sid) {
        await App.SendPrompt(sid, bufferText);
      }
      await DictationService.ClearBuffer();
      bufferText = '';
      lastGoText = '';
      if (bufferEditor) bufferEditor.textContent = '';
      // Close buffer window and stop dictation if configured
      if (bufferCloseOnSend) {
        dictationListening = false;
        await DictationService.ToggleDictation();
        // Return focus to terminal
        tick().then(() => {
          const xtermTextarea = document.querySelector('.xterm-helper-textarea') as HTMLTextAreaElement;
          xtermTextarea?.focus();
        });
      }
    } catch (e) {
      console.error('[Dictation] Send buffer failed:', e);
    }
  }

  async function clearBuffer() {
    try {
      await DictationService.ClearBuffer();
      bufferText = '';
      lastGoText = '';
      if (bufferEditor) bufferEditor.textContent = '';
    } catch (e) {
      console.error('[Dictation] Clear buffer failed:', e);
    }
  }

  // Track if Ctrl is held (WebKit contenteditable may not always report ctrlKey on Enter)
  let ctrlHeld = false;

  function handleBufferKeydown(e: KeyboardEvent) {
    if (e.key === 'Control') {
      ctrlHeld = true;
    }
    // Ctrl+S to send
    if ((e.key === 's' || e.key === 'S') && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      e.stopPropagation();
      sendBuffer();
      return;
    }
    // Ctrl+Enter to send
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey || ctrlHeld)) {
      e.preventDefault();
      e.stopPropagation();
      sendBuffer();
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      clearBuffer();
    }
  }

  function handleBufferKeyup(e: KeyboardEvent) {
    if (e.key === 'Control') {
      ctrlHeld = false;
    }
  }

  // WebKit contenteditable: Ctrl+Enter may arrive as beforeinput "insertParagraph"
  // instead of a proper keydown with ctrlKey. Catch it here.
  function handleBeforeInput(e: InputEvent) {
    if (e.inputType === 'insertParagraph' && ctrlHeld) {
      e.preventDefault();
      sendBuffer();
    }
  }

  // Sort windows by custom tab order
  function sortWindowsByTabOrder(wins: any[], tabOrder: number[] | undefined): any[] {
    if (!tabOrder || tabOrder.length === 0) return wins;
    const indexMap = new Map<number, number>();
    tabOrder.forEach((winIdx, pos) => indexMap.set(winIdx, pos));
    return [...wins].sort((a, b) => {
      const posA = indexMap.has(a.Index) ? indexMap.get(a.Index)! : 9999;
      const posB = indexMap.has(b.Index) ? indexMap.get(b.Index)! : 9999;
      return posA - posB;
    });
  }

  // Load windows when session changes or status changes
  async function loadWindowsForSession(sessionId: string | null, _status?: string) {
    const generation = ++windowsLoadGeneration;
    if (!sessionId) {
      windows = [];
      stopPolling();
      return;
    }

    // Check if session is running
    const sess = get(sessions).find(s => s.id === sessionId);
    if (!sess) {
      windows = [];
      stopPolling();
      return;
    }

    // If session is not running, show stored followedWindows as tabs
    if (sess.status !== 'running') {
      stopPolling();
      // Always show main tab (index 0) plus any followedWindows
      const mainTab = {
        Index: 0,
        Name: sess.name,
        Agent: sess.agent,
        Dead: false,
        Active: true,
        Followed: false,
        TextColor: sess.tabTextColor || '',
        BackgroundColor: sess.tabBackgroundColor || ''
      };

      if (sess.followedWindows && sess.followedWindows.length > 0) {
        // Convert followedWindows to window format for display
        const followedTabs = sess.followedWindows.map((fw: any) => ({
          Index: fw.index,
          Name: fw.name || `Tab ${fw.index}`,
          Agent: fw.agent || sess.agent,
          Dead: false,
          Active: false,
          Followed: true,
          TextColor: fw.text_color || '',
          BackgroundColor: fw.background_color || ''
        }));
        windows = sortWindowsByTabOrder([mainTab, ...followedTabs], sess.tabOrder);
      } else {
        windows = [mainTab];
      }
      lastSessionId = sessionId;
      return;
    }

    // Small delay when session just became running to let tmux windows initialize
    const wasRunningBefore = lastSessionId === sessionId && windows.length > 0;
    if (!wasRunningBefore) {
      await new Promise(r => setTimeout(r, 300));
      if (generation !== windowsLoadGeneration || sessionId !== get(selectedSessionId)) return;
    }

    try {
      const list = await App.GetWindowList(sessionId);
      if (generation !== windowsLoadGeneration || sessionId !== get(selectedSessionId)) return;
      windows = sortWindowsByTabOrder(list || [], sess.tabOrder);

      // Start polling if not already
      if (!pollTimeout) {
        startPolling();
      }
    } catch (e) {
      if (generation !== windowsLoadGeneration || sessionId !== get(selectedSessionId)) return;
      console.error('Failed to load windows:', e);
      windows = [];
    }

    lastSessionId = sessionId;
  }

  function startPolling() {
    if (pollTimeout) return;
    pollTimeout = setTimeout(async () => {
      pollTimeout = null;
      const sessionId = get(selectedSessionId);
      if (sessionId) {
        await loadWindowsForSession(sessionId);
      }
      if (get(selectedSessionId)) startPolling();
    }, 5000); // 5 seconds to reduce CPU usage
  }

  function stopPolling() {
    windowsLoadGeneration++;
    if (pollTimeout) {
      clearTimeout(pollTimeout);
      pollTimeout = null;
    }
  }

  // React to session changes AND status changes
  $: currentSessionStatus = $sessions.find(s => s.id === $selectedSessionId)?.status;
  $: loadWindowsForSession($selectedSessionId, currentSessionStatus);

  // Per-tab activity (busy/waiting/idle) for the current session, so each tab
  // header shows its own status dot — you can see at a glance WHICH tab is
  // waiting/working without opening it. Keyed by window index.
  $: tabActivityByIdx = (() => {
    const map: Record<number, 'idle' | 'busy' | 'waiting'> = {};
    const list = $selectedSessionId ? $tabStatuses[$selectedSessionId] : undefined;
    if (list) {
      for (const ts of list) map[ts.windowIdx] = ts.activity;
    }
    return map;
  })();

  // Update active tmux session for dictation text output
  $: if ($selectedSessionId && dictationEnabled) {
    DictationService.SetActiveTmuxSession($selectedSessionId, $selectedWindowIdx ?? 0);
  }

  // Tab rename state
  let renamingTabIndex: number | null = null;
  let tabRenameValue = '';
  let tabRenameInput: HTMLInputElement;

  // Tab context menu state
  let showTabContextMenu = false;
  let tabContextMenuX = 0;
  let tabContextMenuY = 0;
  let tabContextMenuIndex: number | null = null;
  let tabContextMenuName = '';

  function handleTabClick(index: number) {
    selectWindow(index);
  }

  // Tab drag & drop reordering
  let draggingTabIndex: number | null = null;
  let dragOverTabIndex: number | null = null;
  let droppedTabWindowIdx: number | null = null;

  function handleTabDragStart(e: DragEvent, arrayIdx: number) {
    draggingTabIndex = arrayIdx;
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', String(arrayIdx));
    }
  }

  function handleTabDragEnd() {
    draggingTabIndex = null;
    dragOverTabIndex = null;
  }

  function handleTabDragOver(e: DragEvent, arrayIdx: number) {
    if (draggingTabIndex === null) return;
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    dragOverTabIndex = arrayIdx;
  }

  function handleTabDragLeave(e: DragEvent, arrayIdx: number) {
    if (dragOverTabIndex === arrayIdx) {
      dragOverTabIndex = null;
    }
  }

  async function handleTabDrop(e: DragEvent, arrayIdx: number) {
    e.preventDefault();
    if (draggingTabIndex === null || draggingTabIndex === arrayIdx) {
      draggingTabIndex = null;
      dragOverTabIndex = null;
      return;
    }
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    const fromPos = draggingTabIndex;
    const toPos = arrayIdx;
    // Remember which window was dragged for flash animation
    const draggedWinIdx = windows[fromPos]?.Index;

    draggingTabIndex = null;
    dragOverTabIndex = null;

    try {
      await reorderTab(sessionId, fromPos, toPos);
      // Trigger flash on the moved tab
      droppedTabWindowIdx = draggedWinIdx;
      setTimeout(() => { droppedTabWindowIdx = null; }, 500);
    } catch (err) {
      console.error('Failed to reorder tab:', err);
    }
  }

  function handleTabContextMenu(e: MouseEvent, index: number, name: string) {
    e.preventDefault();
    e.stopPropagation();
    tabContextMenuX = e.clientX;
    tabContextMenuY = e.clientY;
    tabContextMenuIndex = index;
    tabContextMenuName = name;
    showTabContextMenu = true;
  }

  function closeTabContextMenu() {
    showTabContextMenu = false;
    tabContextMenuIndex = null;
  }

  function handleTabContextWindowClick() {
    if (showTabContextMenu) {
      closeTabContextMenu();
    }
  }

  function tabContextRename() {
    if (tabContextMenuIndex !== null) {
      startTabRename(tabContextMenuIndex, tabContextMenuName);
    }
    closeTabContextMenu();
  }

  function tabContextSetColor() {
    if (tabContextMenuIndex !== null && $selectedSessionId) {
      const win = windows.find(w => w.Index === tabContextMenuIndex);
      if (win) {
        tabColorTarget = {
          Index: win.Index,
          Name: win.Name,
          TextColor: win.TextColor || '',
          BackgroundColor: win.BackgroundColor || ''
        };
        showTabColorDialog = true;
      }
    }
    closeTabContextMenu();
  }

  function handleTabColorApplied(event: CustomEvent<{ index: number; textColor: string; backgroundColor: string }>) {
    const { index, textColor, backgroundColor } = event.detail;
    windows = windows.map(win => win.Index === index
      ? { ...win, TextColor: textColor, BackgroundColor: backgroundColor }
      : win);

    // Keep the stopped-session representation in sync without waiting for the
    // next full reload. The backend remains the source of truth.
    sessions.update(items => items.map(sess => {
      if (sess.id !== $selectedSessionId) return sess;
      if (index === 0) {
        return { ...sess, tabTextColor: textColor, tabBackgroundColor: backgroundColor };
      }
      return {
        ...sess,
        followedWindows: (sess.followedWindows || []).map((fw: any) => fw.index === index
          ? { ...fw, text_color: textColor, background_color: backgroundColor }
          : fw)
      };
    }));
    tabColorTarget = null;
  }

  function tabStyle(win: session.WindowInfo): string {
    const styles: string[] = [];
    if (win.BackgroundColor) styles.push(`background: ${win.BackgroundColor}`);
    if (win.TextColor === 'auto' && win.BackgroundColor) {
      const hex = win.BackgroundColor.slice(1);
      const normalized = hex.length === 3 ? hex.split('').map(c => c + c).join('') : hex.slice(0, 6);
      if (/^[0-9a-fA-F]{6}$/.test(normalized)) {
        const r = parseInt(normalized.slice(0, 2), 16);
        const g = parseInt(normalized.slice(2, 4), 16);
        const b = parseInt(normalized.slice(4, 6), 16);
        styles.push(`color: ${(0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.55 ? '#111111' : '#FFFFFF'}`);
      }
    } else if (win.TextColor) {
      styles.push(`color: ${win.TextColor}`);
    }
    return styles.join('; ');
  }

  async function tabContextStop() {
    if (tabContextMenuIndex !== null && $selectedSessionId) {
      await stopTab($selectedSessionId, tabContextMenuIndex);
    }
    closeTabContextMenu();
  }

  async function tabContextDelete() {
    if (tabContextMenuIndex !== null && tabContextMenuIndex !== 0 && $selectedSessionId) {
      deleteTabIndex = tabContextMenuIndex;
      showDeleteTabConfirm = true;
    }
    closeTabContextMenu();
  }

  async function tabContextEditExtraArgs() {
    if (tabContextMenuIndex !== null && $selectedSessionId) {
      try {
        const args = await App.GetExtraArgs($selectedSessionId, tabContextMenuIndex);
        extraArgsValue = args || '';
        extraArgsWindowIdx = tabContextMenuIndex;
        showExtraArgsEditor = true;
      } catch (e) {
        console.error('Failed to get extra args:', e);
      }
    }
    closeTabContextMenu();
  }

  async function saveExtraArgs() {
    if (extraArgsWindowIdx !== null && $selectedSessionId) {
      try {
        await App.SetExtraArgs($selectedSessionId, extraArgsWindowIdx, extraArgsValue.trim());
      } catch (e) {
        console.error('Failed to save extra args:', e);
        errorMessage = `Failed to save extra args: ${e}`;
        showErrorToast = true;
      }
    }
    showExtraArgsEditor = false;
    extraArgsWindowIdx = null;
  }

  function cancelExtraArgs() {
    showExtraArgsEditor = false;
    extraArgsWindowIdx = null;
  }

  function handleExtraArgsKeydown(e: KeyboardEvent) {
    e.stopPropagation();
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      saveExtraArgs();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      cancelExtraArgs();
    }
  }

  async function confirmDeleteTab() {
    if (deleteTabIndex !== null && $selectedSessionId) {
      const killedSession = $selectedSessionId;
      const killedIdx = deleteTabIndex;
      try {
        await deleteTab(killedSession, killedIdx);
        // Tell the Terminal pool to drop the cached PoolEntry for that
        // (session, window) — otherwise a new tab created later that reuses
        // the same tmux window index would inherit the deleted tab's stale
        // WebSocket + xterm DOM and appear blank/unresponsive.
        window.dispatchEvent(new CustomEvent('terminal:destroy-window', {
          detail: { sessionId: killedSession, windowIdx: killedIdx },
        }));
        // Force refresh window list immediately
        await loadWindowsForSession($selectedSessionId, currentSessionStatus);
      } catch (e) {
        errorMessage = `Failed to delete tab: ${e}`;
        showErrorToast = true;
      }
    }
    deleteTabIndex = null;
    focusTerminal();
  }

  async function startTabRename(index: number, currentName: string) {
    renamingTabIndex = index;
    tabRenameValue = currentName;
    await tick();
    tabRenameInput?.focus();
    tabRenameInput?.select();
  }

  async function confirmTabRename() {
    if (renamingTabIndex === null) return;
    const trimmed = tabRenameValue.trim();
    const sessionId = get(selectedSessionId);
    if (trimmed && sessionId) {
      const win = windows.find(w => w.Index === renamingTabIndex);
      if (win && trimmed !== win.Name) {
        await renameTab(sessionId, renamingTabIndex, trimmed);
        // Update local windows list
        windows = windows.map(w =>
          w.Index === renamingTabIndex ? { ...w, Name: trimmed } : w
        );
      }
    }
    renamingTabIndex = null;
  }

  function cancelTabRename() {
    renamingTabIndex = null;
  }

  function handleTabRenameKeydown(e: KeyboardEvent) {
    e.stopPropagation();
    if (e.key === 'Enter') {
      e.preventDefault();
      confirmTabRename();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      cancelTabRename();
    }
  }

  function handleNewTab() {
    showNewTabDialog = true;
  }

  function getAgentColor(agent: string): string {
    const colors: Record<string, string> = {
      claude: '#a78bfa',
      gemini: '#60a5fa',
      aider: '#4ade80',
      codex: '#fbbf24',
      amazonq: '#f87171',
      opencode: '#22d3ee',
      terminal: '#9ca3af',
    };
    return colors[agent?.toLowerCase()] || '#9ca3af';
  }

  // Check if the currently selected tab is stopped (dead pane)
  $: currentTabStopped = (() => {
    if (!$selectedSession || $selectedSession.status !== 'running') return false;
    const winIdx = $selectedWindowIdx;
    if (winIdx === 0) return ($selectedSession as any).mainWindowStopped || false;
    const fw = $selectedSession.followedWindows?.find(w => w.index === winIdx);
    return fw?.stopped || false;
  })();

  $: agentSupportsResume = (() => {
    if (!$selectedSession) return false;
    const agentConfig = $agents.find(a => a.type === $selectedSession.agent);
    return agentConfig?.supportsResume || false;
  })();

  function handleResume() {
    if (!$selectedSession) return;
    dispatch('requestResume');
  }

  async function handleStartStop() {
    if (!$selectedSession) return;
    if (currentTabStopped) {
      // Restart just this stopped tab
      try {
        await restartTab($selectedSession.id, $selectedWindowIdx);
      } catch (e) {
        console.error('Restart tab failed:', e);
        errorMessage = `Failed to restart tab: ${e}`;
        showErrorToast = true;
      }
    } else if ($selectedSession.status === 'running') {
      // Show stop dialog so user can choose: stop tab or entire session
      dispatch('requestStop');
    } else {
      try {
        // Dispatch event to parent to show start dialog (if has tabs)
        dispatch('requestStart');
      } catch (e) {
        console.error('Start failed:', e);
        errorMessage = `Failed to start session: ${e}`;
        showErrorToast = true;
      }
    }
  }

  function handleDelete() {
    if (!$selectedSession) return;
    showDeleteConfirm = true;
  }

  async function handleRefresh() {
    if (!$selectedSession) return;
    try {
      await App.RefreshWindow($selectedSession.id, $selectedWindowIdx);
      focusTerminal();
    } catch (e) {
      errorMessage = `Failed to refresh: ${e}`;
      showErrorToast = true;
    }
  }

  async function confirmDelete() {
    if (!$selectedSession) return;
    try {
      await deleteSession($selectedSession.id);
    } catch (e) {
      errorMessage = `Failed to delete session: ${e}`;
      showErrorToast = true;
    }
  }

  function handleColorClick() {
    dispatch('openColorDialog');
  }

  async function handleFavoriteClick() {
    if (!$selectedSession) return;
    await toggleFavorite($selectedSession.id);
  }

  function hasExtraArgs(winIndex: number): boolean {
    if (!$selectedSession) return false;
    if (winIndex === 0) return !!$selectedSession.extraArgs;
    const fw = $selectedSession.followedWindows?.find(w => w.index === winIndex);
    return !!(fw?.extra_args);
  }

  export let fullDiffActive = false;
  export let activeView: 'terminal' | 'diff' | 'notes' | 'tasks' = 'terminal';

  function handleFullDiffClick() {
    dispatch('openFullDiff');
  }
</script>

{#if $selectedSessionId}
  <div class="tab-bar">
    {#if windows.length > 0}
      <div class="tabs-container">
        {#each windows as win, winArrayIdx (win.Index)}
          <button
            class="tab"
            class:active={$selectedWindowIdx === win.Index && !fullDiffActive}
            class:dead={win.Dead}
            class:stopped={currentSessionStatus !== 'running'}
            class:tab-dragging={draggingTabIndex === winArrayIdx}
            class:tab-drag-over={dragOverTabIndex === winArrayIdx && draggingTabIndex !== winArrayIdx}
            class:tab-dropped={droppedTabWindowIdx === win.Index}
            style={tabStyle(win)}
            draggable={true}
            on:click={() => { if (renamingTabIndex === null) { handleTabClick(win.Index); dispatch('closeFullDiff'); } }}
            on:contextmenu={(e) => handleTabContextMenu(e, win.Index, win.Name)}
            on:dragstart={(e) => handleTabDragStart(e, winArrayIdx)}
            on:dragend={handleTabDragEnd}
            on:dragover={(e) => handleTabDragOver(e, winArrayIdx)}
            on:dragleave={(e) => handleTabDragLeave(e, winArrayIdx)}
            on:drop={(e) => handleTabDrop(e, winArrayIdx)}
          >
            <span class="tab-indicator" style="background: {getAgentColor(win.Agent)}"></span>
            {#if currentSessionStatus === 'running' && !win.Dead && tabActivityByIdx[win.Index] && tabActivityByIdx[win.Index] !== 'idle'}
              <!-- Live per-tab status dot: orange = busy, cyan = waiting input -->
              <span class="tab-status-dot" title={tabActivityByIdx[win.Index] === 'waiting' ? 'Bemenetre vár' : 'Dolgozik'}>
                <StatusIndicator status="running" activity={tabActivityByIdx[win.Index]} size="sm" />
              </span>
            {/if}
            <AgentIcon agent={win.Agent} size="sm" />
            {#if renamingTabIndex === win.Index}
              <!-- svelte-ignore a11y-autofocus -->
              <input
                class="tab-rename-input"
                type="text"
                bind:this={tabRenameInput}
                bind:value={tabRenameValue}
                on:keydown={handleTabRenameKeydown}
                on:blur={confirmTabRename}
                on:click|stopPropagation
              />
            {:else}
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <span class="tab-name" on:dblclick|stopPropagation={() => startTabRename(win.Index, win.Name)}>{win.Name}</span>
            {/if}
            {#if win.Dead}
              <span class="tab-dead">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <circle cx="12" cy="12" r="10"/>
                  <line x1="15" y1="9" x2="9" y2="15"/>
                  <line x1="9" y1="9" x2="15" y2="15"/>
                </svg>
              </span>
            {/if}
            {#if hasExtraArgs(win.Index)}
              <span class="tab-extra-args-badge" title={$t('tabBar.editExtraArgs')}>
                <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <polyline points="4 17 10 11 4 5"/>
                  <line x1="12" y1="19" x2="20" y2="19"/>
                </svg>
              </span>
            {/if}
          </button>
        {/each}
      </div>
    {:else}
      <div class="tabs-container"></div>
    {/if}

    {#if showTabContextMenu}
      <div
        class="tab-context-menu"
        style="left: {tabContextMenuX}px; top: {tabContextMenuY}px"
        on:click|stopPropagation
      >
        <button class="tab-context-menu-item" on:click={tabContextRename}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
          </svg>
          {$t('tabBar.rename')}
        </button>
        <button class="tab-context-menu-item" on:click={tabContextSetColor}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 22a1 1 0 0 0 0-20 10 10 0 0 0 0 20z"/>
            <circle cx="7.5" cy="10.5" r="1" fill="currentColor" stroke="none"/>
            <circle cx="10.5" cy="6.5" r="1" fill="currentColor" stroke="none"/>
            <circle cx="15.5" cy="7.5" r="1" fill="currentColor" stroke="none"/>
            <circle cx="16.5" cy="12.5" r="1" fill="currentColor" stroke="none"/>
          </svg>
          {$t('tabBar.setTabColor')}
        </button>
        {#if tabContextMenuIndex !== null && windows.find(w => w.Index === tabContextMenuIndex)?.Agent !== 'terminal' && windows.find(w => w.Index === tabContextMenuIndex)?.Agent !== 'custom'}
          <button class="tab-context-menu-item" on:click={tabContextEditExtraArgs}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="4 17 10 11 4 5"/>
              <line x1="12" y1="19" x2="20" y2="19"/>
            </svg>
            {$t('tabBar.editExtraArgs')}
          </button>
        {/if}
        {#if currentSessionStatus === 'running' && tabContextMenuIndex !== null && !windows.find(w => w.Index === tabContextMenuIndex)?.Dead}
          <button class="tab-context-menu-item" on:click={tabContextStop}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="6" y="6" width="12" height="12" rx="1"/>
            </svg>
            {$t('tabBar.stopTab')}
          </button>
        {/if}
        {#if tabContextMenuIndex !== 0}
          <button class="tab-context-menu-item delete" on:click={tabContextDelete}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="3 6 5 6 21 6"/>
              <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
            </svg>
            {$t('tabBar.deleteTab')}
          </button>
        {/if}
      </div>
    {/if}

    <!-- Separator -->
    <div class="tab-separator"></div>

    <!-- Full Diff Tab -->
    <button
      class="tab diff-tab"
      class:active={fullDiffActive}
      on:click={handleFullDiffClick}
      title={$t('tabBar.fullDiff')}
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M12 3v18M3 12h18"/>
      </svg>
      <span class="tab-name">{$t('tabBar.diffLabel')}</span>
    </button>

    <!-- Spacer to push controls to right -->
    <div class="tab-spacer"></div>

    <!-- Session Controls -->
    <div class="session-controls">
      <!-- Add Tab Button (only for running sessions) -->
      {#if $selectedSession?.status === 'running'}
        <button class="control-btn add-tab" on:click={handleNewTab} title={$t('tabBar.newTab')}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="12" y1="5" x2="12" y2="19"/>
            <line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
        </button>

        <div class="control-divider"></div>
      {/if}

      <!-- Start/Stop -->
      <button
        class="control-btn {currentTabStopped ? 'start' : ($selectedSession?.status === 'running' ? 'stop' : 'start')}"
        on:click={handleStartStop}
        title={currentTabStopped ? $t('tabBar.startSession') : ($selectedSession?.status === 'running' ? $t('tabBar.stopSession') : $t('tabBar.startSession'))}
      >
        {#if $selectedSession?.status === 'running' && !currentTabStopped}
          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
            <rect x="6" y="6" width="12" height="12" rx="1"/>
          </svg>
        {:else}
          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
            <path d="M8 5v14l11-7z"/>
          </svg>
        {/if}
      </button>

      <!-- Resume (for any session/tab with resume support) -->
      {#if agentSupportsResume}
        <button
          class="control-btn resume"
          on:click={handleResume}
          title={$t('tabBar.resumeConversation')}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M1 4v6h6"/>
            <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/>
          </svg>
        </button>
      {/if}

      <!-- Refresh (redraw tmux pane to fix rendering glitches) -->
      {#if $selectedSession?.status === 'running' && !currentTabStopped}
        <button
          class="control-btn refresh"
          on:click={handleRefresh}
          title={$t('tabBar.refreshWindow')}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="23 4 23 10 17 10"/>
            <polyline points="1 20 1 14 7 14"/>
            <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
          </svg>
        </button>
      {/if}

      <!-- Delete -->
      <button class="control-btn delete" on:click={handleDelete} title={$t('tabBar.deleteSession')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
        </svg>
      </button>

      <!-- Favorite -->
      <button
        class="control-btn favorite"
        class:active={$selectedSession?.favorite}
        on:click={handleFavoriteClick}
        title={$selectedSession?.favorite ? $t('tabBar.removeFromFavorites') : $t('tabBar.addToFavorites')}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill={$selectedSession?.favorite ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="2">
          <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
        </svg>
      </button>

      <!-- Color -->
      <button class="control-btn color" on:click={handleColorClick} title={$t('tabBar.setSessionColor')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10"/>
          <circle cx="12" cy="12" r="3" fill="currentColor"/>
        </svg>
      </button>

      <!-- Dictation -->
      {#if dictationEnabled}
        <div class="dictation-wrapper">
          {#if streamingMode && dictationListening}
            <div class="dictation-buffer" class:dragging={isDragging} class:resizing={isResizing} class:live-preview={!bufferMode} bind:this={bufferPanel} style={bufferPanelStyle}>
              <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
              <div class="buffer-header" role="banner" on:mousedown={onHeaderMousedown}>
                <span class="buffer-title">{bufferMode ? $t('tabBar.dictationBuffer') : $t('tabBar.livePreview')}</span>
                <button class="buffer-close" on:click={() => { clearBuffer(); dictationListening = false; DictationService.ToggleDictation(); }} title={$t('tabBar.closeDictation')}>
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"/>
                    <line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              </div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                class="buffer-editor"
                contenteditable={bufferMode ? 'true' : 'false'}
                bind:this={bufferEditor}
                on:keydown={bufferMode ? handleBufferKeydown : undefined}
                on:keyup={bufferMode ? handleBufferKeyup : undefined}
                on:beforeinput={bufferMode ? handleBeforeInput : undefined}
                on:input={bufferMode ? onEditorInput : undefined}
              ></div>
              {#if bufferMode}
              <div class="buffer-actions">
                <div class="buffer-left-actions">
                  <span class="buffer-hint">{$t('tabBar.bufferHint')}</span>
                  <div class="buffer-toggles">
                    <button class="buffer-setting-toggle" class:active={bufferCloseOnSend} title={$t('tabBar.closeAfterSendTitle')} on:click={async () => {
                        bufferCloseOnSend = !bufferCloseOnSend;
                        try {
                          const settings = await DictationService.GetDictationSettings();
                          settings.bufferCloseOnSend = bufferCloseOnSend;
                          await DictationService.SetDictationSettings(JSON.stringify(settings));
                        } catch (_) {}
                      }}>
                      <span class="mini-toggle-track"><span class="mini-toggle-thumb"></span></span>
                      <span class="buffer-toggle-label">{$t('tabBar.closeAfterSend')}</span>
                    </button>
                  </div>
                </div>
                <div class="buffer-btn-group">
                  <button class="buffer-btn trash" on:click={clearBuffer} title={$t('tabBar.clearText')}>
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
                    </svg>
                  </button>
                  <button class="buffer-btn send" on:click={sendBuffer} title={$t('tabBar.sendToTerminal')} disabled={!bufferText.trim()}>
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="22" y1="2" x2="11" y2="13"/>
                      <polygon points="22 2 15 22 11 13 2 9 22 2"/>
                    </svg>
                    {$t('tabBar.send')}
                  </button>
                </div>
              </div>
              {/if}
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-edge n" on:mousedown={onEdgeMousedown('n')}></div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-edge s" on:mousedown={onEdgeMousedown('s')}></div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-edge e" on:mousedown={onEdgeMousedown('e')}></div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-edge w" on:mousedown={onEdgeMousedown('w')}></div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-corner nw" on:mousedown={onEdgeMousedown('nw')}></div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-corner ne" on:mousedown={onEdgeMousedown('ne')}></div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-corner sw" on:mousedown={onEdgeMousedown('sw')}></div>
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="resize-corner se" on:mousedown={onEdgeMousedown('se')}></div>
            </div>
          {:else if interimText}
            <div class="interim-overlay">
              <span class="interim-text">{interimText}</span>
            </div>
          {/if}
          <button
            class="control-btn dictation"
            class:listening={dictationListening}
            class:voice-active={dictationListening && voiceLevel > 0.05}
            on:click={toggleDictation}
            title={dictationListening ? $t('tabBar.stopDictation') : $t('tabBar.startDictation')}
            style="--voice-glow: {6 + voiceLevel * 30}px; --voice-alpha: {0.4 + voiceLevel * 0.6}; --voice-scale: {1 + voiceLevel * 0.15};"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill={dictationListening ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="2">
              <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
              <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
              <line x1="12" y1="19" x2="12" y2="23"/>
              <line x1="8" y1="23" x2="16" y2="23"/>
            </svg>
          </button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<NewTabDialog bind:show={showNewTabDialog} />

<TabColorDialog
  bind:show={showTabColorDialog}
  sessionId={$selectedSessionId || ''}
  tab={tabColorTarget}
  on:applied={handleTabColorApplied}
  on:close={() => tabColorTarget = null}
/>

<ConfirmDialog
  bind:show={showDeleteConfirm}
  title={$t('tabBar.deleteTitle')}
  message={$t('tabBar.deleteMessage', { name: $selectedSession?.name || '' })}
  confirmText={$t('tabBar.deleteConfirm')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmDelete}
/>

<ConfirmDialog
  bind:show={showDeleteTabConfirm}
  title={$t('tabBar.deleteTabTitle')}
  message={$t('tabBar.deleteTabMessage')}
  confirmText={$t('tabBar.deleteConfirm')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmDeleteTab}
/>

{#if showExtraArgsEditor}
  <div class="dialog-overlay" on:click|self={cancelExtraArgs} on:keydown={handleExtraArgsKeydown} role="dialog" aria-modal="true">
    <div class="extra-args-dialog">
      <div class="extra-args-header">
        <h3>{$t('tabBar.extraArgsTitle')}</h3>
        <button class="close-btn" on:click={cancelExtraArgs}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
      <input
        class="extra-args-input"
        type="text"
        bind:value={extraArgsValue}
        on:keydown={handleExtraArgsKeydown}
        placeholder="--model opus --verbose"
        autofocus
      />
      <span class="extra-args-hint">{$t('tabBar.extraArgsHint')}</span>
      <div class="extra-args-actions">
        <button class="btn-cancel" on:click={cancelExtraArgs}>{$t('common.cancel')}</button>
        <button class="btn-primary" on:click={saveExtraArgs}>{$t('tabBar.extraArgsSave')}</button>
      </div>
    </div>
  </div>
{/if}

<Toast bind:show={showErrorToast} message={errorMessage} variant="error" />

<style>
  .tab-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px 0;
    background: linear-gradient(180deg, rgba(0, 0, 0, 0.3) 0%, rgba(0, 0, 0, 0.2) 100%);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .tabs-container {
    display: flex;
    align-items: flex-end;
    gap: 4px;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .tabs-container::-webkit-scrollbar {
    display: none;
  }

  .tab {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 16px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-bottom: none;
    border-radius: 10px 10px 0 0;
    font-size: 13px;
    font-weight: 500;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
    white-space: nowrap;
    min-width: 0;
  }

  .tab:hover:not(.active) {
    background: rgba(255, 255, 255, 0.06);
    color: #9ca3af;
  }

  .tab.active {
    background: linear-gradient(180deg, rgba(139, 92, 246, 0.15) 0%, rgba(139, 92, 246, 0.08) 100%);
    border-color: rgba(139, 92, 246, 0.3);
    color: white;
    box-shadow: 0 -4px 20px rgba(139, 92, 246, 0.15);
  }

  .tab.tab-dragging {
    opacity: 0.4;
    transform: scale(0.95);
  }

  .tab.tab-drag-over {
    border-left: 2px solid #a78bfa;
    background: rgba(139, 92, 246, 0.1);
  }

  .tab.tab-dropped {
    animation: tab-drop-flash 0.5s ease-out;
  }

  @keyframes tab-drop-flash {
    0% {
      background: rgba(139, 92, 246, 0.4);
      box-shadow: 0 0 12px rgba(139, 92, 246, 0.6);
    }
    100% {
      background: rgba(255, 255, 255, 0.03);
      box-shadow: none;
    }
  }

  .tab.dead,
  .tab.stopped {
    opacity: 0.6;
    filter: grayscale(1);
    color: #6b7280;
  }

  .tab.dead.active,
  .tab.stopped.active {
    opacity: 0.8;
    filter: grayscale(1);
    background: linear-gradient(180deg, rgba(107, 114, 128, 0.15) 0%, rgba(107, 114, 128, 0.08) 100%);
    border-color: rgba(107, 114, 128, 0.3);
    box-shadow: none;
    color: #9ca3af;
  }

  .tab.dead .tab-indicator,
  .tab.stopped .tab-indicator {
    opacity: 0.3;
  }

  .tab-indicator {
    position: absolute;
    top: 0;
    left: 50%;
    transform: translateX(-50%);
    width: 30px;
    height: 3px;
    border-radius: 0 0 3px 3px;
    opacity: 0;
    transition: opacity 0.2s ease;
  }

  /* Inline per-tab activity dot (busy/waiting), shown before the agent icon. */
  .tab-status-dot {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    margin-right: 1px;
  }

  .tab.active .tab-indicator {
    opacity: 1;
  }

  .tab-name {
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .tab-rename-input {
    max-width: 120px;
    font-size: 13px;
    font-weight: 500;
    color: #e4e4e7;
    background: rgba(139, 92, 246, 0.15);
    border: 1px solid rgba(139, 92, 246, 0.4);
    border-radius: 4px;
    padding: 1px 4px;
    outline: none;
    min-width: 60px;
  }

  .tab-rename-input:focus {
    border-color: rgba(139, 92, 246, 0.7);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.2);
  }

  .tab-context-menu {
    position: fixed;
    z-index: 1000;
    min-width: 140px;
    background: #1a1a2e;
    border: 1px solid rgba(139, 92, 246, 0.3);
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
    padding: 4px;
  }

  .tab-context-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 12px;
    font-size: 13px;
    color: #e4e4e7;
    background: none;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s ease;
    text-align: left;
  }

  .tab-context-menu-item:hover {
    background: rgba(139, 92, 246, 0.15);
  }

  .tab-context-menu-item.delete {
    color: #f87171;
  }

  .tab-context-menu-item.delete:hover {
    background: rgba(239, 68, 68, 0.15);
  }

  .tab-dead {
    display: flex;
    align-items: center;
    color: #f87171;
  }

  .tab-extra-args-badge {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 4px;
    background: rgba(139, 92, 246, 0.2);
    color: #a78bfa;
    flex-shrink: 0;
  }

  .tab-separator {
    width: 1px;
    height: 24px;
    background: rgba(255, 255, 255, 0.1);
    margin: 0 8px;
    align-self: center;
    flex-shrink: 0;
  }

  .tab-spacer {
    flex: 1;
  }

  .diff-tab {
    flex-shrink: 0;
    margin-bottom: 0;
  }

  .diff-tab.active {
    background: linear-gradient(180deg, rgba(96, 165, 250, 0.15) 0%, rgba(96, 165, 250, 0.08) 100%);
    border-color: rgba(96, 165, 250, 0.3);
    color: #60a5fa;
    box-shadow: 0 -4px 20px rgba(96, 165, 250, 0.15);
  }

  .diff-tab svg {
    color: #60a5fa;
  }

  .session-controls {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-bottom: 8px;
    flex-shrink: 0;
  }

  .control-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .control-btn:hover {
    background: rgba(255, 255, 255, 0.08);
    border-color: rgba(255, 255, 255, 0.15);
    color: #9ca3af;
  }

  .control-btn.add-tab:hover {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  .control-btn.start {
    color: #4ade80;
  }

  .control-btn.start:hover {
    background: rgba(34, 197, 94, 0.15);
    border-color: rgba(34, 197, 94, 0.3);
    color: #4ade80;
  }

  .control-btn.resume {
    color: #60a5fa;
  }

  .control-btn.resume:hover {
    background: rgba(59, 130, 246, 0.15);
    border-color: rgba(59, 130, 246, 0.3);
    color: #60a5fa;
  }

  .control-btn.refresh {
    color: #34d399;
  }

  .control-btn.refresh:hover {
    background: rgba(16, 185, 129, 0.15);
    border-color: rgba(16, 185, 129, 0.3);
    color: #34d399;
  }

  .control-btn.stop {
    color: #f87171;
  }

  .control-btn.stop:hover {
    background: rgba(239, 68, 68, 0.15);
    border-color: rgba(239, 68, 68, 0.3);
    color: #f87171;
  }

  .control-btn.delete:hover {
    background: rgba(239, 68, 68, 0.15);
    border-color: rgba(239, 68, 68, 0.3);
    color: #f87171;
  }

  .control-btn.favorite {
    color: #6b7280;
  }

  .control-btn.favorite:hover {
    background: rgba(251, 191, 36, 0.15);
    border-color: rgba(251, 191, 36, 0.3);
    color: #fbbf24;
  }

  .control-btn.favorite.active {
    color: #fbbf24;
    text-shadow: 0 0 8px rgba(251, 191, 36, 0.6);
  }

  .control-btn.favorite.active:hover {
    background: rgba(251, 191, 36, 0.2);
    border-color: rgba(251, 191, 36, 0.4);
  }

  .control-btn.color:hover {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  .control-divider {
    width: 1px;
    height: 20px;
    background: rgba(255, 255, 255, 0.1);
    margin: 0 4px;
  }

  .control-btn.dictation {
    color: #6b7280;
    transition: all 0.15s ease-out;
  }

  .control-btn.dictation:hover {
    background: rgba(239, 68, 68, 0.15);
    border-color: rgba(239, 68, 68, 0.3);
    color: #f87171;
  }

  .control-btn.dictation.listening {
    color: #ef4444;
    background: rgba(239, 68, 68, 0.2);
    border-color: rgba(239, 68, 68, 0.4);
  }

  .control-btn.dictation.voice-active {
    box-shadow: 0 0 var(--voice-glow, 6px) rgba(239, 68, 68, var(--voice-alpha, 0.4));
    transform: scale(var(--voice-scale, 1));
  }

  .dictation-wrapper {
    position: relative;
    overflow: visible;
  }

  .session-controls {
    overflow: visible;
  }

  .interim-overlay {
    position: absolute;
    bottom: calc(100% + 8px);
    right: 0;
    background: rgba(30, 30, 40, 0.95);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 8px;
    padding: 6px 12px;
    white-space: nowrap;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    z-index: 100;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    animation: interim-fade-in 0.15s ease-out;
    pointer-events: none;
  }

  .interim-text {
    font-size: 12px;
    color: rgba(239, 68, 68, 0.9);
    font-style: italic;
  }

  @keyframes interim-fade-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .dictation-buffer {
    position: fixed;
    top: 80px;
    right: 12px;
    background: rgba(25, 25, 35, 0.98);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 12px;
    padding: 12px;
    width: 600px;
    height: 320px;
    min-width: 300px;
    min-height: 150px;
    z-index: 9999;
    box-shadow: 0 12px 48px rgba(0, 0, 0, 0.6), 0 0 0 1px rgba(239, 68, 68, 0.1);
    animation: interim-fade-in 0.15s ease-out;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .dictation-buffer.live-preview {
    height: 120px;
    min-height: 80px;
  }

  .dictation-buffer.live-preview .buffer-editor {
    cursor: default;
    user-select: none;
    opacity: 0.9;
  }

  .buffer-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    cursor: grab;
    user-select: none;
  }

  .dictation-buffer.dragging .buffer-header {
    cursor: grabbing;
  }

  .dictation-buffer.dragging,
  .dictation-buffer.resizing {
    user-select: none;
  }

  .buffer-title {
    font-size: 13px;
    font-weight: 600;
    color: #f87171;
    letter-spacing: 0.02em;
  }

  .buffer-close {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.15s ease;
    padding: 0;
  }

  .buffer-close:hover {
    background: rgba(239, 68, 68, 0.15);
    border-color: rgba(239, 68, 68, 0.3);
    color: #f87171;
  }

  .buffer-editor {
    width: 100%;
    flex: 1;
    min-height: 60px;
    overflow-y: auto;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    color: #e5e7eb;
    font-size: 14px;
    line-height: 1.5;
    font-family: inherit;
    padding: 10px;
    outline: none;
    transition: border-color 0.2s;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .buffer-editor:focus {
    border-color: rgba(239, 68, 68, 0.4);
  }

  .buffer-editor :global(.interim-span) {
    color: #b0b8c4;
    font-style: italic;
  }

  .buffer-actions {
    display: flex;
    gap: 8px;
    align-items: center;
    justify-content: space-between;
  }

  .buffer-left-actions {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .buffer-toggles {
    display: flex;
    flex-direction: row;
    gap: 12px;
  }

  .buffer-hint {
    font-size: 11px;
    color: #4b5563;
  }

  .buffer-setting-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
    user-select: none;
    background: none;
    border: none;
    padding: 0;
  }

  .mini-toggle-track {
    display: block;
    width: 28px;
    height: 14px;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 7px;
    position: relative;
    transition: background 0.2s ease;
    flex-shrink: 0;
  }

  .buffer-setting-toggle.active .mini-toggle-track {
    background: rgba(239, 68, 68, 0.5);
  }

  .mini-toggle-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 10px;
    height: 10px;
    background: #4b5563;
    border-radius: 50%;
    transition: all 0.2s ease;
  }

  .buffer-setting-toggle.active .mini-toggle-thumb {
    left: 16px;
    background: #f87171;
  }

  .buffer-toggle-label {
    font-size: 11px;
    color: #6b7280;
  }

  .buffer-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 14px;
    border-radius: 6px;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s ease;
    border: 1px solid transparent;
  }

  .buffer-btn.send {
    background: rgba(239, 68, 68, 0.2);
    border-color: rgba(239, 68, 68, 0.3);
    color: #f87171;
  }

  .buffer-btn.send:hover {
    background: rgba(239, 68, 68, 0.3);
    border-color: rgba(239, 68, 68, 0.5);
  }

  .buffer-btn.send:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .buffer-btn-group {
    display: flex;
    gap: 6px;
  }

  .buffer-btn.trash {
    background: rgba(255, 255, 255, 0.05);
    border-color: rgba(255, 255, 255, 0.1);
    color: #6b7280;
  }

  .buffer-btn.trash:hover {
    background: rgba(239, 68, 68, 0.15);
    border-color: rgba(239, 68, 68, 0.3);
    color: #f87171;
  }


  /* Extra Args Editor */
  .extra-args-dialog {
    background: #1a1a2e;
    border: 1px solid rgba(139, 92, 246, 0.3);
    border-radius: 12px;
    padding: 20px;
    width: 400px;
    max-width: 90vw;
    box-shadow: 0 12px 48px rgba(0, 0, 0, 0.6);
  }

  .extra-args-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
  }

  .extra-args-header h3 {
    font-size: 15px;
    font-weight: 600;
    color: #e4e4e7;
    margin: 0;
  }

  .extra-args-input {
    width: 100%;
    padding: 10px 14px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    font-size: 14px;
    font-family: 'JetBrains Mono', monospace;
    color: #e4e4e7;
    outline: none;
    transition: border-color 0.2s ease;
  }

  .extra-args-input:focus {
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.1);
  }

  .extra-args-input::placeholder {
    color: #4b5563;
  }

  .extra-args-hint {
    display: block;
    font-size: 11px;
    color: #6b7280;
    margin-top: 8px;
    margin-bottom: 16px;
  }

  .extra-args-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  /* Resize edges */
  .resize-edge, .resize-corner { position: absolute; z-index: 1; }
  .resize-edge.n { top: -3px; left: 6px; right: 6px; height: 6px; cursor: ns-resize; }
  .resize-edge.s { bottom: -3px; left: 6px; right: 6px; height: 6px; cursor: ns-resize; }
  .resize-edge.e { right: -3px; top: 6px; bottom: 6px; width: 6px; cursor: ew-resize; }
  .resize-edge.w { left: -3px; top: 6px; bottom: 6px; width: 6px; cursor: ew-resize; }
  .resize-corner.nw { top: -3px; left: -3px; width: 10px; height: 10px; cursor: nwse-resize; }
  .resize-corner.ne { top: -3px; right: -3px; width: 10px; height: 10px; cursor: nesw-resize; }
  .resize-corner.sw { bottom: -3px; left: -3px; width: 10px; height: 10px; cursor: nesw-resize; }
  .resize-corner.se { bottom: -3px; right: -3px; width: 10px; height: 10px; cursor: nwse-resize; }
</style>
