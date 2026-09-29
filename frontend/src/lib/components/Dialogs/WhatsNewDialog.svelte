<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher, tick } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t, locale } from '../../i18n';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';
  import Select from '../common/Select.svelte';
  import InlineMarkdown from '../common/InlineMarkdown.svelte';
  import {
    buildPages, startPage, stepPage,
    type ChangelogEntry, type Page,
  } from '../../utils/whatsNew';

  export let show = false;
  /** Releases new since the last launch, newest first; empty when opened by hand. */
  export let fresh: string[] = [];
  /** The version updated from, when known. */
  export let since = '';
  /** The running version: where a dialog opened by hand starts. */
  export let current = '';

  const dispatch = createEventDispatcher<{ close: void }>();

  // The notes are the same for the life of the process: fetched once.
  let entries: ChangelogEntry[] = [];
  let loaded = false;
  let loadError = '';
  let pages: Page[] = [];
  let index = 0;
  let body: HTMLDivElement | undefined;

  // One block: opening is detected against the previous value in the same
  // pass (see UpdateDialog for why two blocks get the order wrong).
  let lastShow = false;
  $: {
    if (show && !lastShow) void open(fresh, since, current);
    lastShow = show;
  }

  async function open(freshVersions: string[], sinceVersion: string, currentVersion: string) {
    if (!loaded) {
      try {
        entries = (await App.GetChangelog()) || [];
        loaded = true;
        loadError = '';
      } catch (e) {
        loadError = String(e);
      }
    }
    let version = currentVersion;
    if (!version && !freshVersions.length) {
      try { version = await App.GetVersion(); } catch { /* open on the newest */ }
    }
    if (!show) return;
    pages = buildPages(entries, freshVersions, sinceVersion);
    index = startPage(pages, freshVersions[0] || version);
  }

  function go(delta: number) {
    const next = stepPage(index, delta, pages.length);
    if (next === index) return;
    index = next;
    void scrollTop();
  }

  function pick(value: string) {
    const at = pages.findIndex((p) => p.key === value);
    if (at >= 0 && at !== index) {
      index = at;
      void scrollTop();
    }
  }

  async function scrollTop() {
    await tick();
    body?.scrollTo({ top: 0 });
  }

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      close();
      return;
    }
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
    const target = e.target as HTMLElement | null;
    if (target?.closest('input, textarea, [contenteditable="true"]')) return;
    claimKeyForDialog();
    e.preventDefault();
    e.stopPropagation();
    // Older lies to the right, where its button is.
    go(e.key === 'ArrowRight' ? 1 : -1);
  }

  /** A section heading in the reader's language when it is one of the usual ones. */
  // The translator is passed in, not read here: markup that calls a helper
  // re-runs it only when an argument changes, so a language switch would
  // otherwise leave the old words on screen.
  type Translate = (key: string, params?: Record<string, string | number>) => string;

  function kindLabel(kind: string, title: string, tr: Translate): string {
    const key = `whatsNew.kind.${kind}`;
    const label = tr(key);
    return label === key ? title : label;
  }

  function optionLabel(page: Page, tr: Translate): string {
    if (page.combined) return tr('whatsNew.sinceOption', { version: page.since || '' });
    const entry = page.entries[0];
    return entry.date ? `${entry.version} · ${entry.date}` : entry.version;
  }

  $: page = pages[index] as Page | undefined;
  $: options = pages.map((p) => ({ value: p.key, label: optionLabel(p, $t) }));
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    tabindex="-1"
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    aria-labelledby="whats-new-title"
  >
    <div class="dialog-content whats-new">
      <div class="dialog-header">
        <h2 id="whats-new-title">{$t('whatsNew.title')}</h2>
        <DialogCloseButton on:click={close} />
      </div>

      <div class="dialog-body" bind:this={body} data-page={page?.key ?? ''}>
        {#if loadError}
          <p class="notice">{loadError}</p>
        {:else if !loaded}
          <p class="notice">{$t('common.loading')}</p>
        {:else if !page}
          <p class="notice">{$t('whatsNew.empty')}</p>
        {:else}
          {#if page.since}
            <p class="since">
              {page.combined
                ? $t('whatsNew.changesSince', { version: page.since, count: String(page.entries.length) })
                : $t('whatsNew.updatedFrom', { version: page.since })}
            </p>
          {/if}
          <!-- The notes are written in English; only the frame around them is
               translated, and a reader in another language is told so. -->
          {#if $locale !== 'en'}
            <p class="language-note">{$t('whatsNew.englishOnly')}</p>
          {/if}
          {#each page.entries as entry (entry.version)}
            <article class="release">
              <h3 class="release-title">
                <span class="release-version">{entry.version}</span>
                {#if entry.date}<span class="release-date">{entry.date}</span>{/if}
              </h3>
              {#each entry.intro || [] as paragraph}
                <p class="intro" lang="en">
                  <InlineMarkdown text={paragraph} />
                </p>
              {/each}
              {#each entry.sections || [] as section}
                {#if section.title}
                  <h4 class="section-title kind-{section.kind}">{kindLabel(section.kind, section.title, $t)}</h4>
                {/if}
                {#each section.intro || [] as paragraph}
                  <p class="intro" lang="en">
                    <InlineMarkdown text={paragraph} />
                  </p>
                {/each}
                {#if section.items?.length}
                  <ul lang="en">
                    {#each section.items as item}
                      <li>
                        <InlineMarkdown text={item} />
                      </li>
                    {/each}
                  </ul>
                {/if}
              {/each}
            </article>
          {/each}
        {/if}
      </div>

      <div class="dialog-footer">
        <div class="pager">
          <button
            class="btn-secondary page-btn newer"
            on:click={() => go(-1)}
            disabled={index <= 0}
            title="←"
          >‹ {$t('whatsNew.newer')}</button>
          {#if options.length}
            <div class="page-select">
              <Select value={page?.key ?? ''} {options} on:change={(e) => pick(e.detail)} />
            </div>
          {/if}
          <button
            class="btn-secondary page-btn older"
            on:click={() => go(1)}
            disabled={index >= pages.length - 1}
            title="→"
          >{$t('whatsNew.older')} ›</button>
        </div>
        <button class="btn-cancel" on:click={close}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .whats-new {
    width: min(720px, 94vw);
    max-width: min(720px, 94vw);
    height: min(80vh, 820px);
  }

  .dialog-body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .notice {
    color: #a1a1aa;
    font-size: 13px;
  }

  .since {
    margin: 0 0 16px;
    padding: 8px 12px;
    border-radius: 8px;
    background: rgba(var(--accent-rgb), 0.1);
    color: var(--accent-light);
    font-size: 13px;
  }

  .language-note {
    margin: 0 0 16px;
    font-size: 12px;
    color: #71717a;
  }

  .release + .release {
    margin-top: 28px;
    padding-top: 20px;
    border-top: 1px solid rgba(255, 255, 255, 0.06);
  }

  .release-title {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin: 0 0 8px;
    font-size: 16px;
    font-weight: 600;
    color: #f4f4f5;
  }

  .release-date {
    font-size: 12px;
    font-weight: 400;
    color: #71717a;
  }

  .section-title {
    margin: 14px 0 6px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--accent-light);
  }

  .section-title.kind-fixed {
    color: #86efac;
  }

  .section-title.kind-changed {
    color: #fcd34d;
  }

  .intro {
    margin: 0 0 8px;
    font-size: 13px;
    line-height: 1.55;
    color: #d4d4d8;
  }

  ul {
    margin: 0;
    padding-left: 20px;
  }

  li {
    margin: 0 0 8px;
    font-size: 13px;
    line-height: 1.55;
    color: #d4d4d8;
  }

  .dialog-footer {
    justify-content: space-between;
    align-items: center;
  }

  .pager {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .page-btn {
    white-space: nowrap;
  }

  .page-select {
    width: 200px;
    min-width: 0;
  }

  .page-select :global(.custom-select) {
    width: 100%;
  }
</style>
