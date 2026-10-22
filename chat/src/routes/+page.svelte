<script lang="ts">
  import ChatInput from '#lib/components/chat/ChatInput.svelte';
  import ChatPanel from '#lib/components/chat/ChatPanel.svelte';
  import { chatStore } from '#lib/chatStore.js';
  import type { Message } from '#lib/chatStore.js';
  import {
    privatemodeClient,
    clientVerifying,
    clientError,
  } from '#lib/clientStore.js';
  import { locale, t } from '#lib/i18n.js';
  import { isLimitedMode, signIn } from '#lib/authStore.js';
  import { ANONYMOUS_MAX_CHARACTERS } from '#lib/models.js';
  import { ArrowUpRight } from 'lucide-svelte';

  let currentChatId: string | null = $state(null);
  let slowConnection = $state(false);
  let draftMessage: Message | null = $state(null);
  // Keep typed text when sign-in reinitializes the client and remounts the composer.
  let message = $state('');
  const historyCharacters = $derived(
    $chatStore
      .find((chat) => chat.id === currentChatId)
      ?.messages.reduce(
        (total, entry) =>
          total +
          (entry.role === 'system' ? 0 : Array.from(entry.content).length),
        0,
      ) ?? 0,
  );
  const anonymousCharacterCount = $derived(
    historyCharacters + Array.from(message.trim()).length,
  );
  const anonymousConversationExhausted = $derived(
    historyCharacters >= ANONYMOUS_MAX_CHARACTERS,
  );

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
  <h1 class="sr-only">Privatemode Chat</h1>
  {#if $clientVerifying}
    <div
      class="status-message"
      role="status"
    >
      <p>{$t('connection.verifying')}</p>
      {#if slowConnection}
        <p class="slow-hint">
          {$t('connection.slow')}
        </p>
      {/if}
    </div>
  {:else if $clientError}
    <div class="status-message error">
      <p>{$t('connection.failed', { error: $clientError })}</p>
    </div>
  {:else}
    {#if $isLimitedMode}
      <section
        class="guest-allowance"
        aria-label={$t('chat.guestMode')}
      >
        <div class="guest-summary">
          <strong>{$t('chat.guestMode')}</strong>
          <span
            class="guest-counter"
            class:over-limit={anonymousCharacterCount >
              ANONYMOUS_MAX_CHARACTERS}
            aria-label={$t('chat.anonymousCharacters', {
              count: anonymousCharacterCount,
              max: ANONYMOUS_MAX_CHARACTERS,
            })}
          >
            {$t('chat.anonymousCharacters', {
              count: anonymousCharacterCount.toLocaleString($locale),
              max: ANONYMOUS_MAX_CHARACTERS.toLocaleString($locale),
            })}
          </span>
          <p>{$t('chat.guestIncludesReplies')}</p>
        </div>
        <button
          type="button"
          onclick={signIn}
          aria-label={$t('chat.signInMore')}
        >
          {$t('layout.signIn')}<ArrowUpRight size={14} />
        </button>
        <div
          class="guest-notice"
          role="status"
        >
          {#if anonymousConversationExhausted || anonymousCharacterCount > ANONYMOUS_MAX_CHARACTERS}
            {$t('chat.guestLimitReached')}. {$t('chat.signInContinue')}.
          {/if}
        </div>
      </section>
    {/if}
    <ChatPanel
      chatId={currentChatId}
      onBranchOff={handleBranchOff}
    />
    <ChatInput
      privatemodeAIClient={$privatemodeClient}
      {currentChatId}
      {anonymousCharacterCount}
      {anonymousConversationExhausted}
      onChatCreated={handleChatCreated}
      bind:draftMessage
      bind:message
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

  .guest-allowance {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem 1rem;
    padding: 1rem 0;
    border-bottom: 1px solid var(--color-border);
    color: var(--color-text-secondary);
    font-size: 0.75rem;
  }

  .guest-summary {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.25rem 0.75rem;
    flex: 1;
  }

  .guest-summary strong {
    color: var(--color-text-primary);
    font-size: 0.875rem;
    font-weight: 500;
  }

  .guest-summary p {
    flex-basis: 100%;
    margin: 0;
  }

  .guest-counter {
    font-variant-numeric: tabular-nums;
  }

  .guest-counter.over-limit {
    color: var(--color-danger-text);
  }

  .guest-allowance button {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
    border: 1px solid var(--color-border);
    border-radius: 999px;
    padding: 0.375rem 0.625rem;
    background: var(--color-bg-surface);
    color: var(--color-text-primary);
    font: inherit;
    cursor: pointer;
  }

  .guest-allowance button:hover {
    border-color: var(--color-accent);
    color: var(--color-accent);
  }

  .guest-notice:not(:empty) {
    flex-basis: 100%;
    color: var(--color-text-primary);
  }

  @media (max-width: 768px) {
    .content {
      width: 95%;
    }

    .guest-allowance {
      margin-top: 3.5rem;
    }
  }
</style>
