<script lang="ts">
  export let agent: string;
  export let size: 'xs' | 'sm' | 'md' | 'lg' = 'md';

  // Import all icon SVGs
  import claudeIcon from '../../../assets/icons/claude.svg';
  import geminiIcon from '../../../assets/icons/gemini.svg';
  import aiderIcon from '../../../assets/icons/aider.svg';
  import codexIcon from '../../../assets/icons/codex.svg';
  import amazonqIcon from '../../../assets/icons/amazonq.svg';
  import opencodeIcon from '../../../assets/icons/opencode.svg';
  import terminalIcon from '../../../assets/icons/terminal.svg';
  import customIcon from '../../../assets/icons/custom.svg';

  const icons: Record<string, string> = {
    'claude': claudeIcon,
    'gemini': geminiIcon,
    'aider': aiderIcon,
    'codex': codexIcon,
    'amazonq': amazonqIcon,
    'opencode': opencodeIcon,
    'terminal': terminalIcon,
    'custom': customIcon
  };

  $: icon = icons[agent?.toLowerCase()] || customIcon;
  $: sizeValue = {
    xs: 12,
    sm: 16,
    md: 20,
    lg: 28
  }[size];
</script>

<img
  src={icon}
  alt={agent}
  title={agent}
  width={sizeValue}
  height={sizeValue}
  class="agent-icon"
/>

<style>
  .agent-icon {
    display: inline-block;
    vertical-align: middle;
    border-radius: 4px;
  }
</style>
