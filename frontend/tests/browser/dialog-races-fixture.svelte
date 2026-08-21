<script lang="ts">
  import { onMount, tick } from 'svelte';

  export let mode: 'global' | 'palette' | 'command' | 'history' | 'quickjump' | 'quickterminal' | 'scheme' | 'alltasks' | 'taskbadge' | 'dashboard' | 'recovery' | 'update' | 'settings' | 'newsession' | 'newgroup' | 'bgagents' | 'fork' | 'commandmanager' | 'template' = 'global';
  export let onFixtureReady: () => void = () => {};

  let showGlobal = mode === 'global';
  let showPalette = mode === 'palette';
  let showCommand = mode === 'command';
  let showHistory = mode === 'history';
  let showQuickJump = mode === 'quickjump';
  let showQuickTerminal = mode === 'quickterminal';
  let showSchemeImport = mode === 'scheme';
  let showRecovery = mode === 'recovery';
  let showUpdate = mode === 'update';
  let showSettings = mode === 'settings';
  let showNewSession = mode === 'newsession';
  let showNewGroup = mode === 'newgroup';
  let showBgAgents = mode === 'bgagents';
  let showFork = mode === 'fork';
  let showCommandManager = mode === 'commandmanager';
  let showTemplate = mode === 'template';
  let commandSession = 'session-a';
  let commandWindow = 3;
  let historyPath = '/repo-a';
  let FixtureComponent: any = null;

  onMount(() => {
    void loadFixture();
  });

  async function loadFixture() {
    // Import only the component exercised by this page. The former fixture
    // statically imported nine large dialogs, so eight cold Vite requests all
    // transformed the entire graph and some pages remained at the HTML shell
    // for 15+ seconds. Per-mode imports make readiness describe the component,
    // not contention in an unrelated fixture dependency.
    switch (mode) {
      case 'palette': FixtureComponent = (await import('../../src/lib/components/Dialogs/CommandPalette.svelte')).default; break;
      case 'command': FixtureComponent = (await import('../../src/lib/components/Dialogs/CommandPickerDialog.svelte')).default; break;
      case 'history': FixtureComponent = (await import('../../src/lib/components/Dialogs/GitHistoryDialog.svelte')).default; break;
      case 'quickjump': FixtureComponent = (await import('../../src/lib/components/Dialogs/QuickJumpDialog.svelte')).default; break;
      case 'quickterminal': FixtureComponent = (await import('../../src/lib/components/Dialogs/QuickTerminalDialog.svelte')).default; break;
      case 'scheme': FixtureComponent = (await import('../../src/lib/components/Dialogs/SchemeImportDialog.svelte')).default; break;
      case 'alltasks': FixtureComponent = (await import('../../src/lib/components/Dashboard/AllTasks.svelte')).default; break;
      case 'taskbadge': FixtureComponent = null; break;
      case 'dashboard': FixtureComponent = (await import('../../src/lib/components/Dashboard/ProjectDashboard.svelte')).default; break;
      case 'recovery': FixtureComponent = (await import('../../src/lib/components/Dialogs/RecoveryCenterDialog.svelte')).default; break;
      case 'update': FixtureComponent = (await import('../../src/lib/components/Dialogs/UpdateDialog.svelte')).default; break;
      case 'settings': FixtureComponent = (await import('../../src/lib/components/Dialogs/SettingsDialog.svelte')).default; break;
      case 'newsession': FixtureComponent = (await import('../../src/lib/components/Dialogs/NewSessionDialog.svelte')).default; break;
      case 'newgroup': FixtureComponent = (await import('../../src/lib/components/Dialogs/NewGroupDialog.svelte')).default; break;
      case 'bgagents': FixtureComponent = (await import('../../src/lib/components/Dialogs/BgAgentsDialog.svelte')).default; break;
      case 'fork': FixtureComponent = (await import('../../src/lib/components/Dialogs/ForkDialog.svelte')).default; break;
      case 'commandmanager': FixtureComponent = (await import('../../src/lib/components/Dialogs/CommandManagerDialog.svelte')).default; break;
      case 'template': FixtureComponent = (await import('../../src/lib/components/Dialogs/SessionTemplateDialog.svelte')).default; break;
      default: FixtureComponent = (await import('../../src/lib/components/Dialogs/GlobalSearchDialog.svelte')).default;
    }
    // The dynamic component itself is part of the next Svelte flush.
    await tick();
    onFixtureReady();
  }
</script>

<button id="change-command-target" on:click={() => { commandSession = 'session-b'; commandWindow = 9; }}>
  change target
</button>
<button id="reopen-quickjump" on:click={() => { showQuickJump = true; }}>reopen quick jump</button>
<button id="change-history-target" on:click={() => { historyPath = '/repo-b'; }}>
  change repository
</button>
<button id="reopen-history" on:click={() => { showHistory = true; }}>reopen history</button>
<button id="reopen-quickterminal" on:click={() => { showQuickTerminal = true; }}>reopen quick terminal</button>

{#if FixtureComponent}
  {#if mode === 'command'}
    <svelte:component this={FixtureComponent} bind:show={showCommand} sessionId={commandSession} windowIdx={commandWindow} />
  {:else if mode === 'palette'}
    <svelte:component this={FixtureComponent} bind:show={showPalette} />
  {:else if mode === 'history'}
    <svelte:component this={FixtureComponent} bind:show={showHistory} projectId="project-a" sessionId="session-a" windowIdx={3} path={historyPath} />
  {:else if mode === 'quickjump'}
    <svelte:component this={FixtureComponent} bind:show={showQuickJump} />
  {:else if mode === 'quickterminal'}
    <svelte:component this={FixtureComponent} bind:show={showQuickTerminal} sessionId="session-a" />
  {:else if mode === 'scheme'}
    <svelte:component this={FixtureComponent} bind:show={showSchemeImport} />
  {:else if mode === 'recovery'}
    <svelte:component this={FixtureComponent} bind:show={showRecovery} />
  {:else if mode === 'update'}
    <svelte:component this={FixtureComponent} bind:show={showUpdate} />
  {:else if mode === 'settings'}
    <svelte:component this={FixtureComponent} bind:show={showSettings} />
  {:else if mode === 'newsession'}
    <svelte:component this={FixtureComponent} bind:show={showNewSession} />
  {:else if mode === 'newgroup'}
    <svelte:component this={FixtureComponent} bind:show={showNewGroup} />
  {:else if mode === 'bgagents'}
    <svelte:component this={FixtureComponent} bind:show={showBgAgents} />
  {:else if mode === 'fork'}
    <svelte:component this={FixtureComponent} bind:show={showFork} />
  {:else if mode === 'commandmanager'}
    <svelte:component this={FixtureComponent} bind:show={showCommandManager} />
  {:else if mode === 'template'}
    <svelte:component this={FixtureComponent} bind:show={showTemplate} />
  {:else if mode === 'alltasks'}
    <svelte:component this={FixtureComponent} />
  {:else if mode === 'dashboard'}
    <svelte:component this={FixtureComponent} />
  {:else}
    <svelte:component this={FixtureComponent} bind:show={showGlobal} />
  {/if}
{/if}
