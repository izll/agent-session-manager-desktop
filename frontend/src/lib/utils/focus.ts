// Request the active terminal to take keyboard focus.
// Called after actions that cause focus loss (closing dialogs, deleting tabs,
// keyboard shortcut commands, etc.) so subsequent keypresses go to the terminal.
export function focusTerminal() {
  // Skip if user is typing in an input/textarea
  const active = document.activeElement;
  if (active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement) {
    return;
  }
  window.dispatchEvent(new CustomEvent('terminal:focus'));
}
