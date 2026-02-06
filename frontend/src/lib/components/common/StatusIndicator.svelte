<script lang="ts">
  export let status: 'running' | 'paused' | 'stopped' = 'stopped';
  export let activity: 'idle' | 'busy' | 'waiting' = 'idle';
  export let size: 'sm' | 'md' | 'lg' = 'md';

  $: colorClass = {
    running: activity === 'busy' ? 'status-busy' :
             activity === 'waiting' ? 'status-waiting' :
             'status-running',
    paused: 'status-paused',
    stopped: 'status-stopped'
  }[status] || 'status-stopped';

  $: sizeClass = {
    sm: 'w-2 h-2',
    md: 'w-3 h-3',
    lg: 'w-4 h-4'
  }[size];
</script>

<span
  class="status-indicator {colorClass} {sizeClass}"
  title="{status} ({activity})"
></span>

<style>
  .status-indicator {
    display: inline-block;
    border-radius: 50%;
    position: relative;
    flex-shrink: 0;
  }

  .w-2 { width: 8px; }
  .h-2 { height: 8px; }
  .w-3 { width: 12px; }
  .h-3 { height: 12px; }
  .w-4 { width: 16px; }
  .h-4 { height: 16px; }

  /* Running + idle = light gray (connected but not active) */
  .status-running {
    background: #888888;
    box-shadow: 0 0 4px rgba(136, 136, 136, 0.4);
  }

  /* Running + busy = orange (working) */
  .status-busy {
    background: #FFA500;
    box-shadow: 0 0 8px rgba(255, 165, 0, 0.6);
    animation: pulse-glow-orange 1.5s ease-in-out infinite;
  }

  /* Running + waiting = cyan (waiting for input) */
  .status-waiting {
    background: #00CED1;
    box-shadow: 0 0 8px rgba(0, 206, 209, 0.6);
    animation: pulse-glow-cyan 1s ease-in-out infinite;
  }

  .status-paused {
    background: #f97316;
    box-shadow: 0 0 6px rgba(249, 115, 22, 0.5);
  }

  /* Stopped = red/pink */
  .status-stopped {
    background: #FF5F87;
    box-shadow: 0 0 4px rgba(255, 95, 135, 0.4);
  }

  @keyframes pulse-glow-orange {
    0%, 100% {
      box-shadow: 0 0 8px rgba(255, 165, 0, 0.6);
      opacity: 1;
    }
    50% {
      box-shadow: 0 0 16px rgba(255, 165, 0, 0.8);
      opacity: 0.8;
    }
  }

  @keyframes pulse-glow-cyan {
    0%, 100% {
      box-shadow: 0 0 8px rgba(0, 206, 209, 0.6);
    }
    50% {
      box-shadow: 0 0 14px rgba(0, 206, 209, 0.9);
    }
  }
</style>
