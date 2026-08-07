<script lang="ts">
  /**
   * A file's diff as two aligned columns.
   *
   * A unified diff is one column with markers: what was removed, then what
   * replaced it. Side by side the same change is two columns with the pairs
   * level, which is what makes "this became that" legible at a glance rather
   * than reconstructed by counting lines.
   *
   * Both columns scroll as one. They are two halves of a single reading, and
   * letting them drift apart would defeat the alignment the rows exist for.
   */
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n';
  import { buildSideBySide, type SideBySideRow, type UnifiedLine } from '../../utils/sideBySide';

  /** One entry per hunk: its header and its lines, already highlighted. */
  export let hunks: Array<{ header: string; lines: UnifiedLine[]; index: number }> = [];
  /** The hunk the change-stepping is on, marked down the left edge. -1 marks none. */
  export let currentHunk = -1;
  /** Whether the arrows offer to revert. Off where there is nothing to write
   *  back to — a historical commit is read, not edited. */
  export let canRevert = false;

  const dispatch = createEventDispatcher();

  let scroller: HTMLDivElement | null = null;

  $: sections = hunks.map((hunk) => {
    const rows = buildSideBySide(hunk.header, hunk.lines) as SideBySideRow[];
    // Which rows start a block, so the arrow appears once at the top of each
    // rather than on every changed row: it acts on the whole block, and one
    // per line would be a column of buttons that all do the same thing.
    const blockStarts = new Set<number>();
    rows.forEach((row, at) => {
      if (row.kind !== 'change') return;
      if (rows[at - 1]?.kind !== 'change') blockStarts.add(at);
    });
    // Each block as a shape to draw: which rows it covers, and how far the
    // run reaches on each side. A block is often one line on one side and
    // several on the other, and the ribbon between them is what makes that
    // pairing readable without counting rows.
    const blocks: Array<{ start: number; end: number; oldLines: number; newLines: number }> = [];
    rows.forEach((row, at) => {
      if (row.kind !== 'change') return;
      const previous = blocks[blocks.length - 1];
      if (previous && previous.end === at - 1) {
        previous.end = at;
        if (row.oldHtml !== null) previous.oldLines++;
        if (row.newHtml !== null) previous.newLines++;
        return;
      }
      blocks.push({
        start: at,
        end: at,
        oldLines: row.oldHtml !== null ? 1 : 0,
        newLines: row.newHtml !== null ? 1 : 0,
      });
    });

    return { index: hunk.index, header: hunk.header, rows, blockStarts, blocks };
  });

  /** Row height in px. Must match the CSS, or the ribbons drift from the rows
   *  they connect. */
  const ROW_HEIGHT = 19;

  /** Row numbers run across the whole file, not per hunk, so a row can be
   *  found by one number however many hunks precede it. */
  $: rowOffsets = sections.reduce((acc: number[], section) => {
    acc.push((acc[acc.length - 1] ?? 0) + section.rows.length);
    return acc;
  }, []);

  /**
   * Scroll a change into view, for the stepping arrows.
   *
   * By row rather than by hunk: in whole-file view git returns the file as a
   * single hunk, so scrolling to the hunk always went to the top of the file
   * however far down the change was — which looks exactly like a scroll that
   * did not happen.
   *
   * A third of the way down, so the change is read with the code above it,
   * unless it is too tall for that — then near the top, using the whole
   * viewport rather than pushing its own end below the fold.
   */
  export function scrollToRow(rowIndex: number, rowCount = 1) {
    if (!scroller) return;
    const row = scroller.querySelector(`[data-row="${rowIndex}"]`) as HTMLElement | null;
    if (!row) return;

    const view = scroller.clientHeight;
    const blockHeight = row.offsetHeight * Math.max(1, rowCount);
    const margin = blockHeight > view - view / 3 ? Math.min(view / 8, 60) : view / 3;
    scroller.scrollTo({ top: Math.max(0, row.offsetTop - margin), behavior: 'smooth' });
  }

  /** Where each change begins, in row numbers, so the caller can step them. */
  export function changeRows(): Array<{ from: number; to: number }> {
    const runs: Array<{ from: number; to: number }> = [];
    let at = 0;
    for (const section of sections) {
      for (const row of section.rows) {
        if (row.kind === 'change') {
          const last = runs[runs.length - 1];
          if (last && last.to === at - 1) last.to = at;
          else runs.push({ from: at, to: at });
        }
        at++;
      }
    }
    return runs;
  }
</script>

<div class="sbs" bind:this={scroller}>
  {#each sections as section, sectionIndex (section.index)}
    <div class="sbs-hunk" class:current={section.index === currentHunk} data-hunk={section.index}>
      <div class="sbs-header" title={section.header}></div>

      <!-- The ribbons, drawn behind the gutters: one per block, joining where
           its run ends on the left to where it ends on the right. Absolute
           inside the hunk, so a row's own layout is untouched. -->
      <svg class="ribbons" aria-hidden="true" preserveAspectRatio="none" viewBox="0 0 100 {section.rows.length * ROW_HEIGHT}">
        {#each section.blocks as block (block.start)}
          {@const top = block.start * ROW_HEIGHT}
          {@const oldBottom = top + block.oldLines * ROW_HEIGHT}
          {@const newBottom = top + block.newLines * ROW_HEIGHT}
          <!-- Curved rather than a straight taper: the two ends are rarely the
               same height, and a curve reads as one side flowing into the
               other where a wedge reads as an arrow pointing nowhere. -->
          <path
            d="M 0 {top} C 45 {top}, 55 {top}, 100 {top}
               L 100 {newBottom}
               C 55 {newBottom}, 45 {oldBottom}, 0 {oldBottom} Z"
            class="ribbon"
          />
        {/each}
      </svg>

      {#each section.rows as row, i (i)}
        {@const rowIndex = (rowOffsets[sectionIndex - 1] ?? 0) + i}
        {#if row.kind === 'header'}
          <div class="sbs-row meta" data-row={rowIndex}>
            <!-- Already escaped; see utils/highlightLine.ts. -->
            <span class="side both"><code>{@html row.oldHtml ?? ''}</code></span>
          </div>
        {:else}
          <div class="sbs-row" class:changed={row.kind === 'change'} data-row={rowIndex}>
            <span
              class="side left"
              class:removed={row.kind === 'change' && row.oldHtml !== null}
              class:block-top={section.blockStarts.has(i) && row.oldHtml !== null}
              class:block-bottom={row.kind === 'change' && row.oldHtml !== null &&
                (section.rows[i + 1]?.kind !== 'change' || section.rows[i + 1]?.oldHtml === null)}
            >
              {#if row.oldHtml !== null}<code>{@html row.oldHtml}</code>{/if}
            </span>

            <!-- The two line-number columns sit between the panes rather than
                 in front of each, as IntelliJ shows them: the numbers are what
                 the eye crosses from one side to the other by, and at the far
                 edges they are two separate rulers instead of one bridge. -->
            <span class="gutter old">{row.oldNumber ?? ''}</span>
            <span class="marker">
              {#if section.blockStarts.has(i)}
                {#if canRevert}
                  <button
                    class="revert-arrow"
                    title={$t('diff.revertBlock')}
                    on:click={() => dispatch('revert', { hunkIndex: section.index })}
                  >»</button>
                {:else}
                  <span class="marker-mark">»</span>
                {/if}
              {/if}
            </span>
            <span class="gutter new">{row.newNumber ?? ''}</span>

            <span
              class="side right"
              class:added={row.kind === 'change' && row.newHtml !== null}
              class:block-top={section.blockStarts.has(i) && row.newHtml !== null}
              class:block-bottom={row.kind === 'change' && row.newHtml !== null &&
                (section.rows[i + 1]?.kind !== 'change' || section.rows[i + 1]?.newHtml === null)}
            >
              {#if row.newHtml !== null}<code>{@html row.newHtml}</code>{/if}
            </span>
          </div>
        {/if}
      {/each}
    </div>
  {/each}
</div>

<style>
  .sbs {
    height: 100%;
    overflow: auto;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
  }

  /* Hunks are separated by a rule rather than by their "@@" line: the line
     numbers are on screen either side of it now, so the header's own numbers
     are a third copy of something already answered. Kept as a thin band so
     the eye still sees where one region ends. */
  /* Positioned, so the ribbons can be laid over the block's rows. */
  .sbs-hunk {
    position: relative;
  }

  /* Behind everything: the rows keep their own backgrounds, and the ribbon
     shows in the gutter strip between the two panes where nothing else is
     drawn. */
  /* Over the gutter strip between the panes, which is the one place with room
     for it. Its width is the two rulers plus the marker column. */
  .ribbons {
    position: absolute;
    top: 8px;
    left: 50%;
    transform: translateX(-50%);
    width: 7.6em;
    height: calc(100% - 8px);
    pointer-events: none;
    z-index: 0;
  }
  .ribbon {
    fill: rgba(97, 175, 239, 0.14);
    stroke: rgba(97, 175, 239, 0.4);
    stroke-width: 1;
  }

  .sbs-header {
    height: 8px;
    border-top: 1px solid rgba(255, 255, 255, 0.07);
    background: rgba(255, 255, 255, 0.02);
  }
  .sbs-hunk:first-child .sbs-header {
    border-top: none;
  }

  /* Two columns of equal width, each with its own line-number gutter. The
     grid is what keeps a row's two halves level — the alignment the rows are
     built for would be lost to any layout that sized them independently. */
  .sbs-row {
    position: relative;
    z-index: 1;
    display: grid;
    /* Left pane, then the two rulers with a marker between them, then the
       right pane. The grid is what keeps a row's two halves level — the
       alignment the rows are built for would be lost to any layout that sized
       them independently. */
    grid-template-columns: 1fr 3.2em 1.2em 3.2em 1fr;
    line-height: 19px;
  }

  /* One surface across the middle, so the numbers read as a single ruler
     between the panes rather than as decoration on each. */
  .gutter,
  .marker {
    color: #4b5263;
    user-select: none;
    background: rgba(255, 255, 255, 0.03);
    font-variant-numeric: tabular-nums;
  }
  .gutter {
    padding: 0 6px;
    text-align: right;
  }
  .gutter.old {
    border-left: 1px solid rgba(255, 255, 255, 0.06);
  }
  .gutter.new {
    border-right: 1px solid rgba(255, 255, 255, 0.06);
  }
  /* No number on this side: the row exists only on the other one. */


  /* Points from the old side to the new, at the top of each changed block —
     which way to read the pair, and where to click to undo it. */
  .marker {
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .marker-mark {
    opacity: 0.5;
  }

  .revert-arrow {
    padding: 0 2px;
    border: none;
    background: transparent;
    color: var(--accent, #61afef);
    font: inherit;
    line-height: 1;
    cursor: pointer;
    opacity: 0.65;
  }
  .revert-arrow:hover {
    opacity: 1;
    transform: scale(1.25);
  }

  /* The bracket down the side of a changed block, joining the rows it spans.
     A block is often one line on one side and several on the other, and
     without something tying them together the pairing has to be worked out by
     counting rows. */
  .sbs-row.changed .side.removed::before,
  .sbs-row.changed .side.added::before {
    content: '';
    position: absolute;
    left: 0;
    width: 2px;
    top: 0;
    bottom: 0;
  }

  .side {
    position: relative;
    padding: 0 8px;
    white-space: pre;
    overflow: hidden;
    /* The tint has to reach the edge of its pane rather than stopping where
       the text does, or a changed row reads as a ragged stripe. */
    min-width: 0;
  }
  .side code {
    font-family: inherit;
    font-size: inherit;
  }
  .side.both {
    grid-column: 1 / -1;
  }

  /* The tint carries removed/added; the text keeps its syntax colours, as in
     the unified view.
     Stronger than the unified view's, because here it is doing more work: with
     the panes side by side the colour is what distinguishes the changed rows
     from the identical context filling both columns. */
  .side.removed {
    background: rgba(239, 68, 68, 0.14);
  }
  .side.added {
    background: rgba(34, 197, 94, 0.14);
  }
  /* The edge stripe, on the side of the pane facing the middle, so both point
     inward at the gutter the ribbon crosses. */
  .side.removed::after,
  .side.added::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    width: 2px;
  }
  .side.removed::after {
    right: 0;
    background: rgba(239, 68, 68, 0.55);
  }
  .side.added::after {
    left: 0;
    background: rgba(34, 197, 94, 0.55);
  }
  /* Where one side has no line the pane is left plain. Shading it drew a block
     of empty rows standing in for something that was never there — and the
     ribbon between the panes already says where those lines went. */

  /* The block's boundary, carried across the code as well as the gutter: the
     ribbon between the panes marks where a block starts and ends, and without
     the same line over the text the two halves read as separate regions that
     happen to be level. */
  /* Drawn as an inset shadow rather than a border: a border adds to the row's
     height, and the ribbons are drawn at a fixed 19px pitch — one pixel per
     block would drift them out of step down a long file. */
  .side.block-top {
    box-shadow: inset 0 1px 0 rgba(97, 175, 239, 0.3);
  }
  .side.block-bottom {
    box-shadow: inset 0 -1px 0 rgba(97, 175, 239, 0.3);
  }
  .side.block-top.block-bottom {
    box-shadow: inset 0 1px 0 rgba(97, 175, 239, 0.3),
                inset 0 -1px 0 rgba(97, 175, 239, 0.3);
  }

  /* The change the arrows are on, matching the unified view's marker. */
  .sbs-hunk.current .sbs-row.changed .side.removed::after,
  .sbs-hunk.current .sbs-row.changed .side.added::after {
    background: var(--accent, #61afef);
    width: 3px;
  }
  .sbs-hunk.current .sbs-row.changed .marker {
    color: var(--accent, #61afef);
    opacity: 1;
  }
</style>
