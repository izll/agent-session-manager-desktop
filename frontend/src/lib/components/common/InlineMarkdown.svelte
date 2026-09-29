<script lang="ts">
  // A line of release notes: **bold**, `code` and [links](url), drawn as
  // elements with text content, never as raw HTML: whatever the text holds is
  // shown as characters, so markup in it cannot run.
  import { BrowserOpenURL } from '../../../../wailsjs/runtime/runtime';
  import { parseInline, type Inline } from '../../utils/whatsNew';
  import InlineMarkdown from './InlineMarkdown.svelte';

  export let text = '';
  /** Already-parsed segments: how bold and links pass their contents down. */
  export let segments: Inline[] | null = null;

  $: list = segments ?? parseInline(text);
</script>

{#each list as seg}{#if seg.type === 'code'}<code>{seg.text}</code>{:else if seg.type === 'strong'}<strong><InlineMarkdown segments={seg.children} /></strong>{:else if seg.type === 'link'}<a href={seg.href} on:click|preventDefault={() => BrowserOpenURL(seg.href)}><InlineMarkdown segments={seg.children} /></a>{:else}{seg.text}{/if}{/each}

<style>
  code {
    padding: 1px 5px;
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.08);
    font-family: var(--font-mono, monospace);
    font-size: 0.92em;
  }

  strong {
    color: #f4f4f5;
    font-weight: 600;
  }

  a {
    color: var(--accent-light);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
</style>
