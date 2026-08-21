import { get, writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import { defaultTerminalRenderer } from '../utils/terminal';
import { reportError } from './appErrors';
import { activeProjectId } from './projects';

export type TerminalRenderer = 'canvas' | 'webgl' | 'dom';

/** Where the session's git branch is shown, if anywhere. */
export type GitBranchDisplay = 'header' | 'statusbar' | 'off';

/**
 * What has to happen for a terminal selection to reach the clipboard.
 * 'shift' copies only a Shift-held drag; 'select' copies any drag.
 */
export type TerminalCopyMode = 'shift' | 'select';

export interface Settings {
  /** Rebound keyboard shortcuts, keyed by shortcut id. Only what the user has
   *  changed — see stores/shortcuts.ts. */
  shortcutOverrides?: Record<string, unknown>;
  /** Height in px of the diff shown above a view. Pixels, not a fraction: the
   *  pane below is a terminal measured in whole rows. */
  diffAboveHeight?: number;
  /** Show a change inside the whole file rather than only its hunks. Absent
   *  means on: the default is the fuller view. */
  /** Inverted: whole-file is the default, so only the opt-out is stored. */
  diffHunksOnly?: boolean;
  /** Show a diff as two aligned columns rather than one with markers. */
  diffSideBySide?: boolean;
  /** The file the diff had open, per session, so a tab switch resumes. */
  diffLastFile?: Record<string, string>;
  compactList: boolean;
  hideStatusLines: boolean;
  showAgentIcons: boolean;
  /** YOLO shows unless hidden; the resume marker is opt-in. */
  hideYoloBadge: boolean;
  showResumeBadge: boolean;
  splitView: boolean;
  markedSessionId: string;
  /** Session selected when the app last closed, so it reopens there. */
  lastSessionId: string;
  /**
   * Start in the session that was open at shutdown instead of on the
   * dashboard. Off by default — the dashboard is the neutral starting point.
   */
  restoreLastSession: boolean;
  markedWindowIdx: number;
  language: string;
  /** Interface accent colour id (see uiThemes.ts). */
  uiTheme: string;
  /** Custom accent hex, used when uiTheme is 'custom'. */
  uiAccent: string;
  terminalRenderer: TerminalRenderer;
  /**
   * Terminal font stack. Empty means the built-in default.
   *
   * Worth choosing rather than fixing: the default names JetBrains Mono first,
   * which is installed on none of the machines this was tested on, so everyone
   * silently gets a fallback — and which fallback decides whether accents and
   * box-drawing characters render at all.
   */
  terminalFontFamily: string;
  /** What a plain terminal tab runs. Empty means the system default. */
  terminalShell: string;
  /** Where the dictation buffer window was left, in pixels. Absent until it
   *  has been moved or resized once. */
  dictationBuffer?: { x: number; y: number; w: number; h: number } | null;
  /** What this platform offers for terminalShell; supplied by the backend. */
  shellChoices?: Array<{ command: string; label: string }>;
  /**
   * Whether a plain drag copies to the clipboard, or only a Shift-held one.
   * Defaults to 'shift': copying is bound to mouseup either way, never to
   * selection changes, because output-driven clipboard writes freeze WebKit.
   */
  terminalCopyMode: TerminalCopyMode;
  /** Where to show the session's git branch; 'header' by default. */
  gitBranchDisplay: GitBranchDisplay;
  /** Show the diff's file list as a directory tree instead of a flat list. */
  diffFlatFileList: boolean;
  /**
   * Days a deleted session or tab stays in the trash. 0 means the backend
   * default, and "keep everything" is a negative — 0 was already taken by
   * "unset", which every config predating this setting reports.
   */
  trashRetentionDays: number;
  /**
   * Show the experimental Task Master panel. Off unless asked for: opening it
   * runs `npx task-master-ai`, which installs the package on a machine that
   * doesn't have it.
   */
  taskMasterEnabled: boolean;
  /** Default terminal font size in px; 0 means the built-in default. */
  terminalFontSize: number;
  /** Same again for agent tabs; the two never fall back to each other. */
  agentFontSize: number;
  /** Hide the Terminal/Notes/Tasks bar under the tabs. */
  hideViewBar: boolean;
  agentHideViewBar: boolean;
  /** Same again for the bottom bar (path + agent badge). */
  hideStatusBar: boolean;
  agentHideStatusBar: boolean;
  notifyOnWaiting: boolean;
  notifyDesktop: boolean;
  notifyNtfy: boolean;
  ntfyUrl: string;
  terminalTheme: string;
  agentTerminalThemes?: Record<string, string>;
  customTerminalThemes?: Array<{ id: string; name: string; colors: Record<string, string> }>;
}

export const settings = writable<Settings>({
  compactList: false,
  hideStatusLines: false,
  showAgentIcons: true,
  hideYoloBadge: false,
  showResumeBadge: false,
  splitView: false,
  markedSessionId: '',
  lastSessionId: '',
  restoreLastSession: false,
  markedWindowIdx: 0,
  language: 'en',
  uiTheme: 'violet',
  uiAccent: '#8b5cf6',
  // Platform-dependent; see defaultTerminalRenderer(). A saved setting
  // overrides this, so it only decides what a fresh install starts with.
  terminalRenderer: defaultTerminalRenderer(),
  terminalFontFamily: '',
  terminalShell: '',
  terminalCopyMode: 'shift',
  gitBranchDisplay: 'header',
  diffFlatFileList: false,
  trashRetentionDays: 0,
  taskMasterEnabled: false,
  terminalFontSize: 0,
  agentFontSize: 0,
  hideViewBar: false,
  agentHideViewBar: false,
  hideStatusBar: false,
  agentHideStatusBar: false,
  notifyOnWaiting: false,
  notifyDesktop: true,
  notifyNtfy: false,
  ntfyUrl: '',
  terminalTheme: 'asmgr',
  agentTerminalThemes: {},
  customTerminalThemes: []
});

let saveQueue: Promise<void> = Promise.resolve();
let settingsRevision = 0;
let settingsContextGeneration = 0;
let settingsLoadGeneration = 0;

/** Invalidate reads/writes captured under the backend's previous project. */
export function invalidateSettingsContext() {
  settingsContextGeneration++;
  settingsLoadGeneration++;
}

export async function loadSettings(expectedRevision?: number) {
  const context = settingsContextGeneration;
  const generation = ++settingsLoadGeneration;
  try {
    const data = await App.GetSettings();
    if (context !== settingsContextGeneration || generation !== settingsLoadGeneration) return;
    if (expectedRevision !== undefined && expectedRevision !== settingsRevision) return;
    if (data) {
      const loaded = data as Settings;
      // An unset renderer must fall back to the per-platform default rather
      // than to an empty string: the backend leaves it empty when the user has
      // never chosen one, and an empty value here would override
      // defaultTerminalRenderer() with nothing.
      if (!loaded.terminalRenderer) {
        loaded.terminalRenderer = defaultTerminalRenderer();
      }
      settings.set(loaded);
      void App.LogFrontend(`[settings] renderer=${loaded.terminalRenderer}`);
    }
  } catch (e) {
    console.error('Failed to load settings:', e);
  }
}

export async function saveSettings(newSettings: Partial<Settings>, expectedProjectId = get(activeProjectId)) {
  const revision = ++settingsRevision;
  const context = settingsContextGeneration;
  let updated!: Settings;
  settings.update(s => {
    updated = { ...s, ...newSettings };
    return updated;
  });
  const save = saveQueue
    .catch(() => {})
    // A queued snapshot from the project that was left must never be written
    // through the backend's new implicit project target.
    .then(() => context === settingsContextGeneration
      ? App.SaveSettings(updated as any, expectedProjectId)
      : undefined);
  saveQueue = save;
  try {
    await save;
  } catch (e) {
    console.error('Failed to save settings:', e);
    // Reload puts the UI back to what is actually stored, which without a word
    // looks like the app undoing the user's change by itself — worse than the
    // failure, because it reads as the app being broken rather than the save.
    reportError(`Could not save settings: ${e}`);
    // A newer optimistic edit either has a queued save or has already won.
    // Its UI must not be overwritten by a recovery read started for this
    // failed, older save; loadSettings checks again after its own await.
    await loadSettings(revision);
  }
}

export async function flushSettingsSaves() {
  // A save can be appended while the currently observed promise is settling.
  // Drain until the tail stays stable, which is what project switch and quit
  // need before changing/destroying the backend target.
  while (true) {
    const pending = saveQueue;
    await pending.catch(() => {});
    if (pending === saveQueue) return;
  }
}
