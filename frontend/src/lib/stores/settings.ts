import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export type TerminalRenderer = 'canvas' | 'webgl' | 'dom';

export interface Settings {
  compactList: boolean;
  hideStatusLines: boolean;
  showAgentIcons: boolean;
  splitView: boolean;
  markedSessionId: string;
  language: string;
  terminalRenderer: TerminalRenderer;
  notifyOnWaiting: boolean;
  notifyDesktop: boolean;
  notifyNtfy: boolean;
  ntfyUrl: string;
}

export const settings = writable<Settings>({
  compactList: false,
  hideStatusLines: false,
  showAgentIcons: true,
  splitView: false,
  markedSessionId: '',
  language: 'en',
  terminalRenderer: 'canvas',
  notifyOnWaiting: false,
  notifyDesktop: true,
  notifyNtfy: false,
  ntfyUrl: ''
});

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
  settings.update(s => {
    const updated = { ...s, ...newSettings };
    App.SaveSettings(updated as any).catch(console.error);
    return updated;
  });
}
