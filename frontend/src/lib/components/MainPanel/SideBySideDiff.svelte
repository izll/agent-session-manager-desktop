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
  import { createEventDispatcher, onMount, tick } from 'svelte';
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

  // A file too narrow to scroll never fires one, so measure on mount too.
  onMount(measureWidths);


  let leftEl: HTMLDivElement | null = null;
  let rightEl: HTMLDivElement | null = null;

  /**
   * How wide each pane's rows have to be, in px.
   *
   * `width: max-content` gets a container close but not exact: in the webview
   * it settled at 874px while its own contents came to 881, so scrolled fully
   * right seven pixels sat past the end of every row as a stripe the tint could
   * not reach. Nothing in the box model accounts for it — no padding, border or
   * scrollbar — and Chromium does not reproduce it, so it is measured rather
   * than derived.
   *
   * scrollWidth is what the pane will actually scroll through, which is the
   * width the rows must fill to reach its far edge.
   */
  let leftWidth = 0;
  let rightWidth = 0;

  function measureWidths() {
    // Only ever grows to what the pane already scrolls, and only when it is
    // short of it. Assigned unconditionally this runs away: the min-width
    // widens the pane's scrollWidth, which is then measured again as the new
    // target, a pixel or two larger every scroll.
    if (leftEl && leftEl.scrollWidth > leftWidth) leftWidth = leftEl.scrollWidth;
    if (rightEl && rightEl.scrollWidth > rightWidth) rightWidth = rightEl.scrollWidth;
  }


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

  // A new file is a new width. Without this the widest line of whatever was
  // open before keeps the panes scrolling past the end of the current one.
  $: if (paired) {
    leftWidth = 0;
    rightWidth = 0;
    // Re-measured once the new file has been laid out: clearing alone leaves
    // the panes at max-content, which is where the missing pixels come from.
    void tick().then(measureWidths);
  }

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

  /**
   * The wheel over the left pane, applied to the right one.
   *
   * That pane has overflow-y: hidden — a vertical scrollbar would take its
   * width out of the usable area and leave a stripe the rows cannot colour —
   * and hidden means the wheel passes straight through it to whatever is
   * behind. Handing the movement to the right pane keeps the wheel working
   * where the pointer is, and the sync brings the left one along, which is the
   * same path a wheel over the right pane already takes.
   */
  function onLeftWheel(event: WheelEvent) {
    if (!rightEl || event.deltaY === 0) return;
    // Only what this would have scrolled. A wheel at the end of the file should
    // still reach the panel behind, as it would in any other scroller.
    const before = rightEl.scrollTop;
    const wanted = clampScroll(rightEl, before + event.deltaY);
    if (wanted === before) return;

    event.preventDefault();
    echoFrom = null;
    rightEl.scrollTop = wanted;
    syncFrom('right');
  }

  function syncFrom(source: 'left' | 'right') {
    measureWidths();

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

    /**
     * Sideways they move as one, without any pairing.
     *
     * Vertically the sides hold different lines and the pairing decides what
     * belongs beside what. Along a line there is nothing to work out: column 80
     * is column 80 on both sides, and letting them drift apart is what makes a
     * long line impossible to compare — the halves of one change end up at
     * different offsets with no way to tell how far.
     *
     * Clamped to what the follower can take: the two panes rarely have the same
     * longest line, so the shorter one simply stops at its own end.
     */
    const wantedLeft = Math.max(
      0,
      Math.min(from.scrollLeft, to.scrollWidth - to.clientWidth),
    );

    // Only a real move echoes. Setting a position to what it already holds
    // sends no event, and arming the guard for one would eat the next genuine
    // scroll. Either axis moving is enough to produce that one event.
    const movesDown = Math.round(to.scrollTop) !== Math.round(wanted);
    const movesAcross = Math.round(to.scrollLeft) !== Math.round(wantedLeft);
    if (movesDown || movesAcross) {
      echoFrom = source === 'left' ? 'right' : 'left';
      if (movesDown) to.scrollTop = wanted;
      if (movesAcross) to.scrollLeft = wantedLeft;
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
  /**
   * Find text in either pane.
   *
   * Searches the rendered lines rather than the raw diff, because that is what
   * the reader can see: a match in a line that is not displayed would scroll
   * nowhere. The HTML carries syntax highlighting, so tags are stripped before
   * matching — otherwise a search for "span" would hit every coloured token.
   */
  let searchQuery = '';
  let searchHits: number[] = [];
  let searchAt = 0;
  /**
   * The rows to mark: every match faintly, the current one strongly.
   *
   * Held as a Set for the every-row test in the markup — a linear scan per row
   * would be O(rows × hits), which on a large diff is felt.
   */
  let hitRows = new Set<number>();
  let currentHitRow = -1;

  /** Tag-free text of a rendered line, for matching. */
  function plainText(html: string | null): string {
    if (!html) return '';
    // Entities have to be decoded too: the renderer escapes < and &, so a
    // search for "a < b" would never match the "a &lt; b" in the markup.
    const el = document.createElement('div');
    el.innerHTML = html;
    return el.textContent || '';
  }

  /**
   * Row indices containing the query, in display order.
   *
   * A row matches if either side does — the two columns are one document to
   * the person reading them, and reporting a right-hand hit as "not found"
   * because the left side lacks it would be a lie.
   */
  export function search(query: string): number {
    searchQuery = query;
    searchAt = 0;

    if (!query.trim()) {
      searchHits = [];
      hitRows = new Set();
      currentHitRow = -1;
      return 0;
    }

    const needle = query.toLowerCase();
    searchHits = [];
    paired.forEach((row, index) => {
      const text = (plainText(row.oldHtml) + '\n' + plainText(row.newHtml)).toLowerCase();
      if (text.includes(needle)) searchHits.push(index);
    });

    hitRows = new Set(searchHits);
    currentHitRow = searchHits.length ? searchHits[0] : -1;
    if (searchHits.length) scrollToRow(searchHits[0]);
    return searchHits.length;
  }

  /** Step to the next match, or the previous one. Wraps at both ends. */
  export function stepSearch(direction: 1 | -1): number {
    if (!searchHits.length) return 0;
    searchAt = (searchAt + direction + searchHits.length) % searchHits.length;
    currentHitRow = searchHits[searchAt];
    scrollToRow(currentHitRow);
    return searchAt + 1;
  }

  /** Which match is showing, 1-based; 0 when there are none. */
  export function searchPosition(): number {
    return searchHits.length ? searchAt + 1 : 0;
  }

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
   * The new-file line number showing at the top of the right pane.
   *
   * For opening the file somewhere useful: a diff scrolled to a change should
   * open the editor at that change, not at line 1. The right pane is the one
   * asked, because it holds the new file — which is the file the editor will
   * show.
   *
   * Rows without a new-file number (a removed line has none) are skipped
   * forward from, so a viewport whose first row is a deletion still yields the
   * next real line rather than nothing.
   */
  export function topVisibleNewLine(): number | null {
    if (!rightEl) return null;
    const topRow = Math.floor(rightEl.scrollTop / ROW_HEIGHT);

    for (let at = topRow; at < right.length; at++) {
      const number = right[at]?.number;
      if (typeof number === 'number') return number;
    }
    return null;
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
  <div
    class="pane left"
    bind:this={leftEl}
    on:scroll={() => syncFrom('left')}
    on:wheel={onLeftWheel}
  >
    <!-- Sized to the longest line, so every row is that wide. Left to
         themselves the rows are each as wide as their own text, and a short
         changed line's tint stopped where its text did — scrolled right, the
         colour simply ran out. The measured width covers the last few pixels
         `max-content` leaves behind; see measureWidths. -->
    <div class="lines" style={leftWidth ? `min-width: ${leftWidth}px` : ''}>
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
        class:hit={hitRows.has(line.row)}
        class:hit-current={line.row === currentHitRow}
      >
        <span class="gutter">{line.number ?? ''}</span>
        <!-- Already escaped; see utils/highlightLine.ts. -->
        <span class="code"><code>{@html line.html ?? ''}</code></span>
      </div>
    {/each}
    </div>
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
    <div class="lines" style={rightWidth ? `min-width: ${rightWidth}px` : ''}>
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
        class:hit={hitRows.has(line.row)}
        class:hit-current={line.row === currentHitRow}
      >
        <span class="gutter">{line.number ?? ''}</span>
        <span class="code"><code>{@html line.html ?? ''}</code></span>
      </div>
    {/each}
    </div>
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
    /* Opaque, so this reads the same wherever it is placed. Left transparent it
       picked up whatever sat behind it — in the commit-history dialog that is a
       vertical gradient, which tinted the code a faint purple that shifted as
       the page scrolled. The unified pane sets its own background and so never
       showed the problem. */
    background: #0a0a0f;
  }

  /* Search matches.
     Every hit gets a quiet tint so the shape of the results is visible while
     scrolling; the one being stepped to gets a stronger one and a marker in
     the gutter, so "which of these am I on" needs no counting.

     Set with an inset shadow rather than a background, because the row's
     background already carries whether the line was added or removed — and
     that is information the search must not paint over. */
  /* A tint laid over the row's own colour, as a gradient rather than a
     box-shadow.
     box-shadow does not stack across rules — the most specific one wins
     outright — and the block outline already uses it here, composed through
     --edge-* variables. A shadow for the search would have erased that
     outline. A background image layers instead: the row keeps its
     added/removed background underneath and gains the highlight on top. */
  .sbs-line.hit {
    background-image: linear-gradient(rgba(250, 204, 21, 0.07), rgba(250, 204, 21, 0.07));
  }

  .sbs-line.hit-current {
    background-image: linear-gradient(rgba(250, 204, 21, 0.2), rgba(250, 204, 21, 0.2));
  }

  .sbs-line.hit-current .gutter {
    color: #facc15;
    font-weight: 600;
  }

  .pane {
    overflow: auto;
    min-width: 0;
  }

  /* Sized to the longest line in the pane, so every row is that wide.
     Rows sized individually (min-width: max-content on each) are each as wide
     as their OWN text, so a short changed line's tint ended at the pane's edge
     while longer lines carried on past it — scrolled right, the colour and the
     block outline simply ran out partway across. min-width keeps a short file
     filling the pane rather than shrinking to its text. */
  .lines {
    width: max-content;
    min-width: 100%;
  }

  /**
   * Every row as wide as the container, not merely as wide as its own text.
   *
   * Laid out independently a short row ends where its text does, leaving the
   * rest of the line uncoloured once the pane is scrolled past it.
   */
  .lines > .sbs-line {
    min-width: 100%;
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
  /**
   * The left pane has no vertical scrollbar at all.
   *
   * Not merely hidden. A vertical bar takes its width out of the pane's usable
   * area, and the rows end where that area ends — so scrolled fully right, a
   * bar-shaped stripe of background sat beyond them, uncoloured. Measured: 460px
   * visible against 444px usable, and the rows stopped at 444. Painting the bar
   * transparent does not help; the space is still taken. Reserving it with a
   * border is worse — a border is outside the scrollable box, so the tint stops
   * before it too.
   *
   * The pane is still scrolled vertically, by scrollTop from the sync: `hidden`
   * only stops the USER scrolling it directly. The wheel is forwarded
   * explicitly (see onLeftWheel), since hidden would otherwise let it pass
   * through to whatever is behind.
   *
   * Its horizontal bar stays: that one is along the bottom, takes nothing from
   * the width, and is how a long line is scrolled.
   */
  .pane.left {
    overflow-y: hidden;
  }

  /**
   * Fixed height: the sides are kept in step by arithmetic on it, and a row
   * that grew would put everything below it out of register with its pair.
   *
   * `min-width: max-content` so a row is as wide as its own text rather than as
   * wide as the pane. Sized to the pane, a row scrolled sideways ran out at the
   * pane's original right edge — the added/removed tint and the block outline
   * stopped dead partway along, with the code carrying on past them.
   */
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
    /**
     * Opaque, not translucent: the code scrolls underneath, and a see-through
     * ruler would show OTHER lines' red and green tints sliding past behind the
     * numbers.
     *
     * Its own line's tint it must still carry, though — a changed line whose
     * number sits on plain grey reads as if the ruler were a separate thing
     * beside the diff rather than part of the row. So each state has its
     * translucent colour pre-mixed over the diff's background (#0a0a0f) here:
     * the same result, without letting anything through.
     */
    background: #111116;
    border-right: 1px solid rgba(255, 255, 255, 0.06);
    font-variant-numeric: tabular-nums;
    /* Held against the pane's edge while the code scrolls under it: a ruler
       that slides away leaves the lines on screen unnumbered, which is when the
       numbers are most wanted — reading a long line and asking which one it is. */
    position: sticky;
    left: 0;
    z-index: 1;
  }

  /* No min-width: 0 — that lets the code shrink below its own text, which
     undoes the max-content width the row needs to reach past the pane. */
  .code {
    padding: 0 8px;
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
  .sbs-line.removed .gutter {
    background: #2a1216;
  }
  .sbs-line.added .gutter {
    background: #0d241a;
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
  .sbs-line.current:not(.removed):not(.added) .gutter {
    background: #131a25;
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
