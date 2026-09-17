import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export interface Agent {
  type: string;
  name: string;
  icon: string;
  supportsResume: boolean;
  supportsAutoYes: boolean;
  supportsFork: boolean;
  /** Whether the agent's command is on PATH, as the backend saw it. */
  installed: boolean;
  /** The agent's own installation page; empty for Terminal and Custom. */
  installUrl?: string;
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

/**
 * Which agents are installed on a server.
 *
 * Asked separately rather than through the shared store: the store holds what
 * this computer has, and several dialogs read it at once. A remote answer
 * replacing it would leave every other view describing the wrong machine.
 *
 * An empty server id answers about this computer, so a caller with a
 * possibly-remote session does not have to branch.
 */
export async function loadAgentsForServer(serverId: string): Promise<Agent[]> {
  try {
    if (!serverId) {
      return (await App.GetAgents()) as Agent[];
    }
    return (await App.GetAgentsForServer(serverId)) as Agent[];
  } catch (e) {
    console.error('Failed to load agents for server:', e);
    // Empty rather than the local list: claiming a server has agents it does
    // not is worse than showing none, because the failure then happens at
    // start-up with a worse message.
    return [];
  }
}

export function getAgentIcon(agentType: string): string {
  const icons: Record<string, string> = {
    'claude': '🤖',
    'antigravity': '🛸',
    'gemini': '💎',
    'aider': '🔧',
    'codex': '📦',
    'cursor': '🔷',
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
    'antigravity': 'Antigravity',
    'gemini': 'Gemini',
    'aider': 'Aider',
    'codex': 'Codex',
    'cursor': 'Cursor',
    'amazonq': 'Amazon Q',
    'opencode': 'OpenCode',
    'custom': 'Custom',
    'terminal': 'Terminal'
  };
  return names[agentType] || agentType;
}
