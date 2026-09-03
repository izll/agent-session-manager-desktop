<script lang="ts">
  /**
   * A usage ring, after the Plasma widgets this mirrors.
   *
   * The colour is not a plain threshold: it compares how much of the allowance
   * is spent against how much of the window has elapsed. Half the quota gone
   * with half the window left is fine; half of it gone in the first hour is
   * not — and a threshold at 50% cannot tell those apart.
   */
  export let percent: number;
  /** Shown after the ring — the window, for an agent that has more than one. */
  export let label: string = '';
  /** How far through the window we are, 0-100, or -1 when unknown. */
  export let timePercent: number = -1;
  export let title: string = '';

  // Big enough for two digits inside the ring; below about 26px they stop
  // being legible and the ring becomes decoration again.
  const SIZE = 28;
  const STROKE = 3;
  const R = (SIZE - STROKE) / 2;
  const C = 2 * Math.PI * R;

  $: clamped = Math.max(0, Math.min(100, percent));
  $: dash = (C * clamped) / 100;

  // Same rule as the widgets: ahead of the clock is trouble, well behind it is
  // comfortable, and with no clock to compare against fall back to thresholds.
  $: state = timePercent < 0
    ? (clamped < 50 ? 'ok' : clamped < 80 ? 'warn' : 'over')
    : (clamped >= 100 || clamped > timePercent
        ? 'over'
        : clamped > timePercent * 0.75 ? 'warn' : 'ok');
</script>

<span class="usage-ring {state}" title={title || `${label}: ${clamped.toFixed(0)}%`}>
  <svg width={SIZE} height={SIZE} viewBox="0 0 {SIZE} {SIZE}">
    <!-- Rotated so the ring starts at twelve o'clock rather than three. -->
    <g transform="rotate(-90 {SIZE / 2} {SIZE / 2})">
      <circle class="track" cx={SIZE / 2} cy={SIZE / 2} r={R} fill="none" stroke-width={STROKE} />
      <circle
        class="value"
        cx={SIZE / 2} cy={SIZE / 2} r={R}
        fill="none"
        stroke-width={STROKE}
        stroke-linecap="round"
        stroke-dasharray="{dash} {C - dash}"
      />
    </g>
    <!-- The number inside, so the ring is readable without hovering it. No
         percent sign: at this size the digits are the whole budget, and the
         ring already says what they are a proportion of. -->
    <text
      class="ring-value"
      x={SIZE / 2} y={SIZE / 2}
      text-anchor="middle" dominant-baseline="central"
    >{Math.round(clamped)}</text>
  </svg>
  {#if label}<span class="ring-label">{label}</span>{/if}
</span>

<style>
  .usage-ring {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    flex-shrink: 0;
  }
  .track { stroke: rgba(255, 255, 255, 0.12); }
  .ring-value {
    font-size: 9px;
    font-weight: 600;
    fill: currentColor;
  }
  .ring-label {
    font-size: 9px;
    line-height: 1;
    letter-spacing: 0.02em;
    opacity: 0.75;
  }
  .ok .value { stroke: #4ade80; }
  .warn .value { stroke: #fbbf24; }
  .over .value { stroke: #f87171; }
  .ok { color: #4ade80; }
  .ok .ring-label { color: #4ade80; }
  .warn { color: #fbbf24; }
  .warn .ring-label { color: #fbbf24; }
  .over { color: #f87171; }
  .over .ring-label { color: #f87171; }
</style>
