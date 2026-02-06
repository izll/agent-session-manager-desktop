import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export interface Agent {
  type: string;
  name: string;
  icon: string;
  supportsResume: boolean;
  supportsAutoYes: boolean;
  supportsFork: boolean;
}

export const agents = writable<Agent[]>([]);

export async function loadAgents() {
  try {
    const data = await App.GetAgents();
    agents.set(data as Agent[]);
  } catch (e) {
    console.error('Failed to load agents:', e);
  }
}

export function getAgentIcon(agentType: string): string {
  const icons: Record<string, string> = {
    'claude': '🤖',
    'gemini': '💎',
    'aider': '🔧',
    'codex': '📦',
    'amazonq': '🦜',
    'opencode': '💻',
    'custom': '⚙️',
    'terminal': '🖥️'
  };
  return icons[agentType] || '⚙️';
}

export function getAgentName(agentType: string): string {
  const names: Record<string, string> = {
    'claude': 'Claude',
    'gemini': 'Gemini',
    'aider': 'Aider',
    'codex': 'Codex',
    'amazonq': 'Amazon Q',
    'opencode': 'OpenCode',
    'custom': 'Custom',
    'terminal': 'Terminal'
  };
  return names[agentType] || agentType;
}
