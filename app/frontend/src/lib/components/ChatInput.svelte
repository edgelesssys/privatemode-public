<script lang="ts">
  import Icon from '@iconify/svelte';
  import ModelPicker from './ModelPicker.svelte';
  import Tooltip from './Tooltip.svelte';
  import { PrivatemodeClient } from '$lib/privatemodeClient';
  import { chatStore } from '$lib/chatStore';
  import type { AttachedFile } from '$lib/chatStore';
  import { countWords } from '$lib/chatStore';
  import { modelConfig } from '$lib/models';
  import { onMount } from 'svelte';

  export let proxyPort: string;
  export let currentChatId: string | null;
  export let onChatCreated: (chatId: string) => void = () => {};

  let message = '';
  let textarea: HTMLTextAreaElement;
  let selectedModel = '';
  let extendedThinking = false;
  let isGenerating = false;
  let abortController: AbortController | null = null;
  let fileInput: HTMLInputElement;
  let isUploading = false;
  let attachedFiles: AttachedFile[] = [];
  let isDragging = false;
  let isDraggingOverInput = false;

  onMount(() => {
    const savedThinking = localStorage.getItem('privatemode_extended_thinking');
    if (savedThinking) {
      extendedThinking = savedThinking === 'true';
    }

    const handleWindowDragEnter = (e: DragEvent) => {
      if (!supportsFileUploads || isGenerating || isUploading) return;
      if (e.dataTransfer?.types?.includes('Files')) {
        isDragging = true;
      }
    };

    const handleWindowDragLeave = (e: DragEvent) => {
      if (e.relatedTarget === null) {
        isDragging = false;
      }
    };

    const handleWindowDrop = () => {
      isDragging = false;
    };

    window.addEventListener('dragenter', handleWindowDragEnter);
    window.addEventListener('dragleave', handleWindowDragLeave);
    window.addEventListener('drop', handleWindowDrop);

    return () => {
      window.removeEventListener('dragenter', handleWindowDragEnter);
      window.removeEventListener('dragleave', handleWindowDragLeave);
      window.removeEventListener('drop', handleWindowDrop);
    };
  });

  $: currentChat = currentChatId
    ? $chatStore.find((c) => c.id === currentChatId)
    : null;
  $: wordCount = currentChat?.wordCount || 0;
  $: maxWords = selectedModel
    ? modelConfig[selectedModel]?.maxWords || 60000
    : 60000;
  $: messageWordCount = countWords(message);
  $: attachedFilesWordCount = attachedFiles.reduce(
    (total, file) => total + countWords(file.content),
    0,
  );
  $: totalWordCount = wordCount + messageWordCount + attachedFilesWordCount;
  $: totalUsagePercentage = Math.min((totalWordCount / maxWords) * 100, 100);
  $: wouldExceedLimit = totalWordCount > maxWords;
  $: supportsFileUploads = selectedModel
    ? (modelConfig[selectedModel]?.supportsFileUploads ?? false)
    : true;
  $: supportsExtendedThinking = selectedModel
    ? (modelConfig[selectedModel]?.supportsExtendedThinking ?? false)
    : false;
  $: attachButtonTitle = isUploading
    ? 'Uploading file...'
    : isGenerating
      ? 'Cannot attach files while generating'
      : !supportsFileUploads
        ? 'Selected model does not support file uploads'
        : 'Attach a file';

  function autoResize() {
    if (textarea) {
      textarea.style.height = 'auto';
      textarea.style.height = textarea.scrollHeight - 10 + 'px';
    }
  }

  async function sendMessage() {
    if (!message.trim() || !selectedModel || isGenerating || wouldExceedLimit)
      return;

    let chatId = currentChatId;
    if (!chatId) {
      chatId = chatStore.createChat();
      onChatCreated(chatId);
    }

    const userMessage = message.trim();
    const filesToSend = [...attachedFiles];
    message = '';
    attachedFiles = [];

    if (textarea) {
      textarea.style.height = 'auto';
      textarea.focus();
    }

    chatStore.addMessage(chatId, {
      role: 'user',
      content: userMessage,
      attachedFiles: filesToSend.length > 0 ? filesToSend : undefined,
    });

    const assistantMessageId = chatStore.addMessage(chatId, {
      role: 'assistant',
      content: '',
    });

    isGenerating = true;
    abortController = new AbortController();

    try {
      const client = new PrivatemodeClient(proxyPort);
      const chat = $chatStore.find((c) => c.id === chatId);
      if (!chat) throw new Error('Chat not found');

      const messagesToSend = chat.messages.filter(
        (m) => m.id !== assistantMessageId,
      );

      chatStore.setStreaming(chatId, true);
      let accumulatedContent = '';
      let lastUpdate = 0;
      const UPDATE_THROTTLE_MS = 100;

      const reasoningEffort = extendedThinking ? 'high' : 'medium';
      const systemPrompt = modelConfig[selectedModel]?.systemPrompt;
      for await (const chunk of client.streamChatCompletion(
        selectedModel,
        messagesToSend,
        abortController.signal,
        reasoningEffort,
        systemPrompt,
      )) {
        accumulatedContent += chunk;
        const now = Date.now();
        if (now - lastUpdate >= UPDATE_THROTTLE_MS) {
          chatStore.updateMessage(
            chatId,
            assistantMessageId,
            accumulatedContent,
          );
          lastUpdate = now;
        }
      }
      chatStore.updateMessage(chatId, assistantMessageId, accumulatedContent);
    } catch (error) {
      if (error instanceof Error && error.name === 'AbortError') {
        console.log('Generation cancelled by user');
      } else {
        console.error('Error generating response:', error);
        let errorMessage = `Error: ${error instanceof Error ? error.message : 'Unknown error'}`;
        if (error instanceof Error && (error as any).status === 401) {
          errorMessage +=
            '\n\nYour API key may be invalid or expired. You can try to [update your API key](/settings).';
        }
        chatStore.updateMessage(chatId, assistantMessageId, errorMessage);
      }
    } finally {
      chatStore.setStreaming(chatId, false);
      isGenerating = false;
      abortController = null;
      setTimeout(() => textarea?.focus(), 0);
    }
  }

  function stopGeneration() {
    if (abortController) {
      abortController.abort();
    }
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      sendMessage();
    }
  }

  async function handleFileSelect(event: Event) {
    const target = event.target as HTMLInputElement;
    const file = target.files?.[0];
    if (!file) return;

    isUploading = true;
    try {
      const client = new PrivatemodeClient(proxyPort);
      const elements = await client.uploadFile(file);

      const extractedText = elements.map((el) => el.text).join('\n\n');

      attachedFiles = [
        ...attachedFiles,
        {
          name: file.name,
          content: extractedText,
        },
      ];
    } catch (error) {
      console.error('Error uploading file:', error);
      let errorMessage = `Failed to upload file: ${error instanceof Error ? error.message : 'Unknown error'}`;
      if (error instanceof Error && (error as any).status === 401) {
        errorMessage +=
          '\n\nYour API key may be invalid or expired. Please update your API key in settings.';
      }
      alert(errorMessage);
    } finally {
      isUploading = false;
      target.value = '';
    }
  }

  function removeFile(index: number) {
    attachedFiles = attachedFiles.filter((_, i) => i !== index);
  }

  function toggleExtendedThinking() {
    extendedThinking = !extendedThinking;
    localStorage.setItem(
      'privatemode_extended_thinking',
      String(extendedThinking),
    );
  }

  function handleDragOver(event: DragEvent) {
    if (!supportsFileUploads || isGenerating || isUploading) return;
    event.preventDefault();
    isDraggingOverInput = true;
  }

  function handleDragLeave() {
    isDraggingOverInput = false;
  }

  async function handleDrop(event: DragEvent) {
    event.preventDefault();
    isDragging = false;
    isDraggingOverInput = false;

    if (!supportsFileUploads || isGenerating || isUploading) return;

    const file = event.dataTransfer?.files?.[0];
    if (!file) return;

    isUploading = true;
    try {
      const client = new PrivatemodeClient(proxyPort);
      const elements = await client.uploadFile(file);

      const extractedText = elements.map((el) => el.text).join('\n\n');

      attachedFiles = [
        ...attachedFiles,
        {
          name: file.name,
          content: extractedText,
        },
      ];
    } catch (error) {
      console.error('Error uploading file:', error);
      let errorMessage = `Failed to upload file: ${error instanceof Error ? error.message : 'Unknown error'}`;
      if (error instanceof Error && (error as any).status === 401) {
        errorMessage +=
          '\n\nYour API key may be invalid or expired. Please update your API key in settings.';
      }
      alert(errorMessage);
    } finally {
      isUploading = false;
    }
  }
</script>

<div
  class="chat-input-wrapper"
  class:dragging={isDragging}
  class:dragging-over={isDraggingOverInput}
  role="region"
  ondragover={handleDragOver}
  ondragleave={handleDragLeave}
  ondrop={handleDrop}
>
  {#if isDragging}
    <div
      class="drop-overlay"
      class:active={isDraggingOverInput}
    >
      <Icon
        icon="material-symbols:upload-file"
        width="48"
        height="48"
      />
    </div>
  {/if}
  {#if attachedFiles.length > 0}
    <div class="attached-files">
      {#each attachedFiles as file, index}
        <div class="file-chip">
          <Icon
            icon="material-symbols:description"
            width="16"
            height="16"
          />
          <span class="file-name">{file.name}</span>
          <Tooltip text="Remove file">
            <button
              class="remove-file"
              onclick={() => removeFile(index)}
              type="button"
            >
              <Icon
                icon="material-symbols:close"
                width="16"
                height="16"
              />
            </button>
          </Tooltip>
        </div>
      {/each}
    </div>
  {/if}
  <textarea
    bind:this={textarea}
    bind:value={message}
    oninput={autoResize}
    onkeydown={handleKeyDown}
    placeholder="Type a message..."
    class="chat-input"
    rows="1"
    disabled={isGenerating}
  ></textarea>
  <div class="button-row">
    <input
      type="file"
      bind:this={fileInput}
      onchange={handleFileSelect}
      style="display: none;"
    />
    <div class="left-buttons">
      <Tooltip text={attachButtonTitle}>
        <button
          class="attach-button"
          type="button"
          disabled={isGenerating || isUploading || !supportsFileUploads}
          onclick={() => fileInput?.click()}
        >
          <Icon
            icon="material-symbols:attachment"
            width="18"
            height="18"
          />
          {isUploading ? 'Uploading...' : 'Attach'}
        </button>
      </Tooltip>
      {#if supportsExtendedThinking}
        <Tooltip
          text="Extended thinking lets the model reason more deeply about complex tasks, improving response quality at the expense of longer generation time."
        >
          <button
            class="thinking-toggle-button"
            class:active={extendedThinking}
            type="button"
            disabled={isGenerating}
            onclick={toggleExtendedThinking}
          >
            <Icon
              icon="material-symbols:clock-arrow-up"
              width="18"
              height="18"
            />
          </button>
        </Tooltip>
      {/if}
    </div>
    <div class="right-buttons">
      <div class="model-controls">
        {#if totalUsagePercentage >= 75}
          <Tooltip
            text="{totalWordCount.toLocaleString()} / {maxWords.toLocaleString()} words&#10;{Math.max(
              maxWords - totalWordCount,
              0,
            ).toLocaleString()} remaining"
          >
            <div
              class="token-indicator"
              class:warning={totalUsagePercentage >= 75 &&
                totalUsagePercentage < 100}
              class:danger={totalUsagePercentage >= 100}
            >
              <span class="token-percentage"
                >{Math.round(totalUsagePercentage)}%</span
              >
            </div>
          </Tooltip>
        {/if}
        <ModelPicker
          {proxyPort}
          bind:selectedModel
        />
      </div>
      {#if isGenerating}
        <Tooltip text="Stop generating">
          <button
            class="send-button stop"
            type="button"
            onclick={stopGeneration}
          >
            <Icon
              icon="material-symbols:stop"
              width="20"
              height="20"
            />
          </button>
        </Tooltip>
      {:else}
        <Tooltip
          text={wouldExceedLimit
            ? 'Message would exceed token limit'
            : 'Send message'}
        >
          <button
            class="send-button"
            type="button"
            onclick={sendMessage}
            disabled={!message.trim() ||
              !selectedModel ||
              wouldExceedLimit ||
              isUploading}
          >
            <Icon
              icon="material-symbols:send"
              width="20"
              height="20"
            />
          </button>
        </Tooltip>
      {/if}
    </div>
  </div>
</div>

<style>
  .chat-input-wrapper {
    margin-top: auto;
    margin-bottom: 30px;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    border-radius: 1rem;
    border: 1px solid #d1d5db;
    background-color: white;
    transition: border-color 0.2s;
    position: relative;
  }

  .chat-input-wrapper:focus-within {
    border-color: #9ca3af;
  }

  .chat-input-wrapper.dragging {
    border-color: #e9d5ff;
  }

  .chat-input-wrapper.dragging-over {
    border-color: #7a49f6;
  }

  .drop-overlay {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(122, 73, 246, 0.02);
    border-radius: 1rem;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.75rem;
    color: #7a49f6;
    pointer-events: none;
    z-index: 10;
    transition: all 0.2s;
  }

  .drop-overlay.active {
    background-color: rgba(122, 73, 246, 0.08);
    color: #7a49f6;
  }

  .drop-overlay :global(svg) {
    opacity: 0.6;
    transition: opacity 0.2s;
  }

  .drop-overlay.active :global(svg) {
    opacity: 0.9;
  }

  .chat-input {
    width: 100%;
    border: none;
    background: none;
    font-size: 1rem;
    outline: none;
    resize: none;
    min-height: 24px;
    max-height: 200px;
    overflow-y: auto;
    font-family: inherit;
    scrollbar-width: none;
    padding-top: 0.5rem;
  }

  .chat-input:disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }

  .button-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .left-buttons {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .right-buttons {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .attach-button {
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: #6b7280;
    transition: color 0.2s;
    font-size: 0.875rem;
    justify-self: flex-end;
    height: 32px;
  }

  .attach-button:hover {
    color: #374151;
  }

  .attach-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .thinking-toggle-button {
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: #6b7280;
    transition: color 0.2s;
    font-size: 0.875rem;
    justify-self: flex-end;
    height: 32px;
  }

  .thinking-toggle-button:hover {
    color: #374151;
  }

  .thinking-toggle-button.active {
    color: #7a49f6;
  }

  .thinking-toggle-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .send-button {
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #6b7280;
    transition: color 0.2s;
    padding: 0;
  }

  .send-button:hover {
    color: #374151;
  }

  .send-button.stop {
    color: #6b7280;
    transition: color 0.2s;
  }

  .send-button.stop:hover {
    color: #dc2626;
  }

  .send-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .attached-files {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid #e5e7eb;
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

  .file-name {
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .remove-file {
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #6b7280;
    transition: color 0.2s;
    padding: 0;
  }

  .remove-file:hover {
    color: #dc2626;
  }

  .model-controls {
    display: flex;
    align-items: stretch;
    gap: 0.5rem;
    height: 100%;
  }

  .token-indicator {
    display: flex;
    align-items: center;
    padding: 0.25rem 0.5rem;
    background-color: #f3f4f6;
    border-radius: 0.375rem;
    border: 1px solid #e5e7eb;
    cursor: help;
    transition: all 0.2s;
  }

  .token-indicator:hover {
    background-color: #e5e7eb;
    border-color: #d1d5db;
  }

  .token-indicator.warning {
    background-color: #fef3c7;
    border-color: #fbbf24;
  }

  .token-indicator.warning:hover {
    background-color: #fde68a;
    border-color: #f59e0b;
  }

  .token-indicator.danger {
    background-color: #fee2e2;
    border-color: #ef4444;
  }

  .token-indicator.danger:hover {
    background-color: #fecaca;
    border-color: #dc2626;
  }

  .token-percentage {
    font-size: 0.75rem;
    font-weight: 600;
    color: #374151;
  }

  .token-indicator.warning .token-percentage {
    color: #92400e;
  }

  .token-indicator.danger .token-percentage {
    color: #991b1b;
  }
</style>
