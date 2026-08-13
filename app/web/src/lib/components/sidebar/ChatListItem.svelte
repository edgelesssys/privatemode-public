<script lang="ts">
  import { Pencil, Trash2 } from 'lucide-svelte';
  import Tooltip from '../common/Tooltip.svelte';
  import type { Chat } from '$lib/chatStore';
  import { t } from '$lib/i18n';

  export let chat: Chat;
  export let active: boolean;
  export let editing: boolean;
  export let renameValue: string;
  export let collapsed: boolean;
  export let onSelect: () => void;
  export let onStartRename: (event: MouseEvent) => void;
  export let onConfirmRename: () => void;
  export let onCancelRename: () => void;
  export let onDelete: (event: MouseEvent) => void;

  let inputElement: HTMLInputElement | null = null;
  let committed = false;

  $: if (editing && inputElement) {
    committed = false;
    inputElement.focus();
    inputElement.select();
  }

  function truncateTitle(title: string, maxLength: number = 30): string {
    if (title.length <= maxLength) return title;
    return title.slice(0, maxLength) + '...';
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      committed = true;
      onConfirmRename();
    } else if (event.key === 'Escape') {
      committed = true;
      onCancelRename();
    }
  }

  function handleBlur() {
    if (!committed) onConfirmRename();
  }
</script>

<div
  class="chat-item-wrapper"
  class:active
  role="button"
  tabindex={collapsed ? -1 : 0}
  on:click={() => {
    if (!editing) onSelect();
  }}
  on:keydown={(e) => {
    if ((e.key === 'Enter' || e.key === ' ') && !editing) {
      e.preventDefault();
      onSelect();
    }
  }}
>
  {#if editing}
    <input
      class="rename-input"
      type="text"
      aria-label={$t('sidebar.chatName')}
      bind:value={renameValue}
      bind:this={inputElement}
      on:keydown={handleKeydown}
      on:blur={handleBlur}
    />
  {:else}
    <span class="chat-title">{truncateTitle(chat.title)}</span>
    <div class="chat-actions">
      <Tooltip text={$t('sidebar.renameChat')}>
        <button
          class="rename-btn"
          type="button"
          aria-label={$t('sidebar.renameChat')}
          on:click={onStartRename}
        >
          <Pencil size={16} />
        </button>
      </Tooltip>
      <Tooltip text={$t('sidebar.deleteChat')}>
        <button
          class="delete-btn"
          type="button"
          aria-label={$t('sidebar.deleteChat')}
          on:click={onDelete}
        >
          <Trash2 size={16} />
        </button>
      </Tooltip>
    </div>
  {/if}
</div>

<style>
  .chat-item-wrapper {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem;
    border-radius: 0.375rem;
    transition: background-color 0.2s;
    cursor: pointer;
  }

  .chat-item-wrapper:hover {
    background-color: var(--color-bg-hover);
  }

  .chat-item-wrapper.active {
    background-color: var(--color-bg-active);
  }

  .chat-title {
    flex: 1;
    min-width: 0;
    color: var(--color-text-primary);
    font-size: 0.875rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chat-actions {
    display: flex;
    align-items: center;
    flex-shrink: 0;
  }

  .rename-btn,
  .delete-btn {
    background: none;
    border: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity 0.2s;
    flex-shrink: 0;
  }

  .chat-item-wrapper:hover .rename-btn,
  .chat-item-wrapper:hover .delete-btn,
  .chat-item-wrapper:focus-within .rename-btn,
  .chat-item-wrapper:focus-within .delete-btn {
    opacity: 1;
  }

  .rename-btn:hover {
    color: var(--color-text-primary);
  }

  .delete-btn:hover {
    color: var(--color-danger);
  }

  .rename-input {
    flex: 1;
    min-width: 0;
    width: 100%;
    background: var(--color-bg-surface-secondary);
    border: 1px solid var(--color-border-secondary);
    border-radius: 0.25rem;
    color: var(--color-text-primary);
    padding: 0.25rem 0.5rem;
    font-size: 0.875rem;
  }
</style>
