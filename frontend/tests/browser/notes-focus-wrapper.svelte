<script lang="ts">
  import Notes from '../../src/lib/components/MainPanel/Notes.svelte';

  // A terminal's hidden input and a field someone is typing in, then the
  // notes shown the way MainPanel shows them: mounted all along, made active.
  let active = false;
  export function show() {
    active = true;
  }
</script>

<textarea class="xterm-helper-textarea" aria-label="terminal"></textarea>
<input class="typing-field" aria-label="typing" />
<Notes {active} />
