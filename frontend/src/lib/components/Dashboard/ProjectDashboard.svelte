<script lang="ts">
  import { createEventDispatcher, onDestroy, onMount } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import AgentIcon from '../common/AgentIcon.svelte';
  import StatusIndicator from '../common/StatusIndicator.svelte';
  import ProjectStatistics from './ProjectStatistics.svelte';
  import { sessions, selectSession, selectWindow, toggleGroupCollapse, type Session, type Group } from '../../stores/sessions';
  import { groups } from '../../stores/sessions';
  import { activities, type Activity } from '../../stores/activities';
  import { tabStatuses, type TabStatusInfo } from '../../stores/statusLines';
  import { projects, activeProjectId } from '../../stores/projects';
  import { t } from '../../i18n';
  import { focusTerminal } from '../../utils/focus';

  interface ProjectGitSummary {
    sessionId: string;
    path: string;
    repository: boolean;
    repositoryRoot?: string;
    branch: string;
    upstream: string;
    dirty: boolean;
    modifiedFiles: number;
    ahead: number;
    behind: number;
    lastCommitHash: string;
    lastCommitMessage: string;
    lastCommitAuthor: string;
    lastCommitAt: string;
    error?: string;
  }

  const dispatch = createEventDispatcher();
  const GIT_REFRESH_INTERVAL = 15_000;

  // Claude subscription utilization (5h/7d rate-limit windows) — same data
  // the user's KDE usage widget shows. Backend caches for 60s, so fetching
  // on the git refresh cadence is free.
  interface UsageWindow { utilization: number; resetsAt: string }
  interface ClaudeUsage {
    available: boolean;
    fiveHour: UsageWindow;
    sevenDay: UsageWindow;
    sevenDaySonnet: UsageWindow;
    sevenDayOpus: UsageWindow;
  }
  let claudeUsage: ClaudeUsage | null = null;

  // Codex (GPT) usage: newest rate-limit snapshot from the Codex CLI's own
  // session logs. Windows vary by plan (e.g. weekly only, or 5h + weekly).
  interface CodexWindow { usedPercent: number; windowMinutes: number; resetsAt: number }
  interface CodexUsage {
    available: boolean;
    primary?: CodexWindow;
    secondary?: CodexWindow;
    planType?: string;
  }
  let codexUsage: CodexUsage | null = null;

  async function refreshUsage() {
    try {
      claudeUsage = (await App.GetClaudeUsage()) as ClaudeUsage;
    } catch { /* usage is optional decoration */ }
    try {
      codexUsage = (await App.GetCodexUsage()) as CodexUsage;
    } catch { /* optional */ }
  }

  function formatReset(value: string): string {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  }

  // "300 → 5h", "10080 → 7d" — plan-dependent Codex window labels.
  function windowLabel(minutes: number): string {
    if (!minutes) return '';
    if (minutes < 1440) return `${Math.round(minutes / 60)}h`;
    return `${Math.round(minutes / 1440)}d`;
  }

  function formatResetUnix(ts: number): string {
    if (!ts) return '';
    return new Date(ts * 1000).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  }

  let filter = '';
  let dashboardTab: 'overview' | 'statistics' = 'overview';
  let previousDashboardTab: 'overview' | 'statistics' = 'overview';
  let gitSummaries: ProjectGitSummary[] = [];
  let loading = true;
  let refreshing = false;
  let error = '';
  let updatedAt: Date | null = null;
  let refreshTimer: ReturnType<typeof setInterval> | null = null;
  let mounted = false;
  let loadGeneration = 0;
  let loadedProjectId: string | null = null;

  $: currentProject = $projects.find(p => p.id === $activeProjectId);
  $: gitBySession = new Map(gitSummaries.map(summary => [summary.sessionId, summary]));
  $: summary = calculateSummary($sessions, $activities, gitSummaries);
  $: filteredSessions = sortSessions(
    $sessions.filter(session => matchesFilter(session, filter, gitBySession, $groups, $tabStatuses)),
    $activities,
  );
  $: sections = buildSections(filteredSessions, $groups);

  interface DashboardSection {
    id: string;
    name: string;
    color: string;
    collapsed: boolean;
    sessions: Session[];
  }

  // Mirror the sidebar's organization: one section per group (in the groups'
  // own order), plus a trailing section for ungrouped sessions. The
  // attention-sort from filteredSessions is preserved inside each section.
  // `collapsed` is the SAME persisted state the sidebar uses, so folding a
  // group here folds it there too (and vice versa).
  function buildSections(list: Session[], groupList: Group[]): DashboardSection[] {
    const byGroup = new Map<string, Session[]>();
    for (const session of list) {
      const key = session.groupId && groupList.some(g => g.id === session.groupId)
        ? session.groupId : '';
      const bucket = byGroup.get(key);
      if (bucket) bucket.push(session); else byGroup.set(key, [session]);
    }
    const out: DashboardSection[] = [];
    for (const group of groupList) {
      const items = byGroup.get(group.id);
      if (items?.length) out.push({
        id: group.id, name: group.name, color: group.color,
        collapsed: group.collapsed, sessions: items,
      });
    }
    const ungrouped = byGroup.get('');
    if (ungrouped?.length) out.push({ id: '', name: '', color: '', collapsed: false, sessions: ungrouped });
    return out;
  }

  // ProjectSelector keeps this component mounted while switching projects.
  // Invalidate in-flight responses so Git data from the old project can never
  // flash over the newly-selected project's sessions.
  $: if (mounted && $activeProjectId !== loadedProjectId) {
    loadedProjectId = $activeProjectId;
    dashboardTab = 'overview';
    loadGeneration++;
    refreshing = false;
    gitSummaries = [];
    loading = true;
    updatedAt = null;
    error = '';
    void refreshGit();
  }

  $: if (mounted && dashboardTab === 'overview' && previousDashboardTab === 'statistics') {
    void refreshGit();
    void refreshUsage();
  }
  $: previousDashboardTab = dashboardTab;

  onMount(() => {
    mounted = true;
    loadedProjectId = $activeProjectId;
    void refreshGit();
    void refreshUsage();
    refreshTimer = setInterval(() => {
      if (dashboardTab === 'overview') {
        void refreshGit();
        void refreshUsage();
      }
    }, GIT_REFRESH_INTERVAL);
  });

  onDestroy(() => {
    mounted = false;
    loadGeneration++;
    if (refreshTimer) clearInterval(refreshTimer);
  });

  async function refreshGit() {
    if (refreshing) return;
    const projectId = $activeProjectId;
    const generation = ++loadGeneration;
    refreshing = true;
    error = '';
    try {
      const result = await App.GetProjectGitSummaries(projectId);
      if (!mounted || generation !== loadGeneration || projectId !== $activeProjectId) return;
      gitSummaries = (result || []) as ProjectGitSummary[];
      updatedAt = new Date();
    } catch (e) {
      if (!mounted || generation !== loadGeneration || projectId !== $activeProjectId) return;
      error = String(e);
    } finally {
      if (generation === loadGeneration) {
        loading = false;
        refreshing = false;
      }
    }
  }

  function calculateSummary(
    sessionList: Session[],
    activityMap: Record<string, Activity>,
    gitList: ProjectGitSummary[],
  ) {
    let running = 0;
    let busy = 0;
    let waiting = 0;
    let stopped = 0;
    for (const session of sessionList) {
      if (session.status !== 'running') {
        stopped++;
        continue;
      }
      running++;
      if (activityMap[session.id] === 'busy') busy++;
      if (activityMap[session.id] === 'waiting') waiting++;
    }

    const repositoryKeys = new Set<string>();
    const dirtyKeys = new Set<string>();
    for (const git of gitList) {
      if (!git.repository) continue;
      const key = git.repositoryRoot || git.path;
      repositoryKeys.add(key);
      if (git.dirty) dirtyKeys.add(key);
    }
    return {
      total: sessionList.length,
      running,
      busy,
      waiting,
      stopped,
      repositories: repositoryKeys.size,
      dirtyRepositories: dirtyKeys.size,
    };
  }

  function matchesFilter(
    session: Session,
    value: string,
    gitMap: Map<string, ProjectGitSummary>,
    groupList: typeof $groups,
    tabMap: Record<string, TabStatusInfo[]>,
  ): boolean {
    const query = value.trim().toLowerCase();
    if (!query) return true;
    const git = gitMap.get(session.id);
    const group = groupList.find(item => item.id === session.groupId);
    const tabs = tabMap[session.id] || [];
    return [
      session.name,
      session.path,
      session.agent,
      group?.name || '',
      git?.branch || '',
      git?.upstream || '',
      ...tabs.flatMap(tab => [tab.name, tab.agent, tab.statusLine]),
    ].some(value => value.toLowerCase().includes(query));
  }

  function sortSessions(sessionList: Session[], activityMap: Record<string, Activity>): Session[] {
    return [...sessionList].sort((a, b) => attentionWeight(b, activityMap) - attentionWeight(a, activityMap));
  }

  function attentionWeight(session: Session, activityMap: Record<string, Activity>): number {
    if (session.status !== 'running') return 0;
    const activity = activityMap[session.id] || 'idle';
    if (activity === 'waiting') return 4;
    if (activity === 'busy') return 3;
    return 2;
  }

  function sessionActivity(session: Session): Activity {
    return session.status === 'running' ? ($activities[session.id] || 'idle') : 'idle';
  }

  function statusLabel(session: Session): string {
    if (session.status !== 'running') return $t('dashboard.stopped');
    const activity = sessionActivity(session);
    if (activity === 'waiting') return $t('dashboard.waiting');
    if (activity === 'busy') return $t('dashboard.busy');
    return $t('dashboard.running');
  }

  function openSession(sessionId: string, windowIdx?: number) {
    selectSession(sessionId);
    if (windowIdx !== undefined) selectWindow(windowIdx);
    requestAnimationFrame(() => requestAnimationFrame(focusTerminal));
  }

  function tabsFor(session: Session): TabStatusInfo[] {
    return $tabStatuses[session.id] || [];
  }

  function formatCommitDate(value: string): string {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return date.toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
    });
  }
</script>

<div class="dashboard">
  <div class="dashboard-scroll">
    <header class="dashboard-header">
      <div>
        <div class="eyebrow">{currentProject?.name || $t('project.default')}</div>
        <h1>{dashboardTab === 'overview' ? $t('dashboard.title') : $t('statistics.title')}</h1>
        <p>{dashboardTab === 'overview' ? $t('dashboard.subtitle') : $t('statistics.subtitle')}</p>
      </div>
      {#if dashboardTab === 'overview'}
        <div class="header-actions">
          <button class="secondary-button" class:spinning={refreshing} on:click={() => refreshGit()} disabled={refreshing} title={$t('dashboard.refresh')}>
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M23 4v6h-6M1 20v-6h6"/>
              <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
            </svg>
            {$t('dashboard.refresh')}
          </button>
          <button class="primary-button" on:click={() => dispatch('newSession')}>
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
              <path d="M12 5v14M5 12h14"/>
            </svg>
            {$t('dashboard.newSession')}
          </button>
        </div>
      {/if}
    </header>

    <div class="dashboard-tabs" role="tablist" aria-label={$t('statistics.navigation')}>
      <button role="tab" aria-selected={dashboardTab === 'overview'} class:active={dashboardTab === 'overview'} on:click={() => dashboardTab = 'overview'}>
        {$t('statistics.overview')}
      </button>
      <button role="tab" aria-selected={dashboardTab === 'statistics'} class:active={dashboardTab === 'statistics'} on:click={() => dashboardTab = 'statistics'}>
        {$t('statistics.tab')}
      </button>
    </div>

    {#if dashboardTab === 'overview'}
    <section class="summary-grid">
      <div class="summary-card total"><span class="summary-label">{$t('dashboard.totalSessions')}</span><strong>{summary.total}</strong></div>
      <div class="summary-card running"><span class="summary-dot"></span><span class="summary-label">{$t('dashboard.running')}</span><strong>{summary.running}</strong></div>
      <div class="summary-card busy"><span class="summary-dot"></span><span class="summary-label">{$t('dashboard.busy')}</span><strong>{summary.busy}</strong></div>
      <div class="summary-card waiting"><span class="summary-dot"></span><span class="summary-label">{$t('dashboard.waiting')}</span><strong>{summary.waiting}</strong></div>
      <div class="summary-card stopped"><span class="summary-dot"></span><span class="summary-label">{$t('dashboard.stopped')}</span><strong>{summary.stopped}</strong></div>
      <div class="summary-card repositories">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><circle cx="18" cy="6" r="3"/><path d="M6 9v1a5 5 0 005 5h2a5 5 0 005-5V9M12 15v0"/></svg>
        <span class="summary-label">{$t('dashboard.repositories')}</span><strong>{summary.repositories}</strong>
      </div>
      <div class="summary-card dirty"><span class="summary-label">{$t('dashboard.dirtyRepositories')}</span><strong>{summary.dirtyRepositories}</strong></div>
    </section>

    {#if claudeUsage?.available || codexUsage?.available}
      <section class="usage-strip">
        {#if claudeUsage?.available}
          <div class="usage-row">
            <span class="usage-brand">{$t('dashboard.claudeUsage')}</span>
            <div class="usage-window">
              <span class="usage-label">{$t('dashboard.usage5h')}</span>
              <div class="usage-bar">
                <div class="usage-fill" class:warn={claudeUsage.fiveHour.utilization >= 70} class:hot={claudeUsage.fiveHour.utilization >= 90}
                  style="width:{Math.min(100, claudeUsage.fiveHour.utilization)}%"></div>
              </div>
              <span class="usage-pct">{Math.round(claudeUsage.fiveHour.utilization)}%</span>
              {#if claudeUsage.fiveHour.resetsAt}
                <span class="usage-reset">{$t('dashboard.usageResets', { time: formatReset(claudeUsage.fiveHour.resetsAt) })}</span>
              {/if}
            </div>
            <div class="usage-window">
              <span class="usage-label">{$t('dashboard.usage7d')}</span>
              <div class="usage-bar">
                <div class="usage-fill" class:warn={claudeUsage.sevenDay.utilization >= 70} class:hot={claudeUsage.sevenDay.utilization >= 90}
                  style="width:{Math.min(100, claudeUsage.sevenDay.utilization)}%"></div>
              </div>
              <span class="usage-pct">{Math.round(claudeUsage.sevenDay.utilization)}%</span>
            </div>
            <span class="usage-models" title="Sonnet / Opus (7d)">
              S {Math.round(claudeUsage.sevenDaySonnet.utilization)}% · O {Math.round(claudeUsage.sevenDayOpus.utilization)}%
            </span>
          </div>
        {/if}
        {#if codexUsage?.available}
          <div class="usage-row">
            <span class="usage-brand gpt">{$t('dashboard.codexUsage')}</span>
            {#if codexUsage.primary}
              <div class="usage-window">
                <span class="usage-label">{windowLabel(codexUsage.primary.windowMinutes)}</span>
                <div class="usage-bar">
                  <div class="usage-fill" class:warn={codexUsage.primary.usedPercent >= 70} class:hot={codexUsage.primary.usedPercent >= 90}
                    style="width:{Math.min(100, codexUsage.primary.usedPercent)}%"></div>
                </div>
                <span class="usage-pct">{Math.round(codexUsage.primary.usedPercent)}%</span>
                {#if codexUsage.primary.resetsAt}
                  <span class="usage-reset">{$t('dashboard.usageResets', { time: formatResetUnix(codexUsage.primary.resetsAt) })}</span>
                {/if}
              </div>
            {/if}
            {#if codexUsage.secondary}
              <div class="usage-window">
                <span class="usage-label">{windowLabel(codexUsage.secondary.windowMinutes)}</span>
                <div class="usage-bar">
                  <div class="usage-fill" class:warn={codexUsage.secondary.usedPercent >= 70} class:hot={codexUsage.secondary.usedPercent >= 90}
                    style="width:{Math.min(100, codexUsage.secondary.usedPercent)}%"></div>
                </div>
                <span class="usage-pct">{Math.round(codexUsage.secondary.usedPercent)}%</span>
              </div>
            {/if}
            {#if codexUsage.planType}
              <span class="usage-models">{codexUsage.planType}</span>
            {/if}
          </div>
        {/if}
      </section>
    {/if}

    <div class="toolbar">
      <div class="filter-box">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
        <input bind:value={filter} placeholder={$t('dashboard.searchPlaceholder')} />
        {#if filter}<button class="clear-filter" on:click={() => filter = ''} aria-label={$t('tabBar.clearText')}>×</button>{/if}
      </div>
      <div class="update-state">
        {#if error}<span class="error-text" title={error}>{$t('dashboard.refreshError')}</span>{/if}
        {#if updatedAt}<span>{$t('dashboard.updatedAt', { time: updatedAt.toLocaleTimeString() })}</span>{/if}
      </div>
    </div>

    {#if $sessions.length === 0}
      <div class="empty-state">
        <div class="empty-icon">◇</div>
        <h2>{$t('dashboard.noSessions')}</h2>
        <p>{$t('dashboard.noSessionsHint')}</p>
        <button class="primary-button" on:click={() => dispatch('newSession')}>{$t('dashboard.newSession')}</button>
      </div>
    {:else if filteredSessions.length === 0}
      <div class="empty-state compact"><h2>{$t('dashboard.noMatches')}</h2></div>
    {:else}
      {#each sections as section (section.id)}
        {#if sections.length > 1 || section.id !== ''}
          <button
            class="group-section-header"
            class:static={!section.id}
            on:click={() => section.id && toggleGroupCollapse(section.id)}
          >
            {#if section.id}
              <span class="group-chevron" class:expanded={!section.collapsed}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M9 18l6-6-6-6"/></svg>
              </span>
            {/if}
            <span class="group-folder">
              {#if section.collapsed}
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
                </svg>
              {:else}
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="m6 14 1.45-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.55 6a2 2 0 0 1-1.94 1.5H4a2 2 0 0 1-2-2V5c0-1.1.9-2 2-2h3.93a2 2 0 0 1 1.66.9l.82 1.2a2 2 0 0 0 1.66.9H18a2 2 0 0 1 2 2v2"/>
                </svg>
              {/if}
            </span>
            <span class="group-title" style={section.color && !section.color.startsWith('gradient-') ? `color:${section.color}` : ''}>
              {section.name || $t('dashboard.ungrouped')}
            </span>
            <span class="group-count">{section.sessions.length}</span>
            <div class="group-rule"></div>
          </button>
        {/if}
      {#if !section.collapsed}
      <section class="session-grid">
        {#each section.sessions as session (session.id)}
          {@const git = gitBySession.get(session.id)}
          {@const activity = sessionActivity(session)}
          {@const sessionTabs = tabsFor(session)}
          <article class="session-card" class:needs-attention={activity === 'waiting'} style={session.bgColor ? `--session-accent-bg:${session.bgColor}` : ''}>
            <div class="session-accent" style={session.color ? `background:${session.color}` : ''}></div>
            <div class="card-header">
              <div class="session-identity">
                <AgentIcon agent={session.agent} size="md" />
                <div class="session-title-wrap">
                  <h2 style={session.color && !session.color.startsWith('gradient-') ? `color:${session.color}` : ''}>{session.name}</h2>
                  <span class="agent-name">{session.agent}</span>
                </div>
              </div>
              <div class="live-status {activity}">
                <StatusIndicator status={session.status} {activity} size="sm" />
                <span>{statusLabel(session)}</span>
              </div>
            </div>

            <div class="session-path" title={session.path}>{session.path}</div>

            {#if sessionTabs.length > 1}
              <div class="tab-list">
                {#each sessionTabs.slice(0, 5) as tab (tab.windowIdx)}
                  <button class="tab-row" on:click={() => openSession(session.id, tab.windowIdx)} title={tab.statusLine || tab.name}>
                    <AgentIcon agent={tab.agent} size="xs" />
                    <span class="tab-name">{tab.name || tab.agent}</span>
                    <span class="tab-activity {tab.activity}"></span>
                  </button>
                {/each}
              </div>
            {/if}

            <div class="git-panel" class:dirty={git?.dirty}>
              {#if loading && !git}
                <div class="git-loading"><span></span><span></span></div>
              {:else if git?.repository}
                <div class="git-topline">
                  <div class="branch" title={git.upstream || git.branch}>
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="6" cy="6" r="2"/><circle cx="6" cy="18" r="2"/><circle cx="18" cy="6" r="2"/><path d="M6 8v8M8 18c6 0 8-4 8-10"/></svg>
                    <span>{git.branch || 'HEAD'}</span>
                  </div>
                  {#if git.dirty}
                    <span class="git-state dirty">{$t('dashboard.changedFiles', { n: git.modifiedFiles })}</span>
                  {:else}
                    <span class="git-state clean">{$t('dashboard.clean')}</span>
                  {/if}
                </div>
                {#if git.ahead > 0 || git.behind > 0}
                  <div class="divergence">
                    {#if git.ahead > 0}<span>↑ {$t('dashboard.ahead', { n: git.ahead })}</span>{/if}
                    {#if git.behind > 0}<span>↓ {$t('dashboard.behind', { n: git.behind })}</span>{/if}
                  </div>
                {/if}
                {#if git.lastCommitHash}
                  <div class="last-commit" title={`${git.lastCommitAuthor} · ${formatCommitDate(git.lastCommitAt)}`}>
                    <code>{git.lastCommitHash}</code>
                    <span>{git.lastCommitMessage}</span>
                  </div>
                {:else}
                  <div class="git-hint">{$t('dashboard.noCommit')}</div>
                {/if}
              {:else if git?.error}
                <div class="git-hint error" title={git.error}>{$t('dashboard.gitUnavailable')}</div>
              {:else}
                <div class="git-hint">{$t('dashboard.notRepository')}</div>
              {/if}
            </div>

            <button class="open-session" on:click={() => openSession(session.id)}>
              {$t('dashboard.openSession')}
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14M13 6l6 6-6 6"/></svg>
            </button>
          </article>
        {/each}
      </section>
      {/if}
      {/each}
    {/if}
    {:else}
      <ProjectStatistics projectId={$activeProjectId} onOpenSession={openSession} />
    {/if}
  </div>
</div>

<style>
  .dashboard { height: 100%; background: radial-gradient(circle at 15% 0%, rgba(139,92,246,.10), transparent 34%), #0a0a0f; }
  .dashboard-scroll { height: 100%; overflow-y: auto; padding: 28px clamp(20px, 3vw, 42px) 42px; }
  .dashboard-header { display:flex; align-items:flex-end; justify-content:space-between; gap:24px; margin-bottom:22px; }
  .eyebrow { color:#a78bfa; font-size:11px; text-transform:uppercase; letter-spacing:.11em; font-weight:700; margin-bottom:5px; }
  h1 { margin:0; font-size:29px; line-height:1.1; color:#fafafa; letter-spacing:-.03em; }
  .dashboard-header p { margin:7px 0 0; color:#71717a; font-size:13px; }
  .header-actions { display:flex; gap:9px; flex-wrap:wrap; justify-content:flex-end; }
  button { font:inherit; }
  .primary-button,.secondary-button { display:inline-flex; align-items:center; justify-content:center; gap:7px; border-radius:8px; padding:9px 13px; font-size:12px; font-weight:650; cursor:pointer; transition:.15s ease; }
  .primary-button { color:white; background:linear-gradient(135deg,#7c3aed,#8b5cf6); border:1px solid #8b5cf6; box-shadow:0 5px 18px rgba(124,58,237,.22); }
  .primary-button:hover { background:linear-gradient(135deg,#8b5cf6,#a78bfa); transform:translateY(-1px); }
  .secondary-button { color:#d4d4d8; background:rgba(255,255,255,.045); border:1px solid rgba(255,255,255,.09); }
  .secondary-button:hover { color:white; border-color:rgba(139,92,246,.35); background:rgba(139,92,246,.10); }
  .secondary-button:disabled { opacity:.55; cursor:default; transform:none; }
  .spinning svg { animation:spin 1s linear infinite; }
  @keyframes spin { to { transform:rotate(360deg); } }

  .dashboard-tabs { display:inline-flex; gap:3px; margin:-5px 0 18px; padding:3px; border:1px solid rgba(255,255,255,.065); border-radius:9px; background:rgba(12,12,20,.72); }
  .dashboard-tabs button { padding:7px 13px; border:0; border-radius:6px; color:#71717a; background:transparent; cursor:pointer; font-size:11px; font-weight:650; transition:.15s ease; }
  .dashboard-tabs button:hover { color:#d4d4d8; }
  .dashboard-tabs button.active { color:#ede9fe; background:rgba(139,92,246,.18); box-shadow:inset 0 0 0 1px rgba(139,92,246,.2); }

  .summary-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(125px,1fr)); gap:9px; margin-bottom:18px; }
  .summary-card { min-width:0; display:grid; grid-template-columns:auto 1fr auto; align-items:center; gap:7px; padding:13px 14px; border:1px solid rgba(255,255,255,.065); border-radius:10px; background:rgba(20,20,32,.72); color:#71717a; }
  .summary-card.total,.summary-card.dirty { grid-template-columns:1fr auto; }
  .summary-card strong { color:#f4f4f5; font-size:20px; line-height:1; }
  .summary-label { font-size:11px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .summary-dot { width:7px; height:7px; border-radius:50%; background:#888; }
  .summary-card.busy .summary-dot { background:#ffa500; box-shadow:0 0 7px rgba(255,165,0,.5); }
  .summary-card.waiting .summary-dot { background:#00ced1; box-shadow:0 0 7px rgba(0,206,209,.5); }
  .summary-card.stopped .summary-dot { background:#ff5f87; }
  .summary-card.repositories svg { color:#a78bfa; }
  .summary-card.dirty strong { color:#fbbf24; }

  .usage-strip { display:flex; flex-direction:column; gap:7px; margin:-6px 0 15px; padding:9px 14px; border:1px solid rgba(255,255,255,.065); border-radius:10px; background:rgba(20,20,32,.72); }
  .usage-row { display:flex; align-items:center; gap:14px; flex-wrap:wrap; }
  .usage-brand { color:#a78bfa; font-size:11px; font-weight:700; text-transform:uppercase; letter-spacing:.08em; min-width:120px; }
  .usage-brand.gpt { color:#4ade80; }
  .usage-window { display:flex; align-items:center; gap:8px; min-width:0; }
  .usage-label { color:#71717a; font-size:11px; white-space:nowrap; }
  .usage-bar { width:120px; height:6px; border-radius:999px; background:rgba(255,255,255,.07); overflow:hidden; }
  .usage-fill { height:100%; border-radius:999px; background:linear-gradient(90deg,#7c3aed,#a78bfa); transition:width .4s ease; }
  .usage-fill.warn { background:linear-gradient(90deg,#d97706,#fbbf24); }
  .usage-fill.hot { background:linear-gradient(90deg,#dc2626,#fb7185); }
  .usage-pct { color:#e4e4e7; font-size:12px; font-weight:650; min-width:34px; }
  .usage-reset { color:#52525b; font-size:10px; white-space:nowrap; }
  .usage-models { margin-left:auto; color:#71717a; font-size:11px; white-space:nowrap; }

  .toolbar { display:flex; align-items:center; justify-content:space-between; gap:16px; margin-bottom:15px; }
  .filter-box { flex:1; max-width:540px; display:flex; align-items:center; gap:9px; padding:0 12px; height:38px; border-radius:8px; border:1px solid rgba(255,255,255,.075); background:rgba(8,8,14,.68); color:#52525b; }
  .filter-box:focus-within { border-color:rgba(139,92,246,.45); box-shadow:0 0 0 2px rgba(139,92,246,.08); color:#a78bfa; }
  .filter-box input { flex:1; min-width:0; color:#e4e4e7; background:transparent; border:0; outline:0; font-size:12px; }
  .filter-box input::placeholder { color:#52525b; }
  .clear-filter { border:0; background:transparent; color:#71717a; cursor:pointer; font-size:18px; line-height:1; }
  .update-state { display:flex; gap:10px; color:#52525b; font-size:10px; white-space:nowrap; }
  .error-text { color:#fb7185; }

  .session-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(315px,1fr)); gap:13px; }
  .session-grid:not(:last-child) { margin-bottom:24px; }
  .group-section-header { width:100%; display:flex; align-items:center; gap:8px; margin:2px 0 11px; padding:4px 2px; text-align:left; background:none; border:0; border-radius:7px; cursor:pointer; }
  .group-section-header:hover:not(.static) { background:rgba(139,92,246,.06); }
  .group-section-header.static { cursor:default; }
  .group-chevron { display:flex; flex-shrink:0; color:#a1a1aa; transition:transform .2s ease; }
  .group-chevron.expanded { transform:rotate(90deg); }
  /* Same treatment as the sidebar's GroupItem: amber folder with a soft glow */
  .group-folder { display:flex; flex-shrink:0; color:#fbbf24; filter:drop-shadow(0 0 4px rgba(251,191,36,.3)); }
  .group-title { color:#d4d4d8; font-size:13px; font-weight:650; letter-spacing:.01em; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .group-count { flex-shrink:0; color:#71717a; font-size:10px; padding:2px 7px; border-radius:999px; background:rgba(255,255,255,.05); }
  .group-rule { flex:1; height:1px; background:linear-gradient(90deg,rgba(139,92,246,.25),transparent); }
  .session-card { position:relative; min-width:0; display:flex; flex-direction:column; padding:17px; border:1px solid rgba(255,255,255,.07); border-radius:12px; background:linear-gradient(145deg,var(--session-accent-bg,rgba(22,22,35,.92)),rgba(12,12,20,.96)); overflow:hidden; transition:border-color .15s ease,transform .15s ease,box-shadow .15s ease; }
  .session-card:hover { border-color:rgba(139,92,246,.27); transform:translateY(-1px); box-shadow:0 10px 28px rgba(0,0,0,.2); }
  .session-card.needs-attention { border-color:rgba(0,206,209,.26); }
  .session-accent { position:absolute; top:0; left:0; right:0; height:2px; background:linear-gradient(90deg,#7c3aed,#a78bfa); opacity:.9; }
  .card-header { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; }
  .session-identity { min-width:0; display:flex; align-items:center; gap:10px; }
  .session-title-wrap { min-width:0; }
  .session-title-wrap h2 { margin:0; color:#e4e4e7; font-size:14px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .agent-name { display:block; margin-top:2px; color:#62626d; font-size:10px; text-transform:capitalize; }
  .live-status { flex-shrink:0; display:flex; align-items:center; gap:6px; padding:5px 8px; border-radius:999px; background:rgba(255,255,255,.04); color:#8b8b95; font-size:10px; }
  .live-status.waiting { color:#67e8f9; background:rgba(0,206,209,.08); }
  .live-status.busy { color:#fbbf24; background:rgba(255,165,0,.08); }
  .session-path { margin:12px 0 11px; color:#5f5f69; font:10px/1.35 monospace; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .tab-list { display:flex; flex-wrap:wrap; gap:5px; margin:0 0 10px; }
  .tab-row { min-width:0; max-width:145px; display:flex; align-items:center; gap:5px; padding:4px 7px; border-radius:6px; border:1px solid rgba(255,255,255,.05); color:#8b8b95; background:rgba(0,0,0,.16); cursor:pointer; }
  .tab-row:hover { color:#d4d4d8; border-color:rgba(139,92,246,.25); }
  .tab-name { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:9px; }
  .tab-activity { width:5px; height:5px; border-radius:50%; background:#777; margin-left:auto; }
  .tab-activity.busy { background:#ffa500; }.tab-activity.waiting { background:#00ced1; }
  .git-panel { min-height:78px; margin-top:auto; padding:10px; border-radius:8px; border:1px solid rgba(255,255,255,.05); background:rgba(0,0,0,.17); }
  .git-panel.dirty { border-color:rgba(251,191,36,.12); }
  .git-topline { display:flex; justify-content:space-between; gap:8px; align-items:center; }
  .branch { min-width:0; display:flex; align-items:center; gap:6px; color:#c4b5fd; font:10px monospace; }
  .branch span { overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
  .git-state { flex-shrink:0; font-size:9px; padding:3px 6px; border-radius:999px; }
  .git-state.clean { color:#86efac; background:rgba(34,197,94,.08); }.git-state.dirty { color:#fbbf24; background:rgba(251,191,36,.08); }
  .divergence { display:flex; gap:10px; margin-top:7px; color:#8b8b95; font-size:9px; }
  .last-commit { min-width:0; display:flex; align-items:center; gap:7px; margin-top:9px; color:#71717a; font-size:10px; }
  .last-commit code { flex-shrink:0; color:#a78bfa; }.last-commit span { overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
  .git-hint { padding-top:19px; color:#52525b; text-align:center; font-size:10px; }.git-hint.error { color:#fb7185; }
  .git-loading { padding-top:17px; }.git-loading span { display:block; height:7px; margin:6px 0; border-radius:4px; background:linear-gradient(90deg,rgba(255,255,255,.035),rgba(255,255,255,.075),rgba(255,255,255,.035)); background-size:200% 100%; animation:shimmer 1.4s infinite; }.git-loading span:last-child{width:65%;}
  @keyframes shimmer { to { background-position:-200% 0; } }
  .open-session { display:flex; align-items:center; justify-content:space-between; width:100%; margin-top:10px; padding:8px 9px; color:#a78bfa; border:0; border-radius:7px; background:rgba(139,92,246,.07); cursor:pointer; font-size:10px; font-weight:650; }
  .open-session:hover { color:#ddd6fe; background:rgba(139,92,246,.15); }
  .empty-state { min-height:310px; display:flex; flex-direction:column; align-items:center; justify-content:center; text-align:center; color:#71717a; }
  .empty-state.compact { min-height:180px; }.empty-icon { color:#8b5cf6; font-size:54px; line-height:1; }.empty-state h2 { color:#d4d4d8; font-size:16px; margin:12px 0 4px; }.empty-state p { margin:0 0 16px; font-size:12px; }
  @media (max-width:1200px) { .summary-grid { grid-template-columns:repeat(4,1fr); } }
  @media (max-width:780px) { .dashboard-header { align-items:flex-start; flex-direction:column; }.summary-grid { grid-template-columns:repeat(2,1fr); }.toolbar { align-items:stretch; flex-direction:column; }.filter-box{max-width:none}.update-state{justify-content:flex-end} }
</style>
