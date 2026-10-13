<script lang="ts">
  import { Brain } from 'lucide-svelte';
  import Tooltip from '../common/Tooltip.svelte';
  import { t } from '#lib/i18n.js';

  export let active: boolean;
  export let disabled: boolean = false;
  export let onToggle: () => void;

  $: tooltip = $t('chat.thinkingToggle', {
    action: active ? $t('chat.disable') : $t('chat.enable'),
  });
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
