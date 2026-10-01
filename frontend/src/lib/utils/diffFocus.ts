/**
 * Whether switching to the diff may take the keyboard from the element that
 * has it.
 *
 * From nowhere, and from a terminal — xterm reads keys through a hidden
 * textarea, which is the one field that is not being typed in by choice here.
 * Not from anything inside a dialog, nor from a field or an editable area: the
 * switch must not swallow what someone is typing.
 */
export function diffTakesFocusFrom(element: Element | null, own?: Element | null): boolean {
  if (!element || element === document.body || element === document.documentElement) return true;
  // A dialog keeps its focus — unless the view asking lives in that very
  // dialog, as the project note does in the project tasks window.
  const dialog = element.closest('[role="dialog"], [aria-modal="true"]');
  if (dialog && !(own && dialog.contains(own))) return false;
  if (element.classList.contains('xterm-helper-textarea')) return true;
  const tag = element.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return false;
  if ((element as HTMLElement).isContentEditable) return false;
  return true;
}
