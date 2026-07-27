import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export type TerminalRenderer = 'canvas' | 'webgl' | 'dom';

/** Where the session's git branch is shown, if anywhere. */
export type GitBranchDisplay = 'header' | 'statusbar' | 'off';

export interface Settings {
  compactList: boolean;
  hideStatusLines: boolean;
  showAgentIcons: boolean;
  /** YOLO shows unless hidden; the resume marker is opt-in. */
  hideYoloBadge: boolean;
  showResumeBadge: boolean;
  splitView: boolean;
  markedSessionId: string;
  markedWindowIdx: number;
  language: string;
  /** Interface accent colour id (see uiThemes.ts). */
  uiTheme: string;
  /** Custom accent hex, used when uiTheme is 'custom'. */
  uiAccent: string;
  terminalRenderer: TerminalRenderer;
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
  markedWindowIdx: 0,
  language: 'en',
  uiTheme: 'violet',
  uiAccent: '#8b5cf6',
  terminalRenderer: 'canvas',
  gitBranchDisplay: 'header',
  diffFlatFileList: false,
  trashRetentionDays: 0,
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

export async function loadSettings() {
  try {
    const data = await App.GetSettings();
    if (data) {
      settings.set(data as Settings);
    }
  } catch (e) {
    console.error('Failed to load settings:', e);
  }
}

export async function saveSettings(newSettings: Partial<Settings>) {
  let updated!: Settings;
  settings.update(s => {
    updated = { ...s, ...newSettings };
    return updated;
  });
  const save = saveQueue
    .catch(() => {})
    .then(() => App.SaveSettings(updated as any));
  saveQueue = save;
  try {
    await save;
  } catch (e) {
    console.error('Failed to save settings:', e);
    await loadSettings();
  }
}

export async function flushSettingsSaves() {
  await saveQueue.catch(() => {});
}
