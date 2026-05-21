<script lang="ts">
  import { Brain } from 'lucide-svelte';
  import Tooltip from '../common/Tooltip.svelte';

  export let active: boolean;
  export let disabled: boolean = false;
  export let onToggle: () => void;

  $: tooltip = `${active ? 'Disable' : 'Enable'} (extended) thinking mode. This lets the model reason more deeply about the request before providing a response`;
</script>

<Tooltip text={tooltip}>
  <button
    class="thinking-toggle-button"
    class:active
    type="button"
    {disabled}
    aria-label={tooltip}
    on:click={onToggle}
  >
    <Brain size={18} />
  </button>
</Tooltip>

<style>
  .thinking-toggle-button {
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--color-text-secondary);
    transition: color 0.2s;
    font-size: 0.875rem;
    justify-self: flex-end;
    height: 32px;
  }

  .thinking-toggle-button:hover {
    color: var(--color-text-primary);
  }

  .thinking-toggle-button.active {
    color: var(--color-accent);
  }

  .thinking-toggle-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
