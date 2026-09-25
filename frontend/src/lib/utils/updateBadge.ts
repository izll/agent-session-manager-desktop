/**
 * The marker a tab shows when its agent has an update waiting, read by the
 * backend from the agent's own notice on the pane (session/update_notice.go).
 *
 * Nothing here updates anything: the marker says an update is there and what
 * the user can do about it.
 */

export interface UpdateNotice {
  kind: 'available' | 'installed-restart';
  /** The agent is sitting on a prompt about it and does nothing until answered. */
  blocking: boolean;
  current?: string;
  version?: string;
  /** The agent's own command for updating, when its notice names one. */
  command?: string;
}

/**
 * 'blocking': the agent waits on an update prompt. 'restart': installed, the
 * tab still runs the old version. 'available': a newer version exists. '':
 * nothing to show.
 */
export type UpdateBadge = 'blocking' | 'restart' | 'available' | '';

export function updateBadge(tab: { update?: UpdateNotice | null } | undefined | null): UpdateBadge {
  const update = tab?.update;
  if (!update) return '';
  if (update.blocking) return 'blocking';
  if (update.kind === 'installed-restart') return 'restart';
  return 'available';
}

/** A translation key with its parameters. */
export interface Message {
  key: string;
  params: Record<string, string>;
}

/**
 * The tooltip: what is waiting, and what to do about it — two messages, the
 * caller translates and joins them.
 */
export function updateTooltip(
  tab: { update?: UpdateNotice | null } | undefined | null,
  agentName: string,
): Message[] {
  const update = tab?.update;
  if (!update) return [];
  const agent = agentName;
  let what: Message;
  if (update.kind === 'installed-restart') {
    what = update.version
      ? { key: 'sessionItem.updateInstalledVersion', params: { agent, version: update.version } }
      : { key: 'sessionItem.updateInstalled', params: { agent } };
  } else if (update.version && update.current) {
    what = { key: 'sessionItem.updateAvailableFrom', params: { agent, version: update.version, current: update.current } };
  } else if (update.version) {
    what = { key: 'sessionItem.updateAvailableVersion', params: { agent, version: update.version } };
  } else {
    what = { key: 'sessionItem.updateAvailable', params: { agent } };
  }

  let todo: Message;
  if (update.blocking) {
    todo = { key: 'sessionItem.updateHintPrompt', params: {} };
  } else if (update.kind === 'installed-restart') {
    todo = { key: 'sessionItem.updateHintRestart', params: {} };
  } else if (update.command) {
    todo = { key: 'sessionItem.updateHintCommand', params: { command: update.command } };
  } else {
    todo = { key: 'sessionItem.updateHintAvailable', params: {} };
  }
  return [what, todo];
}
