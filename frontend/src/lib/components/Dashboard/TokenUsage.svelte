<script lang="ts">
  /**
   * How many tokens each session used, across every project and machine.
   *
   * The rings in the sidebar say how much of a limit has gone. They cannot say
   * what used it, which is what this is for.
   */
  import { onMount, onDestroy } from 'svelte';
  import { GetTokenUsage, RefreshTokenUsage, SetTokenUsageMachineEnabled } from '../../../../wailsjs/go/main/App';
  import { EventsOn, EventsOff } from '../../../../wailsjs/runtime/runtime';
  import { t } from '../../i18n';
  import { goBack } from '../../stores/navigation';
  import { describeBackendError } from '../../utils/backendError';
  import Select from '../common/Select.svelte';
  import {
    filterUsageGroups, formatShare, formatTokens, groupUsage, searchFindsConversation, shortDirectory,
    usageConversationRows,
    type TokenUsageItem, type UsageBasis, type UsageGroup, type UsageGroupBy,
  } from '../../utils/tokenUsage';

  type UsageMachine = { id: string; name: string; local: boolean; lastScanUnix: number; error: string; enabled: boolean };
  type UsageProgress = { machine: string; done: number; total: number };

  let items: TokenUsageItem[] = [];
  let machines: UsageMachine[] = [];
  let scanning = false;
  let progress: UsageProgress | null = null;
  let loading = true;
  let error = '';

  let days = 7;
  let groupBy: UsageGroupBy = 'session';
  let basis: UsageBasis = 'new';
  let machineFilter = '';
  let expanded = new Set<string>();
  let search = '';
  let machinesOpen = false;

  // How often the table refreshes during a read. Not on every batch: each reload
  // walks every project's sessions.
  const LIVE_RELOAD_MS = 3000;
  let lastLiveLoad = 0;

  // A slow answer for last month must not replace the one for today that was
  // asked for after it.
  let loadGeneration = 0;

  // Reactive rather than plain functions: the markup calls these, and it has to
  // re-render when the language changes.
  $: machineLabel = (id: string, name: string) =>
    id === 'local' ? $t('tokenUsage.thisComputer') : (name || id);

  $: projectLabel = (name: string) => name || $t('project.default');

  $: groupTitle = (group: UsageGroup) => {
    const sample = group.sample;
    switch (groupBy) {
      case 'project':
        return sample.sessionId ? projectLabel(sample.projectName) : $t('tokenUsage.outside');
      case 'machine':
        return machineLabel(sample.machine, sample.machineName);
      case 'model':
        return sample.model || $t('tokenUsage.unknownModel');
      default:
        return sample.sessionName || shortDirectory(sample.cwd) || $t('tokenUsage.outside');
    }
  };

  $: groupSubtitle = (group: UsageGroup) => {
    const sample = group.sample;
    const elsewhere = sample.machine === 'local' ? '' : ` · ${machineLabel(sample.machine, sample.machineName)}`;
    if (groupBy === 'machine') return $t('tokenUsage.conversations', { count: group.conversations.length });
    if (groupBy !== 'session') return `${$t('tokenUsage.conversations', { count: group.conversations.length })}${groupBy === 'project' ? elsewhere : ''}`;
    const place = sample.sessionId ? projectLabel(sample.projectName) : $t('tokenUsage.outside');
    // With one conversation in the row, its id is what someone may be looking for.
    const only = group.conversations.length === 1 ? ` · ${group.conversations[0].conversation.slice(0, 8)}` : '';
    return `${place} · ${machineLabel(sample.machine, sample.machineName)}${only}`;
  };

  $: scanLabel = (machine: UsageMachine) => machine.lastScanUnix
    ? $t('tokenUsage.lastScan', { time: new Date(machine.lastScanUnix * 1000).toLocaleString() })
    : $t('tokenUsage.neverScanned');

  $: periods = [
    { days: 1, label: $t('tokenUsage.periodToday') },
    { days: 7, label: $t('tokenUsage.period7') },
    { days: 30, label: $t('tokenUsage.period30') },
    { days: 0, label: $t('tokenUsage.periodAll') },
  ];
  $: groupOptions = [
    { value: 'session', label: $t('tokenUsage.groupSession') },
    { value: 'project', label: $t('tokenUsage.groupProject') },
    { value: 'machine', label: $t('tokenUsage.groupMachine') },
    { value: 'model', label: $t('tokenUsage.groupModel') },
  ];
  $: basisOptions = [
    { value: 'new', label: $t('tokenUsage.basisNew') },
    { value: 'all', label: $t('tokenUsage.basisAll') },
  ];
  $: enabledMachines = machines.filter((machine) => machine.enabled);
  $: failedMachines = enabledMachines.filter((machine) => machine.error);
  $: machineOptions = [
    { value: '', label: $t('tokenUsage.machineAll') },
    ...enabledMachines.map((machine) => ({ value: machine.id, label: machineLabel(machine.id, machine.name) })),
  ];

  $: shown = machineFilter ? items.filter((item) => item.machine === machineFilter) : items;
  $: grouped = groupUsage(shown, groupBy, basis);
  $: visibleGroups = filterUsageGroups(grouped.groups, search);
  $: isOpen = (group: UsageGroup) => expanded.has(group.key) || searchFindsConversation(group, search);

  function machineScanning(id: string): boolean {
    return scanning && progress?.machine === id;
  }

  function toggle(key: string) {
    const next = new Set(expanded);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    expanded = next;
  }

  function setPeriod(next: number) {
    days = next;
    void load();
  }

  function setGroupBy(value: string) {
    groupBy = value as UsageGroupBy;
    expanded = new Set();
  }

  async function load() {
    const generation = ++loadGeneration;
    try {
      const report = await GetTokenUsage(days);
      if (generation !== loadGeneration) return;
      items = (report?.items || []) as TokenUsageItem[];
      machines = (report?.machines || []) as UsageMachine[];
      // Only ever turned off by the event that says it finished: a reload answered
      // just before a scan began must not end the spinner of the one now running.
      scanning = scanning || !!report?.scanning;
      if (machineFilter && !machines.some((machine) => machine.id === machineFilter && machine.enabled)) machineFilter = '';
      error = '';
    } catch (e) {
      if (generation === loadGeneration) error = describeBackendError(e);
    } finally {
      if (generation === loadGeneration) loading = false;
    }
  }

  async function setMachineEnabled(machine: UsageMachine, input: HTMLInputElement) {
    const enabled = input.checked;
    try {
      await SetTokenUsageMachineEnabled(machine.id, enabled);
    } catch (e) {
      // Nothing changed, so the box goes back: Svelte will not touch it, since
      // the value it was given has not moved.
      input.checked = machine.enabled;
      error = describeBackendError(e);
      return;
    }
    await load();
    // Turned back on, it has catching up to do.
    if (enabled && !scanning) await refresh();
  }

  async function refresh() {
    scanning = true;
    progress = null;
    try {
      await RefreshTokenUsage();
    } catch (e) {
      scanning = false;
      error = describeBackendError(e);
    }
  }

  onMount(() => {
    EventsOn('tokenUsage:progress', (next: UsageProgress) => {
      scanning = true;
      progress = next;
      // A first read can take minutes. Everything read so far is already stored,
      // so the table can fill up in front of the reader instead of sitting empty.
      const now = Date.now();
      if (now - lastLiveLoad >= LIVE_RELOAD_MS) {
        lastLiveLoad = now;
        void load();
      }
    });
    EventsOn('tokenUsage:updated', () => {
      scanning = false;
      progress = null;
      void load();
    });
    // What has been counted so far shows at once; the scan brings it up to date.
    void load().then(refresh);
  });

  onDestroy(() => {
    loadGeneration++;
    EventsOff('tokenUsage:progress');
    EventsOff('tokenUsage:updated');
  });
</script>

<svelte:window
  on:click={() => (machinesOpen = false)}
  on:keydown={(e) => { if (e.key === 'Escape' && machinesOpen) { e.stopPropagation(); machinesOpen = false; } }}
/>

<div class="token-usage" data-view="token-usage">
  <div class="head">
    <button class="back-btn" on:click={goBack} title={$t('allTasks.back')}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M19 12H5"/><path d="M12 19l-7-7 7-7"/>
      </svg>
      {$t('allTasks.back')}
    </button>
    <input
      class="search"
      type="search"
      data-control="search"
      placeholder={$t('tokenUsage.searchPlaceholder')}
      aria-label={$t('tokenUsage.searchPlaceholder')}
      bind:value={search}
    />
    <div class="periods" role="group" aria-label={$t('tokenUsage.period')}>
      {#each periods as period}
        <button class:active={days === period.days} data-period={period.days} on:click={() => setPeriod(period.days)}>{period.label}</button>
      {/each}
    </div>
    <div class="control" data-control="group">
      <span>{$t('tokenUsage.groupBy')}</span>
      <Select value={groupBy} options={groupOptions} small on:change={(e) => setGroupBy(e.detail)} />
    </div>
    <div class="control" data-control="basis" title={$t('tokenUsage.basisHint')}>
      <span>{$t('tokenUsage.basis')}</span>
      <Select value={basis} options={basisOptions} small on:change={(e) => (basis = e.detail === 'all' ? 'all' : 'new')} />
    </div>
    {#if enabledMachines.length > 1}
      <div class="control" data-control="machine">
        <span>{$t('tokenUsage.machine')}</span>
        <Select value={machineFilter} options={machineOptions} small searchable={machineOptions.length > 8} on:change={(e) => (machineFilter = e.detail)} />
      </div>
    {/if}
    <div class="machines-control">
      <button class="machines-button" class:has-failed={failedMachines.length > 0} data-action="machines" aria-expanded={machinesOpen} on:click|stopPropagation={() => (machinesOpen = !machinesOpen)}>
        {$t('tokenUsage.machinesButton', { on: enabledMachines.length, total: machines.length })}
      </button>
      {#if machinesOpen}
        <!-- A list that opens on request: there can be twenty of these, and they
             are set once and rarely looked at again. -->
        <!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
        <div class="machines-panel" data-machines on:click|stopPropagation>
          {#each machines as machine (machine.id)}
            <span class="machine" class:failed={machine.enabled && !!machine.error} class:off={!machine.enabled} data-machine={machine.id}>
              <label class="machine-toggle" title={$t('tokenUsage.readMachine')}>
                <input
                  type="checkbox"
                  checked={machine.enabled}
                  aria-label={`${$t('tokenUsage.readMachine')}: ${machineLabel(machine.id, machine.name)}`}
                  on:change={(e) => setMachineEnabled(machine, e.currentTarget)}
                />
                <span class="machine-name">{machineLabel(machine.id, machine.name)}</span>
              </label>
              {#if !machine.enabled}
                <span>{$t('tokenUsage.machineOff')}</span>
              {:else if machineScanning(machine.id) && progress}
                <span>{$t('tokenUsage.scanning', { done: progress.done, total: progress.total })}</span>
              {:else}
                <span>{scanLabel(machine)}</span>
                {#if machine.error}
                  <span class="machine-error" title={describeBackendError(machine.error)}>{describeBackendError(machine.error)}</span>
                {/if}
              {/if}
            </span>
          {/each}
        </div>
      {/if}
    </div>
    <button class="refresh" data-action="refresh" disabled={scanning} on:click={refresh} title={$t('tokenUsage.refresh')}>
      <svg class:spin={scanning} width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/>
      </svg>
    </button>
  </div>

  <div class="summary" data-summary>
    <div class="stat" data-stat="value">
      <span class="stat-value">{formatTokens(grouped.value)}</span>
      <span class="stat-label">{basis === 'all' ? $t('tokenUsage.summaryAll') : $t('tokenUsage.summaryNew')}</span>
    </div>
    <div class="stat" data-stat="output">
      <span class="stat-value">{formatTokens(grouped.totals.output)}</span>
      <span class="stat-label">{$t('tokenUsage.columnOutput')}</span>
    </div>
    <div class="stat" data-stat="cacheRead">
      <span class="stat-value">{formatTokens(grouped.totals.cacheRead)}</span>
      <span class="stat-label">{$t('tokenUsage.columnCacheRead')}</span>
    </div>
    <div class="stat" data-stat="subagents">
      <span class="stat-value">{formatShare(grouped.value > 0 ? grouped.totals.subagent / grouped.value : 0)}</span>
      <span class="stat-label">{$t('tokenUsage.columnSubagents')}</span>
    </div>
  </div>

  {#if error}
    <div class="error" role="alert">{error}</div>
  {/if}

  {#if (scanning && progress) || failedMachines.length}
    <div class="scan-status" data-scan-status>
      {#if scanning && progress}
        <span>{machineLabel(progress.machine, machines.find((m) => m.id === progress?.machine)?.name || '')}: {$t('tokenUsage.scanning', { done: progress.done, total: progress.total })}</span>
      {/if}
      {#if failedMachines.length}
        <button class="failed-link" data-action="show-failed" on:click|stopPropagation={() => (machinesOpen = true)}>{$t('tokenUsage.machinesFailed', { count: failedMachines.length })}</button>
      {/if}
    </div>
  {/if}

  <div class="table" role="table">
    <div class="row row-head" role="row">
      <span class="col-name" role="columnheader">{$t('tokenUsage.columnName')}</span>
      <span class="col-share" role="columnheader">{$t('tokenUsage.columnShare')}</span>
      <span class="col-num" role="columnheader">{$t('tokenUsage.columnInput')}</span>
      <span class="col-num" role="columnheader">{$t('tokenUsage.columnOutput')}</span>
      <span class="col-num" role="columnheader">{$t('tokenUsage.columnCacheWrite')}</span>
      <span class="col-num" role="columnheader">{$t('tokenUsage.columnCacheRead')}</span>
      <span class="col-num" role="columnheader">{$t('tokenUsage.columnSubagents')}</span>
    </div>
    <div class="rows">
      {#each visibleGroups as group (group.key)}
        <button class="row group-row" role="row" data-group={group.key} aria-expanded={isOpen(group)} on:click={() => toggle(group.key)}>
          <span class="col-name">
            <span class="group-title" title={group.sample.cwd}>{groupTitle(group)}</span>
            <span class="group-subtitle">{groupSubtitle(group)}</span>
          </span>
          <span class="col-share">
            <span class="bar"><span class="bar-fill" style="width: {Math.min(100, group.share * 100)}%"></span></span>
            <span class="share-text" data-share>{formatShare(group.share)}</span>
          </span>
          <span class="col-num">{formatTokens(group.input)}</span>
          <span class="col-num">{formatTokens(group.output)}</span>
          <span class="col-num">{formatTokens(group.cacheWrite)}</span>
          <span class="col-num muted">{formatTokens(group.cacheRead)}</span>
          <span class="col-num">{group.subagent > 0 ? formatShare(group.subagentShare) : '–'}</span>
        </button>
        {#if isOpen(group)}
          {#each usageConversationRows(group, groupBy === 'session') as row (row.key)}
            {#if row.kind === 'tab'}
              <div class="tab-row" data-tab={row.tabName}>
                <span>{row.tabName || $t('tokenUsage.noTab')}</span>
                <span class="tab-share">{formatShare(row.share)}</span>
              </div>
            {:else}
              {@const conversation = row.conversation}
              {@const heading = conversation.name || (groupBy === 'session' ? '' : conversation.tabName)}
              <div class="row conversation-row" role="row" data-conversation={conversation.conversation} style="padding-left: {24 + row.depth * 18}px">
                <span class="col-name">
                  <span class="group-title" title={conversation.conversation}>
                    {#if conversation.background}<span class="bg-badge" title={$t('tokenUsage.backgroundHint')}>bg</span>{/if}
                    {#if row.depth > 0}<span class="child-mark">↳</span>{/if}
                    {#if heading}{heading}{:else}<span class="conversation-id">{conversation.conversation}</span>{/if}
                  </span>
                  <span class="group-subtitle">
                    {#if heading}<span class="conversation-id" title={conversation.conversation}>{conversation.conversation}</span> · {/if}{#if conversation.title && conversation.title !== heading}<span class="auto-title" title={$t('tokenUsage.autoTitleHint')}>{conversation.title}</span> · {/if}{conversation.agent}{conversation.models.length ? ` · ${conversation.models.join(', ')}` : ''}{conversation.lastUnix ? ` · ${new Date(conversation.lastUnix * 1000).toLocaleDateString()}` : ''}
                  </span>
                </span>
                  <span class="col-share">
                    <span class="bar"><span class="bar-fill" style="width: {Math.min(100, conversation.share * 100)}%"></span></span>
                    <span class="share-text">{formatShare(conversation.share)}</span>
                  </span>
                  <span class="col-num">{formatTokens(conversation.input)}</span>
                  <span class="col-num">{formatTokens(conversation.output)}</span>
                  <span class="col-num">{formatTokens(conversation.cacheWrite)}</span>
                  <span class="col-num muted">{formatTokens(conversation.cacheRead)}</span>
                  <span class="col-num">{conversation.subagent > 0 && conversation.value > 0 ? formatShare(conversation.subagent / conversation.value) : '–'}</span>
              </div>
            {/if}
          {/each}
        {/if}
      {:else}
        {#if !loading}
          <div class="empty" data-empty>{search.trim() && grouped.groups.length ? $t('tokenUsage.noMatch') : scanning ? $t('tokenUsage.emptyScanning') : $t('tokenUsage.empty')}</div>
        {/if}
      {/each}
    </div>
  </div>

  <p class="note">{$t('tokenUsage.note')}</p>
</div>

<style>
  .token-usage {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    padding: 16px 20px;
    box-sizing: border-box;
    gap: 10px;
  }
  .head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 12px;
  }
  .back-btn {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 5px 10px;
    font-size: 13px;
    background: transparent;
    border: 1px solid #374151;
    border-radius: 6px;
    color: inherit;
    cursor: pointer;
    white-space: nowrap;
    margin-right: auto;
  }
  .back-btn:hover, .refresh:hover:not(:disabled) {
    background: rgba(107, 114, 128, 0.15);
  }
  .search {
    width: 190px;
    padding: 5px 9px;
    font-size: 12px;
    font-family: inherit;
    color: inherit;
    background: transparent;
    border: 1px solid #374151;
    border-radius: 6px;
    box-sizing: border-box;
  }
  .search:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }
  .periods {
    display: flex;
    border: 1px solid #374151;
    border-radius: 6px;
    overflow: hidden;
  }
  .periods button {
    padding: 5px 10px;
    font-size: 12px;
    background: transparent;
    border: none;
    color: #9ca3af;
    cursor: pointer;
    white-space: nowrap;
  }
  .periods button:hover {
    background: rgba(107, 114, 128, 0.15);
  }
  .periods button.active {
    background: rgba(var(--accent-rgb), 0.18);
    color: #e4e4e7;
  }
  .control {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: #9ca3af;
    white-space: nowrap;
  }
  .refresh {
    background: transparent;
    border: 1px solid #374151;
    color: inherit;
    border-radius: 6px;
    padding: 5px 8px;
    cursor: pointer;
    display: flex;
    align-items: center;
  }
  .refresh:disabled {
    cursor: default;
    opacity: 0.7;
  }
  .spin {
    animation: token-usage-spin 1s linear infinite;
  }
  @keyframes token-usage-spin {
    to { transform: rotate(360deg); }
  }
  .summary {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
    gap: 8px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 12px;
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.02);
  }
  .stat-value {
    font-size: 18px;
    font-weight: 600;
    color: #e4e4e7;
    font-variant-numeric: tabular-nums;
  }
  .stat-label {
    font-size: 11px;
    color: #9ca3af;
  }
  .error {
    padding: 8px 12px;
    border-radius: 6px;
    font-size: 12px;
    background: rgba(239, 68, 68, 0.12);
    color: #f87171;
  }
  .machines-control {
    position: relative;
  }
  .machines-button {
    padding: 5px 10px;
    font-size: 12px;
    font-family: inherit;
    color: #d1d5db;
    background: transparent;
    border: 1px solid #374151;
    border-radius: 6px;
    cursor: pointer;
    white-space: nowrap;
  }
  .machines-button:hover {
    background: rgba(107, 114, 128, 0.15);
  }
  .machines-button.has-failed {
    border-color: rgba(251, 191, 36, 0.5);
  }
  .machines-panel {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    z-index: 20;
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: min(460px, 80vw);
    max-height: 320px;
    overflow-y: auto;
    padding: 6px;
    box-sizing: border-box;
    font-size: 11px;
    color: #9ca3af;
    background: #15161c;
    border: 1px solid #374151;
    border-radius: 8px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
  }
  .scan-status {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 16px;
    font-size: 11px;
    color: #9ca3af;
  }
  .failed-link {
    padding: 0;
    font-size: 11px;
    font-family: inherit;
    color: #fbbf24;
    background: none;
    border: none;
    cursor: pointer;
    text-decoration: underline;
  }
  .machine {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 6px;
    padding: 4px 6px;
    min-width: 0;
    border-radius: 4px;
  }
  .machine:hover {
    background: rgba(107, 114, 128, 0.12);
  }
  .machine-name {
    font-weight: 600;
    color: #d1d5db;
  }
  .machine-toggle {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
  }
  .machine-toggle input {
    margin: 0;
  }
  .machine.off .machine-name {
    color: #6b7280;
    font-weight: 400;
  }
  .machine.failed .machine-name, .machine-error {
    color: #fbbf24;
  }
  .machine-error {
    flex-basis: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .table {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    overflow-x: auto;
  }
  .rows {
    flex: 1;
    min-height: 0;
    min-width: 760px;
    overflow-y: auto;
  }
  .row {
    display: grid;
    grid-template-columns: minmax(180px, 1fr) minmax(130px, 180px) repeat(5, 76px);
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 8px;
    box-sizing: border-box;
    font-size: 12px;
    font-family: inherit;
    color: inherit;
    text-align: left;
    background: transparent;
    border: none;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }
  .row-head {
    min-width: 760px;
    font-size: 11px;
    color: #9ca3af;
    border-bottom: 1px solid rgba(255, 255, 255, 0.12);
  }
  .group-row {
    cursor: pointer;
  }
  .group-row:hover, .group-row:focus-visible {
    background: rgba(107, 114, 128, 0.12);
  }
  .tab-row {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    padding: 5px 8px 3px 24px;
    font-size: 11px;
    font-weight: 600;
    color: #d1d5db;
    background: rgba(255, 255, 255, 0.015);
  }
  .tab-share {
    font-weight: 400;
    color: #9ca3af;
    font-variant-numeric: tabular-nums;
  }
  .auto-title {
    font-style: italic;
    color: #6b7280;
  }
  .bg-badge {
    padding: 0 5px;
    margin-right: 2px;
    font-size: 10px;
    border-radius: 3px;
    color: var(--accent-light, #a78bfa);
    background: rgba(var(--accent-rgb), 0.15);
  }
  .child-mark {
    color: #6b7280;
  }
  .conversation-id {
    font-family: ui-monospace, monospace;
    user-select: text;
  }
  .col-name {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .group-title, .group-subtitle {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .group-title {
    font-size: 13px;
    color: #e4e4e7;
  }
  .group-subtitle {
    font-size: 11px;
    color: #9ca3af;
  }
  .row-head .col-name {
    display: block;
  }
  .col-share {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .bar {
    flex: 1;
    height: 6px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.07);
    overflow: hidden;
  }
  .bar-fill {
    display: block;
    height: 100%;
    background: var(--accent-light, #a78bfa);
  }
  .share-text {
    width: 44px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .col-num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .muted {
    color: #9ca3af;
  }
  .empty {
    padding: 32px 8px;
    text-align: center;
    color: #9ca3af;
    font-size: 13px;
  }
  .note {
    margin: 0;
    font-size: 11px;
    line-height: 1.4;
    color: #6b7280;
  }
</style>
