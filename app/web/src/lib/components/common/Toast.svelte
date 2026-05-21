<script lang="ts">
  import { toasts } from '$lib/toastStore';
  import { X } from 'lucide-svelte';

  function dismiss(id: number) {
    toasts.update((t) => t.filter((toast) => toast.id !== id));
  }
</script>

{#if $toasts.length > 0}
  <div class="toast-container">
    {#each $toasts as toast (toast.id)}
      <div
        class="toast"
        class:error={toast.type === 'error'}
      >
        <span class="toast-message"
          >{#if toast.dangerouslyRenderHTML}{@html toast.message}{:else}{toast.message}{/if}</span
        >
        <button
          class="toast-close"
          type="button"
          aria-label="Dismiss"
          onclick={() => dismiss(toast.id)}
        >
          <X size={14} />
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .toast-container {
    position: fixed;
    top: 16px;
    right: 16px;
    z-index: 100;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 400px;
  }

  .toast {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px;
    border-radius: 8px;
    background: var(--color-bg-surface);
    border: 1px solid var(--color-border);
    color: var(--color-text-primary);
    font-size: 0.875rem;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
    animation: slide-in 0.2s ease-out;
  }

  .toast.error {
    border-color: var(--color-danger);
  }

  .toast-message {
    flex: 1;
  }

  .toast-message :global(a) {
    color: inherit;
    font-weight: 600;
    text-decoration: underline;
  }

  .toast-close {
    background: none;
    border: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 2px;
    display: flex;
    align-items: center;
    flex-shrink: 0;
  }

  .toast-close:hover {
    color: var(--color-text-primary);
  }

  @media (max-width: 768px) {
    .toast-container {
      left: 16px;
      max-width: none;
    }
  }

  @keyframes slide-in {
    from {
      opacity: 0;
      transform: translateX(20px);
    }
    to {
      opacity: 1;
      transform: translateX(0);
    }
  }
</style>
