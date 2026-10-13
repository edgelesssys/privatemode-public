<script lang="ts">
  import { Brain } from 'lucide-svelte';
  import { locale, t } from '#lib/i18n.js';

  export let renderedReasoning: string;
  export let isStreaming: boolean;
  export let thoughtDurationMs: number | undefined;

  function formatThoughtDuration(ms: number): string {
    const seconds = ms / 1000;
    if (seconds < 10) {
      const rounded = Math.max(0.1, Math.round(seconds * 10) / 10);
      return $t(rounded === 1 ? 'chat.second' : 'chat.seconds', {
        count: rounded.toLocaleString($locale),
      });
    }
    const rounded = Math.round(seconds);
    return $t(rounded === 1 ? 'chat.second' : 'chat.seconds', {
      count: rounded.toLocaleString($locale),
    });
  }
</script>

<details class="thinking-details">
  <summary
    class="thinking-summary"
    class:streaming={isStreaming}
  >
    <Brain size={16} />
    <span class="thinking-label">
      {#if isStreaming}
        {$t('chat.thinking')}
      {:else if thoughtDurationMs}
        {$t('chat.thoughtFor', {
          duration: formatThoughtDuration(thoughtDurationMs),
        })}
      {:else}
        {$t('chat.thinking')}
      {/if}
    </span>
  </summary>
  <div class="thinking-content">
    <!-- eslint-disable-next-line svelte/no-at-html-tags -- sanitized by DOMPurify in renderMarkdown -->
    {@html renderedReasoning}
  </div>
</details>

<style>
  .thinking-details {
    align-self: flex-start;
  }

  .thinking-summary {
    width: fit-content;
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.15rem 0;
    cursor: pointer;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--color-text-secondary);
    user-select: none;
    list-style: none;
  }

  .thinking-summary::-webkit-details-marker {
    display: none;
  }

  .thinking-summary.streaming .thinking-label {
    background: linear-gradient(
      90deg,
      var(--color-text-secondary) 40%,
      rgba(255, 255, 255, 0.95) 50%,
      var(--color-text-secondary) 60%
    );
    background-size: 300% 100%;
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
    animation: thinking-shine 2s ease-in-out infinite;
  }

  .thinking-content {
    padding: 0.55rem 0 0.25rem 1.4rem;
    font-size: 0.9rem;
    line-height: 1.45;
    color: var(--color-text-secondary);
  }

  @keyframes thinking-shine {
    0% {
      background-position: 100% 0;
    }
    50% {
      background-position: 0% 0;
    }
    100% {
      background-position: 100% 0;
    }
  }

  .thinking-content :global(p:first-child) {
    margin-top: 0;
  }

  .thinking-content :global(p:last-child) {
    margin-bottom: 0;
  }

  .thinking-content :global(pre) {
    background-color: var(--color-bg-code-block);
    color: #d4d4d4;
    padding: 1rem;
    border-radius: 0.5rem;
    overflow-x: auto;
    margin: 0.5rem 0;
  }

  .thinking-content :global(code) {
    background-color: rgba(0, 0, 0, 0.08);
    padding: 0.125rem 0.25rem;
    border-radius: 0.25rem;
    font-family: 'Courier New', monospace;
    font-size: 0.875em;
  }
</style>
