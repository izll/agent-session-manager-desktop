<script lang="ts">
  /** Says that something is under way, for as long as it is on the page. */
  export let label = '';
</script>

<span class="busy-spinner" data-busy role="status" aria-label={label}></span>

<style>
  .busy-spinner {
    display: inline-block;
    flex-shrink: 0;
    width: 13px;
    height: 13px;
    box-sizing: border-box;
    vertical-align: -2px;
    /* White, as it sits on accent-coloured buttons, where the accent would vanish. */
    border: 2px solid rgba(255, 255, 255, 0.25);
    border-top-color: #fff;
    border-radius: 50%;
    animation: busy-spin 0.8s linear infinite;
  }
  @keyframes busy-spin {
    to { transform: rotate(360deg); }
  }
</style>
