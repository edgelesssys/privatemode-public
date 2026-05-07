<script lang="ts">
  import {
    FileUp,
    File,
    Image,
    X,
    Paperclip,
    Brain,
    Square,
    ArrowUp,
    Mic,
    Copy,
  } from 'lucide-svelte';
  import ModelPicker from './ModelPicker.svelte';
  import Tooltip from './Tooltip.svelte';
  import { chatStore } from '$lib/chatStore';
  import type { AttachedFile, AttachedImage, Message } from '$lib/chatStore';
  import { countWords } from '$lib/chatStore';
  import { modelConfig } from '$lib/models';
  import {
    models as modelsStore,
    modelsLoaded,
    modelsLoading,
  } from '$lib/clientStore';
  import {
    DEFAULT_TRANSCRIPTION_MODEL_ID,
    getAvailableTranscriptionModels,
    transcriptionModel,
  } from '$lib/transcriptionStore';
  import { onMount } from 'svelte';
  import type { PrivatemodeAI } from 'privatemode-ai';
  import { showToast } from '$lib/toastStore';

  export let privatemodeAIClient: PrivatemodeAI | null;
  export let currentChatId: string | null;
  export let onChatCreated: (chatId: string) => void = () => {};
  export let draftMessage: Message | null = null;

  const EXTENDED_THINKING_KEY = 'privatemode_extended_thinking';
  const THINKING_ENABLED_KEY = 'privatemode_thinking_enabled';
  const THINKING_ENABLED_KEY_PREFIX = 'privatemode_thinking_enabled_';

  let message = '';
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
  let isDragging = false;
  let dragCounter = 0;
  let previewImage: AttachedImage | null = null;
  let previewTranscription: AttachedFile | null = null;
  let loadingImageCount = 0;
  let isUnloading = false;
  let mounted = false;

  onMount(() => {
    mounted = true;
    const handleBeforeUnload = () => {
      isUnloading = true;
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

    const handleWindowDragEnter = (e: DragEvent) => {
      if (
        (!supportsFileUploads && !supportsVision) ||
        isGenerating ||
        isUploading
      )
        return;
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
      mounted = false;
      window.removeEventListener('beforeunload', handleBeforeUnload);
      window.removeEventListener('dragenter', handleWindowDragEnter);
      window.removeEventListener('dragleave', handleWindowDragLeave);
      window.removeEventListener('dragover', handleWindowDragOver);
      window.removeEventListener('drop', handleWindowDrop);
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
  $: chatHasMessages = (currentChat?.messages.length ?? 0) > 0;
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
  $: totalUsagePercentage = Math.min((totalWordCount / maxWords) * 100, 100);
  $: wouldExceedLimit = totalWordCount > maxWords;
  $: hasMessageText = message.trim().length > 0;
  $: hasAttachments = attachedFiles.length > 0 || attachedImages.length > 0;
  $: canSend =
    (hasMessageText || hasAttachments) &&
    !!activeModel &&
    !isGenerating &&
    !wouldExceedLimit &&
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
  $: thinkingTooltip =
    supportsExtendedThinking || supportsThinkingToggle
      ? `${thinkingButtonActive ? 'Disable' : 'Enable'} (extended) thinking mode. This lets the model reason more deeply about the request before providing a response`
      : '';
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
  $: attachButtonTitle = isUploadingDocuments
    ? 'Uploading documents...'
    : isGenerating
      ? 'Cannot attach documents while generating'
      : !supportsFileUploads
        ? 'Selected model does not support document uploads'
        : 'Attach documents';
  $: imageButtonTitle = isGenerating
    ? 'Cannot attach images while generating'
    : !supportsVision
      ? 'Selected model does not support images'
      : 'Attach images';
  $: audioButtonTitle = isUploadingAudio
    ? 'Uploading audio...'
    : isGenerating
      ? 'Cannot attach audio while generating'
      : !supportsFileUploads
        ? 'Selected model does not support audio uploads'
        : !selectedTranscriptionModel
          ? $modelsLoaded
            ? 'No transcription model is available'
            : 'Transcription models are still loading'
          : 'Attach audio';

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

    chatStore.setModelId(chatId, activeModel);
    chatStore.addMessage(chatId, {
      role: 'user',
      content: userMessage,
      attachedFiles: filesToSend.length > 0 ? filesToSend : undefined,
      attachedImages: imagesToSend.length > 0 ? imagesToSend : undefined,
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

      type ContentPart =
        | { type: 'text'; text: string }
        | { type: 'image_url'; image_url: { url: string } };
      type ApiMessage = {
        role: string;
        content: string | ContentPart[];
      };
      const apiMessages: ApiMessage[] = [];
      const systemPrompt = modelConfig[activeModel]?.systemPrompt;
      if (systemPrompt) {
        apiMessages.push({ role: 'system', content: systemPrompt });
      }
      for (const msg of messagesToSend) {
        if (msg.attachedFiles && msg.attachedFiles.length > 0) {
          for (const file of msg.attachedFiles) {
            const content =
              file.kind === 'audio-transcription'
                ? `The user attached an audio file named "${file.name}". The following is the speech-to-text transcription of that audio:\n\n${file.content}`
                : `[File: ${file.name}]\n\n${file.content}`;
            apiMessages.push({
              role: msg.role,
              content,
            });
          }
        }
        const hasImages = msg.attachedImages && msg.attachedImages.length > 0;
        if (hasImages) {
          const parts: ContentPart[] = [];
          for (const img of msg.attachedImages!) {
            parts.push({
              type: 'image_url',
              image_url: { url: img.dataUrl },
            });
          }
          if (msg.content.trim()) {
            parts.push({ type: 'text', text: msg.content });
          }
          apiMessages.push({ role: msg.role, content: parts });
        } else if (msg.content.trim()) {
          apiMessages.push({ role: msg.role, content: msg.content });
        }
      }
      const requestBody: Record<string, unknown> = {
        model: activeModel,
        messages: apiMessages,
        stream: true,
      };
      if (supportsExtendedThinking) {
        requestBody.reasoning_effort = extendedThinking ? 'high' : 'medium';
      }
      const activeModelConfig = activeModel
        ? modelConfig[activeModel]
        : undefined;
      const thinkingToggleParam = activeModelConfig?.thinkingToggleParam;
      const shouldSendThinkingToggle =
        supportsThinkingToggle &&
        thinkingToggleParam &&
        (activeModelConfig?.thinkingToggleAlwaysSend || !activeThinkingEnabled);
      if (shouldSendThinkingToggle) {
        requestBody.chat_template_kwargs = {
          [thinkingToggleParam]: activeThinkingEnabled,
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
      if (
        isUnloading ||
        (error instanceof Error && error.name === 'AbortError')
      ) {
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
          true,
        );
        chatStore.setChatError(chatId!, true);
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

  const SUPPORTED_IMAGE_TYPES = [
    'image/png',
    'image/jpeg',
    'image/gif',
    'image/webp',
    'image/avif',
  ];
  const SUPPORTED_AUDIO_TYPES = [
    'audio/aac',
    'audio/flac',
    'audio/m4a',
    'audio/mp3',
    'audio/mpeg',
    'audio/mp4',
    'audio/ogg',
    'audio/wav',
    'audio/webm',
    'audio/x-m4a',
  ];
  const SUPPORTED_AUDIO_EXTENSIONS = [
    '.aac',
    '.flac',
    '.m4a',
    '.mp3',
    '.mp4',
    '.mpeg',
    '.mpga',
    '.oga',
    '.ogg',
    '.wav',
    '.webm',
  ];
  const MAX_AUDIO_FILE_SIZE_BYTES = 50 * 1024 * 1024;

  function isSupportedImage(type: string): boolean {
    return SUPPORTED_IMAGE_TYPES.includes(type);
  }

  function isSupportedAudio(file: globalThis.File): boolean {
    const normalizedName = file.name.toLowerCase();
    return (
      SUPPORTED_AUDIO_TYPES.includes(file.type) ||
      SUPPORTED_AUDIO_EXTENSIONS.some((ext) => normalizedName.endsWith(ext))
    );
  }

  async function attachDocumentFile(file: globalThis.File) {
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
  }

  async function attachAudioFile(file: globalThis.File) {
    if (file.size > MAX_AUDIO_FILE_SIZE_BYTES) {
      throw new Error(`${file.name} exceeds the 50 MB audio upload limit`);
    }

    const model = selectedTranscriptionModel;
    if (!model) {
      throw new Error('No transcription model is available');
    }

    const content = new Uint8Array(await file.arrayBuffer());
    const transcription = (await privatemodeAIClient!.transcribeAudio(
      { name: file.name, content, contentType: file.type },
      { model },
    )) as { text?: string };

    const text = transcription.text?.trim();
    if (!text) {
      throw new Error(`${file.name} did not produce a transcription`);
    }

    attachedFiles = [
      ...attachedFiles,
      {
        name: file.name,
        content: text,
        kind: 'audio-transcription',
      },
    ];
  }

  function showErrorToast(message: string, error?: unknown) {
    let text = message;
    if (error instanceof Error && (error as any).status === 401) {
      text +=
        ' Your API key may be invalid or expired. <a href="/settings">Update your API key</a>.';
    }
    showToast(text, { type: 'error', dangerouslyRenderHTML: true });
  }

  function readImageFile(file: globalThis.File, fallbackName?: string) {
    loadingImageCount++;
    const reader = new FileReader();
    reader.onload = () => {
      attachedImages = [
        ...attachedImages,
        {
          id: crypto.randomUUID(),
          name: file.name || fallbackName || 'pasted-image.png',
          dataUrl: reader.result as string,
        },
      ];
      loadingImageCount--;
    };
    reader.onerror = () => {
      loadingImageCount--;
      console.error('Failed to read image:', file.name);
      showErrorToast(`Failed to read image: ${file.name || 'pasted image'}.`);
    };
    reader.readAsDataURL(file);
  }

  async function uploadAudioFiles(files: globalThis.File[]) {
    if (files.length === 0) return;
    if (!selectedTranscriptionModel) {
      showErrorToast(
        $modelsLoading
          ? 'Transcription models are still loading. Please try again in a moment.'
          : 'No transcription model is available.',
      );
      return;
    }
    isUploadingAudio = true;
    try {
      for (const f of files) {
        await attachAudioFile(f);
      }
    } catch (error) {
      console.error('Error uploading audio:', error);
      showErrorToast(
        `Failed to upload audio: ${error instanceof Error ? error.message : 'Unknown error'}.`,
        error,
      );
    } finally {
      isUploadingAudio = false;
    }
  }

  async function uploadDocumentFiles(files: globalThis.File[]) {
    if (files.length === 0) return;
    isUploadingDocuments = true;
    try {
      for (const f of files) {
        await attachDocumentFile(f);
      }
    } catch (error) {
      console.error('Error uploading file:', error);
      showErrorToast(
        `Failed to upload file: ${error instanceof Error ? error.message : 'Unknown error'}.`,
        error,
      );
    } finally {
      isUploadingDocuments = false;
    }
  }

  function classifyFiles(files: Iterable<globalThis.File>): {
    images: globalThis.File[];
    audio: globalThis.File[];
    documents: globalThis.File[];
    unsupportedImages: globalThis.File[];
  } {
    const images: globalThis.File[] = [];
    const audio: globalThis.File[] = [];
    const documents: globalThis.File[] = [];
    const unsupportedImages: globalThis.File[] = [];
    for (const f of files) {
      if (isSupportedImage(f.type)) {
        if (supportsVision) images.push(f);
        else unsupportedImages.push(f);
      } else if (isSupportedAudio(f)) {
        audio.push(f);
      } else {
        documents.push(f);
      }
    }
    return { images, audio, documents, unsupportedImages };
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

    const { images, audio, documents, unsupportedImages } =
      classifyFiles(files);

    if (unsupportedImages.length > 0) {
      showErrorToast(
        'The selected model does not support image inputs. Please select a vision-enabled model to attach images.',
      );
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

  function copyText(text: string) {
    navigator.clipboard.writeText(text).catch(() => {});
  }

  async function handleDrop(event: DragEvent) {
    event.preventDefault();
    isDragging = false;

    if (isGenerating || isUploading) return;

    const files = event.dataTransfer?.files;
    if (!files || files.length === 0) return;

    const { images, audio, documents, unsupportedImages } =
      classifyFiles(files);

    if (unsupportedImages.length > 0) {
      showErrorToast(
        'The selected model does not support image inputs. Please select a vision-enabled model to attach images.',
      );
    }

    for (const f of images) readImageFile(f);

    if (supportsFileUploads) {
      await uploadAudioFiles(audio);
      await uploadDocumentFiles(documents);
    }
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key !== 'Escape') return;
    previewImage = null;
    previewTranscription = null;
  }}
/>

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

{#if chatDisabled}
  <div
    class="chat-input-wrapper"
    role="region"
  >
    <div class="chat-error-banner">
      {#if chatModelUnavailable}
        The model used in this chat ({currentChat?.modelId}) is no longer
        available.
      {:else}
        This chat can no longer be used due to an error.
      {/if}
      <button
        class="new-chat-link"
        type="button"
        onclick={() => {
          const id = chatStore.createChat();
          onChatCreated(id);
        }}
      >
        Start a new chat
      </button>
      {#if lastNonErrorMessage}
        or
        <button
          class="new-chat-link"
          type="button"
          onclick={() => {
            if (!currentChatId || !lastNonErrorMessage) return;
            const includeInChat = lastNonErrorMessage.role !== 'user';
            const result = chatStore.branchChat(
              currentChatId,
              lastNonErrorMessage.id,
              includeInChat,
            );
            if (result) {
              if (result.message.role === 'user') {
                draftMessage = result.message;
              }
              onChatCreated(result.chatId);
            }
          }}
        >
          branch off from the last message
        </button>
      {/if}
      to continue.
    </div>
  </div>
{:else}
  <div
    class="chat-input-wrapper"
    role="region"
  >
    {#if attachedFiles.length > 0 || attachedImages.length > 0 || loadingImageCount > 0}
      <div class="attached-files">
        {#each Array(loadingImageCount) as _}
          <div class="image-chip loading-image-chip">
            <div class="image-thumbnail-placeholder">
              <div class="loading-spinner"></div>
            </div>
            <span class="file-name">Loading...</span>
          </div>
        {/each}
        {#each attachedImages as image, index}
          <div class="image-chip">
            <button
              class="image-thumbnail-button"
              type="button"
              onclick={() => (previewImage = image)}
            >
              <img
                src={image.dataUrl}
                alt={image.name}
                class="image-thumbnail"
              />
            </button>
            <span class="file-name">{image.name}</span>
            <Tooltip text="Remove image">
              <button
                class="remove-file"
                onclick={() => removeImage(index)}
                type="button"
              >
                <X size={16} />
              </button>
            </Tooltip>
          </div>
        {/each}
        {#each attachedFiles as file, index}
          <div class="file-chip">
            {#if file.kind === 'audio-transcription'}
              <button
                class="file-preview-button"
                type="button"
                onclick={() => (previewTranscription = file)}
              >
                <Mic size={16} />
                <span class="file-name">{file.name}</span>
              </button>
            {:else}
              <File size={16} />
              <span class="file-name">{file.name}</span>
            {/if}
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
      onpaste={handlePaste}
      placeholder="Type a message..."
      class="chat-input"
      rows="1"
      disabled={isGenerating}
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
                ? 'Uploading...'
                : 'Attach documents'}</span
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
              >{isUploadingAudio ? 'Uploading...' : 'Attach audio'}</span
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
              <span class="attach-label">Attach images</span>
            </button>
          </Tooltip>
        {/if}
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
            disabled={chatHasMessages}
            lockedModel={currentChat?.modelId}
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
              disabled={!canSend}
            >
              <ArrowUp size={20} />
            </button>
          </Tooltip>
        {/if}
      </div>
    </div>
  </div>
{/if}

{#if previewImage}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="image-preview-overlay"
    onclick={() => (previewImage = null)}
  >
    <button
      class="image-preview-close"
      type="button"
      onclick={() => (previewImage = null)}
    >
      <X size={24} />
    </button>

    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="image-preview-container"
      onclick={(e) => e.stopPropagation()}
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
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="image-preview-overlay"
    onclick={() => (previewTranscription = null)}
  >
    <button
      class="image-preview-close"
      type="button"
      onclick={() => (previewTranscription = null)}
    >
      <X size={24} />
    </button>

    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="transcription-preview-container"
      onclick={(e) => e.stopPropagation()}
    >
      <div class="transcription-preview-header">
        <h2>{previewTranscription.name}</h2>
        <button
          class="transcription-copy-button"
          type="button"
          onclick={() => copyText(previewTranscription!.content)}
          aria-label="Copy transcription"
        >
          <Copy size={16} />
        </button>
      </div>
      <p>{previewTranscription.content}</p>
    </div>
  </div>
{/if}

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

  .image-thumbnail-placeholder {
    width: 32px;
    height: 32px;
    border-radius: 0.25rem;
    background-color: var(--color-bg-active);
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .loading-spinner {
    width: 16px;
    height: 16px;
    border: 2px solid var(--color-border);
    border-top-color: var(--color-text-secondary);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
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

  .transcription-preview-container p {
    margin: 0;
    white-space: pre-wrap;
    line-height: 1.6;
    color: var(--color-text-secondary);
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

  @media (max-width: 768px) {
    .attach-label {
      display: none;
    }
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
