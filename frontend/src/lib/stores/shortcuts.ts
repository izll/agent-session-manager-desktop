import { derived, get, writable } from 'svelte/store';
import { settings, saveSettings } from './settings';
import { SHORTCUTS, type Binding, type Shortcut } from '../utils/shortcuts';

/**
 * Which keys each shortcut answers to, after the user's changes.
 *
 * Only rebound shortcuts are stored. A shortcut the user has not touched keeps
 * following its default, so moving a default in a later version reaches
 * everyone who never customised it — rather than being pinned to the old value
 * by a stored copy nobody asked for.
 */

/** The user's changes, keyed by shortcut id. */
export type ShortcutOverrides = Record<string, Binding[]>;

/**
 * Bindings in force for every shortcut, defaults included.
 *
 * A stored EMPTY list means the user turned the shortcut off, which is not the
 * same as never having touched it — the first answers to nothing, the second
 * follows its default. Removing the entry is what restores the default, so the
 * two states stay distinguishable.
 */
export const effectiveBindings = derived(settings, ($settings) => {
  const overrides = ($settings.shortcutOverrides || {}) as ShortcutOverrides;
  const map = new Map<string, Binding[]>();
  for (const shortcut of SHORTCUTS) {
    const custom = shortcut.fixed ? undefined : overrides[shortcut.id];
    map.set(shortcut.id, custom === undefined ? shortcut.defaults : custom);
  }
  return map;
});

/** Whether the user has switched this shortcut off entirely. */
export function isDisabled(id: string, overrides: ShortcutOverrides): boolean {
  const custom = overrides[id];
  return Array.isArray(custom) && custom.length === 0;
}

/** The bindings one shortcut currently answers to. */
export function bindingsFor(id: string): Binding[] {
  return get(effectiveBindings).get(id) || [];
}

/**
 * Whether a key event is this shortcut.
 *
 * Modifiers are compared exactly, in both directions: a binding without Shift
 * must not fire when Shift is held, or Ctrl+Shift+P would also trigger Ctrl+P
 * and the user would get two actions from one press.
 */
export function eventMatches(e: KeyboardEvent, binding: Binding): boolean {
  // e.key is uppercase for a shifted letter and layout-dependent for symbols;
  // lowercasing is what makes "n" and "N" the same binding.
  if (e.key.toLowerCase() !== binding.key) return false;
  // metaKey counts as ctrl so a Mac user's Cmd works without a separate
  // binding, which is how the shortcuts behaved before they were configurable.
  if (!!binding.ctrl !== (e.ctrlKey || e.metaKey)) return false;
  if (!!binding.shift !== e.shiftKey) return false;
  if (!!binding.alt !== e.altKey) return false;
  return true;
}

/** Whether a key event triggers the given shortcut. */
export function matchesShortcut(e: KeyboardEvent, id: string): boolean {
  return bindingsFor(id).some((b) => eventMatches(e, b));
}

/** The id of whichever shortcut this event triggers, if any. */
export function shortcutForEvent(e: KeyboardEvent): string | null {
  for (const [id, bindings] of get(effectiveBindings)) {
    if (bindings.some((b) => eventMatches(e, b))) return id;
  }
  return null;
}

/**
 * Rebind a shortcut. Passing an empty list switches it OFF — see
 * restoreDefaultBinding() for putting it back to its default instead.
 *
 * Fixed shortcuts are refused rather than silently ignored: the caller is
 * asking for something that cannot happen, and failing quietly would leave the
 * editor showing a change that was never made.
 */
export async function setBinding(id: string, bindings: Binding[]): Promise<void> {
  const shortcut = requireReboundable(id);
  const current = { ...(get(settings).shortcutOverrides || {}) } as ShortcutOverrides;
  current[shortcut.id] = bindings;
  await saveSettings({ shortcutOverrides: current });
}

/** Switch a shortcut off, so it answers to nothing. */
export async function disableBinding(id: string): Promise<void> {
  await setBinding(id, []);
}

/** Drop the user's change, so the shortcut follows its default again. */
export async function restoreDefaultBinding(id: string): Promise<void> {
  const shortcut = requireReboundable(id);
  const current = { ...(get(settings).shortcutOverrides || {}) } as ShortcutOverrides;
  delete current[shortcut.id];
  await saveSettings({ shortcutOverrides: current });
}

function requireReboundable(id: string) {
  const shortcut = SHORTCUTS.find((s) => s.id === id);
  if (!shortcut) throw new Error(`no such shortcut: ${id}`);
  if (shortcut.fixed) throw new Error(`${id} cannot be rebound`);
  return shortcut;
}

/** Restore every shortcut to its default. */
export async function resetAllBindings(): Promise<void> {
  await saveSettings({ shortcutOverrides: {} });
}

/**
 * Shortcuts that would fire on the same keys as `binding`.
 *
 * Checked before a change is accepted: two actions on one press is not a state
 * the user can get out of except by finding the other shortcut themselves.
 */
export function conflictsWith(binding: Binding, exceptId: string): Shortcut[] {
  const bindings = get(effectiveBindings);
  const clashes: Shortcut[] = [];
  for (const shortcut of SHORTCUTS) {
    if (shortcut.id === exceptId) continue;
    const theirs = bindings.get(shortcut.id) || [];
    if (theirs.some((b) => sameBinding(b, binding))) clashes.push(shortcut);
  }
  return clashes;
}

function sameBinding(a: Binding, b: Binding): boolean {
  return a.key === b.key &&
    !!a.ctrl === !!b.ctrl &&
    !!a.shift === !!b.shift &&
    !!a.alt === !!b.alt;
}

/** A binding as the user reads it: "Ctrl+Shift+N". */
export function formatBinding(binding: Binding, isMac = false): string {
  const parts: string[] = [];
  if (binding.ctrl) parts.push(isMac ? '⌘' : 'Ctrl');
  if (binding.shift) parts.push(isMac ? '⇧' : 'Shift');
  if (binding.alt) parts.push(isMac ? '⌥' : 'Alt');
  parts.push(formatKey(binding.key));
  return parts.join('+');
}

/** Key names as printed on a keyboard, rather than as the DOM spells them. */
function formatKey(key: string): string {
  const named: Record<string, string> = {
    arrowup: '↑',
    arrowdown: '↓',
    arrowleft: '←',
    arrowright: '→',
    pageup: 'PgUp',
    pagedown: 'PgDn',
    escape: 'Esc',
    enter: 'Enter',
    ' ': 'Space',
  };
  return named[key] || key.toUpperCase();
}

/** Turn a key event into the binding it represents, for the capture field. */
export function bindingFromEvent(e: KeyboardEvent): Binding | null {
  const key = e.key.toLowerCase();
  // A modifier on its own is the user still reaching for the combination.
  if (['control', 'shift', 'alt', 'meta'].includes(key)) return null;
  return {
    key,
    ctrl: e.ctrlKey || e.metaKey,
    shift: e.shiftKey,
    alt: e.altKey,
  };
}

/** Set while the editor is capturing, so the global handler stands aside and
 *  the keys being recorded do not also run the actions they are bound to. */
export const capturingShortcut = writable(false);
