<script lang="ts">
  import {
    FileUp,
    File,
    X,
    Paperclip,
    Brain,
    Square,
    ArrowUp,
  } from 'lucide-svelte';
  import ModelPicker from './ModelPicker.svelte';
  import Tooltip from './Tooltip.svelte';
  import { chatStore } from '$lib/chatStore';
  import type { AttachedFile } from '$lib/chatStore';
  import { countWords } from '$lib/chatStore';
  import { modelConfig } from '$lib/models';
  import { onMount } from 'svelte';
  import type { PrivatemodeAI } from 'privatemode-ai';

  export let privatemodeAIClient: PrivatemodeAI | null;
  export let currentChatId: string | null;
  export let onChatCreated: (chatId: string) => void = () => {};

  const EXTENDED_THINKING_KEY = 'privatemode_extended_thinking';
  const THINKING_ENABLED_KEY = 'privatemode_thinking_enabled';

  let message = '';
  let textarea: HTMLTextAreaElement;
  let selectedModel = '';
  let extendedThinking = false;
  let thinkingEnabled = true;
  let isGenerating = false;
  let abortController: AbortController | null = null;
  let fileInput: HTMLInputElement;
  let isUploading = false;
  let attachedFiles: AttachedFile[] = [];
  let isDragging = false;
  let dragCounter = 0;

  onMount(() => {
    const savedThinking = localStorage.getItem(EXTENDED_THINKING_KEY);
    if (savedThinking) {
      extendedThinking = savedThinking === 'true';
    }
    const savedThinkingEnabled = localStorage.getItem(THINKING_ENABLED_KEY);
    if (savedThinkingEnabled) {
      thinkingEnabled = savedThinkingEnabled === 'true';
    }

    const handleWindowDragEnter = (e: DragEvent) => {
      if (!supportsFileUploads || isGenerating || isUploading) return;
      if (e.dataTransfer?.types?.includes('Files')) {
        dragCounter++;
        isDragging = true;
      }
    };

    const handleWindowDragLeave = (_e: DragEvent) => {
      dragCounter--;
      if (dragCounter <= 0) {
        dragCounter = 0;
        isDragging = false;
      }
    };

    const isFileDrag = (e: DragEvent) =>
      e.dataTransfer?.types?.includes('Files') ?? false;

    const handleWindowDrop = (e: DragEvent) => {
      if (isFileDrag(e)) e.preventDefault();
      dragCounter = 0;
      isDragging = false;
    };

    const handleWindowDragOver = (e: DragEvent) => {
      if (isFileDrag(e)) e.preventDefault();
    };

    window.addEventListener('dragenter', handleWindowDragEnter);
    window.addEventListener('dragleave', handleWindowDragLeave);
    window.addEventListener('dragover', handleWindowDragOver);
    window.addEventListener('drop', handleWindowDrop);

    return () => {
      window.removeEventListener('dragenter', handleWindowDragEnter);
      window.removeEventListener('dragleave', handleWindowDragLeave);
      window.removeEventListener('dragover', handleWindowDragOver);
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
  $: thinkingMode = selectedModel
    ? modelConfig[selectedModel]?.thinkingMode
    : undefined;
  $: supportsExtendedThinking = thinkingMode === 'extended';
  $: supportsThinkingToggle = thinkingMode === 'toggle';
  $: thinkingButtonActive = supportsExtendedThinking
    ? extendedThinking
    : supportsThinkingToggle
      ? thinkingEnabled
      : false;
  $: thinkingTooltip = supportsExtendedThinking
    ? 'Extended thinking lets the model reason more deeply about complex tasks, improving response quality at the expense of longer generation time.'
    : supportsThinkingToggle
      ? thinkingEnabled
        ? 'Thinking is enabled. Disable it to make Kimi answer directly without reasoning first.'
        : 'Thinking is disabled. Enable it to let Kimi reason before answering.'
      : '';
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
    if (
      !message.trim() ||
      !selectedModel ||
      isGenerating ||
      wouldExceedLimit ||
      !privatemodeAIClient
    )
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
    const assistantMessageStartedAt = Date.now();

    isGenerating = true;
    abortController = new AbortController();

    try {
      const chat = $chatStore.find((c) => c.id === chatId);
      if (!chat) throw new Error('Chat not found');

      const messagesToSend = chat.messages.filter(
        (m) => m.id !== assistantMessageId,
      );

      const apiMessages: { role: string; content: string }[] = [];
      const systemPrompt = modelConfig[selectedModel]?.systemPrompt;
      if (systemPrompt) {
        apiMessages.push({ role: 'system', content: systemPrompt });
      }
      for (const msg of messagesToSend) {
        if (msg.attachedFiles && msg.attachedFiles.length > 0) {
          for (const file of msg.attachedFiles) {
            apiMessages.push({
              role: msg.role,
              content: `[File: ${file.name}]\n\n${file.content}`,
            });
          }
        }
        apiMessages.push({ role: msg.role, content: msg.content });
      }
      const requestBody: Record<string, unknown> = {
        model: selectedModel,
        messages: apiMessages,
        stream: true,
      };
      if (supportsExtendedThinking) {
        requestBody.reasoning_effort = extendedThinking ? 'high' : 'medium';
      }
      if (supportsThinkingToggle && !thinkingEnabled) {
        requestBody.chat_template_kwargs = {
          thinking: false,
        };
      }

      chatStore.setStreaming(chatId, true);
      let accumulatedContent = '';
      let accumulatedReasoning = '';
      let lastUpdate = 0;
      const UPDATE_THROTTLE_MS = 100;

      for await (const chunk of privatemodeAIClient!.streamChatCompletions(
        requestBody,
        { signal: abortController!.signal },
      )) {
        const parsed = chunk as {
          choices: Array<{ delta: { content?: string; reasoning?: string } }>;
        };
        const delta = parsed.choices[0]?.delta;
        const content = delta?.content;
        const reasoning = delta?.reasoning;
        if (reasoning) {
          accumulatedReasoning += reasoning;
        }
        if (content) {
          accumulatedContent += content;
        }

        if (content || reasoning) {
          const now = Date.now();
          if (now - lastUpdate >= UPDATE_THROTTLE_MS) {
            chatStore.updateMessage(
              chatId,
              assistantMessageId,
              accumulatedContent,
              accumulatedReasoning,
              undefined,
            );
            lastUpdate = now;
          }
        }
      }
      const thoughtDurationMs =
        accumulatedReasoning.trim().length > 0
          ? Date.now() - assistantMessageStartedAt
          : undefined;
      chatStore.updateMessage(
        chatId,
        assistantMessageId,
        accumulatedContent,
        accumulatedReasoning,
        thoughtDurationMs,
      );
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
        chatStore.updateMessage(
          chatId,
          assistantMessageId,
          errorMessage,
          null,
          null,
        );
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
      const content = new Uint8Array(await file.arrayBuffer());
      const elements = (await privatemodeAIClient!.unstructured(
        [{ name: file.name, content, contentType: file.type }],
        { strategy: 'fast' },
      )) as Array<{ text: string }>;

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

  function toggleThinking() {
    if (supportsExtendedThinking) {
      extendedThinking = !extendedThinking;
      localStorage.setItem(EXTENDED_THINKING_KEY, String(extendedThinking));
      return;
    }

    if (supportsThinkingToggle) {
      thinkingEnabled = !thinkingEnabled;
      localStorage.setItem(THINKING_ENABLED_KEY, String(thinkingEnabled));
    }
  }

  async function handleDrop(event: DragEvent) {
    event.preventDefault();
    isDragging = false;

    if (!supportsFileUploads || isGenerating || isUploading) return;

    const file = event.dataTransfer?.files?.[0];
    if (!file) return;

    isUploading = true;
    try {
      const content = new Uint8Array(await file.arrayBuffer());
      const elements = (await privatemodeAIClient!.unstructured(
        [{ name: file.name, content, contentType: file.type }],
        { strategy: 'fast' },
      )) as Array<{ text: string }>;

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

{#if isDragging}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fullpage-drop-overlay"
    ondragover={(e) => e.preventDefault()}
    ondrop={handleDrop}
  >
    <div class="fullpage-drop-content">
      <FileUp size={48} />
      <span>Drop file to attach</span>
    </div>
  </div>
{/if}

<div
  class="chat-input-wrapper"
  role="region"
>
  {#if attachedFiles.length > 0}
    <div class="attached-files">
      {#each attachedFiles as file, index}
        <div class="file-chip">
          <File size={16} />
          <span class="file-name">{file.name}</span>
          <Tooltip text="Remove file">
            <button
              class="remove-file"
              onclick={() => removeFile(index)}
              type="button"
            >
              <X size={16} />
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
          <Paperclip size={18} />
          {isUploading ? 'Uploading...' : 'Attach'}
        </button>
      </Tooltip>
      {#if supportsExtendedThinking || supportsThinkingToggle}
        <Tooltip text={thinkingTooltip}>
          <button
            class="thinking-toggle-button"
            class:active={thinkingButtonActive}
            type="button"
            disabled={isGenerating}
            aria-label={thinkingTooltip}
            onclick={toggleThinking}
          >
            <Brain size={18} />
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
          {privatemodeAIClient}
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
            <Square size={20} />
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
            <ArrowUp size={20} />
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
    border: 1px solid var(--color-border);
    background-color: var(--color-bg-surface);
    box-shadow: 0 9px 30px rgba(0, 0, 0, 0.08);
    transition: border-color 0.2s;
    position: relative;
  }

  .chat-input-wrapper:focus-within {
    border-color: var(--color-border-secondary);
  }

  .fullpage-drop-overlay {
    position: fixed;
    inset: 0;
    background-color: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(6px);
    z-index: 9999;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: overlay-fade-in 0.15s ease-out;
  }

  .fullpage-drop-content {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
    color: rgba(255, 255, 255, 0.9);
    font-size: 1.125rem;
    font-weight: 500;
    letter-spacing: 0.02em;
    padding: 2.5rem 3.5rem;
    border-radius: 1rem;
    border: 2px dashed var(--color-accent);
    background-color: color-mix(in srgb, var(--color-accent) 12%, #1a1a1a);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.05);
    pointer-events: none;
    animation: card-scale-in 0.2s ease-out;
  }

  .fullpage-drop-content span {
    opacity: 0.7;
  }

  @keyframes overlay-fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  @keyframes card-scale-in {
    from {
      opacity: 0;
      transform: scale(0.95);
    }
    to {
      opacity: 1;
      transform: scale(1);
    }
  }

  .chat-input {
    width: 100%;
    border: none;
    background: none;
    color: var(--color-text-primary);
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
    color: var(--color-text-secondary);
    transition: color 0.2s;
    font-size: 0.875rem;
    justify-self: flex-end;
    height: 32px;
  }

  .attach-button:hover {
    color: var(--color-text-primary);
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
    color: var(--color-text-secondary);
    transition: color 0.2s;
    font-size: 0.875rem;
    justify-self: flex-end;
    height: 32px;
  }

  .thinking-toggle-button:hover {
    color: var(--color-text-primary);
  }

  .thinking-toggle-button.active {
    color: var(--color-accent);
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
    color: var(--color-text-secondary);
    transition: color 0.2s;
    padding: 0;
  }

  .send-button:hover {
    color: var(--color-text-primary);
  }

  .send-button.stop {
    color: var(--color-text-secondary);
    transition: color 0.2s;
  }

  .send-button.stop:hover {
    color: var(--color-danger-text);
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
    border-bottom: 1px solid var(--color-border);
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
    color: var(--color-text-secondary);
    transition: color 0.2s;
    padding: 0;
  }

  .remove-file:hover {
    color: var(--color-danger-text);
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
    background-color: var(--color-bg-surface-muted);
    border-radius: 0.375rem;
    border: 1px solid var(--color-border);
    cursor: help;
    transition: all 0.2s;
  }

  .token-indicator:hover {
    background-color: var(--color-bg-active);
    border-color: var(--color-border-secondary);
  }

  .token-indicator.warning {
    background-color: var(--color-warning-bg);
    border-color: var(--color-warning-border);
  }

  .token-indicator.warning:hover {
    background-color: var(--color-warning-bg-hover);
    border-color: var(--color-warning-border-hover);
  }

  .token-indicator.danger {
    background-color: var(--color-danger-indicator-bg);
    border-color: var(--color-error);
  }

  .token-indicator.danger:hover {
    background-color: #fecaca;
    border-color: var(--color-danger-text);
  }

  .token-percentage {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--color-text-primary);
  }

  .token-indicator.warning .token-percentage {
    color: var(--color-warning-text);
  }

  .token-indicator.danger .token-percentage {
    color: #991b1b;
  }

  @media (max-width: 768px) {
    .chat-input-wrapper {
      margin-bottom: 15px;
    }
  }
</style>
