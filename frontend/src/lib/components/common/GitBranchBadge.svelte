<script lang="ts">
  import { tick } from 'svelte';
  import { t } from '../../i18n';
  import {
    gitBranch,
    formatAheadBehind,
    listGitBranches,
    currentGitPath,
    type GitBranchEntry
  } from '../../stores/gitBranch';
  import { portal } from '../../utils/portal';

  /** 'statusbar' matches the muted bottom-bar items; 'header' sits by the name. */
  export let variant: 'header' | 'statusbar' = 'header';

  let isOpen = false;
  let loading = false;
  let failed = false;
  let branches: GitBranchEntry[] = [];
  let total = 0;
  let truncated = false;

  let triggerRef: HTMLButtonElement;
  let menuRef: HTMLDivElement;
  /** Drops replies from a listing whose menu has already been closed/reopened. */
  let listGeneration = 0;

  $: aheadBehind = formatAheadBehind($gitBranch);
  $: tooltip = (() => {
    if (!$gitBranch) return '';
    const parts = [$t('gitBranch.tooltip', { branch: $gitBranch.branch })];
    if ($gitBranch.upstream) {
      parts.push($t('gitBranch.tooltipUpstream', { upstream: $gitBranch.upstream }));
      if ($gitBranch.ahead > 0) parts.push($t('gitBranch.tooltipAhead', { count: $gitBranch.ahead }));
      if ($gitBranch.behind > 0) parts.push($t('gitBranch.tooltipBehind', { count: $gitBranch.behind }));
    }
    return parts.join(' · ');
  })();

  // A branch/path change under the badge invalidates whatever the menu read, so
  // the menu closes rather than showing a listing for the previous session.
  // The tracking variable is assigned INSIDE the block: Svelte 3 orders
  // reactive statements by dependency, not by source order, so a separate
  // assignment could run before this and make the comparison never fire.
  let lastBranchKey = '';
  $: {
    const key = $gitBranch ? `${$gitBranch.path}|${$gitBranch.branch}` : '';
    if (key !== lastBranchKey) {
      lastBranchKey = key;
      close();
    }
  }

  function positionMenu() {
    if (!triggerRef || !menuRef) return;
    const rect = triggerRef.getBoundingClientRect();
    const menuHeight = menuRef.offsetHeight;
    const gap = 6;

    // The header badge has room below it, the status-bar badge does not; both
    // are handled by measuring rather than by branching on the variant.
    const openUpwards = rect.bottom + gap + menuHeight > window.innerHeight;
    menuRef.style.top = openUpwards
      ? `${Math.max(gap, rect.top - gap - menuHeight)}px`
      : `${rect.bottom + gap}px`;

    const left = Math.min(rect.left, window.innerWidth - menuRef.offsetWidth - gap);
    menuRef.style.left = `${Math.max(gap, left)}px`;
  }

  async function loadBranches() {
    const generation = ++listGeneration;
    loading = true;
    failed = false;
    try {
      const list = await listGitBranches(currentGitPath());
      if (generation !== listGeneration) return;
      branches = list.branches || [];
      total = list.total;
      truncated = list.truncated;
    } catch (e) {
      console.error('Failed to list git branches:', e);
      if (generation !== listGeneration) return;
      // The badge itself keeps working; only the menu reports the failure.
      failed = true;
      branches = [];
      total = 0;
      truncated = false;
    } finally {
      if (generation === listGeneration) loading = false;
    }
  }

  async function toggle() {
    if (isOpen) {
      close();
      return;
    }
    isOpen = true;
    // Lazily: the badge is mounted for every session, so listing on render
    // would fork a git process per session instead of per click.
    loadBranches();
    await tick();
    positionMenu();
  }

  function close() {
    if (!isOpen) return;
    isOpen = false;
    // Invalidate any listing still in flight so it can't land into a menu the
    // user has already dismissed.
    listGeneration++;
    loading = false;
  }

  function handleWindowClick(e: MouseEvent) {
    if (!isOpen) return;
    const target = e.target as Node;
    if (triggerRef && triggerRef.contains(target)) return;
    if (menuRef && menuRef.contains(target)) return;
    close();
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (isOpen && e.key === 'Escape') {
      e.stopPropagation();
      close();
    }
  }

  // Re-measure after the content swaps from the loading row to the real list,
  // otherwise an upward-opening menu stays anchored to its placeholder height.
  // The tracking variable is assigned INSIDE the block: Svelte 3 orders
  // reactive statements by dependency, so a separate assignment would run at an
  // unpredictable point and could make this fire on every unrelated update.
  let lastMenuSignature = '';
  $: {
    const signature = `${isOpen}|${loading}|${failed}|${branches.length}|${truncated}`;
    if (signature !== lastMenuSignature) {
      lastMenuSignature = signature;
      if (isOpen) tick().then(positionMenu);
    }
  }
</script>

<svelte:window on:click={handleWindowClick} on:keydown={handleWindowKeydown} on:resize={close} />

<!-- Nothing is rendered outside a git repo: no icon, no placeholder, no gap. -->
{#if $gitBranch && $gitBranch.branch}
  <button
    type="button"
    class="git-branch git-branch-{variant}"
    class:open={isOpen}
    bind:this={triggerRef}
    title={tooltip}
    aria-haspopup="true"
    aria-expanded={isOpen}
    on:click|stopPropagation={toggle}
  >
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <line x1="6" y1="3" x2="6" y2="15"/>
      <circle cx="18" cy="6" r="3"/>
      <circle cx="6" cy="18" r="3"/>
      <path d="M18 9a9 9 0 01-9 9"/>
    </svg>
    <span class="git-branch-name">{$gitBranch.branch}</span>
    {#if aheadBehind}
      <span class="git-branch-counts">{aheadBehind}</span>
    {/if}
    <svg class="git-branch-chevron" class:open={isOpen} width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <polyline points="6 9 12 15 18 9"/>
    </svg>
  </button>
{/if}

{#if isOpen}
  <!-- Read-only listing: the rows are plain text, not buttons, so nothing here
       offers to switch branches. -->
  <div class="branch-menu" bind:this={menuRef} use:portal>
    <div class="branch-menu-title">{$t('gitBranch.listTitle')}</div>
    {#if loading}
      <div class="branch-menu-note">{$t('gitBranch.listLoading')}</div>
    {:else if failed}
      <div class="branch-menu-note error">{$t('gitBranch.listError')}</div>
    {:else if branches.length === 0}
      <div class="branch-menu-note">{$t('gitBranch.listEmpty')}</div>
    {:else}
      <ul class="branch-list">
        {#each branches as branch (branch.name)}
          <li class="branch-row" class:current={branch.current}>
            <span class="branch-check">
              {#if branch.current}
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <polyline points="20 6 9 17 4 12"/>
                </svg>
              {/if}
            </span>
            <span class="branch-row-name" title={branch.name}>{branch.name}</span>
            <span class="branch-row-hash">{branch.hash}</span>
          </li>
        {/each}
      </ul>
      {#if truncated}
        <div class="branch-menu-note">
          {$t('gitBranch.listTruncated', { shown: branches.length, total })}
        </div>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .git-branch {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex-shrink: 0;
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }

  .git-branch svg {
    flex-shrink: 0;
  }

  .git-branch-name {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .git-branch-counts {
    font-size: 11px;
    color: var(--accent-light);
  }

  /* The chevron is the only affordance saying the badge opens something; it
     stays faint so the badge still reads as a label first. */
  .git-branch-chevron {
    opacity: 0.5;
    transition: transform 0.15s ease, opacity 0.15s ease;
  }

  .git-branch:hover .git-branch-chevron,
  .git-branch.open .git-branch-chevron {
    opacity: 1;
  }

  .git-branch-chevron.open {
    transform: rotate(180deg);
  }

  .git-branch-statusbar {
    color: #6b7280;
  }

  .git-branch-statusbar .git-branch-name {
    color: #9ca3af;
  }

  .git-branch:hover .git-branch-name,
  .git-branch.open .git-branch-name {
    color: #e4e4e7;
  }

  /* In the header the branch trails the session name, so it stays subdued
     enough not to compete with it. The generous gap and the hairline before
     it read as two separate pieces of information rather than one run-on
     label — the same separation IDEs put between the file and the branch. */
  .git-branch-header {
    margin-left: 18px;
    padding-left: 18px;
    border-left: 1px solid rgba(255, 255, 255, 0.1);
    color: #6b7280;
    font-weight: 400;
  }

  .git-branch-header .git-branch-name {
    color: #9ca3af;
  }

  /* Portaled to body, so the menu's own rules need :global. */
  :global(.branch-menu) {
    position: fixed;
    z-index: 1000;
    min-width: 220px;
    max-width: 380px;
    max-height: 320px;
    overflow-y: auto;
    background: #1a1a2e;
    border: 1px solid rgba(var(--accent-rgb), 0.3);
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
    padding: 6px;
  }

  :global(.branch-menu .branch-menu-title) {
    padding: 4px 8px 6px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: #6b7280;
  }

  :global(.branch-menu .branch-menu-note) {
    padding: 6px 8px;
    font-size: 11px;
    color: #6b7280;
    font-style: italic;
  }

  :global(.branch-menu .branch-menu-note.error) {
    color: #f87171;
    font-style: normal;
  }

  :global(.branch-menu .branch-list) {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  /* Rows are <li>, have no hover state and no pointer cursor: nothing suggests
     clicking one does anything, because it does not. Text stays selectable so
     a branch name can still be copied out. */
  :global(.branch-menu .branch-row) {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    font-size: 12px;
    color: #9ca3af;
    cursor: default;
    user-select: text;
  }

  :global(.branch-menu .branch-row.current) {
    color: var(--accent-light);
    font-weight: 600;
  }

  :global(.branch-menu .branch-check) {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 12px;
    flex-shrink: 0;
  }

  :global(.branch-menu .branch-row-name) {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  :global(.branch-menu .branch-row-hash) {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 10px;
    color: #4b5563;
    flex-shrink: 0;
  }
</style>
