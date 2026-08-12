<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy, tick } from 'svelte';
  import { t } from '../../i18n';

  export let value: string = '';
  export let options: { value: string; label: string }[] = [];
  export let placeholder: string = '';
  export let small: boolean = false;
  /**
   * Show a filter box above the list.
   *
   * Off by default: for a handful of fixed choices a filter is one more thing
   * to look at. Worth turning on where the options are user data and there can
   * be many of them — a task list, for instance.
   */
  export let searchable: boolean = false;

  const dispatch = createEventDispatcher<{ change: string }>();

  let isOpen = false;
  let triggerRef: HTMLButtonElement;
  let dropdownRef: HTMLDivElement;
  let searchRef: HTMLInputElement | null = null;
  let query = '';
  /** Which filtered option Enter would take. Reset whenever the list changes. */
  let highlighted = 0;

  // Matched case-insensitively on the label, which is what the user can see.
  // Matching the value would search hidden ids and appear to ignore the typing.
  $: filteredOptions = searchable && query.trim()
    ? options.filter((o) => o.label.toLowerCase().includes(query.trim().toLowerCase()))
    : options;

  // Typing narrows the list, so a highlight left where it was could point past
  // the end of it — or at something the user is no longer looking at.
  $: if (query !== undefined) highlighted = 0;

  $: selectedOption = options.find(o => o.value === value);
  $: displayText = selectedOption?.label ?? (placeholder || $t('common.select'));

  // Update dropdown position when open
  $: if (isOpen && triggerRef && dropdownRef) {
    positionDropdown();
  }

  /** Kept clear of the window edge, so the list never sits flush against it. */
  const VIEWPORT_MARGIN = 8;
  /** Below this there is no point opening downwards at all. */
  const MIN_USABLE_HEIGHT = 120;

  function positionDropdown() {
    if (!triggerRef || !dropdownRef) return;
    const rect = triggerRef.getBoundingClientRect();
    dropdownRef.style.position = 'fixed';
    dropdownRef.style.left = `${rect.left}px`;
    dropdownRef.style.width = `${rect.width}px`;
    dropdownRef.style.zIndex = '10000';

    // Open upwards when there is not enough room below.
    //
    // It always opened downwards with no height limit, so a list with more
    // options than fit simply ran off the bottom of the window — and the panel
    // clips its contents, so what was off-screen could not be scrolled to
    // either. A select near the bottom of the window showed its first few
    // options and nothing else.
    const below = window.innerHeight - rect.bottom - VIEWPORT_MARGIN;
    const above = rect.top - VIEWPORT_MARGIN;
    const openUp = below < MIN_USABLE_HEIGHT && above > below;

    if (openUp) {
      dropdownRef.style.top = '';
      dropdownRef.style.bottom = `${window.innerHeight - rect.top}px`;
      dropdownRef.style.maxHeight = `${above}px`;
    } else {
      dropdownRef.style.bottom = '';
      dropdownRef.style.top = `${rect.bottom}px`;
      dropdownRef.style.maxHeight = `${below}px`;
    }
  }

  async function toggle() {
    isOpen = !isOpen;
    if (isOpen) {
      // A stale query would silently hide most of the list next time the
      // dropdown is opened.
      query = '';
      await tick();
      positionDropdown();
      searchRef?.focus();
    }
  }

  function select(optionValue: string) {
    value = optionValue;
    isOpen = false;
    dispatch('change', optionValue);
  }

  /**
   * Keyboard handling in the filter box.
   *
   * Enter takes the highlighted option, so filtering to one result and pressing
   * Enter is enough — without it the only way to choose was to leave the
   * keyboard and click. Arrows move the highlight, which is what makes Enter
   * useful for anything but the first match.
   */
  function handleSearchKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      isOpen = false;
      return;
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      highlighted = Math.min(highlighted + 1, filteredOptions.length - 1);
      return;
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault();
      highlighted = Math.max(highlighted - 1, 0);
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      const option = filteredOptions[highlighted];
      // Nothing to take when the filter matches nothing; Enter should not
      // close the dropdown on an empty list as if a choice had been made.
      if (option) select(option.value);
    }
  }

  function handleClickOutside(event: MouseEvent) {
    const target = event.target as Node;
    const clickedTrigger = triggerRef && triggerRef.contains(target);
    const clickedDropdown = dropdownRef && dropdownRef.contains(target);

    if (!clickedTrigger && !clickedDropdown) {
      isOpen = false;
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      isOpen = false;
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      toggle();
    }
  }

  // Portal action - moves element to body
  function portal(node: HTMLElement) {
    document.body.appendChild(node);

    return {
      destroy() {
        if (node.parentNode) {
          node.parentNode.removeChild(node);
        }
      }
    };
  }
</script>

<svelte:window on:click={handleClickOutside} />

<div class="custom-select" class:small class:open={isOpen}>
  <button
    type="button"
    class="select-trigger"
    bind:this={triggerRef}
    on:click={toggle}
    on:keydown={handleKeydown}
  >
    <span class="select-value">{displayText}</span>
    <svg class="select-arrow" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <polyline points="6 9 12 15 18 9"></polyline>
    </svg>
  </button>
</div>

{#if isOpen}
  <div class="select-dropdown" class:small bind:this={dropdownRef} use:portal>
    {#if searchable}
      <!-- svelte-ignore a11y-autofocus -->
      <input
        class="select-search"
        type="text"
        bind:this={searchRef}
        bind:value={query}
        placeholder={$t('common.filter')}
        on:click|stopPropagation
        on:keydown={handleSearchKeydown}
      />
    {/if}
    {#each filteredOptions as option, index}
      <button
        type="button"
        class="select-option"
        class:selected={option.value === value}
        class:highlighted={searchable && index === highlighted}
        on:mouseenter={() => (highlighted = index)}
        on:click={() => select(option.value)}
      >
        {option.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .custom-select {
    position: relative;
    display: inline-block;
  }

  .select-trigger {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 10px 12px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    color: white;
    font-size: 14px;
    cursor: pointer;
    min-width: 120px;
    transition: all 0.15s ease;
  }

  .small .select-trigger {
    padding: 6px 10px;
    font-size: 13px;
    color: #9ca3af;
    background: rgba(255, 255, 255, 0.05);
    border-radius: 6px;
  }

  .select-trigger:hover {
    border-color: rgba(255, 255, 255, 0.2);
  }

  .select-trigger:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  .open .select-trigger {
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  .select-value {
    flex: 1;
    text-align: left;
  }

  .select-arrow {
    transition: transform 0.15s ease;
    opacity: 0.6;
  }

  .open .select-arrow {
    transform: rotate(180deg);
  }

  /* Dropdown styles need :global because it's portaled to body */
  :global(.select-dropdown) {
    background: #1a1a2e;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.5);
    /* Scrolls rather than clips: a max-height is set when the dropdown is
       positioned, and with `hidden` anything past it was simply unreachable. */
    overflow-y: auto;
    animation: dropdownIn 0.15s ease-out;
  }

  @keyframes dropdownIn {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  /* Sticky, so it stays reachable while the list scrolls under it — a filter
     you have to scroll back up to reach is a filter people stop using. */
  :global(.select-dropdown .select-search) {
    position: sticky;
    top: 0;
    z-index: 1;
    display: block;
    width: calc(100% - 16px);
    margin: 8px;
    padding: 6px 9px;
    box-sizing: border-box;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 6px;
    color: inherit;
    font-size: 13px;
    font-family: inherit;
  }

  :global(.select-dropdown .select-search:focus) {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  :global(.select-dropdown .select-option) {
    display: block;
    width: 100%;
    padding: 10px 12px;
    background: transparent;
    border: none;
    color: #d1d5db;
    font-size: 14px;
    text-align: left;
    cursor: pointer;
    transition: all 0.1s ease;
  }

  :global(.select-dropdown.small .select-option) {
    padding: 8px 10px;
    font-size: 13px;
  }

  :global(.select-dropdown .select-option:hover) {
    background: rgba(var(--accent-rgb), 0.15);
    color: white;
  }

  /* Where Enter would land. Distinct from :hover so the keyboard and the mouse
     do not fight over which one is showing — mouseenter moves the highlight,
     so they always agree. */
  :global(.select-dropdown .select-option.highlighted) {
    background: rgba(255, 255, 255, 0.08);
  }

  :global(.select-dropdown .select-option.selected) {
    background: rgba(var(--accent-rgb), 0.2);
    color: white;
  }
</style>
