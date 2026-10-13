<script lang="ts">
  import { chatStore } from '#lib/chatStore.js';
  import type { Message } from '#lib/chatStore.js';
  import { t } from '#lib/i18n.js';

  export let chatModelUnavailable: boolean;
  export let unavailableModelId: string | undefined;
  export let currentChatId: string | null;
  export let lastNonErrorMessage: Message | undefined;
  export let onChatCreated: (chatId: string) => void;
  export let onDraftMessage: (message: Message) => void;

  function startNewChat() {
    const id = chatStore.createChat();
    onChatCreated(id);
  }

  function branchOff() {
    if (!currentChatId || !lastNonErrorMessage) return;
    const includeInChat = lastNonErrorMessage.role !== 'user';
    const result = chatStore.branchChat(
      currentChatId,
      lastNonErrorMessage.id,
      includeInChat,
    );
    if (result) {
      if (result.message.role === 'user') {
        onDraftMessage(result.message);
      }
      onChatCreated(result.chatId);
    }
  }
</script>

<div class="chat-error-banner">
  {#if chatModelUnavailable}
    {$t('chat.modelUnavailable', { model: unavailableModelId ?? '' })}
  {:else}
    {$t('chat.unusable')}
  {/if}
  <button
    class="new-chat-link"
    type="button"
    on:click={startNewChat}
  >
    {$t('chat.startNew')}
  </button>
  {#if lastNonErrorMessage}
    {$t('chat.or')}
    <button
      class="new-chat-link"
      type="button"
      on:click={branchOff}
    >
      {$t('chat.branchLast')}
    </button>
  {/if}
  {$t('chat.toContinue')}
</div>

<style>
  .chat-error-banner {
    padding: 0.75rem 1rem;
    font-size: 0.875rem;
    color: var(--color-text-secondary);
    text-align: center;
    line-height: 1.5;
  }

  .new-chat-link {
    background: none;
    border: none;
    padding: 0;
    color: var(--color-accent);
    text-decoration: underline;
    font: inherit;
    cursor: pointer;
  }

  .new-chat-link:hover {
    opacity: 0.8;
  }
</style>
