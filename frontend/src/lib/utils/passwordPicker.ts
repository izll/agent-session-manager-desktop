/** Choosing a password to type. Pure, so what is offered can be tested alone. */

export type PasswordEntry = {
  source: string;
  id: string;
  name: string;
  username: string;
  group: string;
};

/** A search matches name, user or group, in any case. */
export function filterPasswords(entries: PasswordEntry[], query: string): PasswordEntry[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return entries;
  return entries.filter((entry) =>
    `${entry.name} ${entry.username} ${entry.group}`.toLowerCase().includes(needle));
}

/**
 * Whether a tab runs an agent, which sends what it reads to a model provider.
 *
 * A terminal tab is a shell, and whatever else a custom one runs the user started
 * knowingly. Anything else gets asked about, including a tab not found, which is
 * the safer way to be wrong.
 */
export function tabRunsAgent(
  session: { agent?: string; followedWindows?: { index: number; agent?: string }[] } | null | undefined,
  windowIdx: number,
): boolean {
  if (!session) return true;
  const tab = (session.followedWindows || []).find((window) => window.index === windowIdx);
  const agent = (tab ? tab.agent : session.agent) || '';
  return agent !== 'terminal' && agent !== 'custom';
}

/** A source the user defined with two commands is named this way by the backend. */
export function isCustomSource(source: string): boolean {
  return source.startsWith('custom:');
}

/**
 * Starting points for a custom source, for tools with a list that prints one name a
 * line. Only that: nothing here is treated differently from what the user types.
 */
export const CUSTOM_SOURCE_PRESETS = [
  { name: 'gopass', list: 'gopass ls --flat', get: 'gopass show -o {name}' },
  { name: 'rbw', list: 'rbw ls', get: 'rbw get {name}' },
  { name: 'LastPass', list: 'lpass ls --format=%/as%/ag%an', get: 'lpass show --password {name}' },
] as const;

/** One Bitwarden account, with its server, is a source of its own. */
export function isBitwardenAccount(source: string): boolean {
  return source.startsWith('bitwarden:');
}

/** What a source is called: the user's name for it, or the name of its kind. */
export function sourceDisplayName(
  source: string,
  sources: { source: string; name?: string }[],
  translated: (key: string) => string,
): string {
  if (isCustomSource(source) || isBitwardenAccount(source)) {
    return sources.find((candidate) => candidate.source === source)?.name || '';
  }
  return translated(`passwords.source.${source}`);
}

const PRESS_ENTER_KEY = 'asmgr.passwords.pressEnter';

/** Whether Enter follows a password, as last set. Off until someone turns it on. */
export function loadPressEnter(): boolean {
  try {
    return localStorage.getItem(PRESS_ENTER_KEY) === 'true';
  } catch {
    return false;
  }
}

export function savePressEnter(value: boolean): void {
  try {
    localStorage.setItem(PRESS_ENTER_KEY, String(value));
  } catch {
    // Not remembered, which costs one click next time.
  }
}

