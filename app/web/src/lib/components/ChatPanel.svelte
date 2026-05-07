<script lang="ts">
  import { chatStore } from '$lib/chatStore';
  import type { Message, AttachedFile, AttachedImage } from '$lib/chatStore';
  import {
    MessageSquare,
    Copy,
    GitBranch,
    File,
    Mic,
    Check,
    Brain,
    X,
    CircleAlert,
    Loader2,
  } from 'lucide-svelte';
  import Tooltip from './Tooltip.svelte';
  import { renderMarkdown } from '$lib/markdown';
  import logoDark from '$lib/assets/favicon-dark.svg';
  import logoLight from '$lib/assets/favicon-light.svg';
  import { isDark } from '$lib/themeStore';
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
  let transcriptionCopied = false;

  interface RenderedMessage extends Message {
    renderedContent: string;
    renderedReasoning: string;
  }

  function formatThoughtDuration(ms: number): string {
    const seconds = ms / 1000;
    if (seconds < 10) {
      const rounded = Math.max(0.1, Math.round(seconds * 10) / 10);
      return `${rounded} ${rounded === 1 ? 'second' : 'seconds'}`;
    }

    const rounded = Math.round(seconds);
    return `${rounded} ${rounded === 1 ? 'second' : 'seconds'}`;
  }

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
  }) as RenderedMessage[];

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

    previousChatId = chatId;
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

  function copyText(text: string) {
    navigator.clipboard.writeText(text).catch(() => {});
  }

  function copyTranscription(text: string) {
    copyText(text);
    transcriptionCopied = true;
    setTimeout(() => {
      transcriptionCopied = false;
    }, 2000);
  }
</script>

<svelte:window
  on:keydown={(e) =>
    e.key === 'Escape' &&
    ((previewImage = null), (previewTranscription = null))}
/>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="chat-panel"
  bind:this={chatPanelElement}
  on:click={handleActionClick}
>
  {#if !chatId}
    <div class="empty-state">
      <MessageSquare size={64} />
      <h2>No chat selected</h2>
      <p>Start a new conversation to begin</p>
    </div>
  {:else if messages.length === 0}
    <div class="empty-state">
      <MessageSquare size={64} />
      <h2>Start a conversation</h2>
      <p>Send a message to begin chatting</p>
    </div>
  {:else}
    <div class="messages">
      {#each renderedMessages as message (message.id)}
        <div
          class="message {message.role}"
          class:error={message.isError}
          data-message-id={message.id}
        >
          <div class="message-bubble">
            {#if message.isError}
              <div class="message-header error-header">
                <span class="role">
                  <CircleAlert size={16} />
                  Error
                </span>
              </div>
            {:else if message.role === 'assistant'}
              <div class="message-header">
                <span class="role">
                  <img
                    src={logo}
                    alt="Privatemode Logo"
                    width="16"
                    height="16"
                  />
                  Privatemode
                </span>
              </div>
            {/if}
            {#if (message.attachedImages && message.attachedImages.length > 0) || (message.attachedFiles && message.attachedFiles.length > 0)}
              <div class="attached-files">
                {#if message.attachedImages}
                  {#each message.attachedImages as image}
                    <div class="image-chip">
                      {#if image.dataUrl}
                        <button
                          class="image-thumbnail-button"
                          type="button"
                          on:click={() => (previewImage = image)}
                        >
                          <img
                            src={image.dataUrl}
                            alt={image.name}
                            class="image-thumbnail"
                          />
                        </button>
                      {:else}
                        <div class="image-thumbnail placeholder"></div>
                      {/if}
                      <span class="file-name">{image.name}</span>
                    </div>
                  {/each}
                {/if}
                {#if message.attachedFiles}
                  {#each message.attachedFiles as file}
                    <div class="file-chip">
                      {#if file.kind === 'audio-transcription'}
                        <button
                          class="file-preview-button"
                          type="button"
                          on:click={() => (previewTranscription = file)}
                        >
                          <Mic size={16} />
                          <span class="file-name">{file.name}</span>
                        </button>
                      {:else}
                        <File size={16} />
                        <span class="file-name">{file.name}</span>
                      {/if}
                    </div>
                  {/each}
                {/if}
              </div>
            {/if}
            {#if chat?.isStreaming && message.id === messages[messages.length - 1]?.id && !message.content && !message.reasoning}
              <div class="loading-indicator">
                <Loader2
                  size={18}
                  class="spinning"
                />
              </div>
            {/if}
            {#if message.role === 'assistant' && message.reasoning}
              <details class="thinking-details">
                <summary
                  class="thinking-summary"
                  class:streaming={chat?.isStreaming &&
                    message.id === messages[messages.length - 1]?.id}
                >
                  <Brain size={16} />
                  <span class="thinking-label">
                    {#if chat?.isStreaming && message.id === messages[messages.length - 1]?.id}
                      Thinking
                    {:else if message.thoughtDurationMs}
                      Thought for {formatThoughtDuration(
                        message.thoughtDurationMs,
                      )}
                    {:else}
                      Thinking
                    {/if}
                  </span>
                </summary>
                <div class="thinking-content">
                  {@html message.renderedReasoning}
                </div>
              </details>
            {/if}
            {#if message.content}
              <div class="message-content">
                {@html message.renderedContent}
              </div>
            {/if}
          </div>
          <div
            class="message-actions"
            class:hidden={chat?.isStreaming &&
              message.id === messages[messages.length - 1]?.id}
          >
            <Tooltip text="Copy message">
              <button
                class="copy-message-button"
                disabled={chat?.isStreaming &&
                  message.id === messages[messages.length - 1]?.id}
              >
                <Copy size={16} />
              </button>
            </Tooltip>
            {#if !message.isError}
              <Tooltip text="Branch off from here">
                <button
                  class="branch-message-button"
                  disabled={chat?.isStreaming &&
                    message.id === messages[messages.length - 1]?.id}
                >
                  <GitBranch size={16} />
                </button>
              </Tooltip>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if previewImage}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="image-preview-overlay"
    on:click={() => (previewImage = null)}
  >
    <button
      class="image-preview-close"
      type="button"
      on:click={() => (previewImage = null)}
    >
      <X size={24} />
    </button>

    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="image-preview-container"
      on:click|stopPropagation
    >
      <img
        src={previewImage.dataUrl}
        alt={previewImage.name}
        class="image-preview-full"
      />
    </div>
  </div>
{/if}

{#if previewTranscription}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="image-preview-overlay"
    on:click={() => (previewTranscription = null)}
  >
    <button
      class="image-preview-close"
      type="button"
      on:click={() => (previewTranscription = null)}
    >
      <X size={24} />
    </button>

    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="transcription-preview-container"
      on:click|stopPropagation
    >
      <div class="transcription-preview-header">
        <h2>{previewTranscription.name}</h2>
        <button
          class="transcription-copy-button"
          class:copied={transcriptionCopied}
          type="button"
          on:click={() => copyTranscription(previewTranscription!.content)}
          aria-label={transcriptionCopied
            ? 'Copied transcription'
            : 'Copy transcription'}
        >
          {#if transcriptionCopied}
            <Check size={16} />
          {:else}
            <Copy size={16} />
          {/if}
        </button>
      </div>
      <p>{previewTranscription.content}</p>
    </div>
  </div>
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

  .empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    color: var(--color-text-tertiary);
    gap: 1rem;
  }

  .empty-state h2 {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 600;
    color: var(--color-text-secondary);
  }

  .empty-state p {
    margin: 0;
    font-size: 1rem;
  }

  .messages {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }

  .message {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    max-width: 80%;
  }

  .message.user {
    align-self: flex-end;
    color: var(--color-text-on-user-bubble);
  }

  .message.assistant {
    align-self: flex-start;
    color: var(--color-text-primary);
  }

  .message.error .message-bubble {
    border: 1px solid var(--color-error);
    border-radius: 0.75rem;
    padding: 1rem;
    background-color: var(--color-danger-indicator-bg);
  }

  .error-header .role {
    color: var(--color-error);
  }

  .message.error .message-content {
    color: var(--color-text-primary);
  }

  .loading-indicator {
    display: flex;
    align-items: center;
    padding: 0.25rem 0;
    color: var(--color-text-secondary);
  }

  .loading-indicator :global(.spinning) {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .message-bubble {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .message.user .message-bubble {
    padding: 1rem;
    border-radius: 0.75rem;
    background-color: var(--color-bg-surface-muted);
  }

  .message-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.75rem;
    opacity: 0.8;
    gap: 1rem;
  }

  .role {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    font-weight: 600;
  }

  .message-content {
    font-size: 0.95rem;
    line-height: 1.5;
    word-wrap: break-word;
  }

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

  .message-content :global(code)::-webkit-scrollbar {
    height: 4px;
  }

  .message-content :global(code)::-webkit-scrollbar-thumb {
    border-radius: 2px;
    background: transparent;
    transition: background 0.2s;
  }

  .message-content
    :global(.code-block-wrapper:hover code)::-webkit-scrollbar-thumb {
    background: rgba(255, 255, 255, 0.3);
  }

  .message.user .message-content {
    color: var(--color-text-on-user-bubble);
  }

  .message-content :global(p) {
    margin: 0.5rem 0;
  }

  .message-content :global(p:first-child) {
    margin-top: 0;
  }

  .message-content :global(p:last-child) {
    margin-bottom: 0;
  }

  .message-content :global(code) {
    background-color: rgba(0, 0, 0, 0.1);
    padding: 0.125rem 0.25rem;
    border-radius: 0.25rem;
    font-family: 'Courier New', monospace;
    font-size: 0.875em;
  }

  .message-content :global(pre) {
    background-color: var(--color-bg-code-block);
    color: #d4d4d4;
    padding: 1rem;
    border-radius: 0.5rem;
    overflow-x: auto;
    margin: 0.5rem 0;
  }

  .message-content :global(pre code) {
    background-color: transparent;
    padding: 0;
    font-size: 0.875rem;
    color: inherit;
  }

  .message-content :global(h1),
  .message-content :global(h2),
  .message-content :global(h3),
  .message-content :global(h4),
  .message-content :global(h5),
  .message-content :global(h6) {
    margin: 1rem 0 0.5rem 0;
    font-weight: 600;
  }

  .message-content :global(h1:first-child),
  .message-content :global(h2:first-child),
  .message-content :global(h3:first-child),
  .message-content :global(h4:first-child),
  .message-content :global(h5:first-child),
  .message-content :global(h6:first-child) {
    margin-top: 0;
  }

  .message-content :global(ul),
  .message-content :global(ol) {
    margin: 0.5rem 0;
    padding-left: 1.5rem;
  }

  .message-content :global(li) {
    margin: 0.25rem 0;
  }

  .message-content :global(blockquote) {
    border-left: 3px solid rgba(0, 0, 0, 0.2);
    padding-left: 1rem;
    margin: 0.5rem 0;
    font-style: italic;
  }

  .message.user .message-content :global(blockquote) {
    border-left-color: rgba(255, 255, 255, 0.3);
  }

  .message-content :global(a) {
    color: var(--color-accent);
    text-decoration: underline;
  }

  .message.user .message-content :global(a) {
    color: #93c5fd;
  }

  .message-content :global(table) {
    border-collapse: collapse;
    width: 100%;
    margin: 0.5rem 0;
  }

  .message-content :global(th),
  .message-content :global(td) {
    border: 1px solid rgba(0, 0, 0, 0.2);
    padding: 0.5rem;
    text-align: left;
  }

  .message.user .message-content :global(th),
  .message.user .message-content :global(td) {
    border-color: rgba(255, 255, 255, 0.3);
  }

  .message-content :global(th) {
    background-color: rgba(0, 0, 0, 0.05);
    font-weight: 600;
  }

  .message.user .message-content :global(th) {
    background-color: rgba(255, 255, 255, 0.1);
  }

  .message-content :global(hr) {
    border: none;
    border-top: 1px solid rgba(0, 0, 0, 0.2);
    margin: 1rem 0;
  }

  .message.user .message-content :global(hr) {
    border-top-color: rgba(255, 255, 255, 0.3);
  }

  .attached-files {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
  }

  .file-chip {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.375rem 0.5rem;
    background-color: var(--color-bg-surface-muted);
    border-radius: 0.5rem;
    font-size: 0.875rem;
    color: var(--color-text-primary);
  }

  .file-preview-button {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0;
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .file-preview-button:hover {
    color: var(--color-accent);
  }

  .image-chip {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.375rem 0.5rem;
    background-color: var(--color-bg-surface-muted);
    border-radius: 0.5rem;
    font-size: 0.875rem;
    color: var(--color-text-primary);
  }

  .image-thumbnail-button {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    border-radius: 0.25rem;
    display: flex;
  }

  .image-thumbnail-button:hover .image-thumbnail {
    opacity: 0.8;
  }

  .image-thumbnail {
    width: 32px;
    height: 32px;
    object-fit: cover;
    border-radius: 0.25rem;
  }

  .image-thumbnail.placeholder {
    background-color: var(--color-bg-active);
    animation: placeholder-pulse 1.5s ease-in-out infinite;
  }

  @keyframes placeholder-pulse {
    0%,
    100% {
      opacity: 0.4;
    }
    50% {
      opacity: 0.8;
    }
  }

  .image-preview-overlay {
    position: fixed;
    inset: 0;
    background-color: rgba(0, 0, 0, 0.8);
    backdrop-filter: blur(4px);
    z-index: 10000;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: overlay-fade-in 0.15s ease-out;
  }

  @keyframes overlay-fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  .image-preview-close {
    position: absolute;
    top: 1rem;
    right: 1rem;
    background: none;
    border: none;
    color: rgba(255, 255, 255, 0.8);
    cursor: pointer;
    padding: 0.5rem;
    border-radius: 0.5rem;
    display: flex;
    transition: color 0.2s;
  }

  .image-preview-close:hover {
    color: white;
  }

  .image-preview-full {
    max-width: 90vw;
    max-height: 90vh;
    object-fit: contain;
    border-radius: 0.5rem;
  }

  .transcription-preview-container {
    width: min(720px, calc(100vw - 2rem));
    max-height: 80vh;
    overflow-y: auto;
    padding: 1.25rem;
    border-radius: 0.5rem;
    background-color: var(--color-bg-surface);
    color: var(--color-text-primary);
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.35);
  }

  .transcription-preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 1rem;
  }

  .transcription-preview-header h2 {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
  }

  .transcription-copy-button {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    padding: 0.375rem;
    border: none;
    border-radius: 0.375rem;
    background: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    transition:
      background-color 0.2s,
      color 0.2s;
  }

  .transcription-copy-button:hover {
    background-color: var(--color-bg-hover);
    color: var(--color-text-primary);
  }

  .transcription-copy-button.copied {
    color: var(--color-accent-green-check);
  }

  .transcription-preview-container p {
    margin: 0;
    white-space: pre-wrap;
    line-height: 1.6;
    color: var(--color-text-secondary);
  }

  .message.user .file-chip,
  .message.user .image-chip {
    background-color: rgba(0, 0, 0, 0.05);
  }

  .file-name {
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .message-content :global(.code-block-wrapper) {
    position: relative;
  }

  .message-content :global(.copy-button) {
    position: absolute;
    top: 0.5rem;
    right: 0.5rem;
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.375rem 0.625rem;
    background-color: rgba(255, 255, 255, 0.1);
    border: 1px solid rgba(255, 255, 255, 0.2);
    border-radius: 0.375rem;
    color: #d4d4d4;
    font-size: 0.75rem;
    cursor: pointer;
    transition: all 0.2s;
    font-family: inherit;
  }

  .message-content :global(.copy-button:hover) {
    background-color: rgba(255, 255, 255, 0.2);
    border-color: rgba(255, 255, 255, 0.3);
  }

  .message-content :global(.copy-button svg) {
    flex-shrink: 0;
  }

  .message-content :global(.copy-button .copied-text) {
    display: none;
  }

  .message-content :global(.copy-button.copied .copy-text) {
    display: none;
  }

  .message-content :global(.copy-button.copied .copied-text) {
    display: inline;
  }

  .message-content :global(.copy-button.copied) {
    background-color: rgba(34, 197, 94, 0.2);
    border-color: rgba(34, 197, 94, 0.3);
  }

  .message-actions {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    transition: opacity 0.2s;
  }

  .message-actions.hidden {
    visibility: hidden;
    pointer-events: none;
  }

  .message.user .message-actions {
    opacity: 0;
  }

  .message.user:hover .message-actions {
    opacity: 1;
  }

  .copy-message-button {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0.375rem;
    background: none;
    border: none;
    border-radius: 0.375rem;
    color: var(--color-text-secondary);
    cursor: pointer;
    transition: all 0.2s;
  }

  .copy-message-button:hover,
  .branch-message-button:hover {
    background-color: var(--color-bg-hover);
    color: var(--color-text-primary);
  }

  .copy-message-button.copied {
    color: var(--color-accent-green-check);
  }

  .branch-message-button {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0.375rem;
    background: none;
    border: none;
    border-radius: 0.375rem;
    color: var(--color-text-secondary);
    cursor: pointer;
    transition: all 0.2s;
  }

  @media (max-width: 768px) {
    .message {
      max-width: 95%;
    }
    .message-actions {
      opacity: 1;
    }
  }
</style>
