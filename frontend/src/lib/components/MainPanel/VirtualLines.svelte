<script lang="ts">
  /**
   * Renders a long list of diff lines by keeping only the visible ones in the
   * DOM.
   *
   * Whole-file diffs are the reason this exists. Showing a change in the file
   * it lives in means every line of that file is in the list, and a 3000-line
   * file used to mean 3000 <div><code> nodes inserted synchronously — which is
   * what froze the app on a large repository and forced the 2000-line cap the
   * whole-file view would otherwise hit immediately.
   *
   * Every line has the same height, which is what makes this simple: the
   * scroll position gives the first visible index by division, and the rest is
   * two spacers. A diff line is a single row of monospace text with no
   * wrapping, so equal heights are a property of the content, not an
   * approximation.
   */
  import { createEventDispatcher, onMount, tick } from 'svelte';
  import { memoHighlightLine } from '../../utils/highlightLine';
  import type { LanguageSupport } from '@codemirror/language';

  /** One entry per rendered row. */
  /** The diff's lines as TEXT. Colouring happens per visible line — see
   *  `slice` below — because parsing the whole file up front is what made a
   *  large one slow to open. */
  export let lines: Array<{ type: string; text: string; hunkIndex: number }> = [];
  /** The grammar to colour with, or null for plain text. */
  export let language: LanguageSupport | null = null;
  /** Row height in px. Must match the CSS, or rows drift out of place. */
  export let lineHeight = 18;
  /**
   * Rows rendered beyond each edge of the viewport. Enough that a flick of the
   * wheel does not outrun the update and show blank space; small enough that
   * the DOM stays short.
   */
  export let overscan = 20;
  /**
   * The run of lines the change-stepping is currently on, marked down the left
   * edge. Both ends inclusive; -1 marks none.
   *
   * Line positions rather than a hunk number: in whole-file view git is asked
   * for the entire file as one hunk, so every change in the file shares a hunk
   * index — marking by it lit up all of them at once.
   */
  export let blockFrom = -1;
  export let blockTo = -1;

  const dispatch = createEventDispatcher();

  let viewport: HTMLDivElement | null = null;
  let scrollTop = 0;
  let viewportHeight = 0;

  /**
   * A new list starts at the top.
   *
   * The viewport keeps its scroll position when its contents are replaced, so
   * opening a file after a longer one left it scrolled to wherever the previous
   * file had been — most visibly when stepping past the last file back to the
   * first, which arrived showing its final lines.
   *
   * Compared by identity: buildFlatLines produces a new array per file, and the
   * contents are large enough that comparing them would cost more than the
   * scroll it corrects.
   */
  let lastLines: unknown = null;
  $: if (lines !== lastLines) {
    lastLines = lines;
    scrollTop = 0;
    if (viewport) viewport.scrollTop = 0;
  }

  $: total = lines.length;
  $: first = Math.max(0, Math.floor(scrollTop / lineHeight) - overscan);
  $: visibleCount = Math.ceil(viewportHeight / lineHeight) + overscan * 2;
  $: last = Math.min(total, first + visibleCount);
  /**
   * The visible lines, coloured as they are taken.
   *
   * This is where the saving is: a 10,000-line file costs about 500ms to
   * colour in full and under a millisecond for one screenful. The cache in
   * memoHighlightLine is what keeps scrolling cheap — a line leaving and
   * re-entering the viewport is not parsed twice.
   */
  $: slice = lines.slice(first, last).map((line) => ({
    ...line,
    html: memoHighlightLine(line.text, language),
  }));
  $: topPad = first * lineHeight;
  $: bottomPad = Math.max(0, (total - last) * lineHeight);

  function onScroll() {
    if (viewport) scrollTop = viewport.scrollTop;
    // Announced so the position can be recorded while it is still readable: on
    // the way out the binding is already gone.
    dispatch('viewscroll');
  }

  function measure() {
    if (viewport) viewportHeight = viewport.clientHeight;
  }

  onMount(() => {
    measure();
    // The panel is resizable (the diff splitter) and the window is too, so the
    // visible count cannot be worked out once.
    const observer = new ResizeObserver(measure);
    if (viewport) observer.observe(viewport);
    return () => observer.disconnect();
  });

  /**
   * Scroll a change into view.
   *
   * A third of the way down, so the change is read with the code above it —
   * pinned to the top edge that context is hidden, and centred half the screen
   * goes on context already read.
   *
   * Unless the change is too tall for that: a block longer than the room below
   * a third would run off the bottom, so a long one starts near the top and
   * uses the whole viewport. `lines` is how many lines the change spans.
   */
  export async function scrollToLine(index: number, lines = 1) {
    if (!viewport) return;
    await tick();

    const view = viewport.clientHeight;
    const blockHeight = Math.max(1, lines) * lineHeight;
    const margin = blockHeight > view - view / 3
      ? Math.min(view / 8, lineHeight * 3)
      : view / 3;

    viewport.scrollTo({ top: Math.max(0, index * lineHeight - margin), behavior: 'smooth' });
  }

  /** The first line index currently on screen, for "which hunk am I on". */
  export function firstVisibleLine(): number {
    return Math.floor(scrollTop / lineHeight);
  }

  /** The exact offset, for putting the view back where it was after a tab
   *  switch destroyed and rebuilt it. Not firstVisibleLine(): that rounds to a
   *  line, and restoring from it would nudge the file every time. */
  export function scrollOffset(): number {
    return scrollTop;
  }

  /**
   * Put the view back at an offset it was at before.
   *
   * Without the tick the height is not yet in place — the list is rendered from
   * `slice`, which the reset above has just emptied — and the assignment is
   * clamped to a scrollHeight that does not exist yet, landing at the top.
   */
  export async function restoreOffset(top: number) {
    if (!viewport || top <= 0) return;
    await tick();
    viewport.scrollTop = top;
    scrollTop = viewport.scrollTop;
  }
</script>

<div class="virtual-viewport" bind:this={viewport} on:scroll={onScroll}>
  <div style="height: {topPad}px"></div>
  {#each slice as line, i (first + i)}
    <div
      class="diff-line {line.type}"
      class:in-block={blockFrom >= 0 && first + i >= blockFrom && first + i <= blockTo}
      style="height: {lineHeight}px"
    >
      <!-- Already escaped by the highlighter; see utils/highlightLine.ts. -->
      <code>{@html line.html}</code>
    </div>
  {/each}
  <div style="height: {bottomPad}px"></div>
</div>

<style>
  .virtual-viewport {
    height: 100%;
    overflow-y: auto;
    overflow-x: auto;
  }
  .diff-line {
    /* Fixed height, matching lineHeight: the arithmetic above depends on it,
       and a row that grows would put every row after it out of position.
       The horizontal padding matches the other view so the code starts at the
       same column in both; vertical padding would eat into the fixed height,
       so the line-height does that work instead. */
    box-sizing: border-box;
    white-space: pre;
    padding: 0 16px;
    line-height: 18px;
  }
  .diff-line code {
    font-family: inherit;
    font-size: inherit;
  }

  /* The add/remove tints live here rather than in Diff.svelte, because Svelte
     scopes CSS to the component that declares it: the rules there apply to the
     lines that component renders, and these lines are rendered by this one.
     Kept identical to Diff.svelte's — the two views must not disagree about
     what an added line looks like.

     The tint alone carries added/removed; the text keeps its syntax colours. */
  .diff-line.add {
    background: rgba(34, 197, 94, 0.08);
  }
  .diff-line.remove {
    background: rgba(239, 68, 68, 0.08);
  }
  .diff-line.header {
    color: #7f848e;
    background: rgba(255, 255, 255, 0.03);
  }
  .diff-line.meta {
    color: #6b7280;
  }

  /* The change the arrows are on. A bar down the left edge rather than a tint:
     the add/remove tints already carry meaning on these lines, and a second
     one over them would say two things at once about the same line. */
  .diff-line.in-block.add,
  .diff-line.in-block.remove {
    box-shadow: inset 3px 0 0 var(--accent, #61afef);
  }
</style>
