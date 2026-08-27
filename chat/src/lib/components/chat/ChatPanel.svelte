<script lang="ts">
  import { chatStore } from '$lib/chatStore';
  import type { Message, AttachedFile, AttachedImage } from '$lib/chatStore';
  import ImagePreviewModal from '../common/ImagePreviewModal.svelte';
  import TranscriptionPreviewModal from '../common/TranscriptionPreviewModal.svelte';
  import EmptyState from './EmptyState.svelte';
  import ChatMessage from './ChatMessage.svelte';
  import { renderMarkdown } from '$lib/markdown';
  import logoDark from '$lib/assets/favicon-dark.svg';
  import logoLight from '$lib/assets/favicon-light.svg';
  import { isDark } from '$lib/themeStore';
  import { t } from '$lib/i18n';
  import 'highlight.js/styles/github-dark.css';

  export let chatId: string | null;
  export let onBranchOff: (
    newChatId: string,
    message: Message,
  ) => void = () => {};

  $: logo = $isDark ? logoLight : logoDark;

  let messages: Message[] = [];
  let chatPanelElement: HTMLDivElement;
  let previousChatId: string | null = null;
  let previousMessageCount = 0;
  let previewImage: AttachedImage | null = null;
  let previewTranscription: AttachedFile | null = null;

  $: chat = chatId ? $chatStore.find((c) => c.id === chatId) : null;
  $: messages = chat?.messages || [];
  $: renderedMessages = messages.map((msg) => {
    const displayContent =
      msg.isError && msg.content.startsWith('Error: ')
        ? msg.content.slice('Error: '.length)
        : msg.content;
    return {
      ...msg,
      renderedContent: renderMarkdown(displayContent),
      renderedReasoning: renderMarkdown(msg.reasoning || ''),
    };
  });

  $: messageCount = messages.length;
  $: if (chatId && chatPanelElement && messageCount >= 0) {
    const chatChanged = chatId !== previousChatId;
    const messageCountIncreased = messageCount > previousMessageCount;

    requestAnimationFrame(() => {
      if (chatChanged) {
        chatPanelElement.scrollTo({
          top: chatPanelElement.scrollHeight,
          behavior: 'instant',
        });
      } else if (messageCountIncreased && messageCount > 0) {
        chatPanelElement.scrollTo({
          top: chatPanelElement.scrollHeight,
          behavior: 'smooth',
        });
      }
    });

    // eslint-disable-next-line no-useless-assignment -- read on the next run of this reactive block
    previousChatId = chatId;
    // eslint-disable-next-line no-useless-assignment -- read on the next run of this reactive block
    previousMessageCount = messageCount;
  }

  function handleActionClick(event: MouseEvent) {
    const target = event.target as HTMLElement;

    const branchButton = target.closest(
      '.branch-message-button',
    ) as HTMLButtonElement;
    if (branchButton) {
      const messageDiv = branchButton.closest('.message');
      const messageId = messageDiv?.getAttribute('data-message-id');
      if (chatId && messageId) {
        const msg = messages.find((m) => m.id === messageId);
        const includeInChat = msg?.role !== 'user';
        const result = chatStore.branchChat(chatId, messageId, includeInChat);
        if (result) onBranchOff(result.chatId, result.message);
      }
      return;
    }

    const button = target.closest(
      '.copy-button, .copy-message-button',
    ) as HTMLButtonElement;
    if (!button) return;

    if (button.classList.contains('copy-message-button')) {
      const messageDiv = button.closest('.message');
      const messageId = messageDiv?.getAttribute('data-message-id');
      const message = messages.find((m) => m.id === messageId);
      if (message) {
        navigator.clipboard.writeText(message.content).catch(() => {});
      }
    } else {
      const code = decodeURIComponent(button.dataset.code || '');
      navigator.clipboard.writeText(code).catch(() => {});
    }

    button.classList.add('copied');
    setTimeout(() => button.classList.remove('copied'), 2000);
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="chat-panel"
  bind:this={chatPanelElement}
  on:click={handleActionClick}
>
  {#if !chatId}
    <EmptyState
      title={$t('chat.noChatTitle')}
      subtitle={$t('chat.noChatSubtitle')}
    />
  {:else if messages.length === 0}
    <EmptyState
      title={$t('chat.emptyTitle')}
      subtitle={$t('chat.emptySubtitle')}
    />
  {:else}
    <div class="messages">
      {#each renderedMessages as message (message.id)}
        <ChatMessage
          {message}
          {logo}
          isLastStreaming={!!chat?.isStreaming &&
            message.id === messages[messages.length - 1]?.id}
          onPreviewImage={(img) => (previewImage = img)}
          onPreviewTranscription={(file) => (previewTranscription = file)}
        />
      {/each}
    </div>
  {/if}
</div>

{#if previewImage}
  <ImagePreviewModal
    image={previewImage}
    onClose={() => (previewImage = null)}
  />
{/if}

{#if previewTranscription}
  <TranscriptionPreviewModal
    file={previewTranscription}
    onClose={() => (previewTranscription = null)}
  />
{/if}

<style>
  .chat-panel {
    flex: 1;
    overflow-y: auto;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    --scrollbar-color: transparent;
  }

  .chat-panel:hover {
    --scrollbar-color: var(--color-scrollbar);
  }

  .chat-panel::-webkit-scrollbar {
    width: 4px;
  }

  .chat-panel::-webkit-scrollbar-thumb {
    border-radius: 2px;
    background: var(--scrollbar-color);
    transition: background 0.2s;
  }

  .messages {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }
</style>
