<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import AgentIcon from '../common/AgentIcon.svelte';
  import { t } from '../../i18n';
  import { sessions } from '../../stores/sessions';

  export let projectId = '';
  export let onOpenSession: (sessionId: string) => void;

  interface StatsSummary {
    observedMs: number;
    busyMs: number;
    waitingMs: number;
    idleMs: number;
    waitingEvents: number;
    busyPercent: number;
  }

  interface StatsDay {
    date: string;
    busyMs: number;
    waitingMs: number;
    idleMs: number;
  }

  interface StatsAgent {
    agent: string;
    observedMs: number;
    busyMs: number;
    waitingMs: number;
    idleMs: number;
    waitingEvents: number;
    sharePercent: number;
  }

  interface StatsSession {
    sessionId: string;
    sessionName: string;
    agents: string;
    observedMs: number;
    busyMs: number;
    waitingMs: number;
    idleMs: number;
    waitingEvents: number;
  }

  interface ProjectStats {
    days: number;
    recordingFrom: string;
    updatedAt: string;
    summary: StatsSummary;
    series: StatsDay[];
    agents: StatsAgent[];
    sessions: StatsSession[];
  }

  const emptySummary: StatsSummary = {
    observedMs: 0, busyMs: 0, waitingMs: 0, idleMs: 0,
    waitingEvents: 0, busyPercent: 0,
  };

  let days = 7;
  let data: ProjectStats | null = null;
  let loading = true;
  let error = '';
  let mounted = false;
  let loadedProjectId = '';
  let loadedDays = 0;
  let loadGeneration = 0;

  $: summary = data?.summary || emptySummary;
  $: currentSessionIds = new Set($sessions.map(session => session.id));
  $: maxDayMs = Math.max(1, ...(data?.series || []).map(day => day.busyMs + day.waitingMs + day.idleMs));
  $: maxSessionBusyMs = Math.max(1, ...(data?.sessions || []).map(session => session.busyMs));

  $: if (mounted && (projectId !== loadedProjectId || days !== loadedDays)) {
    loadedProjectId = projectId;
    loadedDays = days;
    void loadStatistics(projectId, days);
  }

  onMount(() => {
    mounted = true;
    loadedProjectId = projectId;
    loadedDays = days;
    void loadStatistics(projectId, days);
  });

  onDestroy(() => {
    mounted = false;
    loadGeneration++;
  });

  async function loadStatistics(requestedProjectId: string, requestedDays: number) {
    const generation = ++loadGeneration;
    loading = true;
    error = '';
    data = null;
    try {
      const result = await App.GetProjectActivityStatistics(requestedProjectId, requestedDays);
      if (!mounted || generation !== loadGeneration || requestedProjectId !== projectId || requestedDays !== days) return;
      data = result as ProjectStats;
    } catch (e) {
      if (!mounted || generation !== loadGeneration || requestedProjectId !== projectId || requestedDays !== days) return;
      error = String(e);
    } finally {
      if (mounted && generation === loadGeneration) loading = false;
    }
  }

  function formatDuration(ms: number): string {
    if (!ms || ms < 60_000) return ms > 0 ? '<1m' : '0m';
    const totalMinutes = Math.round(ms / 60_000);
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    if (hours === 0) return `${minutes}m`;
    return minutes ? `${hours}h ${minutes}m` : `${hours}h`;
  }

  function formatDate(date: string, compact = false): string {
    const value = new Date(`${date}T12:00:00`);
    if (Number.isNaN(value.getTime())) return date;
    return value.toLocaleDateString(undefined, compact
      ? { month: 'numeric', day: 'numeric' }
      : { month: 'short', day: 'numeric' });
  }

  function formatTimestamp(value: string): string {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return date.toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit',
    });
  }

  function barHeight(ms: number): number {
    return Math.max(0, Math.min(100, (ms / maxDayMs) * 100));
  }

  function shouldLabelDay(index: number, length: number): boolean {
    if (length <= 7) return true;
    if (length <= 30) return index % 5 === 0 || index === length - 1;
    return index % 15 === 0 || index === length - 1;
  }

  function firstAgent(agents: string): string {
    return agents.split(',')[0]?.trim() || 'custom';
  }
</script>

<section class="statistics" aria-busy={loading}>
  <div class="statistics-toolbar">
    <div class="range-selector" aria-label={$t('statistics.range')}>
      <button aria-pressed={days === 7} class:active={days === 7} on:click={() => days = 7}>{$t('statistics.days7')}</button>
      <button aria-pressed={days === 30} class:active={days === 30} on:click={() => days = 30}>{$t('statistics.days30')}</button>
      <button aria-pressed={days === 90} class:active={days === 90} on:click={() => days = 90}>{$t('statistics.days90')}</button>
    </div>
    <button class="refresh-button" class:spinning={loading} disabled={loading} on:click={() => loadStatistics(projectId, days)}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M23 4v6h-6M1 20v-6h6"/>
        <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
      </svg>
      {$t('statistics.refresh')}
    </button>
  </div>

  {#if error}
    <div class="error-banner" role="alert">{$t('statistics.loadError')} <span title={error}>{error}</span></div>
  {/if}

  {#if data}
  <div class="metric-grid">
    <article class="metric-card observed">
      <span>{$t('statistics.observedTime')}</span>
      <strong>{formatDuration(summary.observedMs)}</strong>
      <small>{$t('statistics.coverageHint')}</small>
    </article>
    <article class="metric-card busy">
      <span>{$t('statistics.busyTime')}</span>
      <strong>{formatDuration(summary.busyMs)}</strong>
      <small>{$t('statistics.activeRate', { percent: Math.round(summary.busyPercent) })}</small>
    </article>
    <article class="metric-card waiting">
      <span>{$t('statistics.waitingTime')}</span>
      <strong>{formatDuration(summary.waitingMs)}</strong>
      <small>{$t('statistics.waitingEvents', { count: summary.waitingEvents })}</small>
    </article>
    <article class="metric-card idle">
      <span>{$t('statistics.idleTime')}</span>
      <strong>{formatDuration(summary.idleMs)}</strong>
      <small>{$t('statistics.idleHint')}</small>
    </article>
  </div>
  {/if}

  {#if loading && !data}
    <div class="loading-grid">
      <div></div><div></div><div></div>
    </div>
  {:else if error}
    <div class="retry-hint">{$t('statistics.retryHint')}</div>
  {:else if summary.observedMs === 0}
    <div class="empty-state">
      <div class="empty-icon">
        <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M3 3v18h18"/><path d="m7 16 4-5 3 3 4-7"/>
        </svg>
      </div>
      <h2>{$t('statistics.noData')}</h2>
      <p>{$t('statistics.noDataHint')}</p>
    </div>
  {:else}
    <div class="chart-grid">
      <article class="chart-card timeline-card">
        <div class="card-heading">
          <div>
            <h2>{$t('statistics.timeline')}</h2>
            <p>{$t('statistics.timelineHint')}</p>
          </div>
          <div class="legend">
            <span><i class="busy"></i>{$t('statistics.busy')}</span>
            <span><i class="waiting"></i>{$t('statistics.waiting')}</span>
            <span><i class="idle"></i>{$t('statistics.idle')}</span>
          </div>
        </div>
        <div class="timeline-scroll" role="img" aria-label={$t('statistics.timelineAria')}>
          <div class="timeline" class:wide={days > 30}>
            {#each data?.series || [] as day, index (day.date)}
              {@const total = day.busyMs + day.waitingMs + day.idleMs}
              <div class="day" title={`${formatDate(day.date)} · ${formatDuration(total)}`}>
                <div class="bar-space">
                  <div class="stack" style={`height:${barHeight(total)}%`}>
                    {#if total > 0}
                      <div class="segment busy" style={`height:${day.busyMs / total * 100}%`} title={`${$t('statistics.busy')}: ${formatDuration(day.busyMs)}`}></div>
                      <div class="segment waiting" style={`height:${day.waitingMs / total * 100}%`} title={`${$t('statistics.waiting')}: ${formatDuration(day.waitingMs)}`}></div>
                      <div class="segment idle" style={`height:${day.idleMs / total * 100}%`} title={`${$t('statistics.idle')}: ${formatDuration(day.idleMs)}`}></div>
                    {/if}
                  </div>
                </div>
                <span>{shouldLabelDay(index, data?.series.length || 0) ? formatDate(day.date, true) : ''}</span>
              </div>
            {/each}
          </div>
        </div>
      </article>

      <article class="chart-card agent-card">
        <div class="card-heading">
          <div>
            <h2>{$t('statistics.agentBreakdown')}</h2>
            <p>{$t('statistics.agentBreakdownHint')}</p>
          </div>
        </div>
        <div class="agent-list">
          {#each data?.agents || [] as agent (agent.agent)}
            <div class="agent-row">
              <div class="agent-label">
                <AgentIcon agent={agent.agent} size="sm" />
                <span>{agent.agent}</span>
                <strong>{formatDuration(agent.busyMs)}</strong>
              </div>
              <div class="agent-track">
                <div style={`width:${agent.sharePercent}%`}></div>
              </div>
              <small>{$t('statistics.agentDetail', {
                share: Math.round(agent.sharePercent),
                waiting: formatDuration(agent.waitingMs),
              })}</small>
            </div>
          {/each}
        </div>
      </article>
    </div>

    <article class="chart-card session-card">
      <div class="card-heading">
        <div>
          <h2>{$t('statistics.topSessions')}</h2>
          <p>{$t('statistics.topSessionsHint')}</p>
        </div>
      </div>
      <div class="session-table">
        <div class="session-table-head">
          <span>{$t('statistics.session')}</span>
          <span>{$t('statistics.busy')}</span>
          <span>{$t('statistics.waiting')}</span>
          <span>{$t('statistics.observed')}</span>
        </div>
        {#each data?.sessions || [] as session, index (session.sessionId)}
          <button
            class="session-row"
            class:historical={!currentSessionIds.has(session.sessionId)}
            disabled={!currentSessionIds.has(session.sessionId)}
            title={!currentSessionIds.has(session.sessionId) ? $t('statistics.deletedSession') : ''}
            on:click={() => onOpenSession(session.sessionId)}
          >
            <span class="rank">{index + 1}</span>
            <span class="session-name">
              <AgentIcon agent={firstAgent(session.agents)} size="xs" />
              <span>
                <strong>{session.sessionName || session.sessionId}</strong>
                <small>{session.agents}{#if !currentSessionIds.has(session.sessionId)} · {$t('statistics.deletedSession')}{/if}</small>
              </span>
            </span>
            <span class="time-cell busy-text">{formatDuration(session.busyMs)}</span>
            <span class="time-cell waiting-text">{formatDuration(session.waitingMs)}</span>
            <span class="time-cell">{formatDuration(session.observedMs)}</span>
            <span class="session-progress"><i style={`width:${session.busyMs / maxSessionBusyMs * 100}%`}></i></span>
          </button>
        {/each}
      </div>
    </article>
  {/if}

  <footer class="statistics-note">
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>
    </svg>
    <span>
      {$t('statistics.disclaimer')}
      {#if data?.recordingFrom}
        {$t('statistics.recordingFrom', { time: formatTimestamp(data.recordingFrom) })}
      {/if}
    </span>
  </footer>
</section>

<style>
  .statistics { min-width:0; }
  button { font:inherit; }
  .statistics-toolbar { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-bottom:13px; }
  .range-selector { display:flex; gap:3px; padding:3px; border:1px solid rgba(255,255,255,.065); border-radius:8px; background:rgba(8,8,14,.6); }
  .range-selector button { padding:6px 11px; border:0; border-radius:5px; color:#71717a; background:transparent; cursor:pointer; font-size:10px; font-weight:650; }
  .range-selector button.active { color:#ede9fe; background:rgba(139,92,246,.17); }
  .refresh-button { display:flex; align-items:center; gap:6px; padding:7px 10px; border:1px solid rgba(255,255,255,.075); border-radius:7px; color:#a1a1aa; background:rgba(255,255,255,.035); cursor:pointer; font-size:10px; }
  .refresh-button:hover { color:#e4e4e7; border-color:rgba(139,92,246,.3); }
  .refresh-button:disabled { opacity:.55; cursor:default; }
  .spinning svg { animation:spin 1s linear infinite; }
  @keyframes spin { to { transform:rotate(360deg); } }
  .error-banner { margin-bottom:12px; padding:9px 11px; border:1px solid rgba(244,63,94,.2); border-radius:8px; color:#fb7185; background:rgba(244,63,94,.06); font-size:10px; }
  .error-banner span { color:#9f5968; }
  .retry-hint { min-height:180px; display:grid; place-items:center; color:#71717a; font-size:10px; }

  .metric-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(170px,1fr)); gap:10px; margin-bottom:13px; }
  .metric-card { position:relative; min-width:0; padding:15px 16px; border:1px solid rgba(255,255,255,.065); border-radius:11px; background:linear-gradient(145deg,rgba(22,22,35,.9),rgba(13,13,21,.94)); overflow:hidden; }
  .metric-card::before { content:""; position:absolute; inset:0 auto 0 0; width:2px; background:#8b5cf6; }
  .metric-card.busy::before { background:#f59e0b; }.metric-card.waiting::before { background:#22d3ee; }.metric-card.idle::before { background:#64748b; }
  .metric-card span { display:block; color:#71717a; font-size:10px; text-transform:uppercase; letter-spacing:.07em; }
  .metric-card strong { display:block; margin:7px 0 4px; color:#f4f4f5; font-size:23px; line-height:1; letter-spacing:-.03em; }
  .metric-card small { color:#52525b; font-size:9px; }
  .metric-card.busy strong { color:#fbbf24; }.metric-card.waiting strong { color:#67e8f9; }

  .chart-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(100%,360px),1fr)); gap:13px; margin-bottom:13px; }
  .chart-card { min-width:0; padding:16px; border:1px solid rgba(255,255,255,.065); border-radius:11px; background:rgba(18,18,29,.82); }
  .card-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:15px; }
  .card-heading h2 { margin:0; color:#e4e4e7; font-size:13px; }
  .card-heading p { margin:4px 0 0; color:#52525b; font-size:9px; }
  .legend { display:flex; align-items:center; gap:10px; color:#71717a; font-size:9px; }
  .legend span { display:flex; align-items:center; gap:4px; }.legend i { width:6px; height:6px; border-radius:2px; }
  .legend i.busy,.segment.busy { background:#f59e0b; }.legend i.waiting,.segment.waiting { background:#22d3ee; }.legend i.idle,.segment.idle { background:#3f3f52; }

  .timeline-scroll { min-width:0; overflow-x:auto; padding-bottom:3px; }
  .timeline { min-width:420px; height:190px; display:flex; align-items:stretch; gap:5px; padding-top:5px; border-bottom:1px solid rgba(255,255,255,.06); background:repeating-linear-gradient(to bottom,rgba(255,255,255,.035) 0,rgba(255,255,255,.035) 1px,transparent 1px,transparent 42px); }
  .timeline.wide { min-width:720px; gap:2px; }
  .day { flex:1; min-width:5px; display:flex; flex-direction:column; justify-content:flex-end; text-align:center; }
  .bar-space { height:158px; display:flex; align-items:flex-end; justify-content:center; }
  .stack { width:min(18px,80%); min-height:1px; display:flex; flex-direction:column; justify-content:flex-end; border-radius:3px 3px 0 0; overflow:hidden; transition:height .25s ease; }
  .segment { width:100%; min-height:0; }.day > span { height:23px; padding-top:6px; color:#52525b; font-size:8px; white-space:nowrap; }

  .agent-list { display:flex; flex-direction:column; gap:14px; }
  .agent-label { display:flex; align-items:center; gap:7px; color:#a1a1aa; font-size:10px; text-transform:capitalize; }
  .agent-label strong { margin-left:auto; color:#e4e4e7; font-size:10px; }
  .agent-track { height:5px; margin:7px 0 5px; border-radius:999px; background:rgba(255,255,255,.05); overflow:hidden; }
  .agent-track div { height:100%; border-radius:inherit; background:linear-gradient(90deg,#7c3aed,#a78bfa); }
  .agent-row small { color:#52525b; font-size:8px; }

  .session-card { margin-bottom:13px; }
  .session-table { min-width:0; }
  .session-table-head,.session-row { display:grid; grid-template-columns:minmax(190px,1fr) 90px 90px 90px; align-items:center; gap:10px; }
  .session-table-head { padding:0 12px 7px 35px; color:#52525b; font-size:8px; text-transform:uppercase; letter-spacing:.07em; }
  .session-row { position:relative; width:100%; padding:9px 12px; border:0; border-top:1px solid rgba(255,255,255,.045); color:#8b8b95; background:transparent; text-align:left; cursor:pointer; }
  .session-row:hover { background:rgba(139,92,246,.055); }
  .session-row:disabled { cursor:default; opacity:.62; }
  .session-row:disabled:hover { background:transparent; }
  .rank { position:absolute; left:8px; color:#52525b; font-size:9px; }
  .session-name { min-width:0; display:flex; align-items:center; gap:8px; padding-left:15px; }
  .session-name > span { min-width:0; }.session-name strong,.session-name small { display:block; overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
  .session-name strong { color:#d4d4d8; font-size:10px; }.session-name small { margin-top:2px; color:#52525b; font-size:8px; text-transform:capitalize; }
  .time-cell { font:10px monospace; }.busy-text { color:#fbbf24; }.waiting-text { color:#67e8f9; }
  .session-progress { position:absolute; left:0; right:0; bottom:0; height:1px; background:rgba(255,255,255,.02); }
  .session-progress i { display:block; height:100%; background:linear-gradient(90deg,#7c3aed,#a78bfa); opacity:.5; }

  .statistics-note { display:flex; align-items:flex-start; gap:7px; color:#52525b; font-size:9px; line-height:1.45; }
  .statistics-note svg { flex-shrink:0; margin-top:1px; color:#71717a; }
  .empty-state { min-height:340px; display:flex; flex-direction:column; align-items:center; justify-content:center; text-align:center; color:#52525b; }
  .empty-icon { display:grid; place-items:center; width:64px; height:64px; border:1px solid rgba(139,92,246,.18); border-radius:18px; color:#8b5cf6; background:rgba(139,92,246,.06); }
  .empty-state h2 { margin:14px 0 5px; color:#d4d4d8; font-size:15px; }.empty-state p { max-width:420px; margin:0; font-size:10px; line-height:1.5; }
  .loading-grid { display:grid; grid-template-columns:2fr 1fr; gap:13px; }
  .loading-grid div { min-height:260px; border-radius:11px; background:linear-gradient(90deg,rgba(255,255,255,.025),rgba(255,255,255,.055),rgba(255,255,255,.025)); background-size:200% 100%; animation:shimmer 1.4s infinite; }
  .loading-grid div:last-child { grid-column:1/-1; min-height:160px; }
  @keyframes shimmer { to { background-position:-200% 0; } }

  @media (max-width:950px) {
    .chart-grid { grid-template-columns:1fr; }
    .session-table { overflow-x:auto; }
    .session-table-head,.session-row { min-width:620px; }
  }
  @media (max-width:650px) {
    .statistics-toolbar { align-items:stretch; flex-direction:column; }
    .range-selector { align-self:flex-start; }
    .refresh-button { align-self:flex-end; }
    .card-heading { flex-direction:column; }
  }
</style>
