<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy, tick } from 'svelte';

  export let value: string = '';
  export let options: { value: string; label: string }[] = [];
  export let placeholder: string = 'Select...';
  export let small: boolean = false;

  const dispatch = createEventDispatcher<{ change: string }>();

  let isOpen = false;
  let triggerRef: HTMLButtonElement;
  let dropdownRef: HTMLDivElement;

  $: selectedOption = options.find(o => o.value === value);
  $: displayText = selectedOption?.label ?? placeholder;

  // Update dropdown position when open
  $: if (isOpen && triggerRef && dropdownRef) {
    positionDropdown();
  }

  function positionDropdown() {
    if (!triggerRef || !dropdownRef) return;
    const rect = triggerRef.getBoundingClientRect();
    dropdownRef.style.position = 'fixed';
    dropdownRef.style.top = `${rect.bottom}px`;
    dropdownRef.style.left = `${rect.left}px`;
    dropdownRef.style.width = `${rect.width}px`;
    dropdownRef.style.zIndex = '10000';
  }

  async function toggle() {
    isOpen = !isOpen;
    if (isOpen) {
      await tick();
      positionDropdown();
    }
  }

  function select(optionValue: string) {
    value = optionValue;
    isOpen = false;
    dispatch('change', optionValue);
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
    {#each options as option}
      <button
        type="button"
        class="select-option"
        class:selected={option.value === value}
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
    font-size: 12px;
    color: #9ca3af;
    background: rgba(255, 255, 255, 0.05);
    border-radius: 6px;
  }

  .select-trigger:hover {
    border-color: rgba(255, 255, 255, 0.2);
  }

  .select-trigger:focus {
    outline: none;
    border-color: rgba(139, 92, 246, 0.5);
  }

  .open .select-trigger {
    border-color: rgba(139, 92, 246, 0.5);
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
    overflow: hidden;
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
    font-size: 12px;
  }

  :global(.select-dropdown .select-option:hover) {
    background: rgba(139, 92, 246, 0.15);
    color: white;
  }

  :global(.select-dropdown .select-option.selected) {
    background: rgba(139, 92, 246, 0.2);
    color: white;
  }
</style>
