// The order of a tab's views in the view bar: terminal, notes, tasks, files
// and diff.
//
// Set for every tab in Settings, and per tab from the tab's menu. A stored
// order is read leniently: anything unknown is dropped and any view it lacks
// is added at the end, so an order saved before a view existed still shows
// that view, and none can be lost by editing.

export type ViewId = 'terminal' | 'notes' | 'tasks' | 'browser' | 'diff';

export const DEFAULT_VIEW_ORDER: readonly ViewId[] = ['terminal', 'notes', 'tasks', 'browser', 'diff'];

/** Translation keys of the views' names, as the view bar shows them. */
export const VIEW_LABEL_KEYS: Record<ViewId, string> = {
  terminal: 'mainPanel.terminal',
  notes: 'mainPanel.notes',
  tasks: 'mainPanel.tasks',
  browser: 'mainPanel.browser',
  diff: 'tabBar.diffLabel',
};

function isViewId(value: unknown): value is ViewId {
  return typeof value === 'string' && (DEFAULT_VIEW_ORDER as readonly string[]).includes(value);
}

/**
 * Every view once: those `order` names, as it names them, then the rest in
 * the default order. The rest go last rather than to a guessed place, so a
 * view the user put somewhere stays there.
 */
export function normalizeViewOrder(order: readonly unknown[] | null | undefined): ViewId[] {
  const out: ViewId[] = [];
  for (const view of order || []) {
    if (isViewId(view) && !out.includes(view)) out.push(view);
  }
  for (const view of DEFAULT_VIEW_ORDER) {
    if (!out.includes(view)) out.push(view);
  }
  return out;
}

/** The order a tab shows: its own if it has one, the global one otherwise. */
export function resolveViewOrder(
  tabOrder: readonly unknown[] | null | undefined,
  globalOrder: readonly unknown[] | null | undefined,
): ViewId[] {
  return normalizeViewOrder(tabOrder && tabOrder.length ? tabOrder : globalOrder);
}

/** The same order with one view moved to another position. */
export function moveView(order: readonly ViewId[], from: number, to: number): ViewId[] {
  const next = [...order];
  if (from < 0 || from >= next.length || to < 0 || to >= next.length || from === to) return next;
  const [view] = next.splice(from, 1);
  next.splice(to, 0, view);
  return next;
}

/** Whether `order` is the default — stored as no order at all. */
export function isDefaultViewOrder(order: readonly ViewId[]): boolean {
  return order.length === DEFAULT_VIEW_ORDER.length && order.every((v, i) => v === DEFAULT_VIEW_ORDER[i]);
}
