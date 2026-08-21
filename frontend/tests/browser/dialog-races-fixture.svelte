<script lang="ts">
  import { onMount, tick } from 'svelte';
  import GlobalSearchDialog from '../../src/lib/components/Dialogs/GlobalSearchDialog.svelte';
  import CommandPickerDialog from '../../src/lib/components/Dialogs/CommandPickerDialog.svelte';
  import GitHistoryDialog from '../../src/lib/components/Dialogs/GitHistoryDialog.svelte';
  import QuickJumpDialog from '../../src/lib/components/Dialogs/QuickJumpDialog.svelte';
  import QuickTerminalDialog from '../../src/lib/components/Dialogs/QuickTerminalDialog.svelte';

  export let mode: 'global' | 'command' | 'history' | 'quickjump' | 'quickterminal' = 'global';
  export let onFixtureReady: () => void = () => {};

  let showGlobal = mode === 'global';
  let showCommand = mode === 'command';
  let showHistory = mode === 'history';
  let showQuickJump = mode === 'quickjump';
  let showQuickTerminal = mode === 'quickterminal';
  let commandSession = 'session-a';
  let commandWindow = 3;
  let historyPath = '/repo-a';

  onMount(() => {
    // Signal readiness from the component lifecycle, after Svelte has flushed
    // the initial dialog DOM. mount() returning only proves that the fixture
    // module evaluated; under a cold parallel Vite start those are distinct
    // milestones.
    void tick().then(() => {
      requestAnimationFrame(onFixtureReady);
    });
  });
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

<GlobalSearchDialog bind:show={showGlobal} />
<CommandPickerDialog bind:show={showCommand} sessionId={commandSession} windowIdx={commandWindow} />
<GitHistoryDialog bind:show={showHistory} path={historyPath} />
<QuickJumpDialog bind:show={showQuickJump} />
<QuickTerminalDialog bind:show={showQuickTerminal} sessionId="session-a" />
