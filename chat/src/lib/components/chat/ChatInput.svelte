<script lang="ts">
  import {
    Image,
    Paperclip,
    Square,
    ArrowUp,
    Mic,
    LockKeyhole,
    Info,
  } from 'lucide-svelte';
  import ModelPicker from './ModelPicker.svelte';
  import Tooltip from '../common/Tooltip.svelte';
  import ImagePreviewModal from '../common/ImagePreviewModal.svelte';
  import TranscriptionPreviewModal from '../common/TranscriptionPreviewModal.svelte';
  import FileChip from './FileChip.svelte';
  import ImageChip from './ImageChip.svelte';
  import LoadingImageChip from './LoadingImageChip.svelte';
  import DropZone from './DropZone.svelte';
  import ChatErrorBanner from './ChatErrorBanner.svelte';
  import TokenIndicator from './TokenIndicator.svelte';
  import ThinkingToggle from './ThinkingToggle.svelte';
  import { chatStore } from '#lib/chatStore.js';
  import type { AttachedFile, AttachedImage, Message } from '#lib/chatStore.js';
  import { countWords } from '#lib/chatStore.js';
  import { ANONYMOUS_MAX_CHARACTERS, modelConfig } from '#lib/models.js';
  import { isLimitedMode, signIn } from '#lib/authStore.js';
  import {
    models as modelsStore,
    modelsLoaded,
    modelsLoading,
  } from '#lib/clientStore.js';
  import type { PrivatemodeClient } from '#lib/clientStore.js';
  import {
    DEFAULT_TRANSCRIPTION_MODEL_ID,
    getAvailableTranscriptionModels,
    transcriptionModel,
  } from '#lib/transcriptionStore.js';
  import {
    classifyFiles,
    extractDocumentText,
    readImageAsAttached,
    transcribeAudio,
  } from '#lib/uploads.js';
  import { onMount } from 'svelte';
  import { resolve } from '$app/paths';
  import { showToast } from '#lib/toastStore.js';
  import { locale, t } from '#lib/i18n.js';

  export let privatemodeAIClient: PrivatemodeClient | null;
  export let currentChatId: string | null;
  export let onChatCreated: (chatId: string) => void = () => {};
  export let draftMessage: Message | null = null;
  export let message = '';
  export let anonymousCharacterCount: number;
  export let anonymousConversationExhausted: boolean;

  const EXTENDED_THINKING_KEY = 'privatemode_extended_thinking';
  const THINKING_ENABLED_KEY = 'privatemode_thinking_enabled';
  const THINKING_ENABLED_KEY_PREFIX = 'privatemode_thinking_enabled_';

  let textarea: HTMLTextAreaElement;
  let selectedModel = '';
  let extendedThinking = false;
  let thinkingEnabled = true;
  let thinkingEnabledByModel: Record<string, boolean> = {};
  let isGenerating = false;
  let abortController: AbortController | null = null;
  let fileInput: HTMLInputElement;
  let imageInput: HTMLInputElement;
  let audioInput: HTMLInputElement;
  let isUploadingDocuments = false;
  let isUploadingAudio = false;
  let attachedFiles: AttachedFile[] = [];
  let attachedImages: AttachedImage[] = [];
  let previewImage: AttachedImage | null = null;
  let previewTranscription: AttachedFile | null = null;
  let loadingImageCount = 0;
  let mounted = false;

  onMount(() => {
    mounted = true;
    const handleBeforeUnload = () => {
      abortController?.abort();
    };
    window.addEventListener('beforeunload', handleBeforeUnload);
    const savedThinking = localStorage.getItem(EXTENDED_THINKING_KEY);
    if (savedThinking) {
      extendedThinking = savedThinking === 'true';
    }
    const savedThinkingEnabled = localStorage.getItem(THINKING_ENABLED_KEY);
    if (savedThinkingEnabled) {
      thinkingEnabled = savedThinkingEnabled === 'true';
    }
    requestAnimationFrame(autoResize);

    return () => {
      mounted = false;
      window.removeEventListener('beforeunload', handleBeforeUnload);
    };
  });

  $: if (draftMessage) {
    if (draftMessage.role === 'user') {
      message = draftMessage.content;
      attachedFiles = draftMessage.attachedFiles
        ? [...draftMessage.attachedFiles]
        : [];
      attachedImages = draftMessage.attachedImages
        ? [...draftMessage.attachedImages]
        : [];
      requestAnimationFrame(() => autoResize());
    }
    draftMessage = null;
  }

  $: currentChat = currentChatId
    ? $chatStore.find((c) => c.id === currentChatId)
    : null;
  $: activeModel = currentChat?.modelId ?? selectedModel;
  $: chatHasError = currentChat?.hasError ?? false;
  $: chatModelUnavailable =
    $modelsLoaded &&
    !!currentChat?.modelId &&
    !$modelsStore.some((m) => m.id === currentChat!.modelId);
  $: chatDisabled = chatHasError || chatModelUnavailable;
  $: lastNonErrorMessage = currentChat?.messages
    ? [...currentChat.messages].reverse().find((m) => !m.isError)
    : undefined;
  $: wordCount = currentChat?.wordCount || 0;
  $: maxWords = activeModel
    ? modelConfig[activeModel]?.maxWords || 60000
    : 60000;
  $: messageWordCount = countWords(message);
  $: attachedFilesWordCount = attachedFiles.reduce(
    (total, file) => total + countWords(file.content),
    0,
  );
  $: totalWordCount = wordCount + messageWordCount + attachedFilesWordCount;
  $: wouldExceedLimit = totalWordCount > maxWords;
  $: hasMessageText = message.trim().length > 0;
  $: hasAttachments = attachedFiles.length > 0 || attachedImages.length > 0;
  $: anonymousHistoryBlocked =
    currentChat?.messages.some(
      (msg) =>
        msg.role === 'system' ||
        msg.attachedFiles?.length ||
        msg.attachedImages?.length,
    ) ?? false;
  $: anonymousBlocked =
    $isLimitedMode &&
    (anonymousCharacterCount > ANONYMOUS_MAX_CHARACTERS ||
      anonymousConversationExhausted ||
      hasAttachments ||
      anonymousHistoryBlocked);
  $: canSend =
    (hasMessageText || hasAttachments) &&
    !!activeModel &&
    !isGenerating &&
    !wouldExceedLimit &&
    !anonymousBlocked &&
    !chatDisabled &&
    !isUploading &&
    !!privatemodeAIClient;
  $: supportsFileUploads = activeModel
    ? (modelConfig[activeModel]?.supportsFileUploads ?? false)
    : true;
  $: supportsVision = activeModel
    ? (modelConfig[activeModel]?.supportsVision ?? false)
    : false;
  $: if (!supportsVision) attachedImages = [];
  $: thinkingMode = activeModel
    ? modelConfig[activeModel]?.thinkingMode
    : undefined;
  $: supportsExtendedThinking = thinkingMode === 'extended';
  $: supportsThinkingToggle = thinkingMode === 'toggle';
  $: if (
    mounted &&
    activeModel &&
    supportsThinkingToggle &&
    thinkingEnabledByModel[activeModel] === undefined
  ) {
    const savedModelThinkingEnabled = localStorage.getItem(
      `${THINKING_ENABLED_KEY_PREFIX}${activeModel}`,
    );
    thinkingEnabledByModel = {
      ...thinkingEnabledByModel,
      [activeModel]:
        savedModelThinkingEnabled === null
          ? true
          : savedModelThinkingEnabled === 'true',
    };
  }
  $: activeThinkingEnabled = activeModel
    ? (thinkingEnabledByModel[activeModel] ?? true)
    : thinkingEnabled;
  $: thinkingButtonActive = supportsExtendedThinking
    ? extendedThinking
    : supportsThinkingToggle
      ? activeThinkingEnabled
      : false;
  $: isUploading = isUploadingDocuments || isUploadingAudio;
  $: availableTranscriptionModels =
    getAvailableTranscriptionModels($modelsStore);
  $: selectedTranscriptionModel =
    availableTranscriptionModels.find((m) => m.id === $transcriptionModel)
      ?.id ??
    availableTranscriptionModels.find(
      (m) => m.id === DEFAULT_TRANSCRIPTION_MODEL_ID,
    )?.id ??
    availableTranscriptionModels[0]?.id ??
    null;
  $: attachButtonTitle = $isLimitedMode
    ? $t('chat.signInUploads')
    : isUploadingDocuments
      ? $t('chat.uploadingDocuments')
      : isGenerating
        ? $t('chat.cannotAttachDocumentsGenerating')
        : !supportsFileUploads
          ? $t('chat.noDocumentUploadSupport')
          : $t('chat.attachDocuments');
  $: imageButtonTitle = $isLimitedMode
    ? $t('chat.signInUploads')
    : isGenerating
      ? $t('chat.cannotAttachImagesGenerating')
      : !supportsVision
        ? $t('chat.noImageSupport')
        : $t('chat.attachImages');
  $: audioButtonTitle = $isLimitedMode
    ? $t('chat.signInUploads')
    : isUploadingAudio
      ? $t('chat.uploadingAudio')
      : isGenerating
        ? $t('chat.cannotAttachAudioGenerating')
        : !supportsFileUploads
          ? $t('chat.noAudioUploadSupport')
          : !selectedTranscriptionModel
            ? $modelsLoaded
              ? $t('chat.noTranscriptionModel')
              : $t('chat.transcriptionModelsLoading')
            : $t('chat.attachAudio');

  function autoResize() {
    if (textarea) {
      textarea.style.height = 'auto';
      textarea.style.height = textarea.scrollHeight - 10 + 'px';
    }
  }

  async function sendMessage() {
    if (!canSend || !activeModel || !privatemodeAIClient) return;

    let chatId = currentChatId;
    if (!chatId) {
      chatId = chatStore.createChat();
      onChatCreated(chatId);
    }

    const userMessage = message.trim();
    const filesToSend = [...attachedFiles];
    const imagesToSend = [...attachedImages];
    message = '';
    attachedFiles = [];
    attachedImages = [];

    if (textarea) {
      textarea.style.height = 'auto';
      textarea.focus();
    }

    const activeModelConfig = modelConfig[activeModel];
    const thinkingToggleParam = activeModelConfig?.thinkingToggleParam;
    const shouldSendThinkingToggle =
      supportsThinkingToggle &&
      thinkingToggleParam &&
      (activeModelConfig?.thinkingToggleAlwaysSend || !activeThinkingEnabled);

    isGenerating = true;
    abortController = new AbortController();

    try {
      await chatStore.send({
        client: privatemodeAIClient,
        chatId,
        modelId: activeModel,
        userMessage,
        attachedFiles: filesToSend.length > 0 ? filesToSend : undefined,
        attachedImages: imagesToSend.length > 0 ? imagesToSend : undefined,
        systemPrompt: activeModelConfig?.systemPrompt[$locale],
        reasoningEffort: supportsExtendedThinking
          ? extendedThinking
            ? activeModelConfig?.reasoningEffort?.extended
            : activeModelConfig?.reasoningEffort?.normal
          : undefined,
        thinkingToggle:
          shouldSendThinkingToggle && thinkingToggleParam
            ? { paramName: thinkingToggleParam, enabled: activeThinkingEnabled }
            : undefined,
        signal: abortController.signal,
      });
    } catch (error) {
      if (
        abortController.signal.aborted ||
        (error instanceof Error && error.name === 'AbortError')
      ) {
        console.log('Generation cancelled by user');
      } else {
        console.error('Error generating response:', error);
      }
    } finally {
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

  function showErrorToast(message: string, error?: unknown) {
    const isAuthError =
      error instanceof Error && (error as { status?: number }).status === 401;
    showToast(isAuthError ? `${message} ${$t('error.authSuffix')}` : message, {
      type: 'error',
      link: isAuthError
        ? { href: resolve('/settings'), label: $t('error.updateApiKey') }
        : undefined,
    });
  }

  function readImageFile(file: globalThis.File, fallbackName?: string) {
    if ($isLimitedMode) return;
    loadingImageCount++;
    readImageAsAttached(file, fallbackName)
      .then((image) => {
        attachedImages = [...attachedImages, image];
      })
      .catch(() => {
        console.error('Failed to read image:', file.name);
        showErrorToast(
          $t('error.readImage', {
            name: file.name || $t('error.pastedImage'),
          }),
        );
      })
      .finally(() => {
        loadingImageCount--;
      });
  }

  async function uploadAudioFiles(files: globalThis.File[]) {
    if ($isLimitedMode || files.length === 0) return;
    const client = privatemodeAIClient;
    if (!client) {
      showErrorToast($t('error.connecting'));
      return;
    }
    const model = selectedTranscriptionModel;
    if (!model) {
      showErrorToast(
        $modelsLoading
          ? $t('error.transcriptionLoading')
          : $t('error.noTranscriptionModel'),
      );
      return;
    }
    isUploadingAudio = true;
    try {
      for (const f of files) {
        const attached = await transcribeAudio(client, f, model);
        attachedFiles = [...attachedFiles, attached];
      }
    } catch (error) {
      console.error('Error uploading audio:', error);
      showErrorToast(
        $t('error.uploadAudio', {
          error: error instanceof Error ? error.message : $t('error.unknown'),
        }),
        error,
      );
    } finally {
      isUploadingAudio = false;
    }
  }

  async function uploadDocumentFiles(files: globalThis.File[]) {
    if ($isLimitedMode || files.length === 0) return;
    const client = privatemodeAIClient;
    if (!client) {
      showErrorToast($t('error.connecting'));
      return;
    }
    isUploadingDocuments = true;
    try {
      for (const f of files) {
        const attached = await extractDocumentText(client, f);
        attachedFiles = [...attachedFiles, attached];
      }
    } catch (error) {
      console.error('Error uploading file:', error);
      showErrorToast(
        $t('error.uploadFile', {
          error: error instanceof Error ? error.message : $t('error.unknown'),
        }),
        error,
      );
    } finally {
      isUploadingDocuments = false;
    }
  }

  async function handlePaste(event: ClipboardEvent) {
    if (isGenerating || isUploading) return;
    const items = event.clipboardData?.items;
    if (!items) return;
    const files: globalThis.File[] = [];
    for (const item of items) {
      if (item.kind !== 'file') continue;
      const file = item.getAsFile();
      if (file) files.push(file);
    }
    if (files.length === 0) return;
    event.preventDefault();
    if ($isLimitedMode) {
      showToast($t('chat.signInUploads'));
      return;
    }

    const { images, audio, documents, unsupportedImages } = classifyFiles(
      files,
      supportsVision,
    );

    if (unsupportedImages.length > 0) {
      showErrorToast($t('error.unsupportedImages'));
    }
    for (const f of images) readImageFile(f);
    if (supportsFileUploads) {
      await uploadAudioFiles(audio);
      await uploadDocumentFiles(documents);
    }
  }

  async function handleFileSelect(event: Event) {
    const target = event.target as HTMLInputElement;
    const files = target.files;
    if (!files || files.length === 0) return;
    try {
      await uploadDocumentFiles(Array.from(files));
    } finally {
      target.value = '';
    }
  }

  async function handleAudioSelect(event: Event) {
    const target = event.target as HTMLInputElement;
    const files = target.files;
    if (!files || files.length === 0) return;
    try {
      await uploadAudioFiles(Array.from(files));
    } finally {
      target.value = '';
    }
  }

  function removeFile(index: number) {
    attachedFiles = attachedFiles.filter((_, i) => i !== index);
  }

  function handleImageSelect(event: Event) {
    const target = event.target as HTMLInputElement;
    const files = target.files;
    if (!files || files.length === 0) return;
    for (const file of files) {
      readImageFile(file);
    }
    target.value = '';
  }

  function removeImage(index: number) {
    attachedImages = attachedImages.filter((_, i) => i !== index);
  }

  function toggleThinking() {
    if (supportsExtendedThinking) {
      extendedThinking = !extendedThinking;
      localStorage.setItem(EXTENDED_THINKING_KEY, String(extendedThinking));
      return;
    }

    if (supportsThinkingToggle) {
      const nextThinkingEnabled = !activeThinkingEnabled;
      if (activeModel) {
        thinkingEnabledByModel = {
          ...thinkingEnabledByModel,
          [activeModel]: nextThinkingEnabled,
        };
        localStorage.setItem(
          `${THINKING_ENABLED_KEY_PREFIX}${activeModel}`,
          String(nextThinkingEnabled),
        );
        return;
      }
      thinkingEnabled = nextThinkingEnabled;
      localStorage.setItem(THINKING_ENABLED_KEY, String(nextThinkingEnabled));
    }
  }

  async function handleDroppedFiles(files: globalThis.File[]) {
    if (isGenerating || isUploading) return;
    if (files.length === 0) return;
    if ($isLimitedMode) {
      showToast($t('chat.signInUploads'));
      return;
    }

    const { images, audio, documents, unsupportedImages } = classifyFiles(
      files,
      supportsVision,
    );

    if (unsupportedImages.length > 0) {
      showErrorToast($t('error.unsupportedImages'));
    }

    for (const f of images) readImageFile(f);

    if (supportsFileUploads) {
      await uploadAudioFiles(audio);
      await uploadDocumentFiles(documents);
    }
  }
</script>

<DropZone
  enabled={(supportsFileUploads || supportsVision) &&
    !isGenerating &&
    !isUploading}
  onDrop={handleDroppedFiles}
/>

{#if chatDisabled}
  <div
    class="chat-input-wrapper"
    data-models-loaded={$modelsLoaded}
    role="region"
    aria-label={$t('chat.messageInput')}
  >
    <ChatErrorBanner
      {chatModelUnavailable}
      unavailableModelId={currentChat?.modelId}
      {currentChatId}
      {lastNonErrorMessage}
      {onChatCreated}
      onDraftMessage={(msg) => (draftMessage = msg)}
    />
  </div>
{:else}
  <div
    class="chat-input-wrapper"
    data-models-loaded={$modelsLoaded}
    role="region"
    aria-label={$t('chat.messageInput')}
  >
    {#if attachedFiles.length > 0 || attachedImages.length > 0 || loadingImageCount > 0}
      <div class="attached-files">
        {#each Array.from({ length: loadingImageCount }, (_, i) => i) as i (i)}
          <LoadingImageChip />
        {/each}
        {#each attachedImages as image, index (image)}
          <ImageChip
            {image}
            onPreview={(img) => (previewImage = img)}
            onRemove={() => removeImage(index)}
          />
        {/each}
        {#each attachedFiles as file, index (file)}
          <FileChip
            {file}
            onPreview={(f) => (previewTranscription = f)}
            onRemove={() => removeFile(index)}
          />
        {/each}
      </div>
    {/if}
    <textarea
      bind:this={textarea}
      bind:value={message}
      oninput={autoResize}
      onkeydown={handleKeyDown}
      onpaste={handlePaste}
      placeholder={$t('chat.typeMessage')}
      aria-label={$t('chat.typeMessageLabel')}
      class="chat-input"
      rows="1"
      disabled={isGenerating}
      aria-describedby={anonymousBlocked ? 'anonymous-limits' : undefined}
    ></textarea>
    <div class="button-row">
      <!--
      In theory, Unstructured should also support images [1], but I
      haven't been able to get it working reliably.
      [1]: https://docs.unstructured.io/ui/supported-file-types
      -->
      <input
        type="file"
        multiple
        accept=".md,.html,.htm,.csv,.txt,.text,.pdf,.doc,.docx,.ppt,.pptx,.xls,.xlsx,.odt,.ods,.odp,.rtf"
        bind:this={fileInput}
        onchange={handleFileSelect}
        style="display: none;"
      />
      <input
        type="file"
        multiple
        accept="image/png,image/jpeg,image/gif,image/webp,image/avif"
        bind:this={imageInput}
        onchange={handleImageSelect}
        style="display: none;"
      />
      <input
        type="file"
        multiple
        accept="audio/*"
        bind:this={audioInput}
        onchange={handleAudioSelect}
        style="display: none;"
      />
      <div class="left-buttons">
        {#if $isLimitedMode}
          <Tooltip text={$t('chat.signInUploads')}>
            <button
              class="attach-button guest-attach"
              type="button"
              onclick={signIn}
              disabled={isGenerating}
              aria-label={$t('chat.signInUploads')}
            >
              <Paperclip size={18} />
              <span class="attach-label">{$t('chat.attachFiles')}</span>
              <LockKeyhole size={12} />
            </button>
          </Tooltip>
        {:else}
          <Tooltip text={attachButtonTitle}>
            <button
              class="attach-button"
              type="button"
              disabled={isGenerating ||
                isUploadingDocuments ||
                !supportsFileUploads}
              onclick={() => fileInput?.click()}
            >
              <Paperclip size={18} />
              <span class="attach-label"
                >{isUploadingDocuments
                  ? $t('chat.uploading')
                  : $t('chat.attachDocuments')}</span
              >
            </button>
          </Tooltip>
          <Tooltip text={audioButtonTitle}>
            <button
              class="attach-button"
              type="button"
              disabled={isGenerating ||
                isUploadingAudio ||
                !supportsFileUploads ||
                !selectedTranscriptionModel}
              onclick={() => audioInput?.click()}
            >
              <Mic size={18} />
              <span class="attach-label"
                >{isUploadingAudio
                  ? $t('chat.uploading')
                  : $t('chat.attachAudio')}</span
              >
            </button>
          </Tooltip>
          {#if supportsVision}
            <Tooltip text={imageButtonTitle}>
              <button
                class="attach-button"
                type="button"
                disabled={isGenerating}
                onclick={() => imageInput?.click()}
              >
                <Image size={18} />
                <span class="attach-label">{$t('chat.attachImages')}</span>
              </button>
            </Tooltip>
          {/if}
        {/if}
        {#if supportsExtendedThinking || supportsThinkingToggle}
          <ThinkingToggle
            active={thinkingButtonActive}
            disabled={isGenerating}
            onToggle={toggleThinking}
          />
        {/if}
      </div>
      <div class="right-buttons">
        <div class="model-controls">
          <TokenIndicator
            {totalWordCount}
            {maxWords}
          />
          <ModelPicker
            {privatemodeAIClient}
            bind:selectedModel
            disabled={!!currentChat?.modelId}
            lockedModel={currentChat?.modelId}
          />
        </div>
        {#if isGenerating}
          <Tooltip text={$t('chat.stopGenerating')}>
            <button
              class="send-button stop"
              type="button"
              aria-label={$t('chat.stopGenerating')}
              onclick={stopGeneration}
            >
              <Square size={20} />
            </button>
          </Tooltip>
        {:else}
          <Tooltip
            text={anonymousBlocked
              ? $t('chat.signInContinue')
              : wouldExceedLimit
                ? $t('chat.tokenLimit')
                : $t('chat.sendMessage')}
          >
            <button
              class="send-button"
              type="button"
              aria-label={$t('chat.sendMessage')}
              onclick={sendMessage}
              disabled={!canSend}
            >
              <ArrowUp size={20} />
            </button>
          </Tooltip>
        {/if}
      </div>
    </div>
  </div>
  {#if anonymousBlocked}
    <div
      id="anonymous-limits"
      class="anonymous-limits"
    >
      <Info size={14} />
      <span
        >{hasAttachments || anonymousHistoryBlocked
          ? $t('chat.guestAttachments')
          : $t('chat.guestLimitReached')}.</span
      >
      <button
        type="button"
        onclick={signIn}>{$t('chat.signInContinue')}</button
      >
    </div>
  {/if}
{/if}

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
  .chat-input-wrapper {
    margin-top: auto;
    margin-bottom: 8px;
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
    border-color: var(--color-accent);
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

  .chat-input::placeholder {
    color: var(--color-text-secondary);
    opacity: 1;
  }

  .anonymous-limits {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.25rem;
    padding: 0 0.25rem;
    margin-bottom: 8px;
    color: var(--color-text-secondary);
    font-size: 0.75rem;
  }

  .anonymous-limits button {
    border: none;
    padding: 0;
    background: none;
    color: var(--color-accent);
    text-decoration: underline;
    font: inherit;
    cursor: pointer;
  }

  .guest-attach {
    gap: 0.375rem;
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

  .model-controls {
    display: flex;
    align-items: stretch;
    gap: 0.5rem;
    height: 100%;
  }

  /* On mobile the attach labels are hidden visually but kept in the
     accessibility tree so the buttons retain their accessible names. */
  @media (max-width: 768px) {
    .attach-label {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }
  }
</style>
