/**
 * Tabs in the order the user arranged them.
 *
 * tabOrder lists window indexes in display order; a tab it does not mention
 * (added since the order was saved) goes after the ones it does, in the order
 * it came. The tab bar and the session list both sort with this, so a
 * reordered tab sits in the same place in both.
 */
export function sortByTabOrder<T>(tabs: T[], tabOrder: number[] | undefined | null, indexOf: (tab: T) => number): T[] {
  if (!tabOrder || tabOrder.length === 0) return tabs;
  const position = new Map<number, number>();
  tabOrder.forEach((windowIdx, pos) => position.set(windowIdx, pos));
  const at = (tab: T) => position.get(indexOf(tab)) ?? Number.MAX_SAFE_INTEGER;
  return [...tabs].sort((a, b) => at(a) - at(b));
}
