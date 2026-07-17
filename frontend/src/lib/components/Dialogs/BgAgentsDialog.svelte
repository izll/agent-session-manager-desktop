<script lang="ts">
  import { onDestroy } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { loadSessions, selectSession, selectWindow, sessions, groups } from '../../stores/sessions';
  import { focusTerminal } from '../../utils/focus';
  import Select from '../common/Select.svelte';
  import { t } from '../../i18n';

  export let show = false;

  interface BgAgent {
    id: string;
    sessionId: string;
    pid: number;
    cwd: string;
    name: string;
    status: string;
    startedAt: number;
  }

  let agents: BgAgent[] = [];
  let filter = '';
  let loading = false;
  let logsFor: string | null = null;
  let logsText = '';
  let error = '';
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  $: if (show) startPolling(); else stopPolling();

  function startPolling() {
    if (pollTimer) return;
    void refresh();
    pollTimer = setInterval(() => void refresh(), 5000);
  }

  function stopPolling() {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
    logsFor = null;
    logsText = '';
    error = '';
    attachFor = null;
    filter = '';
  }

  onDestroy(stopPolling);

  async function refresh() {
    loading = agents.length === 0;
    try {
      agents = ((await App.ListBackgroundAgents()) || []) as BgAgent[];
      error = '';
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  $: filteredAgents = filter.trim()
    ? agents.filter(a => {
        const q = filter.trim().toLowerCase();
        return [a.name, a.id, a.cwd, a.status].some(v => (v || '').toLowerCase().includes(q));
      })
    : agents;

  function close() {
    show = false;
  }

  function shortCwd(cwd: string): string {
    const parts = cwd.split('/');
    return parts.slice(-2).join('/') || cwd;
  }

  function uptime(startedAt: number): string {
    if (!startedAt) return '';
    const mins = Math.max(0, Math.round((Date.now() - startedAt) / 60000));
    if (mins < 60) return `${mins}m`;
    if (mins < 1440) return `${Math.floor(mins / 60)}h ${mins % 60}m`;
    return `${Math.floor(mins / 1440)}d`;
  }

  async function toggleLogs(agent: BgAgent) {
    attachFor = null;
    if (logsFor === agent.id) { logsFor = null; logsText = ''; return; }
    logsFor = agent.id;
    logsText = '…';
    try {
      logsText = await App.GetBackgroundAgentLogs(agent.id);
    } catch (e) {
      logsText = String(e);
    }
  }

  async function stopAgent(agent: BgAgent) {
    try {
      await App.StopBackgroundAgent(agent.id);
      await refresh();
    } catch (e) {
      error = String(e);
    }
  }

  // Attach chooser. Two targets:
  //  - TAB into an EXISTING running session: group filter cascades into the
  //    session picker (any running session, not just cwd matches — the cwd
  //    match is only used to pre-select a sensible default).
  //  - NEW session: optional group.
  let attachFor: BgAgent | null = null;
  let attachMode: 'tab' | 'new' = 'new';
  let attachGroupFilter = 'all'; // 'all' | '' (ungrouped) | groupId
  let attachSessionId = '';
  let attachGroupId = '';

  $: attachRunning = $sessions.filter(s => s.status === 'running');
  $: attachFiltered = attachGroupFilter === 'all'
    ? attachRunning
    : attachRunning.filter(s => (s.groupId || '') === attachGroupFilter);
  // Keep the selection valid when the group filter narrows the list.
  $: if (attachFor && attachMode === 'tab' && attachFiltered.length > 0 &&
         !attachFiltered.some(s => s.id === attachSessionId)) {
    attachSessionId = attachFiltered[0].id;
  }

  function openAttach(agent: BgAgent) {
    if (attachFor?.id === agent.id) { attachFor = null; return; }
    attachFor = agent;
    logsFor = null;
    const matches = $sessions.filter(s => s.path === agent.cwd);
    const runningMatch = matches.find(s => s.status === 'running');
    attachGroupFilter = 'all';
    if (runningMatch) {
      attachMode = 'tab';
      attachSessionId = runningMatch.id;
    } else {
      attachMode = 'new';
      attachSessionId = $sessions.find(s => s.status === 'running')?.id || '';
    }
    attachGroupId = matches.find(m => m.groupId)?.groupId || '';
  }

  async function confirmAttach() {
    if (!attachFor) return;
    const agent = attachFor;
    try {
      if (attachMode === 'tab' && attachSessionId) {
        const winIdx = await App.AttachBackgroundAgentAsTab(attachSessionId, agent.id, agent.name);
        await loadSessions();
        close();
        selectSession(attachSessionId);
        if (typeof winIdx === 'number' && winIdx >= 0) selectWindow(winIdx);
      } else {
        const sessionId = await App.AttachBackgroundAgent(agent.id, agent.cwd, agent.name, attachGroupId);
        await loadSessions();
        close();
        selectSession(sessionId);
      }
      attachFor = null;
      requestAnimationFrame(() => requestAnimationFrame(focusTerminal));
    } catch (e) {
      error = String(e);
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close();
  }
</script>

<svelte:window on:keydown={show ? handleKeydown : undefined} />

{#if show}
  <div class="dialog-overlay" on:click={close}>
    <div class="dialog-content" on:click|stopPropagation>
      <div class="dialog-header">
        <h2>{$t('bgAgents.title')}{#if agents.length > 0} <span class="agent-count">{agents.length}</span>{/if}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      {#if agents.length > 5}
        <div class="filter-box">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
          <input bind:value={filter} placeholder={$t('bgAgents.filterPlaceholder')} />
          {#if filter}<button class="clear-filter" on:click={() => filter = ''}>×</button>{/if}
        </div>
      {/if}

      {#if error}<div class="error-line">{error}</div>{/if}

      {#if loading}
        <div class="empty">{$t('bgAgents.loading')}</div>
      {:else if agents.length === 0}
        <div class="empty">
          {$t('bgAgents.empty')}
          <span class="hint">{$t('bgAgents.emptyHint')}</span>
        </div>
      {:else if filteredAgents.length === 0}
        <div class="empty">{$t('bgAgents.noMatches')}</div>
      {:else}
        <div class="agent-list">
          {#each filteredAgents as agent (agent.id)}
            <div class="agent-row">
              <span class="status-dot {agent.status}"></span>
              <div class="agent-info">
                <span class="agent-name">{agent.name || agent.id}</span>
                <span class="agent-meta">{agent.id} · {shortCwd(agent.cwd)} · {uptime(agent.startedAt)} · {agent.status}</span>
              </div>
              <div class="agent-actions">
                <button on:click={() => openAttach(agent)} title={$t('bgAgents.attachDesc')}>{$t('bgAgents.attach')}</button>
                <button on:click={() => toggleLogs(agent)}>{$t('bgAgents.logs')}</button>
                <button class="danger" on:click={() => stopAgent(agent)}>{$t('bgAgents.stop')}</button>
              </div>
            </div>
            {#if attachFor?.id === agent.id}
              <div class="attach-config">
                <div class="attach-grid">
                  <button type="button" class="attach-card" class:selected={attachMode === 'tab'}
                    disabled={attachRunning.length === 0}
                    on:click={() => attachMode = 'tab'}>
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <rect x="3" y="5" width="18" height="14" rx="2"/>
                      <path d="M3 9h18M9 5v4"/>
                    </svg>
                    <span class="attach-card-title">{$t('bgAgents.asTab')}</span>
                    <span class="attach-card-desc">{$t('bgAgents.asTabDesc')}</span>
                  </button>
                  <button type="button" class="attach-card" class:selected={attachMode === 'new'}
                    on:click={() => attachMode = 'new'}>
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <rect x="3" y="4" width="18" height="16" rx="2"/>
                      <path d="M12 9v6M9 12h6"/>
                    </svg>
                    <span class="attach-card-title">{$t('bgAgents.asNew')}</span>
                    <span class="attach-card-desc">{$t('bgAgents.asNewDesc')}</span>
                  </button>
                </div>

                <div class="attach-fields">
                  {#if attachMode === 'tab'}
                    <div class="attach-field">
                      <span class="attach-label">{$t('bgAgents.filterGroup')}</span>
                      <Select
                        value={attachGroupFilter}
                        options={[
                          { value: 'all', label: $t('bgAgents.allGroups') },
                          { value: '', label: $t('bgAgents.noGroup') },
                          ...$groups.map(g => ({ value: g.id, label: g.name }))
                        ]}
                        on:change={(e) => attachGroupFilter = e.detail}
                      />
                    </div>
                    <div class="attach-field">
                      <span class="attach-label">{$t('bgAgents.session')}</span>
                      {#if attachFiltered.length > 0}
                        <Select
                          value={attachSessionId}
                          options={attachFiltered.map(m => ({ value: m.id, label: m.name }))}
                          on:change={(e) => attachSessionId = e.detail}
                        />
                      {:else}
                        <span class="cascade-empty">{$t('bgAgents.noRunning')}</span>
                      {/if}
                    </div>
                  {:else}
                    <div class="attach-field">
                      <span class="attach-label">{$t('bgAgents.filterGroup')}</span>
                      <Select
                        value={attachGroupId}
                        options={[{ value: '', label: $t('bgAgents.noGroup') }, ...$groups.map(g => ({ value: g.id, label: g.name }))]}
                        on:change={(e) => attachGroupId = e.detail}
                      />
                    </div>
                  {/if}
                </div>

                <div class="attach-buttons">
                  <button class="cancel" on:click={() => attachFor = null}>{$t('bgAgents.cancel')}</button>
                  <button class="confirm" on:click={confirmAttach}
                    disabled={attachMode === 'tab' && attachFiltered.length === 0}>
                    {$t('bgAgents.attach')}
                  </button>
                </div>
              </div>
            {/if}
            {#if logsFor === agent.id}
              <pre class="agent-logs">{logsText}</pre>
            {/if}
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    /* the global .dialog-content caps max-width at 400px — this list needs room */
    width: min(760px, 92vw);
    max-width: min(760px, 92vw);
    max-height: 78vh;
    overflow-y: auto;
    background: linear-gradient(180deg, #1a1a2e 0%, #0f0f1a 100%);
    border: 1px solid rgba(139, 92, 246, 0.25);
    border-radius: 12px;
    padding: 18px;
  }
  .dialog-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
  .dialog-header h2 { margin: 0; color: #e4e4e7; font-size: 16px; }
  .close-btn { background: none; border: 0; color: #71717a; font-size: 20px; cursor: pointer; }
  .close-btn:hover { color: #e4e4e7; }
  .error-line { color: #fb7185; font-size: 11px; margin-bottom: 8px; }
  .agent-count {
    display: inline-block; margin-left: 6px; padding: 1px 8px; border-radius: 999px;
    background: rgba(139, 92, 246, 0.15); color: #a78bfa; font-size: 11px; vertical-align: 2px;
  }
  .filter-box {
    display: flex; align-items: center; gap: 8px; margin-bottom: 10px;
    padding: 0 10px; height: 34px; border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.08); background: rgba(0, 0, 0, 0.25); color: #52525b;
  }
  .filter-box:focus-within { border-color: rgba(139, 92, 246, 0.45); color: #a78bfa; }
  .filter-box input { flex: 1; min-width: 0; background: transparent; border: 0; outline: 0; color: #e4e4e7; font-size: 12px; }
  .filter-box input::placeholder { color: #52525b; }
  .clear-filter { border: 0; background: transparent; color: #71717a; cursor: pointer; font-size: 16px; line-height: 1; }
  .empty { color: #71717a; text-align: center; padding: 32px 0; font-size: 13px; }
  .empty .hint { display: block; margin-top: 6px; font-size: 11px; color: #52525b; }
  .agent-list { display: flex; flex-direction: column; gap: 4px; }
  .agent-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 10px; border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.05);
    background: rgba(0, 0, 0, 0.18);
  }
  .status-dot { width: 8px; height: 8px; border-radius: 50%; background: #71717a; flex-shrink: 0; }
  .status-dot.busy { background: #ffa500; box-shadow: 0 0 6px rgba(255, 165, 0, 0.5); }
  .status-dot.idle { background: #4ade80; }
  .agent-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .agent-name { color: #e4e4e7; font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .agent-meta { color: #71717a; font-size: 10px; font-family: monospace; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .agent-actions { display: flex; gap: 6px; flex-shrink: 0; }
  .agent-actions button {
    padding: 5px 10px; border-radius: 6px; font-size: 11px; cursor: pointer;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05); color: #d4d4d8;
  }
  .agent-actions button:hover { border-color: rgba(139, 92, 246, 0.5); color: #ddd6fe; }
  .agent-actions button.danger:hover { border-color: rgba(251, 113, 133, 0.6); color: #fb7185; }
  .attach-config {
    margin: 2px 0 6px;
    padding: 14px;
    border-radius: 10px;
    background: rgba(139, 92, 246, 0.05);
    border: 1px solid rgba(139, 92, 246, 0.18);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .attach-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .attach-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    padding: 12px 10px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 10px;
    cursor: pointer;
    transition: all 0.15s ease;
    color: #9ca3af;
    text-align: center;
  }
  .attach-card:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.15);
  }
  .attach-card.selected {
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(139, 92, 246, 0.5);
    color: #a78bfa;
  }
  .attach-card:disabled { opacity: 0.4; cursor: default; }
  .attach-card-title { font-size: 12px; font-weight: 650; color: #e4e4e7; }
  .attach-card.selected .attach-card-title { color: #ddd6fe; }
  .attach-card-desc { font-size: 10px; color: #71717a; line-height: 1.35; }
  .attach-fields { display: flex; gap: 14px; flex-wrap: wrap; }
  .attach-field { display: flex; flex-direction: column; gap: 5px; min-width: 180px; flex: 1; }
  .attach-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #9ca3af;
  }
  .cascade-empty { color: #71717a; font-size: 11px; font-style: italic; padding: 7px 0; }
  .attach-buttons { display: flex; gap: 8px; justify-content: flex-end; }
  .attach-buttons button {
    padding: 7px 16px;
    border-radius: 7px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .attach-buttons button.cancel {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.04);
    color: #a1a1aa;
  }
  .attach-buttons button.cancel:hover { color: #e4e4e7; border-color: rgba(255, 255, 255, 0.25); }
  .attach-buttons button.confirm {
    border: 1px solid #8b5cf6;
    background: linear-gradient(135deg, #7c3aed, #8b5cf6);
    color: white;
    box-shadow: 0 4px 14px rgba(124, 58, 237, 0.25);
  }
  .attach-buttons button.confirm:hover:not(:disabled) {
    background: linear-gradient(135deg, #8b5cf6, #a78bfa);
  }
  .attach-buttons button.confirm:disabled { opacity: 0.45; cursor: default; box-shadow: none; }
  .agent-logs {
    margin: 2px 0 6px; padding: 10px; border-radius: 8px;
    background: rgba(0, 0, 0, 0.4); border: 1px solid rgba(255, 255, 255, 0.05);
    color: #a1a1aa; font-size: 11px; line-height: 1.45;
    max-height: 260px; overflow: auto; white-space: pre-wrap; word-break: break-word;
  }
</style>
