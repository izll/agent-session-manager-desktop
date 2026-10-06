<script lang="ts">
  import { tick } from 'svelte';
  import { BrowserOpenURL } from '../../../../wailsjs/runtime/runtime';
  import { renderMarkdown } from '../../utils/markdownRender';
  import { classifyLink, headingSlugs, resolveRelativePath } from '../../utils/markdownLinks';
  import { markMatches, clearMarks } from '../../utils/domFind';
  import DiffFindBar from './DiffFindBar.svelte';

  /** The file's text. */
  export let source = '';
  /** The file's path relative to the browsed root; links resolve against it. */
  export let filePath = '';
  /** Loads an image of the repository, by root-relative path, as a data URL. */
  export let loadImage: (path: string) => Promise<string>;
  /** Opens another file of the repository in the Files view. */
  export let openFile: (path: string) => void;

  let body: HTMLElement | null = null;
  let renderGeneration = 0;

  // Sanitised in renderMarkdown; nothing reaches {@html} without it.
  $: html = renderMarkdown(source);
  $: if (body) void afterRender(html, filePath);

  // --- Find -----------------------------------------------------------------

  let showFind = false;
  let findQuery = '';
  let hits: HTMLElement[] = [];
  let hitAt = -1;
  let findBar: { focus(): Promise<void> } | undefined;

  /** Opens the find bar, starting from a selection in the page if there is one. */
  export async function openFind() {
    const selected = window.getSelection();
    const text = selected?.toString() || '';
    if (text && !text.includes('\n') && body?.contains(selected!.anchorNode)) findQuery = text;
    showFind = true;
    await tick();
    runFind();
    await findBar?.focus();
  }

  function runFind() {
    if (!body) return;
    hits = markMatches(body, findQuery);
    hitAt = hits.length ? 0 : -1;
    showHit();
  }

  function stepFind(direction: 1 | -1) {
    if (!hits.length) return;
    hitAt = (hitAt + direction + hits.length) % hits.length;
    showHit();
  }

  function showHit() {
    hits.forEach((hit, i) => hit.classList.toggle('current', i === hitAt));
    hits[hitAt]?.scrollIntoView({ block: 'center' });
  }

  function closeFind() {
    showFind = false;
    if (body) clearMarks(body);
    hits = [];
    hitAt = -1;
  }

  /** Fills in the repository's images once the HTML is in the page. */
  async function afterRender(_html: string, fromFile: string) {
    const generation = ++renderGeneration;
    await tick();
    if (!body || generation !== renderGeneration) return;
    // A new page has none of the old marks; an open search is run again on it.
    if (showFind) runFind();
    for (const img of Array.from(body.querySelectorAll<HTMLImageElement>('img[data-md-src]'))) {
      const path = resolveRelativePath(fromFile, img.dataset.mdSrc || '');
      if (!path) {
        img.classList.add('broken');
        continue;
      }
      loadImage(path).then(
        (url) => { if (generation === renderGeneration) img.src = url; },
        () => { if (generation === renderGeneration) img.classList.add('broken'); },
      );
    }
  }

  function scrollToHeading(slug: string) {
    if (!body) return;
    const headings = Array.from(body.querySelectorAll<HTMLElement>('h1, h2, h3, h4, h5, h6'));
    const slugs = headingSlugs(headings.map((h) => h.textContent || ''));
    const wanted = slug.toLowerCase();
    const index = slugs.indexOf(wanted);
    if (index >= 0) headings[index].scrollIntoView({ block: 'start' });
  }

  // Every link is handled here and none by the webview: left to itself it
  // would navigate the whole app away to the target.
  function handleClick(event: MouseEvent) {
    const anchor = (event.target as HTMLElement | null)?.closest('a');
    if (!anchor || !body?.contains(anchor)) return;
    event.preventDefault();
    const link = classifyLink(anchor.getAttribute('href') || '', filePath);
    if (link.kind === 'external') {
      BrowserOpenURL(link.url);
    } else if (link.kind === 'anchor') {
      scrollToHeading(link.slug);
    } else if (link.kind === 'file') {
      if (link.path === filePath) {
        if (link.slug) scrollToHeading(link.slug);
      } else {
        openFile(link.path);
      }
    }
  }
</script>

<!-- The click is delegated from the links inside, which are focusable and
     answer Enter on their own; the article itself is not a control. -->
<div class="markdown-view" data-markdown-view>
  {#if showFind}
    <!-- The diff's find bar: the same keys, counter and look everywhere. -->
    <DiffFindBar
      bind:this={findBar}
      bind:query={findQuery}
      hitCount={hits.length}
      {hitAt}
      isolateKeys
      on:search={runFind}
      on:step={(e) => stepFind(e.detail)}
      on:close={closeFind}
    />
  {/if}
  <div class="markdown-scroll">
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions a11y_click_events_have_key_events -->
    <article class="markdown-body" bind:this={body} on:click={handleClick}>
      {@html html}
    </article>
  </div>
</div>

<style>
  /* A flex child of the file pane, as the editor it stands in for is. */
  .markdown-view {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .markdown-scroll {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow: auto;
  }

  .markdown-body {
    max-width: 900px;
    margin: 0 auto;
    padding: 20px 28px 48px;
    /* Prose in the interface's font: the pane around it is monospaced for
       code, and a document set in it reads like a source file. */
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
    font-size: 14px;
    line-height: 1.65;
    color: #d4d4d8;
    overflow-wrap: break-word;
    user-select: text;
  }

  .markdown-body :global(> :first-child) { margin-top: 0; }
  .markdown-body :global(h1),
  .markdown-body :global(h2),
  .markdown-body :global(h3),
  .markdown-body :global(h4),
  .markdown-body :global(h5),
  .markdown-body :global(h6) {
    margin: 1.4em 0 0.6em;
    line-height: 1.3;
    color: #f4f4f5;
    font-weight: 600;
  }
  .markdown-body :global(h1) { font-size: 1.8em; padding-bottom: 0.3em; border-bottom: 1px solid rgba(255, 255, 255, 0.1); }
  .markdown-body :global(h2) { font-size: 1.45em; padding-bottom: 0.3em; border-bottom: 1px solid rgba(255, 255, 255, 0.08); }
  .markdown-body :global(h3) { font-size: 1.2em; }
  .markdown-body :global(h4) { font-size: 1.05em; }
  .markdown-body :global(h5),
  .markdown-body :global(h6) { font-size: 0.95em; color: #a1a1aa; }

  .markdown-body :global(p),
  .markdown-body :global(ul),
  .markdown-body :global(ol),
  .markdown-body :global(blockquote),
  .markdown-body :global(pre),
  .markdown-body :global(table),
  .markdown-body :global(details) { margin: 0 0 1em; }

  .markdown-body :global(ul),
  .markdown-body :global(ol) { padding-left: 1.8em; }
  .markdown-body :global(li + li) { margin-top: 0.25em; }
  .markdown-body :global(li > ul),
  .markdown-body :global(li > ol) { margin: 0.25em 0 0; }
  .markdown-body :global(li:has(> input[type='checkbox'])) { list-style: none; margin-left: -1.4em; }
  .markdown-body :global(input[type='checkbox']) { margin-right: 0.45em; vertical-align: -1px; accent-color: var(--accent); }

  .markdown-body :global(a) {
    color: var(--accent-light, #93c5fd);
    text-decoration: none;
    cursor: pointer;
  }
  .markdown-body :global(a:hover) { text-decoration: underline; }

  .markdown-body :global(code) {
    font-family: var(--font-mono, 'JetBrains Mono', 'Fira Code', monospace);
    font-size: 0.88em;
    padding: 0.15em 0.4em;
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.08);
  }
  .markdown-body :global(pre) {
    padding: 12px 14px;
    border-radius: 7px;
    overflow: auto;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.07);
    line-height: 1.5;
  }
  .markdown-body :global(pre code) {
    padding: 0;
    background: none;
    font-size: 0.86em;
    white-space: pre;
  }

  .markdown-body :global(blockquote) {
    padding: 0.1em 1em;
    color: #a1a1aa;
    border-left: 3px solid rgba(var(--accent-rgb), 0.5);
  }
  .markdown-body :global(blockquote > :last-child) { margin-bottom: 0; }

  .markdown-body :global(hr) {
    height: 1px;
    margin: 1.6em 0;
    border: 0;
    background: rgba(255, 255, 255, 0.12);
  }

  .markdown-body :global(table) {
    display: block;
    width: max-content;
    max-width: 100%;
    overflow: auto;
    border-collapse: collapse;
  }
  .markdown-body :global(th),
  .markdown-body :global(td) {
    padding: 6px 12px;
    border: 1px solid rgba(255, 255, 255, 0.12);
  }
  .markdown-body :global(th) { font-weight: 600; color: #f4f4f5; background: rgba(255, 255, 255, 0.04); }
  .markdown-body :global(tr:nth-child(2n) td) { background: rgba(255, 255, 255, 0.02); }

  .markdown-body :global(img) {
    max-width: 100%;
    border-radius: 3px;
  }
  .markdown-body :global(img.broken) {
    display: inline-block;
    min-width: 1em;
    min-height: 1em;
    outline: 1px dashed rgba(255, 255, 255, 0.25);
  }

  .markdown-body :global(summary) { cursor: pointer; }
  .markdown-body :global(mark.find-hit) {
    color: inherit;
    border-radius: 2px;
    background: rgba(250, 204, 21, 0.28);
    box-shadow: 0 0 0 1px rgba(250, 204, 21, 0.45);
  }
  .markdown-body :global(mark.find-hit.current) { background: rgba(var(--accent-rgb), 0.55); }
  .markdown-body :global(kbd) {
    font-family: var(--font-mono, 'JetBrains Mono', 'Fira Code', monospace);
    font-size: 0.85em;
    padding: 0.1em 0.4em;
    border-radius: 4px;
    border: 1px solid rgba(255, 255, 255, 0.2);
    border-bottom-width: 2px;
  }
</style>
