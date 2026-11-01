/**
 * Adding up token usage for the usage view. Pure, so what the numbers mean can
 * be tested without a browser.
 */

export type TokenUsageItem = {
  machine: string;
  machineName: string;
  conversation: string;
  agent: string;
  cwd: string;
  lastUnix: number;
  model: string;
  subagent: boolean;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  responses: number;
  projectId: string;
  projectName: string;
  sessionId: string;
  sessionName: string;
  tabName: string;
};

/** Which project the user put a row in, by usageRowKey. */
export type UsageLinks = Record<string, string>;

/** The row someone sees in the session grouping, which is what gets linked. */
export function usageRowKey(item: TokenUsageItem): string {
  return usageGroupKey(item, 'session');
}

export type UsageGroupBy = 'session' | 'project' | 'machine' | 'model';

/**
 * What a share is a share of.
 *
 * 'new' leaves cache reads out. They were 96% of everything measured on a real
 * machine, because every turn re-reads the whole conversation from the cache:
 * counted in, they say which conversation is longest rather than which one is
 * consuming the most, and they swamp every other figure.
 */
export type UsageBasis = 'new' | 'all';

export type UsageTotals = {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  responses: number;
  /** The basis figure of what subagents did. */
  subagent: number;
};

export type UsageConversation = UsageTotals & {
  key: string;
  /** Lowercase id and tab name: what names this one conversation. */
  searchOwn: string;
  conversation: string;
  tabName: string;
  agent: string;
  models: string[];
  lastUnix: number;
  value: number;
  share: number;
};

export type UsageGroup = UsageTotals & {
  key: string;
  /** How the group is named, resolved by the view since some are translated. */
  sample: TokenUsageItem;
  /** Lowercase text of everything in the group, for the filter. */
  search: string;
  value: number;
  share: number;
  subagentShare: number;
  conversations: UsageConversation[];
};

export function usageValue(totals: Pick<UsageTotals, 'input' | 'output' | 'cacheRead' | 'cacheWrite'>, basis: UsageBasis): number {
  const fresh = totals.input + totals.output + totals.cacheWrite;
  return basis === 'all' ? fresh + totals.cacheRead : fresh;
}

function emptyTotals(): UsageTotals {
  return { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, responses: 0, subagent: 0 };
}

function addItem(totals: UsageTotals, item: TokenUsageItem, basis: UsageBasis) {
  totals.input += item.input;
  totals.output += item.output;
  totals.cacheRead += item.cacheRead;
  totals.cacheWrite += item.cacheWrite;
  totals.responses += item.responses;
  if (item.subagent) totals.subagent += usageValue(item, basis);
}

/**
 * The key an item is grouped under.
 *
 * A conversation no session could be tied to is grouped by machine and
 * directory, so an agent started in a terminal elsewhere still reads as one
 * line per place rather than one per conversation.
 *
 * "Not in any session" is told apart from the default project, whose id is
 * also empty.
 */
export function usageGroupKey(item: TokenUsageItem, groupBy: UsageGroupBy, links: UsageLinks = {}): string {
  switch (groupBy) {
    case 'project': {
      // By the name's letters, not its case: what was typed on two rows on two
      // machines is meant to be one project.
      const linked = links[usageRowKey(item)];
      if (linked) return `link:${linked.toLowerCase()}`;
      return item.sessionId ? `project:${item.projectId}` : 'outside';
    }
    case 'machine':
      return `machine:${item.machine}`;
    case 'model':
      return `model:${item.model}`;
    default:
      return item.sessionId ? `session:${item.sessionId}` : `dir:${item.machine}:${item.cwd}`;
  }
}

export function groupUsage(items: TokenUsageItem[], groupBy: UsageGroupBy, basis: UsageBasis, links: UsageLinks = {}): { groups: UsageGroup[]; totals: UsageTotals; value: number } {
  const totals = emptyTotals();
  const groups = new Map<string, UsageGroup>();
  const conversations = new Map<string, Map<string, UsageConversation>>();

  for (const item of items) {
    addItem(totals, item, basis);

    const key = usageGroupKey(item, groupBy, links);
    let group = groups.get(key);
    if (!group) {
      group = { ...emptyTotals(), key, sample: item, search: '', value: 0, share: 0, subagentShare: 0, conversations: [] };
      groups.set(key, group);
      conversations.set(key, new Map());
    }
    addItem(group, item, basis);
    group.search += ` ${itemSearchText(item)} ${(links[usageRowKey(item)] || '').toLowerCase()}`;

    const conversationKey = `${item.machine}:${item.conversation}`;
    const inGroup = conversations.get(key)!;
    let conversation = inGroup.get(conversationKey);
    if (!conversation) {
      conversation = {
        ...emptyTotals(), key: conversationKey, searchOwn: `${item.conversation} ${item.tabName}`.toLowerCase(),
        conversation: item.conversation, tabName: item.tabName,
        agent: item.agent, models: [], lastUnix: 0, value: 0, share: 0,
      };
      inGroup.set(conversationKey, conversation);
    }
    addItem(conversation, item, basis);
    if (item.model && !conversation.models.includes(item.model)) conversation.models.push(item.model);
    conversation.lastUnix = Math.max(conversation.lastUnix, item.lastUnix);
  }

  const value = usageValue(totals, basis);
  const sorted = [...groups.values()];
  for (const group of sorted) {
    group.value = usageValue(group, basis);
    group.share = value > 0 ? group.value / value : 0;
    group.subagentShare = group.value > 0 ? group.subagent / group.value : 0;
    group.conversations = [...conversations.get(group.key)!.values()];
    for (const conversation of group.conversations) {
      conversation.value = usageValue(conversation, basis);
      conversation.share = group.value > 0 ? conversation.value / group.value : 0;
    }
    group.conversations.sort((a, b) => b.value - a.value || a.key.localeCompare(b.key));
  }
  sorted.sort((a, b) => b.value - a.value || a.key.localeCompare(b.key));
  return { groups: sorted, totals, value };
}

function itemSearchText(item: TokenUsageItem): string {
  return [item.sessionName, item.tabName, item.projectName, item.machineName, item.cwd, item.conversation, item.model, item.agent]
    .join(' ').toLowerCase();
}

/**
 * The groups a search finds, with their figures left whole.
 *
 * Shares stay shares of everything. Worked out again from what the filter left,
 * the session being looked for would always read as 100%, which is the one thing
 * it is not.
 */
export function filterUsageGroups(groups: UsageGroup[], query: string): UsageGroup[] {
  const needle = query.trim().toLowerCase();
  return needle ? groups.filter((group) => group.search.includes(needle)) : groups;
}

/** Whether a search names one of the group's conversations, so it opens by itself. */
export function searchFindsConversation(group: UsageGroup, query: string): boolean {
  const needle = query.trim().toLowerCase();
  return !!needle && group.conversations.some((conversation) => conversation.searchOwn.includes(needle));
}

/** The project names already used, for offering them again. */
export function usageLinkNames(links: UsageLinks): string[] {
  return [...new Set(Object.values(links))].sort((a, b) => a.localeCompare(b));
}

/** 1 234 567 reads as 1.2 M: the magnitude is what is being compared. */
export function formatTokens(value: number): string {
  const units: [number, string][] = [[1e12, 'T'], [1e9, 'G'], [1e6, 'M'], [1e3, 'k']];
  for (const [size, unit] of units) {
    if (value >= size) {
      const scaled = value / size;
      return `${scaled >= 100 ? scaled.toFixed(0) : scaled.toFixed(1)} ${unit}`;
    }
  }
  return String(Math.round(value));
}

/**
 * A share as a figure. Below a tenth of a percent it says so rather than "0%",
 * which reads as nothing at all.
 */
export function formatShare(share: number): string {
  if (share <= 0) return '0%';
  if (share < 0.001) return '<0.1%';
  return `${(share * 100).toFixed(1)}%`;
}

/** The last two parts of a path: enough to recognise it, short enough for a row. */
export function shortDirectory(directory: string): string {
  if (!directory) return '';
  const parts = directory.replace(/\\/g, '/').split('/').filter(Boolean);
  return parts.slice(-2).join('/') || directory;
}
