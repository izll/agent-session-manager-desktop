/**
 * The dictation hotkey, kept where the terminal can see it.
 *
 * The hotkey is registered system-wide through gohook, which observes key
 * events rather than claiming them — on X11 it cannot claim them at all. So
 * pressing it while a pane has focus both starts dictation AND delivers the
 * keystroke to whatever is running there: Ctrl+Alt+G reached Codex and killed
 * the agent.
 *
 * The listener cannot swallow the event, so the pane has to decline it. That
 * means the terminal needs to know which combination is currently bound, and
 * the binding is a user setting rather than a constant.
 */

export interface DictationHotkey {
  ctrl: boolean;
  alt: boolean;
  shift: boolean;
  key: string;
}

let current: DictationHotkey | null = null;

/** Records the binding the backend reported. Called wherever settings load. */
export function setDictationHotkey(hotkey: Partial<DictationHotkey> | null | undefined): void {
  const key = typeof hotkey?.key === 'string' ? hotkey.key.trim().toLowerCase() : '';
  if (!key) {
    current = null;
    return;
  }
  current = {
    ctrl: !!hotkey?.ctrl,
    alt: !!hotkey?.alt,
    shift: !!hotkey?.shift,
    key,
  };
}

export function getDictationHotkey(): DictationHotkey | null {
  return current;
}

/**
 * True when this event is the dictation hotkey.
 *
 * Compares event.key rather than event.code so a rebind to a letter matches
 * what the user actually typed on their layout. A binding with no modifier at
 * all is ignored: a bare letter would make the pane undoing every press of it,
 * which is a far worse failure than the one being fixed.
 */
export function matchesDictationHotkey(event: KeyboardEvent): boolean {
  const hotkey = current;
  if (!hotkey) return false;
  if (!hotkey.ctrl && !hotkey.alt && !hotkey.shift) return false;
  if (event.ctrlKey !== hotkey.ctrl) return false;
  if (event.altKey !== hotkey.alt) return false;
  if (event.shiftKey !== hotkey.shift) return false;
  return typeof event.key === 'string' && event.key.toLowerCase() === hotkey.key;
}
