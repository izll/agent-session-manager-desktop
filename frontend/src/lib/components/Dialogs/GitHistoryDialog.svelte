<script lang="ts">
  /**
   * Browsing a repository's history.
   *
   * Three panes, left to right: which branch, which commit, and what it
   * changed. That last pane is the same shape as the diff view — a file tree
   * beside the selected file's diff — because it is the same question asked of
   * a commit rather than of the working tree, and answering it differently
   * would mean learning the layout twice.
   *
   * Loaded a page at a time. A repository of any age holds tens of thousands
   * of commits, and reading them all to show the twenty on screen would make
   * opening this a visible pause.
   */
  import { createEventDispatcher, tick } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main, session } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';
  import { buildFileTree, flattenTree, type TreeRow } from '../../utils/fileTree';
  import { highlightLine } from '../../utils/highlightLine';
  import { cachedLanguage, loadLanguage } from '../../utils/codemirror';
  import SideBySideDiff from '../MainPanel/SideBySideDiff.svelte';
  import { settings, saveSettings } from '../../stores/settings';

  export let show = false;
  /** The working directory whose history this is. */
  export let path = '';

  const dispatch = createEventDispatcher();

  let branches: main.GitBranchEntry[] = [];
  /** Empty means the current checkout, which is what opens first. */
  let branch = '';
  let currentBranch = '';

  let commits: main.GitCommit[] = [];
  let hasMore = false;
  let nextSkip = 0;
  let loading = false;
  let loadingMore = false;
  let error = '';

  let selectedHash = '';
  let files: session.DiffFileSummary[] = [];
  let selectedPath = '';
  let diff: session.DiffFile | null = null;
  let filesLoading = false;
  let diffLoading = false;

  let listEl: HTMLDivElement | null = null;
  let collapsed = new Set<string>();

  /**
   * The panes fold away leftwards and the dialog can fill the window, because
   * this is read at two different distances: skimming what happened lately
   * needs the commit list, and reading one change needs the diff and nothing
   * else. Folding a pane rather than shrinking it keeps the space it used.
   */
  let branchesCollapsed = false;
  let commitsCollapsed = false;
  let messageCollapsed = false;
  let maximised = false;

  /** Whole file rather than a few lines either side of the change: seeing
   *  three lines of context says what changed, not what it changed within. */
  let wholeFile = false;

  /**
   * Two aligned columns rather than one with markers. Shares the setting with
   * the diff view: it is one preference about how a diff is read, not two.
   */
  $: sideBySide = $settings.diffSideBySide === true;

  // Pane widths, dragged by the splitters between them.
  let branchWidth = 190;
  let commitWidth = 340;
  let treeWidth = 280;

  const MIN_PANE = 120;

  $: selectedCommit = commits.find((c) => c.hash === selectedHash) ?? null;

  /**
   * The header carries what the folded panes would have shown.
   *
   * Fold the branches away and it names the branch; fold the commit list too
   * and it adds the commit. They are what you lose first, so they come back
   * here rather than leaving the dialog unable to say what is on screen.
   */
  $: headerTitle = [
    $t('history.title'),
    branchesCollapsed ? (branch || currentBranch) : '',
    commitsCollapsed ? (selectedCommit?.subject ?? '') : '',
  ].filter(Boolean).join(' - ');

  function startPaneDrag(e: MouseEvent, which: 'branch' | 'commit' | 'tree') {
    e.preventDefault();
    const startX = e.clientX;
    const startW = which === 'branch' ? branchWidth : which === 'commit' ? commitWidth : treeWidth;

    const move = (ev: MouseEvent) => {
      const next = Math.max(MIN_PANE, startW + ev.clientX - startX);
      if (which === 'branch') branchWidth = next;
      else if (which === 'commit') commitWidth = next;
      else treeWidth = next;
    };
    const end = () => {
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', end);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', end);
  }

  /**
   * How tall the commit message pane is.
   *
   * A fixed 86px was enough for a subject line and little else, so a message
   * with a real body — the ones worth reading — had to be scrolled a few lines
   * at a time while most of the window sat empty below it.
   *
   * Remembered for the session rather than saved: it is a working adjustment
   * made while reading one commit, not a preference.
   */
  let messageHeight = 86;
  const MIN_MESSAGE_HEIGHT = 48;

  function startMessageDrag(e: MouseEvent) {
    e.preventDefault();
    const startY = e.clientY;
    const startH = messageHeight;

    const move = (ev: MouseEvent) => {
      // Capped against the window as well as the minimum: dragged past the
      // bottom, the pane would push the file list and the diff off the dialog
      // entirely, with no way back except closing it.
      const most = Math.max(MIN_MESSAGE_HEIGHT, window.innerHeight - 320);
      messageHeight = Math.min(most, Math.max(MIN_MESSAGE_HEIGHT, startH + ev.clientY - startY));
    };
    const end = () => {
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', end);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', end);
  }

  /** Double-click resets it, which is quicker than dragging back by eye. */
  function resetMessageHeight() {
    messageHeight = 86;
  }

  $: if (show && path) void open();

  async function open() {
    if (loading) return;
    error = '';
    await Promise.all([loadBranches(), loadHistory(true)]);
  }

  async function loadBranches() {
    try {
      const list = await App.ListGitBranches(path);
      branches = list?.branches ?? [];
      currentBranch = branches.find((b) => b.current)?.name ?? '';
    } catch (e) {
      console.error('Reading the branch list failed:', e);
      branches = [];
    }
  }

  async function loadHistory(reset: boolean) {
    if (reset) {
      loading = true;
      commits = [];
      nextSkip = 0;
      selectedHash = '';
      files = [];
      diff = null;
      selectedPath = '';
    } else {
      loadingMore = true;
    }

    try {
      const page = await App.GetGitHistory(path, branch, reset ? 0 : nextSkip);
      if (!page?.repository) {
        error = $t('history.notARepository');
        commits = [];
        return;
      }
      error = '';
      commits = reset ? (page.commits ?? []) : [...commits, ...(page.commits ?? [])];
      hasMore = page.hasMore;
      nextSkip = page.skip;
      if (reset && commits.length > 0) void selectCommit(commits[0].hash);
    } catch (e: any) {
      error = String(e?.message ?? e);
    } finally {
      loading = false;
      loadingMore = false;
    }
  }

  /** Loads the next page as the list nears its end, rather than on a button:
   *  scrolling is how you move through history, so it should not stop. */
  function onListScroll() {
    if (!listEl || !hasMore || loadingMore || loading) return;
    const remaining = listEl.scrollHeight - listEl.scrollTop - listEl.clientHeight;
    if (remaining < 400) void loadHistory(false);
  }

  async function changeBranch(name: string) {
    branch = name;
    await loadHistory(true);
  }

  async function selectCommit(hash: string) {
    selectedHash = hash;
    files = [];
    diff = null;
    selectedPath = '';
    filesLoading = true;
    try {
      files = (await App.GetGitCommitFiles(path, hash)) ?? [];
      if (files.length > 0) void selectFile(files[0].path);
    } catch (e) {
      console.error('Reading the commit failed:', e);
    } finally {
      filesLoading = false;
    }
  }

  async function selectFile(file: string) {
    selectedPath = file;
    diffLoading = true;
    try {
      diff = await App.GetGitCommitDiff(path, selectedHash, file, wholeFile);
    } catch (e) {
      console.error('Reading the diff failed:', e);
      diff = null;
    } finally {
      diffLoading = false;
    }
  }

  // The grammar is loaded per file, and highlightLine takes "no grammar" as
  // plain text — so a file whose language has not arrived yet renders
  // uncoloured rather than not at all.
  let languageForPath = '';
  $: if (selectedPath && selectedPath !== languageForPath) {
    languageForPath = selectedPath;
    void loadLanguage(selectedPath).then(() => {
      // Reassigned to re-run the highlighting now the grammar is there.
      diff = diff;
    });
  }

  // Re-read when the view changes, since it is a different request rather than
  // a different rendering of the same data.
  let wholeFileFor = false;
  $: if (show && selectedPath && wholeFile !== wholeFileFor) {
    wholeFileFor = wholeFile;
    void selectFile(selectedPath);
  }

  $: tree = buildFileTree(files);
  $: rows = flattenTree(tree, collapsed) as TreeRow<session.DiffFileSummary>[];

  function toggleDir(dirPath: string) {
    const next = new Set(collapsed);
    if (next.has(dirPath)) next.delete(dirPath);
    else next.add(dirPath);
    collapsed = next;
  }

  /**
   * The diff's lines, coloured the way the diff view colours them.
   *
   * A hunk carries its lines as one `body` string, not as a list — the first
   * version of this read a `lines` array that does not exist, so every hunk
   * rendered as its "@@" header and nothing else.
   */
  $: diffLines = (diff?.hunks ?? []).flatMap((hunk: any) => [
    { type: 'header', html: escapeHtml(hunk.header ?? '') },
    ...String(hunk.body ?? '')
      .split('\n')
      // git's body ends with a newline, which would otherwise render as a
      // blank line between every hunk.
      .filter((line, index, all) => !(line === '' && index === all.length - 1))
      .map((line) => ({
        type: lineType(line),
        html: highlightLine(line, cachedLanguage(selectedPath)),
      })),
  ]);

  /** Line indices where a change begins, for stepping between them. */
  $: changeStarts = diffLines.reduce((starts: number[], line, index) => {
    const isChange = line.type === 'add' || line.type === 'remove';
    const previous = diffLines[index - 1];
    const startsRun = isChange && !(previous && (previous.type === 'add' || previous.type === 'remove'));
    if (startsRun) starts.push(index);
    return starts;
  }, []);

  let currentChange = -1;
  /**
   * The block the marker points at, which is not always where the cursor is.
   *
   * Entering a file puts the cursor one step BEFORE the block it scrolled to,
   * so that leaving costs the same number of presses in either direction. The
   * marker cannot follow that: it would leave the block on screen unmarked and
   * then appear only on the press that seemed to do nothing.
   */
  let markedChange = -1;

  /**
   * Whether the walk is paused at the end of a file, offered the next one.
   *
   * Running out of changes used to move on immediately, so the name of the
   * file being entered flashed by with the file itself — there was no moment
   * in which to read it. The first press past the last change stops here and
   * names what is next; the press after that goes.
   */
  let atFileEdge: 1 | -1 | null = null;

  /**
   * The lines making up the change currently stepped to.
   *
   * A marker on the first line alone says where a block starts but not how far
   * it runs, and in whole-file view a block is an island in a long file — the
   * question is which lines to read, not where to look first.
   */
  $: currentBlock = (() => {
    if (markedChange < 0 || markedChange >= changeStarts.length) return { from: -1, to: -1 };
    const from = changeStarts[markedChange];
    let to = from;
    while (to + 1 < diffLines.length &&
           (diffLines[to + 1].type === 'add' || diffLines[to + 1].type === 'remove')) {
      to++;
    }
    return { from, to };
  })();

  /**
   * Reset when the file changes, not when its lines do.
   *
   * Keyed on the path rather than on diffLines: stepping writes currentChange,
   * which re-runs anything depending on it, and a reactive statement that both
   * reads the diff and writes the cursor re-entered itself — the first render
   * never finished, so the window stayed blank.
   */
  let changeCursorFor = '';
  $: if (selectedPath !== changeCursorFor) {
    changeCursorFor = selectedPath;
    currentChange = -1;
    markedChange = -1;
    atFileEdge = null;
  }

  /**
   * Move to the next or previous change.
   *
   * In whole-file view the changes are islands in a long file, and scrolling
   * to find them is most of the work — which is the same reason the diff view
   * has this.
   */
/**
   * Scroll an element to a third of the way down its scroller.
   *
   * scrollIntoView only offers start/center/end. Centring wastes half the screen
   * on context already read — a change is read downwards, so the room below it is
   * worth more than the room above.
   */
  /** How many lines the change at this position spans. */
  function blockLength(position: number): number {
    const from = changeStarts[position];
    if (from === undefined) return 1;
    let to = from;
    while (to + 1 < diffLines.length &&
           (diffLines[to + 1].type === 'add' || diffLines[to + 1].type === 'remove')) {
      to++;
    }
    return to - from + 1;
  }

  /**
   * Bring a change into view in whichever renderer is showing.
   *
   * The columns pair lines up, so their rows do not line up with the unified
   * positions changeStarts holds — the nth change is the nth run of changed
   * rows there.
   */
  function revealChange(position: number) {
    if (sideBySide) {
      const runs = sideBySideView?.changeRows() ?? [];
      const run = runs[position];
      if (run) sideBySideView?.scrollToRow(run.from, run.to - run.from + 1);
      return;
    }
    scrollToThird(diffEl,
      diffEl?.querySelector(`[data-line="${changeStarts[position]}"]`) ?? null,
      blockLength(position));
  }

  function scrollToThird(scroller: HTMLElement | null, target: Element | null, lines = 1) {
    if (!scroller || !target) return;
    const element = target as HTMLElement;
    const view = scroller.clientHeight;
    // How tall the change is, not how tall its first line is: passed the row
    // alone this always measured one line, so a long block never triggered the
    // rule below and ran off the bottom of the screen.
    const blockHeight = element.offsetHeight * Math.max(1, lines);
    // A block taller than the room below a third would run off the bottom, so
    // a long one starts near the top and uses the whole viewport instead.
    const margin = blockHeight > view - view / 3
      ? Math.min(view / 8, 60)
      : view / 3;
    scroller.scrollTo({ top: Math.max(0, element.offsetTop - margin), behavior: 'smooth' });
  }

  async function stepChange(delta: number) {
    stepDirection = delta > 0 ? 1 : -1;
    // Already offered the next file, and asked again in the same direction:
    // this is the press that accepts it.
    if (atFileEdge === delta) {
      atFileEdge = null;
      await stepFile(delta);
      return;
    }
    atFileEdge = null;

    if (!changeStarts.length) {
      await stepFile(delta);
      return;
    }
    // A file opened by clicking it sits at -1, "before the first change",
    // which is only "before" when moving forward. Read literally, stepping up
    // from there is -2 — outside the file — so the up arrow would leave it
    // immediately instead of going to its last change.
    const from = currentChange === -1 && delta < 0 ? changeStarts.length : currentChange;
    const next = from + delta;

    // Out of changes here. Stop and name the file this would go to, rather
    // than arriving there before the name has been read.
    if (next < 0 || next >= changeStarts.length) {
      atFileEdge = delta > 0 ? 1 : -1;
      return;
    }

    currentChange = next;
    markedChange = next;
    await tick();
    revealChange(next);
  }

  /**
   * Move to the next or previous file, landing on a change rather than at the
   * top of it.
   *
   * Opening the file was previously the whole step, so crossing a boundary
   * took two presses: one to arrive, another to reach the first change. And
   * entering a file backwards left the cursor before its first change, so the
   * next press up went straight out the other side.
   */
  async function stepFile(delta: number) {
    const order = fileOrder;
    if (!order.length) return;
    const at = order.findIndex((f) => f === selectedPath);
    // Wraps: a commit is read as a loop, and stopping dead at the last file
    // means going back to the tree to start again.
    const next = (at + delta + order.length) % order.length;

    // A commit touching one file wraps onto itself. Re-reading it would throw
    // away the position and land at the far end, so the arrow appeared to jump
    // to the other side of the file for no reason. There is nowhere to go, so
    // it stays put.
    if (next === at) return;

    // Claim the path for this step, so the reset below cannot fire against it
    // and undo the landing set at the end.
    changeCursorFor = order[next];
    await selectFile(order[next]);
    // selectFile reset the cursor; wait for the new file's changes to be
    // worked out before landing on one.
    await tick();

    if (!changeStarts.length) return;
    // Scroll to the change at the end we came in from — first when moving
    // forward, last when moving back — but set the position one step BEFORE
    // it, in our own direction of travel. Two effects, both wanted:
    //
    //  - Leaving takes the same number of presses whichever way you are going.
    //    Landing on the change itself lets a single press leave a file with
    //    one change, so working through a review passes over such files
    //    without ever stopping in them.
    //  - The first press then lands on the change already on screen, which is
    //    what makes the file feel entered rather than skipped through.
    // Land on the change itself, in both senses. Setting the cursor one step
    // before it made the next press move onto the change already on screen —
    // which looks like a press that did nothing, and doubled every step
    // through a review.
    const landing = delta > 0 ? 0 : changeStarts.length - 1;
    currentChange = landing;
    markedChange = landing;
    await tick();
    revealChange(landing);
  }

  /** The files in the order the tree shows them, which is the order stepping
   *  should follow — walking the flat list would jump between directories. */
  $: fileOrder = rows.filter((r) => r.kind === 'file').map((r: any) => r.file.path);

  $: selectedFileSummary = files.find((f) => f.path === selectedPath) ?? null;

  /**
   * The file a step would eventually reach, named on the button.
   *
   * Always named, not only at the file's last change: the question a user has
   * is "where does this arrow go", and answering it only on the final press
   * means the answer arrives when they no longer need it. What changes at the
   * boundary is whether the step lands there next — the tooltip says which.
   *
   * Written as expressions rather than through a helper. Svelte works out what
   * a reactive statement depends on by reading the statement and cannot see
   * through a call, so a statement calling a helper was ordered before the
   * values that helper reads and ran against undefined — which left the
   * window blank rather than merely wrong.
   */
  $: currentFileIndex = fileOrder.findIndex((f) => f === selectedPath);

  /**
   * Which file a step would move to, or empty while it stays in this one.
   *
   * Mirrors stepChange exactly, including its reading of -1 as "before the
   * first change going forward, after the last going back". Anything less and
   * the hint either promises a file change the arrow does not make, or stays
   * silent about one it does — which is what made it appear at random.
   */
  function fileAfterStep(delta: number, order: string[], path: string): string {
    if (!order.length) return '';
    const index = order.findIndex((f) => f === path);
    const next = ((index + delta) % order.length + order.length) % order.length;
    // A lone file has nowhere to go.
    if (next === index) return '';
    return order[next]?.split('/').pop() ?? '';
  }

  // Everything it depends on is passed in, so Svelte can see the dependencies:
  // it tracks what a reactive statement reads directly, not what a function it
  // calls reads inside.
  $: nextFileName = fileAfterStep(1, fileOrder, selectedPath);
  $: prevFileName = fileAfterStep(-1, fileOrder, selectedPath);

  /**
   * The hints float over the diff, on the edge the step is heading towards.
   *
   * Which way you are going decides which one shows: both at once is two
   * answers to a question that has one, and they would sit over the code
   * either side of what you are reading.
   */
  let stepDirection: 1 | -1 = 1;
  // Shown while the offer stands, rather than predicted from the cursor: the
  // label and the jump would otherwise land on the same press, so the name was
  // never read before the file it named replaced the one on screen.
  $: hintBelow = atFileEdge === 1 ? nextFileName : '';
  $: hintAbove = atFileEdge === -1 ? prevFileName : '';

  let diffEl: HTMLDivElement | null = null;
  let sideBySideView: {
    scrollToRow(row: number, count?: number): void;
    changeRows(): Array<{ from: number; to: number }>;
  } | null = null;

  function escapeHtml(text: string): string {
    return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  /** The hunks as two-column input: header, index, and highlighted lines with
   *  the +/- dropped — which column a line is in says what happened to it. */
  $: sideBySideHunks = (diff?.hunks ?? []).map((hunk: any, index: number) => ({
    header: hunk.header ?? '',
    index,
    lines: String(hunk.body ?? '')
      .split('\n')
      .filter((line, at, all) => !(line === '' && at === all.length - 1))
      .map((line) => ({
        type: lineType(line),
        html: highlightLine(line, cachedLanguage(selectedPath)),
      })),
  }));

  function lineType(line: string): string {
    if (line.startsWith('+') && !line.startsWith('+++')) return 'add';
    if (line.startsWith('-') && !line.startsWith('---')) return 'remove';
    if (line.startsWith('@@')) return 'header';
    if (line.startsWith('diff ') || line.startsWith('index ') ||
        line.startsWith('+++') || line.startsWith('---')) return 'meta';
    return 'context';
  }

  /** Dates are formatted here rather than in Go: the interface knows the
   *  user's locale and the backend does not. */
  function when(iso: string): string {
    if (!iso) return '';
    const date = new Date(iso);
    return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
  }

  function close() {
    show = false;
    dispatch('close');
  }

  /**
   * Handled on the window rather than on the overlay.
   *
   * The overlay only sees a key when focus is inside it, and nothing here
   * takes focus on open — so Escape did nothing at all, which for a dialog is
   * the one shortcut a user assumes works.
   */
  function onWindowKeydown(e: KeyboardEvent) {
    if (!show) return;
    onKeydown(e);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    }
    if (e.key === 'F7') {
      e.preventDefault();
      void stepChange(e.shiftKey ? -1 : 1);
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;

    e.preventDefault();
    const at = commits.findIndex((c) => c.hash === selectedHash);
    const next = e.key === 'ArrowDown' ? at + 1 : at - 1;
    if (next < 0 || next >= commits.length) return;
    void selectCommit(commits[next].hash);
    void scrollSelectedIntoView(next);
  }

  async function scrollSelectedIntoView(index: number) {
    await tick();
    listEl?.querySelector(`[data-commit="${index}"]`)?.scrollIntoView({ block: 'nearest' });
  }

</script>

<svelte:window on:keydown={onWindowKeydown} />

{#if show}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div class="dialog-overlay" on:click|self={close} on:keydown={onKeydown} role="dialog" tabindex="-1">
    <div class="dialog-content history-dialog" class:maximised>
      <div class="dialog-header">
        <h2 class="history-title" title={headerTitle}>{headerTitle}</h2>
        <button
          class="header-btn"
          title={maximised ? $t('history.restore') : $t('history.maximise')}
          on:click={() => (maximised = !maximised)}
        >
          {#if maximised}
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/>
            </svg>
          {:else}
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/>
            </svg>
          {/if}
        </button>
        <button class="close-btn" on:click={close} aria-label={$t('common.close')}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="history-body">
        <!-- Branches -->
        {#if branchesCollapsed}
          <button
            class="fold-strip"
            title={$t('history.showBranches')}
            on:click={() => (branchesCollapsed = false)}
          >›</button>
        {:else}
        <div class="branch-pane" style="width: {branchWidth}px">
          <div class="pane-title">
            {$t('history.branches')}
            <button
              class="fold-btn"
              title={$t('history.hideBranches')}
              on:click={() => (branchesCollapsed = true)}
            >‹</button>
          </div>
          <button
            class="branch-row"
            class:selected={branch === ''}
            on:click={() => changeBranch('')}
          >
            {currentBranch || $t('history.currentBranch')}
            <span class="branch-tag">{$t('history.current')}</span>
          </button>
          {#each branches.filter((b) => !b.current) as entry (entry.name)}
            <button
              class="branch-row"
              class:selected={branch === entry.name}
              on:click={() => changeBranch(entry.name)}
              title={entry.name}
            >
              {entry.name}
            </button>
          {/each}
        </div>
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div class="pane-splitter" on:mousedown={(e) => startPaneDrag(e, 'branch')}></div>
        {/if}

        <!-- Commits -->
        {#if commitsCollapsed}
          <button
            class="fold-strip"
            title={$t('history.showCommits')}
            on:click={() => (commitsCollapsed = false)}
          >›</button>
        {:else}
        <div class="commit-pane" style="width: {commitWidth}px" bind:this={listEl} on:scroll={onListScroll}>
          <div class="pane-title">
            {$t('history.commits')}
            <button
              class="fold-btn"
              title={$t('history.hideCommits')}
              on:click={() => (commitsCollapsed = true)}
            >‹</button>
          </div>
          {#if loading}
            <p class="pane-note">{$t('common.loading')}</p>
          {:else if error}
            <p class="pane-note error">{error}</p>
          {:else if commits.length === 0}
            <p class="pane-note">{$t('history.noCommits')}</p>
          {:else}
            {#each commits as commit, index (commit.hash)}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <div
                class="commit-row"
                class:selected={commit.hash === selectedHash}
                data-commit={index}
                role="option"
                aria-selected={commit.hash === selectedHash}
                tabindex="-1"
                on:click={() => selectCommit(commit.hash)}
              >
                <div class="commit-subject">
                  {#if (commit.parents?.length ?? 0) > 1}
                    <span class="merge-badge" title={$t('history.merge')}>⑂</span>
                  {/if}
                  {commit.subject}
                </div>
                <div class="commit-meta">
                  <span class="hash">{commit.shortHash}</span>
                  <span class="author">{commit.author}</span>
                  <span class="date">{when(commit.committed)}</span>
                </div>
                {#if commit.refs?.length}
                  <div class="commit-refs">
                    {#each commit.refs as ref}<span class="ref">{ref}</span>{/each}
                  </div>
                {/if}
              </div>
            {/each}
            {#if loadingMore}
              <p class="pane-note">{$t('common.loading')}</p>
            {/if}
          {/if}
        </div>
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div class="pane-splitter" on:mousedown={(e) => startPaneDrag(e, 'commit')}></div>
        {/if}

        <!-- What the commit changed -->
        <div class="change-pane">
          {#if selectedCommit}
            <div
              class="commit-detail"
              class:collapsed={messageCollapsed}
              style={messageCollapsed ? '' : `height: ${messageHeight}px`}
            >
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <div
                class="detail-subject"
                role="button"
                tabindex="-1"
                title={messageCollapsed ? $t('history.showMessage') : $t('history.hideMessage')}
                on:click={() => (messageCollapsed = !messageCollapsed)}
              >
                <span class="fold-caret">{messageCollapsed ? '▾' : '▴'}</span>
                {selectedCommit.subject}
              </div>
              {#if selectedCommit.body && !messageCollapsed}
                <pre class="detail-body">{selectedCommit.body}</pre>
              {/if}
              {#if !messageCollapsed}
              <div class="detail-meta">
                {selectedCommit.author} &lt;{selectedCommit.email}&gt; · {when(selectedCommit.committed)}
                · <span class="hash">{selectedCommit.shortHash}</span>
              </div>
              {/if}
            </div>
            <!-- Dragged to give a long commit message more room. Hidden while
                 the message is folded away, where there is nothing to resize.
                 svelte-ignore a11y-no-noninteractive-element-interactions -->
            {#if !messageCollapsed}
              <div
                class="message-resizer"
                role="separator"
                aria-orientation="horizontal"
                title={$t('history.resizeMessage')}
                on:mousedown={startMessageDrag}
                on:dblclick={resetMessageHeight}
              ></div>
            {/if}
          {/if}

          <div class="change-split">
            <div class="file-tree" style="width: {treeWidth}px">
              {#if filesLoading}
                <p class="pane-note">{$t('common.loading')}</p>
              {:else if files.length === 0}
                <p class="pane-note">{$t('history.noFiles')}</p>
              {:else}
                {#each rows as row (row.kind + ':' + (row.kind === 'dir' ? row.path : row.file.path))}
                  {#if row.kind === 'dir'}
                    <!-- svelte-ignore a11y-click-events-have-key-events -->
                    <div
                      class="tree-row dir"
                      style="padding-left: {8 + row.depth * 12}px"
                      role="button"
                      tabindex="-1"
                      on:click={() => toggleDir(row.path)}
                    >
                      <span class="chevron">{collapsed.has(row.path) ? '▸' : '▾'}</span>
                      <span class="label">{row.label}</span>
                      <span class="counts">
                        <span class="added">+{row.added}</span><span class="removed">−{row.removed}</span>
                      </span>
                    </div>
                  {:else}
                    <!-- svelte-ignore a11y-click-events-have-key-events -->
                    <div
                      class="tree-row file"
                      class:selected={row.file.path === selectedPath}
                      style="padding-left: {20 + row.depth * 12}px"
                      role="button"
                      tabindex="-1"
                      on:click={() => selectFile(row.file.path)}
                      title={row.file.path}
                    >
                      <span class="label">{row.file.path.split('/').pop()}</span>
                      <span class="counts">
                        <span class="added">+{row.file.added}</span><span class="removed">−{row.file.removed}</span>
                      </span>
                    </div>
                  {/if}
                {/each}
              {/if}
            </div>

            <!-- svelte-ignore a11y-no-static-element-interactions -->
            <div class="pane-splitter" on:mousedown={(e) => startPaneDrag(e, 'tree')}></div>

            <div class="diff-side">
              {#if selectedPath}
                <!-- The tree shows only the basename, so the full path belongs
                     here — two same-named files in different directories are
                     otherwise indistinguishable once selected. -->
                <div class="selected-path" title={selectedPath}>
                  <span class="selected-text">
                    <span class="selected-dir">{selectedPath.split('/').slice(0, -1).join('/')}{selectedPath.includes('/') ? '/' : ''}</span><span class="selected-name">{selectedPath.split('/').pop()}</span>
                  </span>
                  {#if selectedFileSummary}
                    <span class="selected-stats">
                      <span class="added">+{selectedFileSummary.added}</span>
                      <span class="removed">−{selectedFileSummary.removed}</span>
                    </span>
                  {/if}
                  <span class="selected-nav">
                    <button
                      class="nav-btn"
                      title={prevFileName
                        ? $t('diff.prevGoesTo', { file: prevFileName })
                        : $t('history.prevChange')}
                      on:click={() => stepChange(-1)}
                    >↑</button>
                    <button
                      class="nav-btn"
                      title={nextFileName
                        ? $t('diff.nextGoesTo', { file: nextFileName })
                        : $t('history.nextChange')}
                      on:click={() => stepChange(1)}
                    >↓</button>
                    <button
                      class="nav-btn wide"
                      class:active={wholeFile}
                      title={wholeFile ? $t('diff.showHunksOnly') : $t('diff.showWholeFile')}
                      on:click={() => (wholeFile = !wholeFile)}
                    >{wholeFile ? $t('diff.wholeFile') : $t('diff.hunksOnly')}</button>
                    <button
                      class="nav-btn"
                      class:active={sideBySide}
                      title={sideBySide ? $t('diff.showUnified') : $t('diff.showSideBySide')}
                      on:click={() => saveSettings({ diffSideBySide: !sideBySide })}
                    >⫲</button>
                  </span>
                </div>
              {/if}

            <!-- Wrapper so the hints can float over the diff: the pane below
                 is the scroller, and anything positioned inside it would
                 scroll away with the code. -->
            <div class="diff-viewport">
              {#if hintAbove}
                <button
                  class="file-hint top"
                  title={$t('diff.prevGoesTo', { file: hintAbove })}
                  on:click={() => stepChange(-1)}
                >↑ {hintAbove}</button>
              {/if}
              {#if hintBelow}
                <button
                  class="file-hint bottom"
                  title={$t('diff.nextGoesTo', { file: hintBelow })}
                  on:click={() => stepChange(1)}
                >↓ {hintBelow}</button>
              {/if}

            {#if sideBySide}
              <SideBySideDiff
                bind:this={sideBySideView}
                hunks={sideBySideHunks}
                currentChange={markedChange}
              />
            {:else}
            <div class="diff-pane" bind:this={diffEl}>
              {#if diffLoading}
                <p class="pane-note">{$t('common.loading')}</p>
              {:else if diffLines.length === 0}
                <p class="pane-note">{$t('history.selectFile')}</p>
              {:else}
                {#each diffLines as line, i (i)}
                  <div
                    class="diff-line {line.type}"
                    class:in-block={i >= currentBlock.from && i <= currentBlock.to}
                    data-line={i}
                  >
                    <!-- Already escaped; see utils/highlightLine.ts. -->
                    <code>{@html line.html}</code>
                  </div>
                {/each}
              {/if}
            </div>
            {/if}
            </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .history-dialog {
    /* The shared .dialog-content caps at 400px, which cannot hold three panes. */
    width: min(1500px, 96vw);
    max-width: min(1500px, 96vw);
    height: min(88vh, 1000px);
    display: flex;
    flex-direction: column;
  }

  .history-dialog.maximised {
    width: 100vw;
    max-width: 100vw;
    height: 100vh;
    max-height: 100vh;
    border-radius: 0;
  }

  /* Flex rather than a grid of fixed columns: a folded pane has to give its
     space to the diff, and a dragged one has to keep the width it was given. */
  .history-body {
    display: flex;
    flex: 1 1 auto;
    min-height: 0;
  }

  /* A wide grab area pulled back over its neighbours by negative margins, so
     the strip is comfortable to hit without taking that width from the panes.
     At the old 5px-minus-2px there were three pixels to aim at, and catching
     them was a matter of luck. z-index keeps it above the pane contents: the
     file rows next to it take the mouse too, and without this the row won on
     the overlap — which is the half that made the splitter feel dead. */
  .pane-splitter {
    flex: 0 0 auto;
    width: 11px;
    margin: 0 -5px;
    cursor: col-resize;
    background: transparent;
    z-index: 5;
  }
  .pane-splitter:hover {
    background: rgba(97, 175, 239, 0.35);
  }

  /* A folded pane leaves a strip to bring it back — a pane with no way to
     reopen it is a pane the user has lost. */
  .fold-strip {
    flex: 0 0 auto;
    width: 16px;
    border: none;
    border-right: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(255, 255, 255, 0.02);
    color: inherit;
    opacity: 0.5;
    cursor: pointer;
  }
  .fold-strip:hover {
    opacity: 1;
    background: rgba(255, 255, 255, 0.06);
  }

  .fold-btn {
    margin-left: auto;
    padding: 0 4px;
    border: none;
    background: transparent;
    color: inherit;
    opacity: 0.6;
    cursor: pointer;
    font-size: 1.1em;
    line-height: 1;
  }
  .fold-btn:hover {
    opacity: 1;
  }

  .header-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    margin-right: 8px;
    border: none;
    border-radius: 7px;
    background: rgba(255, 255, 255, 0.05);
    color: #6b7280;
    cursor: pointer;
  }
  .header-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }
  .header-btn:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .header-btn.on {
    color: var(--accent, #61afef);
    background: rgba(97, 175, 239, 0.16);
  }

  /* One line that truncates: a commit subject is long enough that it will,
     and the buttons after it must keep their place. */
  .history-title {
    flex: 1 1 auto;
    min-width: 0;
    margin-right: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }


  .branch-pane,
  .commit-pane {
    flex: 0 0 auto;
    overflow-y: auto;
    border-right: 1px solid rgba(255, 255, 255, 0.05);
    background: #08080c;
  }
  /* Takes whatever the panes to its left are not using, so folding one widens
     the diff instead of leaving a gap — which is the point of folding it. */
  .change-pane {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .pane-title {
    display: flex;
    align-items: center;
    padding: 8px 12px 4px;
    font-size: 0.72em;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    opacity: 0.5;
  }
  .pane-note {
    padding: 18px 14px;
    opacity: 0.6;
    font-size: 0.88em;
  }
  .pane-note.error {
    color: #e06c75;
  }

  .branch-row {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 6px 12px;
    border: none;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .branch-row:hover {
    background: rgba(255, 255, 255, 0.05);
  }
  .branch-row.selected {
    background: rgba(255, 255, 255, 0.09);
    box-shadow: inset 2px 0 0 var(--accent, #61afef);
  }
  .branch-tag {
    font-size: 0.68em;
    opacity: 0.5;
    text-transform: uppercase;
  }

  .commit-row {
    padding: 8px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    cursor: pointer;
  }
  .commit-row:hover {
    background: rgba(255, 255, 255, 0.04);
  }
  .commit-row.selected {
    background: rgba(255, 255, 255, 0.09);
    box-shadow: inset 2px 0 0 var(--accent, #61afef);
  }
  .commit-subject {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .merge-badge {
    opacity: 0.6;
    margin-right: 3px;
  }
  .commit-meta {
    display: flex;
    gap: 8px;
    margin-top: 3px;
    font-size: 0.75em;
    opacity: 0.55;
  }
  .commit-meta .author {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .commit-meta .date {
    margin-left: auto;
    flex: 0 0 auto;
  }
  .hash {
    font-family: monospace;
  }
  .commit-refs {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }
  .ref {
    padding: 1px 6px;
    border-radius: 9px;
    background: rgba(97, 175, 239, 0.16);
    color: #61afef;
    font-size: 0.7em;
  }

  /* A fixed height rather than a share of the dialog: a percentage made every
     commit give the panes below it a different amount of room, so moving
     through the list resized the file tree and the diff on every selection.
     A long message scrolls within this instead. */
  .message-resizer {
    flex: 0 0 auto;
    height: 6px;
    cursor: row-resize;
    background: transparent;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }

  .message-resizer:hover {
    background: rgba(var(--accent-rgb), 0.25);
  }

  .commit-detail {
    flex: 0 0 auto;
    /* The inline style sets this when the message is open; the value here is
       the fallback for the folded state, where the subject line is all there
       is to show. */
    height: 86px;
    padding: 10px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    overflow-y: auto;
  }
  .detail-subject {
    display: flex;
    align-items: baseline;
    gap: 7px;
    font-weight: 600;
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .detail-subject:hover {
    color: var(--accent, #61afef);
  }
  .fold-caret {
    opacity: 0.5;
    font-size: 0.8em;
  }
  /* Folded, it is a single bar rather than a panel with one line in it. */
  .commit-detail.collapsed {
    height: auto;
    padding: 7px 14px;
    background: #0d0d14;
  }
  .detail-body {
    margin: 6px 0 0;
    font-size: 0.84em;
    opacity: 0.8;
    white-space: pre-wrap;
    font-family: inherit;
  }
  .detail-meta {
    margin-top: 6px;
    font-size: 0.75em;
    opacity: 0.55;
  }

  .change-split {
    display: flex;
    flex: 1 1 auto;
    min-height: 0;
  }
  /* Set back from the diff beside it, so the list and the code do not read as
     one surface.
     Opaque rather than a translucent black: the dialog's own backdrop is a
     vertical gradient, and a see-through panel over it picks up that shading —
     the tree came out lighter at the top than the diff beside it, which is the
     "gradient background" this kept looking like. */
  .file-tree {
    flex: 0 0 auto;
    overflow-y: auto;
    border-right: 1px solid rgba(255, 255, 255, 0.05);
    background: #08080c;
    padding: 4px 0;
  }
  .tree-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-top: 3px;
    padding-bottom: 3px;
    padding-right: 10px;
    cursor: pointer;
    font-size: 0.86em;
  }
  .tree-row:hover {
    background: rgba(255, 255, 255, 0.05);
  }
  .tree-row.selected {
    background: rgba(var(--accent-rgb), 0.12);
    box-shadow: inset 2px 0 0 var(--accent, #61afef);
  }
  .tree-row .label {
    flex: 1 1 auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chevron {
    flex: 0 0 auto;
    opacity: 0.5;
    font-size: 0.8em;
  }
  .counts {
    flex: 0 0 auto;
    display: flex;
    gap: 5px;
    font-size: 0.8em;
    font-variant-numeric: tabular-nums;
  }
  .added {
    color: #98c379;
  }
  .removed {
    color: #e06c75;
  }

  /* Column, so the path bar sits above the scrolling diff rather than
     scrolling away with it — the same arrangement as the diff view. */
  .diff-side {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  /* Taken from the diff view so the two read as the same thing. */
  .selected-path {
    display: flex;
    align-items: center;
    height: 31px;
    box-sizing: border-box;
    gap: 8px;
    padding: 7px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    background: #0d0d14;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
    color: #d4d4d8;
    flex-shrink: 0;
  }
  /* The directory may shrink away, the file name never does — that is the part
     worth keeping when the path is too long for the pane. */
  .selected-text {
    display: flex;
    min-width: 0;
    overflow: hidden;
  }
  .selected-dir {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.55;
  }
  .selected-name {
    flex: 0 0 auto;
  }
  .selected-stats {
    display: flex;
    gap: 6px;
    font-size: 0.9em;
  }
  .selected-nav {
    margin-left: auto;
    display: flex;
    gap: 4px;
  }
  .nav-btn {
    padding: 2px 7px;
    font-size: 11px;
    line-height: 1.4;
    cursor: pointer;
    border-radius: 4px;
    border: 1px solid rgba(255, 255, 255, 0.18);
    background: rgba(255, 255, 255, 0.06);
    color: inherit;
  }
  .nav-btn.wide {
    padding: 2px 10px;
  }

  .diff-viewport {
    position: relative;
    flex: 1 1 auto;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    /* Opaque, and the same black the unified pane below uses.
       Without it the side-by-side view has no background of its own, so the
       dialog's backdrop — a vertical gradient — shows through and tints the
       code a faint purple that shifts down the page. The unified pane sets
       this on itself and so never showed the problem. */
    background: #0a0a0f;
  }

  /* Taken from the diff view, so the two behave the same way. */
  .file-hint {
    position: absolute;
    right: 18px;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 5px;
    max-width: 60%;
    padding: 3px 10px;
    font-size: 11px;
    font-family: inherit;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
    border-radius: 12px;
    border: 1px solid rgba(97, 175, 239, 0.35);
    /* Opaque, not tinted: it sits over code, and a translucent label with text
       behind it is unreadable. */
    background: #1f2430;
    color: #c9d1d9;
    opacity: 0.9;
  }
  .file-hint:hover {
    opacity: 1;
    border-color: rgba(97, 175, 239, 0.6);
  }
  .file-hint.top {
    top: 8px;
  }
  .file-hint.bottom {
    bottom: 8px;
  }
  .nav-btn:hover {
    background: rgba(255, 255, 255, 0.14);
  }
  .nav-btn.active {
    border-color: var(--accent, #61afef);
    color: var(--accent, #61afef);
  }

  .diff-pane {
    flex: 1 1 auto;
    min-width: 0;
    overflow: auto;
    background: #0a0a0f;
    padding: 4px 0;
    font-family: var(--terminal-font, monospace);
    font-size: 13px;
  }
  /* Kept identical to the diff view's: the two must not disagree about what an
     added line looks like. The tint alone carries added/removed, so the text
     keeps its syntax colours. */
  .diff-line {
    white-space: pre;
    padding: 0 14px;
    line-height: 19px;
    /* As wide as the widest line, not as wide as its own text. The pane
       scrolls sideways, and a row sized to its own content ends where the text
       does — so the add/remove tint stopped mid-row and the rest of the line
       showed the pane behind it, which reads as a gradient. */
    min-width: max-content;
  }
  .diff-line code {
    font-family: inherit;
    font-size: inherit;
  }
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
  /* The block the arrows are on. A bar down its left edge rather than a tint:
     the add/remove tints already carry meaning here, and a second one over
     them would say two things at once about the same line. */
  .diff-line.in-block.add,
  .diff-line.in-block.remove {
    box-shadow: inset 3px 0 0 var(--accent, #61afef);
  }
  /* "diff --git", "index abc..def" and the +++/--- pair: notation about the
     file rather than its content. */
  .diff-line.meta {
    color: #6b7280;
  }
</style>
