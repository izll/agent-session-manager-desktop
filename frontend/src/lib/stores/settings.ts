import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export type TerminalRenderer = 'canvas' | 'webgl' | 'dom';

export interface Settings {
  compactList: boolean;
  hideStatusLines: boolean;
  showAgentIcons: boolean;
  /** Both badges show by default, so these hide rather than show. */
  hideYoloBadge: boolean;
  hideResumeBadge: boolean;
  splitView: boolean;
  markedSessionId: string;
  markedWindowIdx: number;
  language: string;
  terminalRenderer: TerminalRenderer;
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
  hideResumeBadge: false,
  splitView: false,
  markedSessionId: '',
  markedWindowIdx: 0,
  language: 'en',
  terminalRenderer: 'canvas',
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
