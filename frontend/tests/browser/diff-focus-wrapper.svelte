<script lang="ts">
  import Diff from '../../src/lib/components/MainPanel/Diff.svelte';

  // A terminal's hidden input and a field someone is typing in, then the diff
  // shown the way MainPanel shows it: mounted all along, made active.
  let active = false;
  let takeFocus = true;
  export function show(focusTaking = true) {
    takeFocus = focusTaking;
    active = true;
  }
</script>

<textarea class="xterm-helper-textarea" aria-label="terminal"></textarea>
<input class="typing-field" aria-label="typing" />
<Diff {active} {takeFocus} initialMode="session" />
