<script lang="ts">
  import { Copy, GitBranch, CircleAlert, Loader2 } from 'lucide-svelte';
  import Tooltip from '../common/Tooltip.svelte';
  import FileChip from './FileChip.svelte';
  import ImageChip from './ImageChip.svelte';
  import ThoughtBlock from './ThoughtBlock.svelte';
  import type { Message, AttachedFile, AttachedImage } from '$lib/chatStore';

  export let message: Message & {
    renderedContent: string;
    renderedReasoning: string;
  };
  export let isLastStreaming: boolean;
  export let logo: string;
  export let onPreviewImage: (image: AttachedImage) => void;
  export let onPreviewTranscription: (file: AttachedFile) => void;
</script>

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
            <ImageChip
              {image}
              onPreview={onPreviewImage}
            />
          {/each}
        {/if}
        {#if message.attachedFiles}
          {#each message.attachedFiles as file}
            <FileChip
              {file}
              onPreview={onPreviewTranscription}
            />
          {/each}
        {/if}
      </div>
    {/if}
    {#if isLastStreaming && !message.content && !message.reasoning}
      <div class="loading-indicator">
        <Loader2
          size={18}
          class="spinning"
        />
      </div>
    {/if}
    {#if message.role === 'assistant' && message.reasoning}
      <ThoughtBlock
        renderedReasoning={message.renderedReasoning}
        isStreaming={isLastStreaming}
        thoughtDurationMs={message.thoughtDurationMs}
      />
    {/if}
    {#if message.content}
      <div class="message-content">
        {@html message.renderedContent}
      </div>
    {/if}
  </div>
  <div
    class="message-actions"
    class:hidden={isLastStreaming}
  >
    <Tooltip text="Copy message">
      <button
        class="copy-message-button"
        disabled={isLastStreaming}
      >
        <Copy size={16} />
      </button>
    </Tooltip>
    {#if !message.isError}
      <Tooltip text="Branch off from here">
        <button
          class="branch-message-button"
          disabled={isLastStreaming}
        >
          <GitBranch size={16} />
        </button>
      </Tooltip>
    {/if}
  </div>
</div>

<style>
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
    --chip-bg: rgba(0, 0, 0, 0.05);
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

  .copy-message-button,
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

  .copy-message-button:hover,
  .branch-message-button:hover {
    background-color: var(--color-bg-hover);
    color: var(--color-text-primary);
  }

  .copy-message-button.copied {
    color: var(--color-accent-green-check);
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
