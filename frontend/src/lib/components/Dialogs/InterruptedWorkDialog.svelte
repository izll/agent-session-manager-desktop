<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';
  import AgentIcon from '../common/AgentIcon.svelte';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';
  import { reopenInterruptedSessions } from '../../stores/sessions';
  import { describeBackendError } from '../../utils/backendError';
  import {
    splitChoice, summarize, tabsLine,
    type InterruptedSession, type ReopenResult,
  } from '../../utils/interruptedWork';
  import { t } from '../../i18n';

  // Offered once after a restart: the sessions that were running when the
  // machine (or tmux) went away, each with only the tabs that were running.
  export let show = false;
  export let sessions: InterruptedSession[] = [];
  /** Named when the user has more than one project, so the list has a place. */
  export let projectName = '';

  const dispatch = createEventDispatcher<{ done: { ok: number; failed: number } }>();

  type RowState = 'pending' | 'ok' | 'error';

  let selected = new Set<string>();
  let states: Record<string, RowState> = {};
  let errors: Record<string, string> = {};
  let busy = false;
  // Set once a run has finished with failures: the dialog stays to show them,
  // and the only thing left to do is to close it.
  let finished = false;
  let openedFor: InterruptedSession[] | null = null;

  // A fresh offer starts with everything ticked — the common answer is "all of
  // it" — and with no state left from an earlier one.
  $: if (show && sessions !== openedFor) {
    openedFor = sessions;
    selected = new Set(sessions.map((s) => s.id));
    states = {};
    errors = {};
    busy = false;
    finished = false;
  }

  // Per-session progress while the backend works through the list. The final
  // results are applied as well, so nothing depends on every event arriving.
  const stopProgress = EventsOn('interrupted:progress', (result: ReopenResult) => {
    if (show && busy) apply(result);
  });
  onDestroy(() => stopProgress?.());

  function apply(result: ReopenResult) {
    states = { ...states, [result.id]: result.ok ? 'ok' : 'error' };
    if (!result.ok) errors = { ...errors, [result.id]: describeBackendError(result.error || '') };
  }

  function toggle(id: string) {
    if (busy || finished) return;
    const next = new Set(selected);
    if (next.has(id)) next.delete(id); else next.add(id);
    selected = next;
  }

  async function reopenSelected() {
    if (busy) return;
    const { reopen, dismiss } = splitChoice(sessions, selected);
    busy = true;
    states = Object.fromEntries(reopen.map((id) => [id, 'pending' as RowState]));
    let results: ReopenResult[] = [];
    try {
      results = await reopenInterruptedSessions(reopen, dismiss);
    } catch (e) {
      results = reopen.map((id) => ({ id, ok: false, error: String(e) }));
    }
    results.forEach(apply);
    busy = false;
    const summary = summarize(results);
    if (summary.failed === 0) {
      show = false;
      dispatch('done', summary);
      return;
    }
    finished = true;
    dispatch('done', summary);
  }

  async function notNow() {
    if (busy) return;
    if (finished) {
      show = false;
      return;
    }
    const all = sessions.map((s) => s.id);
    show = false;
    try {
      await reopenInterruptedSessions([], all);
    } catch {
      // Not worth an error of its own: at worst the offer comes back next time.
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      notNow();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    data-dialog="interrupted-work"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('interrupted.title')}</h2>
        <DialogCloseButton on:click={notNow} disabled={busy} />
      </div>

      <div class="dialog-body">
        <p class="intro">
          {projectName ? $t('interrupted.introProject', { project: projectName }) : $t('interrupted.intro')}
        </p>

        <div class="session-list">
          {#each sessions as session (session.id)}
            {@const line = tabsLine(session)}
            <label class="session-row" class:selected={selected.has(session.id)} data-session-id={session.id}>
              <input
                type="checkbox"
                checked={selected.has(session.id)}
                disabled={busy || finished}
                on:change={() => toggle(session.id)}
              />
              <span class="agents">
                {#each session.agents as agent}
                  <AgentIcon {agent} size="sm" />
                {/each}
              </span>
              <span class="details">
                <span class="name" style={session.color ? `color: ${session.color}` : ''}>{session.name}</span>
                {#if line}
                  <span class="tabs" data-tabs>{$t(line.key, line.values)}</span>
                {/if}
                {#if errors[session.id]}
                  <span class="row-error">{errors[session.id]}</span>
                {/if}
              </span>
              <span class="state" data-state={states[session.id] || ''}>
                {#if states[session.id] === 'pending'}
                  <span class="spinner small" aria-label={$t('interrupted.reopening')}></span>
                {:else if states[session.id] === 'ok'}
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" aria-label={$t('interrupted.reopened')}>
                    <polyline points="20 6 9 17 4 12"/>
                  </svg>
                {:else if states[session.id] === 'error'}
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" aria-label={$t('interrupted.failed')}>
                    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                {/if}
              </span>
            </label>
          {/each}
        </div>

        <p class="hint">{$t('interrupted.settingHint')}</p>
      </div>

      <div class="dialog-footer">
        <button class="btn-cancel" on:click={notNow} disabled={busy}>
          {finished ? $t('common.close') : $t('interrupted.notNow')}
        </button>
        {#if !finished}
          <button class="btn btn-primary" on:click={reopenSelected} disabled={busy || selected.size === 0}>
            {#if busy}
              <span class="spinner small"></span>
              {$t('interrupted.reopening')}
            {:else}
              {$t('interrupted.reopenSelected', { count: selected.size })}
            {/if}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    width: 520px;
    max-width: 92vw;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
  }

  .dialog-body {
    overflow-y: auto;
  }

  .intro {
    margin: 0 0 16px;
    font-size: 14px;
    line-height: 1.5;
    color: #9ca3af;
  }

  .session-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .session-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 8px;
    cursor: pointer;
    transition: background 0.15s ease, border-color 0.15s ease;
  }

  .session-row:hover {
    background: rgba(255, 255, 255, 0.05);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .session-row.selected {
    background: rgba(var(--accent-rgb), 0.1);
    border-color: rgba(var(--accent-rgb), 0.3);
  }

  .session-row input[type="checkbox"] {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    accent-color: var(--accent);
  }

  .agents {
    display: inline-flex;
    gap: 2px;
    flex-shrink: 0;
  }

  .details {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }

  .name {
    font-size: 13px;
    font-weight: 500;
    color: #e4e4e7;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .tabs {
    font-size: 12px;
    color: #6b7280;
  }

  .row-error {
    font-size: 12px;
    color: #f87171;
    overflow-wrap: anywhere;
  }

  .state {
    display: inline-flex;
    width: 16px;
    flex-shrink: 0;
  }

  .state[data-state="ok"] {
    color: #4ade80;
  }

  .state[data-state="error"] {
    color: #f87171;
  }

  .hint {
    margin: 16px 0 0;
    font-size: 12px;
    color: #6b7280;
  }

  .btn-primary {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .spinner {
    display: inline-block;
    width: 16px;
    height: 16px;
    border: 2px solid rgba(var(--accent-rgb), 0.2);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  /* An accent spinner on the accent-coloured button would all but vanish. */
  .btn .spinner {
    border-color: color-mix(in srgb, currentColor 30%, transparent);
    border-top-color: currentColor;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
