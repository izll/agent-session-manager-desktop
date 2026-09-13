/**
 * Accented characters typed through an input method, delivered exactly once.
 *
 * On Linux, WebKitGTK routes keys through the input method (IBus on GNOME).
 * Any key the IM turns into a non-ASCII character — every á, é, ő, ű on a
 * Hungarian layout — arrives as a keydown with keyCode 229, an input event
 * carrying the character, and a compositionend. There is no compositionstart.
 * Traced in a nested X server with IBus and the hu layout: plain letters take
 * the ordinary keydown path, only the accented ones take this one.
 *
 * xterm.js handles that badly twice over. Its keydown handler reads the
 * character back out of its hidden textarea a tick later and sends it. Its
 * compositionend handler sends it again, slicing the textarea from a start
 * position that only compositionstart sets — so the position is stale — and
 * the textarea is never cleared, because nothing but Enter and Ctrl+C clear
 * it. Typing é, á, ő produced "é", "á" "á", "ő" "áő": doubled, then tripled,
 * then whatever had piled up in the textarea.
 *
 * So such a commit is taken over here. The character is sent from the input
 * event's own data, the textarea is emptied so xterm's delayed read finds no
 * change, and the orphaned compositionend that follows is kept from reaching
 * xterm. A commit this does not recognise is not touched at all. A real
 * composition — a dead key, where compositionstart does fire — is left
 * entirely to xterm, which handles it correctly.
 *
 * The listeners sit on the container in the capture phase, so they run before
 * xterm's own listeners on the textarea.
 */
export function guardImeCommits(
  container: HTMLElement,
  terminal: {
    readonly textarea: HTMLTextAreaElement | undefined;
    input(data: string, wasUserInput?: boolean): void;
  },
  // Whether keys may reach the pane right now. xterm asks the same question of
  // its custom key handler on keydown; the input event is never put to it.
  acceptsInput: () => boolean = () => true,
): () => void {
  let composing = false;
  // After a real composition ends, xterm reads the composed text out of the
  // textarea on a later tick. Emptying the textarea before that would lose it.
  let finalizing = false;
  let imeKeydown = false;
  // Whether the last commit was sent from here. Only then is its
  // compositionend redundant; otherwise xterm still has to see it.
  let tookOver = false;

  const fromTextarea = (e: Event) =>
    !!terminal.textarea && e.target === terminal.textarea;

  const onKeydown = (e: KeyboardEvent) => {
    if (!fromTextarea(e)) return;
    imeKeydown = e.keyCode === 229;
    tookOver = false;
    // xterm snapshots the textarea on a 229 keydown and later sends whatever
    // was added to it. Starting from empty means it can only ever see the
    // commit that is taken over below — and that one is removed again.
    if (imeKeydown && !composing && !finalizing) terminal.textarea!.value = '';
  };

  const onCompositionStart = (e: CompositionEvent) => {
    if (fromTextarea(e)) composing = true;
  };

  const onCompositionEnd = (e: CompositionEvent) => {
    if (!fromTextarea(e)) return;
    if (!composing) {
      // Orphaned. If its character was sent from the input event, xterm must
      // not send it again; if not, xterm is the only one left to send it.
      if (tookOver) e.stopImmediatePropagation();
      tookOver = false;
      return;
    }
    composing = false;
    finalizing = true;
    // xterm's read is a setTimeout(0) scheduled after this one; wait past it.
    setTimeout(() => setTimeout(() => { finalizing = false; }, 0), 0);
  };

  const onInput = (e: Event) => {
    if (!fromTextarea(e) || composing) return;
    const ev = e as InputEvent;
    const committed = ev.inputType === 'insertFromComposition'
      || (ev.inputType === 'insertText' && imeKeydown);
    if (!committed || !ev.data) return;
    e.stopImmediatePropagation();
    tookOver = true;
    terminal.textarea!.value = '';
    if (acceptsInput()) terminal.input(ev.data, true);
  };

  container.addEventListener('keydown', onKeydown, true);
  container.addEventListener('compositionstart', onCompositionStart, true);
  container.addEventListener('compositionend', onCompositionEnd, true);
  container.addEventListener('input', onInput, true);
  return () => {
    container.removeEventListener('keydown', onKeydown, true);
    container.removeEventListener('compositionstart', onCompositionStart, true);
    container.removeEventListener('compositionend', onCompositionEnd, true);
    container.removeEventListener('input', onInput, true);
  };
}
