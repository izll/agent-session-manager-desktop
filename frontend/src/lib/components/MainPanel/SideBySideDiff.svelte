<script lang="ts">
  /**
   * A file's diff as two aligned columns.
   *
   * A unified diff is one column with markers: what was removed, then what
   * replaced it. Side by side the same change is two columns with the pairs
   * level, which is what makes "this became that" legible at a glance rather
   * than reconstructed by counting lines.
   *
   * Two scrollers, not one grid. A grid has to pad the shorter side to keep the
   * pairs level, so a long insertion shows a screenful of blank rows beside it.
   * Scrolled independently and kept in step, each side holds only its own
   * lines: the one with nothing to show waits while the other runs through the
   * insertion, and moves on at the far end. That waiting is the behaviour, and
   * it is what IntelliJ does.
   */
  import { createEventDispatcher, tick } from 'svelte';
  import { t } from '../../i18n';
  import {
    buildSideBySide,
    splitSides,
    matchingLine,
    type SideBySideRow,
    type SideLine,
    type UnifiedLine,
  } from '../../utils/sideBySide';

  /** One entry per hunk: its header and its lines, already highlighted. */
  export let hunks: Array<{ header: string; lines: UnifiedLine[]; index: number }> = [];
  /**
   * Which change the stepping is on, counted in the order changeRows() returns
   * them. -1 marks none.
   *
   * An ordinal rather than a row: the caller counts changes, and the columns
   * pair lines up, so their rows do not match the positions the unified view
   * holds. Both sides agree on the nth change, which is all this needs to say.
   */
  export let currentChange = -1;
  /** Whether the arrows offer to revert. Off where there is nothing to write
   *  back to — a historical commit is read, not edited. */
  export let canRevert = false;

  const dispatch = createEventDispatcher();

  /** Row height in px. Must match the CSS: the sides are kept in step, and the
   *  ribbons placed, by arithmetic on it. */
  const ROW_HEIGHT = 19;

  let leftEl: HTMLDivElement | null = null;
  let rightEl: HTMLDivElement | null = null;

  /**
   * How far each pane is scrolled, tracked separately.
   *
   * A ribbon joins a block's left end to its right end, and with the panes
   * scrolled independently those two ends move by different amounts — through
   * an insertion the left stands still while the right runs on. One shared
   * offset cannot say that: it leaves every ribbon anchored to whichever pane
   * it was measured from, and they pile up at the top of the strip.
   */
  let leftScroll = 0;
  let rightScroll = 0;
  /** The strip's own height, so only the ribbons on screen are drawn. */
  let stripHeight = 0;

  /**
   * The whole file as paired rows, then split into two columns.
   *
   * The pairing still decides which line belongs beside which — the split only
   * stops the shorter side being padded out to match.
   */
  $: paired = hunks.flatMap((hunk) =>
    (buildSideBySide(hunk.header, hunk.lines) as SideBySideRow[]).map((row) => ({
      ...row,
      hunkIndex: hunk.index,
    })),
  );

  $: sides = splitSides(paired);

  /**
   * Each changed block as a shape to draw: where its run starts and ends on
   * each side, in that side's own line numbers.
   *
   * With the sides split, a block's two halves are at different heights in
   * their own columns — which is exactly what the ribbon between them has to
   * show, and what a shared row number can no longer say.
   */
  interface Block {
    hunkIndex: number;
    /** First and last paired row of the block, for stepping and the marker. */
    fromRow: number;
    toRow: number;
    /** The block's extent in each column's own lines. -1 where it has none. */
    leftFrom: number;
    leftTo: number;
    rightFrom: number;
    rightTo: number;
  }

  $: blocks = ((): Block[] => {
    const found: Block[] = [];
    paired.forEach((row, at) => {
      if (row.kind !== 'change') return;
      const previous = found[found.length - 1];
      if (previous && previous.toRow === at - 1) previous.toRow = at;
      else {
        found.push({
          hunkIndex: row.hunkIndex,
          fromRow: at,
          toRow: at,
          leftFrom: -1,
          leftTo: -1,
          rightFrom: -1,
          rightTo: -1,
        });
      }
    });

    // Where each block sits in each column. A pure insertion has no lines on
    // the left at all — there the ribbon collapses to a wedge into a single
    // point, drawn where the left side waits, which is right after the last
    // line it does have above the block.
    const place = (lines: SideLine[], block: Block, from: 'leftFrom' | 'rightFrom', to: 'leftTo' | 'rightTo') => {
      let waiting = 0;
      for (let at = 0; at < lines.length; at++) {
        if (lines[at].row < block.fromRow) {
          waiting = at + 1;
          continue;
        }
        if (lines[at].row > block.toRow) break;
        if (block[from] === -1) block[from] = at;
        block[to] = at;
      }
      // Recorded even when absent, so the ribbon has somewhere to land.
      if (block[from] === -1) block[to] = waiting;
    };
    found.forEach((block) => {
      place(sides.left, block, 'leftFrom', 'leftTo');
      place(sides.right, block, 'rightFrom', 'rightTo');
    });
    return found;
  })();

  /**
   * A column's lines, each knowing whether it opens or closes a changed block.
   *
   * The rule is the neighbour within this column, not within the paired rows: a
   * block that is three lines on one side and one on the other has to be boxed
   * as three lines here and one there. Asking the paired rows would draw the
   * line at the pairing's boundary, which on the shorter side falls in the
   * middle of nothing.
   *
   * Adjacency in the column is not enough on its own, though — two separate
   * blocks with only untouched lines between them are not adjacent HERE if
   * those lines live on the other side. Hence the row numbers: consecutive
   * paired rows mean one block, a gap means two.
   */
  function mark(lines: SideLine[], side: 'left' | 'right') {
    return lines.map((line, at) => {
      const changed = line.kind === 'change';
      const previous = lines[at - 1];
      const next = lines[at + 1];

      /**
       * A block that has no lines on THIS side still has to show where it
       * belongs, or the band arrives from the strip and stops dead at the pane.
       * There is no row to outline — an insertion has none on the left — so the
       * mark goes on the boundary between the lines it happens between: a rule
       * along the top of the line that follows it.
       *
       * That boundary is the line the side waits on while the other runs
       * through the insertion, which is exactly where the band's two edges meet.
       */
      const absent = (block: Block) =>
        (side === 'left' ? block.leftFrom : block.rightFrom) === -1;
      const waitsAt = (block: Block) => (side === 'left' ? block.leftTo : block.rightTo);

      const gapAbove = blocks.find((block) => absent(block) && waitsAt(block) === at);
      // A block at the very END of the file waits past the last line, where
      // there is no row to put a rule above. It goes under the last line
      // instead — otherwise an insertion at the bottom shows nothing at all on
      // the other side.
      const gapBelow = at === lines.length - 1 &&
        blocks.find((block) => absent(block) && waitsAt(block) >= lines.length);

      // Which colour the rule takes: the change is only on the other side, so
      // it is an addition when this is the left pane and vice versa.
      const gapKind = side === 'left' ? 'added' : 'removed';

      return {
        ...line,
        at,
        hunkIndex: paired[line.row]?.hunkIndex ?? -1,
        first: changed && !(previous?.kind === 'change' && previous.row === line.row - 1),
        last: changed && !(next?.kind === 'change' && next.row === line.row + 1),
        gap: gapAbove ? gapKind : null,
        gapUnder: gapBelow ? gapKind : null,
      };
    });
  }

  // blocks is named here so Svelte orders these after it: it works the order
  // out by READING the statement, and cannot see through the call to mark —
  // which does read blocks. Left implicit, these can run first, against an
  // undefined value.
  $: left = blocks && mark(sides.left, 'left');
  $: right = blocks && mark(sides.right, 'right');


  /**
   * Keeping the two sides in step.
   *
   * Whichever is scrolled leads; the other is placed at the line the pairing
   * says belongs beside it, which through an insertion is the same line for as
   * long as the insertion lasts.
   *
   * Two elements each answering the other's scroll never settles, so the
   * follower's own event has to be dropped. It is identified by which side is
   * expected to report next rather than by a timer: a wheel sends events faster
   * than a frame, and a guard held open for a frame swallows the next real
   * scroll along with the echo — which is why the wheel stopped working.
   */
  let echoFrom: 'left' | 'right' | null = null;

  function syncFrom(source: 'left' | 'right') {
    // Read on every scroll, including the follower's own, so both ends of a
    // ribbon are drawn against where their pane actually is.
    leftScroll = leftEl?.scrollTop ?? 0;
    rightScroll = rightEl?.scrollTop ?? 0;

    // The echo of a move this made. Cleared, so the very next event from that
    // side leads normally.
    if (echoFrom === source) {
      echoFrom = null;
      return;
    }

    const from = source === 'left' ? leftEl : rightEl;
    const to = source === 'left' ? rightEl : leftEl;
    if (!from || !to) return;

    const fromLines = source === 'left' ? left : right;
    const toLines = source === 'left' ? right : left;

    // Which line is at the top of the leading side, and how far into it — the
    // fraction is carried across so the two do not jerk a row apart.
    const topLine = Math.floor(from.scrollTop / ROW_HEIGHT);
    const offset = from.scrollTop - topLine * ROW_HEIGHT;
    const target = matchingLine(fromLines, toLines, topLine);

    const wanted = clampScroll(to, target * ROW_HEIGHT + offset);
    // Only a real move echoes. Setting scrollTop to what it already holds sends
    // no event, and arming the guard for one would eat the next genuine scroll.
    if (Math.round(to.scrollTop) !== Math.round(wanted)) {
      echoFrom = source === 'left' ? 'right' : 'left';
      to.scrollTop = wanted;
    }

    // Re-read after moving the follower: its ribbon ends belong at the position
    // just written, not the one it held when this scroll arrived.
    leftScroll = leftEl?.scrollTop ?? 0;
    rightScroll = rightEl?.scrollTop ?? 0;

    // Announced so the position can be recorded while it is still readable: on
    // the way out the binding is already gone.
    dispatch('viewscroll');
  }

  /** A scroll position the element can actually take. Past the end it simply
   *  stops, and asking for more leaves the two sides disagreeing about where
   *  they are. */
  function clampScroll(element: HTMLElement, top: number): number {
    return Math.max(0, Math.min(top, element.scrollHeight - element.clientHeight));
  }

  /**
   * Scroll a change into view, for the stepping arrows.
   *
   * Led from whichever side actually holds the change: an insertion exists only
   * on the right, and asking the left to lead would scroll to the line it is
   * waiting on and leave the insertion off-screen.
   *
   * A third of the way down, so the change is read with the code above it,
   * unless it is too tall for that — then near the top, using the whole
   * viewport rather than pushing its own end below the fold.
   */
  export function scrollToRow(rowIndex: number, rowCount = 1) {
    const block = blocks.find((b) => b.fromRow <= rowIndex && rowIndex <= b.toRow)
      ?? blocks.find((b) => b.fromRow === rowIndex);

    // Prefer the side with more of the block on it: that is the side whose
    // scroll position decides what is readable.
    const rightLines = block ? block.rightTo - block.rightFrom + 1 : 0;
    const leftLines = block ? block.leftTo - block.leftFrom + 1 : 0;
    const useRight = !block || block.leftFrom === -1 || rightLines >= leftLines;

    const lead = useRight ? rightEl : leftEl;
    const lines = useRight ? right : left;
    if (!lead) return;

    let at = block ? (useRight ? block.rightFrom : block.leftFrom) : -1;
    if (at < 0) at = lines.findIndex((line) => line.row >= rowIndex);
    if (at < 0) return;

    const view = lead.clientHeight;
    const blockHeight = Math.max(1, useRight ? rightLines : leftLines, rowCount) * ROW_HEIGHT;
    const margin = blockHeight > view - view / 3 ? Math.min(view / 8, 60) : view / 3;
    const wanted = at * ROW_HEIGHT - margin;
    const top = clampScroll(lead, wanted);

    /**
     * Near the end of the file the pane runs out of scroll before the change
     * reaches its third, and it is left sitting at the bottom of the view. The
     * follower is not stuck in the same place — the two sides have different
     * numbers of lines below — so where the leader cannot go far enough, the
     * change is brought up by scrolling the OTHER side past it and letting this
     * one follow.
     */
    const other = useRight ? leftEl : rightEl;
    const short = wanted - top > ROW_HEIGHT;
    if (short && other) {
      const otherLines = useRight ? left : right;
      const otherAt = matchingLine(lines, otherLines, at);
      const otherTop = clampScroll(other, otherAt * ROW_HEIGHT - margin);
      // Only worth it if the other side really can get further.
      if (otherTop - other.scrollTop > top - lead.scrollTop) {
        echoFrom = null;
        other.scrollTo({ top: otherTop, behavior: 'smooth' });
        return;
      }
    }

    // Cleared so this move is not mistaken for an echo: it leads, and the other
    // side must follow it.
    echoFrom = null;
    lead.scrollTo({ top, behavior: 'smooth' });
    // The other side follows on the scroll events this produces.
  }

  /** Where each change begins and ends, in paired-row numbers, so the caller
   *  can step them without knowing how the columns are laid out. */
  export function changeRows(): Array<{ from: number; to: number }> {
    return blocks.map((block) => ({ from: block.fromRow, to: block.toRow }));
  }

  /**
   * Where a paired row's block sits in the ORIGINAL hunk lines.
   *
   * Pairing reorders: a removal and the addition replacing it share one row
   * here, but in the hunk they are two lines, removals first. So a row number
   * cannot be handed to anything working from the hunk — a patch, for one —
   * without being translated back.
   */
  export function hunkRange(fromRow: number, toRow: number): { from: number; to: number } | null {
    let at = 0;
    let from = -1;
    let to = -1;

    for (const [rowIndex, row] of paired.entries()) {
      // How many hunk lines this row accounts for: a paired change is one line
      // on each side it has one, context and headers are one line total.
      const count = row.kind === 'change'
        ? (row.oldHtml !== null ? 1 : 0) + (row.newHtml !== null ? 1 : 0)
        : 1;
      if (rowIndex >= fromRow && rowIndex <= toRow && count > 0) {
        if (from === -1) from = at;
        to = at + count - 1;
      }
      at += count;
    }

    return from === -1 ? null : { from, to };
  }

  /**
   * The view's position, for restoring it after a tab switch rebuilt it.
   *
   * The right pane's offset alone: the left follows from it through the same
   * pairing that keeps them in step while scrolling, so storing both would risk
   * putting them back in a state they could never have scrolled into.
   */
  export function scrollOffset(): number {
    return rightEl?.scrollTop ?? 0;
  }

  /**
   * Scroll so a given line of the OLD file is in view.
   *
   * By line number rather than by offset, for after the file has been rewritten
   * — a revert removes lines, so every pixel below them has moved and an offset
   * taken beforehand points somewhere else entirely.
   */
  export async function scrollToOldLine(number: number) {
    await tick();
    if (!leftEl) return;
    // The first line at or after the one asked for: the exact line may itself
    // have been part of what was reverted.
    let at = left.findIndex((line) => line.number !== null && line.number >= number);
    if (at < 0) at = left.length - 1;
    if (at < 0) return;

    echoFrom = null;
    const view = leftEl.clientHeight;
    leftEl.scrollTop = clampScroll(leftEl, at * ROW_HEIGHT - view / 3);
    syncFrom('left');
  }

  /** Put the view back where it was, and let the left side follow. */
  export async function restoreOffset(top: number) {
    if (top <= 0) return;
    await tick();
    if (!rightEl) return;
    // Restoring is a lead, not an echo — the left pane has to answer it.
    echoFrom = null;
    rightEl.scrollTop = clampScroll(rightEl, top);
    syncFrom('right');
  }

  /** The block stepped to, so the mark covers just that change rather than
   *  every change sharing its hunk. */
  $: markedBlock = currentChange >= 0 ? blocks[currentChange] : undefined;

  /**
   * Each block as the shape to draw for it, in the strip's own coordinates.
   *
   * The left edge is placed against the left pane's scroll and the right edge
   * against the right pane's, because that is where those lines actually are —
   * through an insertion the left end holds still at the waiting line while the
   * right end travels, and the ribbon stretches between them. That stretch is
   * the picture of "these lines are new".
   *
   * Only the blocks near the viewport are kept. A large file has thousands, and
   * an off-screen path still costs to lay out.
   */
  $: shapes = blocks
    .map((block) => {
      const leftTop = (block.leftFrom === -1 ? block.leftTo : block.leftFrom) * ROW_HEIGHT - leftScroll;
      const rightTop = (block.rightFrom === -1 ? block.rightTo : block.rightFrom) * ROW_HEIGHT - rightScroll;
      return {
        key: block.fromRow,
        current: block === markedBlock,
        // Which colour the band takes, matching the lines it joins: green where
        // the change only adds, red where it only removes, blue where it
        // replaces. A band in a different colour from both its ends reads as a
        // third thing rather than as the join between them.
        kind: block.leftFrom === -1 ? 'added' : block.rightFrom === -1 ? 'removed' : 'changed',
        y1: leftTop,
        y2: rightTop,
        // A block absent from a side comes to a point: its two edges meet at
        // the line that side is waiting on.
        y1b: block.leftFrom === -1 ? leftTop : (block.leftTo + 1) * ROW_HEIGHT - leftScroll,
        y2b: block.rightFrom === -1 ? rightTop : (block.rightTo + 1) * ROW_HEIGHT - rightScroll,
      };
    })
    .filter((shape) => Math.max(shape.y1b, shape.y2b) > -40 && Math.min(shape.y1, shape.y2) < stripHeight + 40);
</script>

<div class="sbs">
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="pane left" bind:this={leftEl} on:scroll={() => syncFrom('left')}>
    {#each left as line (line.at)}
      <div
        class="sbs-line"
        class:meta={line.kind === 'header'}
        class:removed={line.kind === 'change'}
        class:current={markedBlock !== undefined &&
          line.row >= markedBlock.fromRow && line.row <= markedBlock.toRow}
        class:block-top={line.first}
        class:block-bottom={line.last}
        class:gap-added={line.gap === 'added'}
        class:gap-removed={line.gap === 'removed'}
        class:gap-under-added={line.gapUnder === 'added'}
        class:gap-under-removed={line.gapUnder === 'removed'}
      >
        <span class="gutter">{line.number ?? ''}</span>
        <!-- Already escaped; see utils/highlightLine.ts. -->
        <span class="code"><code>{@html line.html ?? ''}</code></span>
      </div>
    {/each}
  </div>

  <!-- The strip between the panes: the ribbons joining each block's two halves,
       and the arrow that reverts it. It has no scrollbar of its own — it is
       moved by whichever pane is being scrolled. -->
  <div class="middle" bind:clientHeight={stripHeight}>
    <svg class="ribbons" aria-hidden="true" viewBox="0 0 100 {stripHeight}" preserveAspectRatio="none">
      {#each shapes as shape (shape.key)}
        <!--
          A filled band, with its top and bottom drawn as separate strokes.

          One stroked path outlines the whole shape, including its two vertical
          ends — which lands a line down the inner edge of each pane, where the
          block's own outline already is. Filling and stroking separately keeps
          the band continuous into the blocks it joins: the outline on the left
          runs into the top curve, across, and out into the outline on the
          right, so the two halves read as one region rather than as two boxes
          with a decoration between them.

          Curved rather than a straight taper: the ends are rarely level, and a
          curve reads as one side flowing into the other where a wedge reads as
          an arrow pointing nowhere.
        -->
        {@const top = `M 0 ${shape.y1} C 40 ${shape.y1}, 60 ${shape.y2}, 100 ${shape.y2}`}
        {@const bottom = `M 0 ${shape.y1b} C 40 ${shape.y1b}, 60 ${shape.y2b}, 100 ${shape.y2b}`}
        <path
          class="ribbon-fill {shape.kind}"
          class:current={shape.current}
          d="{top} L 100 {shape.y2b} C 60 {shape.y2b}, 40 {shape.y1b}, 0 {shape.y1b} Z"
        />
        <path class="ribbon-edge {shape.kind}" class:current={shape.current} d={top} />
        <path class="ribbon-edge {shape.kind}" class:current={shape.current} d={bottom} />
      {/each}
    </svg>

    <!-- The revert arrow sits at the top of the ribbon, which is the higher of
         its two ends. Tied to the right end alone it slid down to the waiting
         line whenever a block existed only on the left, putting the arrow well
         below the lines it acts on. -->
    {#each shapes as shape (shape.key)}
      <div class="arrow-slot" style="top: {Math.min(shape.y1, shape.y2)}px">
        {#if canRevert}
          <button
            class="revert-arrow"
            title={$t('diff.revertBlock')}
            on:click={() => {
              const block = blocks.find((b) => b.fromRow === shape.key);
              if (!block) return;
              // The rows the block spans as well as its hunk. In whole-file
              // view the hunk is the entire file, so the hunk alone cannot say
              // which change was clicked.
              dispatch('revert', {
                hunkIndex: block.hunkIndex,
                fromRow: block.fromRow,
                toRow: block.toRow,
              });
            }}
          >»</button>
        {:else}
          <span class="marker-mark">»</span>
        {/if}
      </div>
    {/each}
  </div>

  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="pane" bind:this={rightEl} on:scroll={() => syncFrom('right')}>
    {#each right as line (line.at)}
      <div
        class="sbs-line"
        class:meta={line.kind === 'header'}
        class:added={line.kind === 'change'}
        class:current={markedBlock !== undefined &&
          line.row >= markedBlock.fromRow && line.row <= markedBlock.toRow}
        class:block-top={line.first}
        class:block-bottom={line.last}
        class:gap-added={line.gap === 'added'}
        class:gap-removed={line.gap === 'removed'}
        class:gap-under-added={line.gapUnder === 'added'}
        class:gap-under-removed={line.gapUnder === 'removed'}
      >
        <span class="gutter">{line.number ?? ''}</span>
        <span class="code"><code>{@html line.html ?? ''}</code></span>
      </div>
    {/each}
  </div>
</div>

<style>
  .sbs {
    display: grid;
    /* The two panes, with the ribbon strip between them: the strip is where the
       eye crosses from one side to the other, and it is kept narrow because it
       carries only the ribbons and the revert arrow. */
    grid-template-columns: 1fr 3.4em 1fr;
    /* Both: height:100% for a block parent, flex:1 for a flex one. The panes
       are 100% of this, and if it has no definite height of its own they size
       to their content and never scroll — which puts both columns on the outer
       scrollbar, moving as one. */
    height: 100%;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
  }

  .pane {
    overflow: auto;
    min-width: 0;
    /* The two panes never scroll horizontally together, so each keeps its own
       long lines to itself rather than dragging the other sideways. */
  }

  /**
   * The left pane's scrollbar is hidden.
   *
   * It sits on that pane's right edge — between the code and the strip — and
   * cut every band away from the block it joins. The two panes move together,
   * so one bar says everything two would: the right one is kept, on the outer
   * edge where it interrupts nothing.
   *
   * Hidden, not disabled. The pane still scrolls by wheel, keyboard and the
   * arrows, and it is still what the right pane is synchronised against.
   */
  .pane.left {
    scrollbar-width: none;
  }
  .pane.left::-webkit-scrollbar {
    width: 0;
    height: 0;
  }

  /* Fixed height: the sides are kept in step by arithmetic on it, and a row
     that grew would put everything below it out of register with its pair. */
  .sbs-line {
    display: flex;
    height: 19px;
    line-height: 19px;
    white-space: pre;
  }

  /* At the outer edge of its own pane, ahead of the code, where an editor puts
     it — the number belongs to the line it labels, and a ruler trailing its own
     text reads as part of the code. */
  .gutter {
    flex: 0 0 3.4em;
    padding: 0 6px;
    text-align: right;
    color: #4b5263;
    user-select: none;
    background: rgba(255, 255, 255, 0.03);
    border-right: 1px solid rgba(255, 255, 255, 0.06);
    font-variant-numeric: tabular-nums;
  }

  .code {
    padding: 0 8px;
    min-width: 0;
  }
  .code code {
    font-family: inherit;
    font-size: inherit;
  }

  /* The tint carries removed/added; the text keeps its syntax colours, as in
     the unified view. Stronger than there, because with the panes side by side
     the colour is what distinguishes a changed row from the identical context
     filling both columns. */
  .sbs-line.removed {
    background: rgba(239, 68, 68, 0.14);
  }
  .sbs-line.added {
    background: rgba(34, 197, 94, 0.14);
  }
  .sbs-line.meta {
    color: #7f848e;
    background: rgba(255, 255, 255, 0.03);
  }

  /**
   * The block outline and the current-change stripe, composed rather than
   * enumerated.
   *
   * Both are inset shadows on the same row — a border would add to its height,
   * and the ribbons are placed at a fixed 19px pitch, so one pixel per block
   * would drift them out of step down a long file. But box-shadow does not
   * stack across rules: the most specific one wins outright. Writing every
   * combination of top/bottom/current/side out by hand is eight rules that have
   * to agree with each other.
   *
   * So each part sets a variable, and one rule draws whatever they hold. A part
   * that does not apply contributes a shadow of zero size, which paints
   * nothing.
   */
  .sbs-line {
    --edge-top: 0 0 0 transparent;
    --edge-bottom: 0 0 0 transparent;
    --edge-side: 0 0 0 transparent;
    box-shadow: inset var(--edge-top), inset var(--edge-bottom), inset var(--edge-side);
  }

  /* The outline of a changed block, so a run of changed lines reads as one
     region rather than as a column of separately tinted lines.

     In the colour of the change, the same as the band in the strip, so the
     outline runs into the band's edge and out the other side: the two halves
     read as one region rather than as two boxes with a decoration between
     them. */
  .sbs-line.removed.block-top {
    --edge-top: 0 1px 0 rgba(239, 68, 68, 0.5);
  }
  .sbs-line.removed.block-bottom {
    --edge-bottom: 0 -1px 0 rgba(239, 68, 68, 0.5);
  }
  .sbs-line.added.block-top {
    --edge-top: 0 1px 0 rgba(34, 197, 94, 0.5);
  }
  .sbs-line.added.block-bottom {
    --edge-bottom: 0 -1px 0 rgba(34, 197, 94, 0.5);
  }

  /* Where a block has no lines on this side at all — an insertion has none on
     the left, a deletion none on the right — there is no row to outline, so the
     mark goes on the boundary the change happens at: a rule along the top of
     the line the side waits on. Without it the band arrives from the strip and
     stops dead at the edge of the pane. */
  .sbs-line.gap-added {
    --edge-top: 0 1px 0 rgba(34, 197, 94, 0.5);
  }
  .sbs-line.gap-removed {
    --edge-top: 0 1px 0 rgba(239, 68, 68, 0.5);
  }
  /* At the end of the file the side waits past its last line, where there is no
     row to put a rule above — it goes under the last one instead. */
  .sbs-line.gap-under-added {
    --edge-bottom: 0 -1px 0 rgba(34, 197, 94, 0.5);
  }
  .sbs-line.gap-under-removed {
    --edge-bottom: 0 -1px 0 rgba(239, 68, 68, 0.5);
  }

  /* The change the arrows are on, marked down the edge facing the middle so
     both sides point at the ribbon between them. */
  .sbs-line.current.removed {
    --edge-side: -3px 0 0 var(--accent, #61afef);
  }
  .sbs-line.current.added {
    --edge-side: 3px 0 0 var(--accent, #61afef);
  }
  /* A block that exists on one side only still marks the other, so the change
     stepped to is visible on both — otherwise a pure insertion lights up the
     right and leaves the left with nothing to say where it is waiting. */
  .sbs-line.current:not(.removed):not(.added) {
    background: rgba(97, 175, 239, 0.1);
  }

  /* Moved by whichever pane is scrolled rather than scrolling itself: it has
     no content of its own to scroll, only marks belonging to the panes. */
  /* No background of its own: a tinted strip between the panes reads as a
     divider, and the band crossing it then looks like something laid over a
     gap rather than the two blocks being one region. */
  .middle {
    position: relative;
    overflow: hidden;
  }
  /* Exactly the strip: the shapes are computed in viewport coordinates, so
     there is nothing off-screen to hold. The viewBox is set to the same pixel
     height, which keeps the ribbon coordinates the px the rows are laid out
     in — a mismatch would squash them vertically. */
  .ribbons {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }
  /* The band takes the colour of the change it joins — green where the lines
     are only added, red where only removed, blue where one replaces the other.
     Matching the rows either side is what makes the band read as the join
     between them rather than as a third thing in its own colour. */
  .ribbon-fill {
    stroke: none;
  }
  .ribbon-fill.added {
    fill: rgba(34, 197, 94, 0.14);
  }
  .ribbon-fill.removed {
    fill: rgba(239, 68, 68, 0.14);
  }
  .ribbon-fill.changed {
    fill: rgba(97, 175, 239, 0.12);
  }

  /* Only the top and bottom are stroked. Outlining the filled shape would draw
     a line down the inner edge of each pane too, doubling the block's own
     outline; drawn as two open curves instead, the band's edges continue the
     outline across the strip. */
  .ribbon-edge {
    fill: none;
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
  .ribbon-edge.added {
    stroke: rgba(34, 197, 94, 0.5);
  }
  .ribbon-edge.removed {
    stroke: rgba(239, 68, 68, 0.5);
  }
  .ribbon-edge.changed {
    stroke: rgba(97, 175, 239, 0.45);
  }

  /* The change being stepped through, in the accent, as the rows are. */
  .ribbon-fill.current {
    fill: rgba(97, 175, 239, 0.26);
  }
  .ribbon-edge.current {
    stroke: var(--accent, #61afef);
  }

  /* Points from the old side to the new, at the top of each block — which way
     to read the pair, and where to click to undo it. */
  .arrow-slot {
    position: absolute;
    left: 0;
    right: 0;
    height: 19px;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  /* Larger and heavier than the code around it: the strip is narrow and the
     arrow has to be legible in it at a glance, without being hunted for. */
  .marker-mark {
    color: #e6e6e6;
    font-size: 17px;
    font-weight: 700;
    line-height: 1;
    opacity: 0.55;
  }
  /* White rather than the accent: it sits ON the coloured band, and an accent
     arrow against a green or red field competes with it instead of reading as
     a control. */
  .revert-arrow {
    padding: 0 2px;
    border: none;
    background: transparent;
    color: #e6e6e6;
    font-family: inherit;
    font-size: 17px;
    font-weight: 700;
    line-height: 1;
    cursor: pointer;
    opacity: 0.8;
  }
  .revert-arrow:hover {
    opacity: 1;
    transform: scale(1.25);
  }
</style>
