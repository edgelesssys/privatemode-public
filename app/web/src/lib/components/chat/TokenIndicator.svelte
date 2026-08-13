<script lang="ts">
  import Tooltip from '../common/Tooltip.svelte';
  import { locale, t } from '$lib/i18n';

  export let totalWordCount: number;
  export let maxWords: number;

  $: percentage = Math.min((totalWordCount / maxWords) * 100, 100);
  $: visible = percentage >= 75;
  $: warning = percentage >= 75 && percentage < 100;
  $: danger = percentage >= 100;
  $: remaining = Math.max(maxWords - totalWordCount, 0);
  $: tooltip = $t('chat.words', {
    current: totalWordCount.toLocaleString($locale),
    maximum: maxWords.toLocaleString($locale),
    remaining: remaining.toLocaleString($locale),
  });
</script>

{#if visible}
  <Tooltip text={tooltip}>
    <div
      class="token-indicator"
      class:warning
      class:danger
    >
      <span class="token-percentage">{Math.round(percentage)}%</span>
    </div>
  </Tooltip>
{/if}

<style>
  .token-indicator {
    display: flex;
    align-items: center;
    padding: 0.25rem 0.5rem;
    background-color: var(--color-bg-surface-muted);
    border-radius: 0.375rem;
    border: 1px solid var(--color-border);
    cursor: help;
    transition: all 0.2s;
  }

  .token-indicator:hover {
    background-color: var(--color-bg-active);
    border-color: var(--color-border-secondary);
  }

  .token-indicator.warning {
    background-color: var(--color-warning-bg);
    border-color: var(--color-warning-border);
  }

  .token-indicator.warning:hover {
    background-color: var(--color-warning-bg-hover);
    border-color: var(--color-warning-border-hover);
  }

  .token-indicator.danger {
    background-color: var(--color-danger-indicator-bg);
    border-color: var(--color-error);
  }

  .token-indicator.danger:hover {
    background-color: #fecaca;
    border-color: var(--color-danger-text);
  }

  .token-percentage {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--color-text-primary);
  }

  .token-indicator.warning .token-percentage {
    color: var(--color-warning-text);
  }

  .token-indicator.danger .token-percentage {
    color: #991b1b;
  }
</style>
