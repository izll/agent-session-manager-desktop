/**
 * Which key presses step through the sessions, for the terminal to refuse.
 *
 * The step itself is the app's shortcut handler, which listens on the window
 * in the capture phase. The pane only has to keep the key out of the program
 * running in it — and only when the app is going to act on it. It used to
 * refuse Alt+Up/Down unconditionally, so after the shortcut was rebound or
 * switched off, the pane still never received those keys, and the keys it was
 * rebound to reached the pane as well as stepping.
 *
 * The bindings live in the shortcut store, which utils/terminal cannot import:
 * the store reads settings, and settings imports utils/terminal, so a static
 * import would be a cycle — one that breaks at load time, because the store
 * builds a derived store from settings the moment it is evaluated. The store
 * is handed in instead, by the component that owns the terminals, before any
 * terminal exists. No imports here, so the rule can be run under plain node.
 */

/** Which shortcut a key event triggers, if any: shortcutForEvent. */
export type ShortcutResolver = (event: KeyboardEvent) => string | null;

let resolveShortcut: ShortcutResolver | null = null;

export function registerShortcutResolver(resolver: ShortcutResolver | null): void {
  resolveShortcut = resolver;
}

/**
 * Whether the app will step to another session on this key.
 *
 * Decided the way the app's handler decides it, so the two cannot disagree:
 * that handler ignores keys without Ctrl, Cmd or Alt, and acts on the first
 * shortcut the event resolves to. Before anything is registered nothing is
 * refused — a key kept from the pane that nothing acts on is simply lost.
 */
export function isSessionStepKey(event: KeyboardEvent): boolean {
  if (!resolveShortcut) return false;
  if (!event.ctrlKey && !event.metaKey && !event.altKey) return false;
  const id = resolveShortcut(event);
  return id === 'session.prev' || id === 'session.next';
}
