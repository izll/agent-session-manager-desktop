/**
 * Every keyboard shortcut the app has, in one list.
 *
 * The bindings used to live in the handlers that acted on them and, separately,
 * as strings in the help dialog. Two lists meaning the same thing drift: the
 * help was where a shortcut's description lived, the code was where its keys
 * lived, and nothing tied them together. Now the code matches against this and
 * the help renders from it, so a rebound key changes both.
 *
 * A binding is stored as its parts rather than as a string like "Ctrl+Shift+N",
 * because matching a KeyboardEvent means comparing parts anyway, and parsing a
 * display string back into them is a source of mistakes that no longer exists.
 */

/** One key combination. */
export interface Binding {
  /** The `KeyboardEvent.key` value, lowercased for letters. */
  key: string;
  ctrl?: boolean;
  shift?: boolean;
  alt?: boolean;
}

/** What a shortcut is for, and what it is currently bound to. */
export interface Shortcut {
  /** Stable identifier — what customisations are stored against. */
  id: string;
  /** Which group it appears under in the help and the editor. */
  category: 'navigation' | 'session' | 'search' | 'other';
  /** i18n key for the description. */
  descKey: string;
  /** The default binding(s). Several where the app deliberately accepts more
   *  than one — the palette answers to both Ctrl+K and Ctrl+Shift+P. */
  defaults: Binding[];
  /**
   * Fixed shortcuts cannot be rebound, and the editor shows them greyed out
   * rather than hiding them — a user looking for Esc should find it and see
   * why, instead of wondering whether the list is incomplete.
   *
   * These are not arbitrary exclusions. Each is a key whose meaning comes from
   * the context it is pressed in rather than from us: Escape closes whatever is
   * open, Enter activates what is focused, and "/" starts filtering only while
   * the session list has focus. Rebinding them would not move a behaviour, it
   * would break the one place that behaviour makes sense.
   */
  fixed?: boolean;
  /**
   * Shown in the help instead of the binding, for the few entries that are not
   * key presses at all (Ctrl and the mouse wheel).
   */
  displayKey?: string;
}

export const SHORTCUTS: Shortcut[] = [
  // ---- Navigation ----
  {
    id: 'session.prev',
    category: 'navigation',
    descKey: 'help.navPrevSession',
    defaults: [
      { key: 'arrowup', ctrl: true, shift: true },
      { key: 'arrowup', alt: true },
    ],
  },
  {
    id: 'session.next',
    category: 'navigation',
    descKey: 'help.navNextSession',
    defaults: [
      { key: 'arrowdown', ctrl: true, shift: true },
      { key: 'arrowdown', alt: true },
    ],
  },
  {
    id: 'session.moveUp',
    category: 'navigation',
    descKey: 'help.navMoveUp',
    defaults: [{ key: 'k', ctrl: true, shift: true }],
  },
  {
    id: 'session.moveDown',
    category: 'navigation',
    descKey: 'help.navMoveDown',
    defaults: [{ key: 'j', ctrl: true, shift: true }],
  },
  {
    id: 'tab.next',
    category: 'navigation',
    descKey: 'help.navTabNext',
    defaults: [{ key: 'pagedown', ctrl: true }],
  },
  {
    id: 'tab.prev',
    category: 'navigation',
    descKey: 'help.navTabPrev',
    defaults: [{ key: 'pageup', ctrl: true }],
  },
  {
    id: 'session.attach',
    category: 'navigation',
    descKey: 'help.navAttach',
    defaults: [{ key: 'enter' }],
    fixed: true,
  },

  // ---- Session actions ----
  {
    id: 'session.new',
    category: 'session',
    descKey: 'help.actionNewSession',
    defaults: [{ key: 'n', ctrl: true, shift: true }],
  },
  {
    id: 'group.new',
    category: 'session',
    descKey: 'help.actionNewGroup',
    defaults: [{ key: 'g', ctrl: true, shift: true }],
  },
  {
    id: 'session.start',
    category: 'session',
    descKey: 'help.actionStartSession',
    defaults: [{ key: 's', ctrl: true, shift: true }],
  },
  {
    id: 'session.stop',
    category: 'session',
    descKey: 'help.actionStopSession',
    defaults: [{ key: 'x', ctrl: true, shift: true }],
  },
  {
    id: 'session.delete',
    category: 'session',
    descKey: 'help.actionDeleteSession',
    defaults: [{ key: 'd', ctrl: true, shift: true }],
  },
  {
    id: 'session.favorite',
    category: 'session',
    descKey: 'help.actionToggleFavorite',
    defaults: [{ key: '8', ctrl: true, shift: true }],
  },
  {
    id: 'history.show',
    category: 'other',
    descKey: 'help.actionGitHistory',
    // Not Ctrl+Shift+H, which opens the help; Y for "history" is free.
    defaults: [{ key: 'y', ctrl: true, shift: true }],
  },
  {
    id: 'quickJump.open',
    category: 'navigation',
    descKey: 'help.actionQuickJump',
    // Ctrl+J for "jump", and free: nothing else in the app claims it.
    defaults: [{ key: 'j', ctrl: true }],
  },
  {
    id: 'quickJump.add',
    category: 'navigation',
    descKey: 'help.actionQuickJumpAdd',
    // Not Ctrl+Shift+J, which already moves a session down the sidebar.
    // Alt+J keeps the same letter for the same idea without the collision.
    defaults: [{ key: 'j', alt: true }],
  },

  // ---- Search ----
  {
    id: 'palette.open',
    category: 'search',
    descKey: 'help.actionCommandPalette',
    defaults: [
      { key: 'k', ctrl: true },
      { key: 'p', ctrl: true, shift: true },
    ],
  },
  {
    id: 'search.global',
    category: 'search',
    descKey: 'help.actionGlobalSearch',
    defaults: [{ key: 'f', ctrl: true, shift: true }],
  },
  {
    id: 'terminal.search',
    category: 'search',
    descKey: 'help.actionSearchScrollback',
    defaults: [{ key: 'l', ctrl: true, shift: true }],
  },
  {
    id: 'commands.picker',
    category: 'search',
    descKey: 'help.actionCommandPicker',
    defaults: [{ key: 'p', ctrl: true }],
  },
  {
    id: 'diff.nextChange',
    category: 'navigation',
    descKey: 'help.diffNextChange',
    // F7/Shift+F7 is what IntelliJ and Visual Studio use for stepping through
    // a diff, so the keys are already in the fingers of anyone who reviews
    // changes in one of those.
    defaults: [{ key: 'f7', ctrl: true }],
  },
  {
    id: 'diff.prevChange',
    category: 'navigation',
    descKey: 'help.diffPrevChange',
    defaults: [{ key: 'f7', ctrl: true, shift: true }],
  },
  {
    id: 'session.filter',
    category: 'search',
    descKey: 'help.actionFilterSessions',
    defaults: [{ key: '/' }],
    fixed: true,
  },

  // ---- Other ----
  {
    id: 'help.show',
    category: 'other',
    descKey: 'help.actionShowHelp',
    defaults: [{ key: 'h', ctrl: true, shift: true }],
  },
  {
    id: 'update.check',
    category: 'other',
    descKey: 'help.actionCheckUpdates',
    defaults: [{ key: 'u', ctrl: true, shift: true }],
  },
  {
    id: 'sessions.import',
    category: 'other',
    descKey: 'help.actionImportSessions',
    defaults: [{ key: 'i', ctrl: true, shift: true }],
  },
  {
    id: 'terminal.fontReset',
    category: 'other',
    descKey: 'help.actionResetFontSize',
    defaults: [{ key: '0', ctrl: true }],
  },
  {
    id: 'terminal.zoom',
    category: 'other',
    descKey: 'help.actionZoomTerminal',
    defaults: [],
    fixed: true,
    displayKey: 'wheel',
  },
  {
    id: 'dialog.close',
    category: 'other',
    descKey: 'help.actionCloseDialogs',
    defaults: [{ key: 'escape' }],
    fixed: true,
  },
];

/** Favourite slots (Ctrl+Shift+1..7) are one entry in the help but seven
 *  bindings in the code, so they are described here rather than listed. */
export const FAVOURITE_SHORTCUT: Shortcut = {
  id: 'session.favouriteSlot',
  category: 'navigation',
  descKey: 'help.navFavorite',
  defaults: [],
  fixed: true,
  displayKey: 'Ctrl+Shift+1…7',
};

const BY_ID = new Map(SHORTCUTS.map((s) => [s.id, s]));

/** Look up a shortcut's definition. */
export function shortcutById(id: string): Shortcut | undefined {
  return BY_ID.get(id);
}
