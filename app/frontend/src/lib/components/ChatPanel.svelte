<script lang="ts">
  import { chatStore } from '$lib/chatStore';
  import type { Message } from '$lib/chatStore';
  import Icon from '@iconify/svelte';
  import { renderMarkdown } from '$lib/markdown';
  import logo from '$lib/assets/logo-small.svg';
  import 'highlight.js/styles/github-dark.css';

  export let chatId: string | null;

  let messages: Message[] = [];
  let chatPanelElement: HTMLDivElement;
  let previousChatId: string | null = null;
  let previousMessageCount = 0;

  interface RenderedMessage extends Message {
    renderedContent: string;
  }

  $: chat = chatId ? $chatStore.find((c) => c.id === chatId) : null;
  $: messages = chat?.messages || [];
  $: renderedMessages = messages.map((msg) => ({
    ...msg,
    renderedContent: renderMarkdown(msg.content),
  })) as RenderedMessage[];

  $: if (chatId && chatPanelElement) {
    const chatChanged = chatId !== previousChatId;
    const messageCountIncreased = messages.length > previousMessageCount;

    requestAnimationFrame(() => {
      if (chatChanged) {
        chatPanelElement.scrollTo({
          top: chatPanelElement.scrollHeight,
          behavior: 'instant',
        });
      } else if (messageCountIncreased && messages.length > 0) {
        const lastMessage = chatPanelElement.querySelector(
          '.message:last-child',
        ) as HTMLElement;
        if (lastMessage) {
          lastMessage.scrollIntoView({ behavior: 'smooth', block: 'start' });
        }
      }
    });

    previousChatId = chatId;
    previousMessageCount = messages.length;
  }

  function handleCopyClick(event: MouseEvent) {
    const target = event.target as HTMLElement;
    const button = target.closest(
      '.copy-button, .copy-message-button',
    ) as HTMLButtonElement;
    if (!button) return;

    if (button.classList.contains('copy-message-button')) {
      const messageDiv = button.closest('.message');
      const messageId = messageDiv?.getAttribute('data-message-id');
      const message = messages.find((m) => m.id === messageId);
      if (message) {
        navigator.clipboard.writeText(message.content);
      }
    } else {
      const code = decodeURIComponent(button.dataset.code || '');
      navigator.clipboard.writeText(code);
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
  on:click={handleCopyClick}
>
  {#if !chatId}
    <div class="empty-state">
      <Icon
        icon="material-symbols:chat-outline"
        width="64"
        height="64"
      />
      <h2>No chat selected</h2>
      <p>Start a new conversation to begin</p>
    </div>
  {:else if messages.length === 0}
    <div class="empty-state">
      <Icon
        icon="material-symbols:chat-bubble-outline"
        width="64"
        height="64"
      />
      <h2>Start a conversation</h2>
      <p>Send a message to begin chatting</p>
    </div>
  {:else}
    <div class="messages">
      {#each renderedMessages as message (message.id)}
        <div
          class="message {message.role}"
          data-message-id={message.id}
        >
          {#if message.role === 'assistant'}
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
              <button class="copy-message-button">
                <Icon
                  icon="material-symbols:content-copy-outline"
                  width="16"
                  height="16"
                />
              </button>
            </div>
          {/if}
          {#if message.attachedFiles && message.attachedFiles.length > 0}
            <div class="attached-files">
              {#each message.attachedFiles as file}
                <div class="file-chip">
                  <Icon
                    icon="material-symbols:description"
                    width="16"
                    height="16"
                  />
                  <span class="file-name">{file.name}</span>
                </div>
              {/each}
            </div>
          {/if}
          <div class="message-content">
            {@html message.renderedContent}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

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
    --scrollbar-color: rgb(0, 0, 0, 0.3);
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
    color: #9ca3af;
    gap: 1rem;
  }

  .empty-state h2 {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 600;
    color: #6b7280;
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
    gap: 0.5rem;
    border-radius: 0.75rem;
    max-width: 80%;
  }

  .message.user {
    padding: 1rem;
    align-self: flex-end;
    background-color: #ffffff;
    color: #232323;
  }

  .message.assistant {
    align-self: flex-start;
    color: #1f2937;
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
    color: #232323;
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
    background-color: #1e1e1e;
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
    color: #7a49f6;
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
    background-color: #f3f4f6;
    border-radius: 0.5rem;
    font-size: 0.875rem;
    color: #374151;
  }

  .message.user .file-chip {
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

  .copy-message-button {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0.375rem;
    background-color: rgba(255, 255, 255, 0.9);
    border: 1px solid rgba(0, 0, 0, 0.1);
    border-radius: 0.375rem;
    color: #6b7280;
    cursor: pointer;
    opacity: 0;
    transition: all 0.2s;
    z-index: 10;
  }

  .message.assistant:hover .copy-message-button {
    opacity: 1;
  }

  .copy-message-button:hover {
    background-color: #ffffff;
    border-color: rgba(0, 0, 0, 0.2);
    color: #374151;
  }

  .copy-message-button.copied {
    background-color: rgba(34, 197, 94, 0.1);
    border-color: rgba(34, 197, 94, 0.3);
    color: #22c55e;
  }
</style>
