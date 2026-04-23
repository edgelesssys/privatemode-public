<script lang="ts">
  import ChatInput from '$lib/components/ChatInput.svelte';
  import ChatPanel from '$lib/components/ChatPanel.svelte';
  import { chatStore } from '$lib/chatStore';
  import type { Message } from '$lib/chatStore';
  import {
    privatemodeClient,
    clientVerifying,
    clientError,
  } from '$lib/clientStore';

  let currentChatId: string | null = $state(null);
  let slowConnection = $state(false);
  let draftMessage: Message | null = $state(null);

  $effect(() => {
    const unsubscribe = chatStore.currentChatId.subscribe((id) => {
      currentChatId = id;
    });
    return unsubscribe;
  });

  $effect(() => {
    chatStore.hydrateImages();
  });

  $effect(() => {
    if (!$clientVerifying) {
      slowConnection = false;
      return;
    }
    const timer = setTimeout(() => {
      slowConnection = true;
    }, 5000);
    return () => clearTimeout(timer);
  });

  function handleChatCreated(chatId: string) {
    chatStore.currentChatId.set(chatId);
  }

  function handleBranchOff(chatId: string, message: Message) {
    draftMessage = message;
    chatStore.currentChatId.set(chatId);
  }
</script>

<div class="content">
  {#if $clientVerifying}
    <div class="status-message">
      <p>Verifying secure connection...</p>
      {#if slowConnection}
        <p
          class="slow-hint"
          role="status"
          aria-live="polite"
        >
          This is taking longer than usual - your connection may be slow.
        </p>
      {/if}
    </div>
  {:else if $clientError}
    <div class="status-message error">
      <p>Failed to establish secure connection: {$clientError}</p>
    </div>
  {:else}
    <ChatPanel
      chatId={currentChatId}
      onBranchOff={handleBranchOff}
    />
    <ChatInput
      privatemodeAIClient={$privatemodeClient}
      {currentChatId}
      onChatCreated={handleChatCreated}
      bind:draftMessage
    />
  {/if}
</div>

<style>
  .content {
    width: 80%;
    height: 100%;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }

  .status-message {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    color: var(--color-text-secondary);
    font-size: 1rem;
  }

  .status-message.error {
    color: var(--color-error);
  }

  .slow-hint {
    margin-top: 0.5rem;
    font-size: 0.875rem;
    color: var(--color-text-tertiary);
  }

  @media (max-width: 768px) {
    .content {
      width: 95%;
    }
  }
</style>
