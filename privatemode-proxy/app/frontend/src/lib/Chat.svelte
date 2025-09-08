<script lang="ts">
  import {
    saveChatStore,
    chatsStorage,
    addMessage,
    updateChatSettings,
    checkStateChange,
    showSetChatSettings,
    submitExitingPromptsNow,
    continueMessage,
    getMessage,
    currentChatMessages,
    setCurrentChat,
    currentChatId,
    preferredModelStorage,
    preferredReasoningStorage
  } from './Storage.svelte'
  import {
    type Message,
    type Chat,
    type ChatSettings
  } from './Types.svelte'
  import Messages from './Messages.svelte'
  import { restartProfile, getProfiles, getProfile, setSystemPrompt } from './Profiles.svelte'
  import { afterUpdate, onMount, onDestroy } from 'svelte'
  import Fa from 'svelte-fa/src/fa.svelte'
  import {
    faCommentSlash,
    faCircleCheck,
    faSpinner,
    faChevronDown
  } from '@fortawesome/free-solid-svg-icons/index'
  import FileUploadButton from './FileUploadButton.svelte'
  import { v4 as uuidv4 } from 'uuid'
  import { get } from 'svelte/store'
  import { autoGrowInputOnEvent, scrollToBottom, sizeTextElements } from './Util.svelte'
  import ChatSettingsModal from './ChatSettingsModal.svelte'
  import Footer from './Footer.svelte'
  import { ChatRequest } from './ChatRequest.svelte'
  import send from '../assets/send.svg'
  import { FILE_MESSAGE_PREFIX } from './FileUploadService.svelte'
  import logoSmall from '../assets/logo-small.svg'
  import { TestChatController } from './SmokeTest'

  export let params = { chatId: '' }
  const chatId: number = parseInt(params.chatId)

  let chatRequest = new ChatRequest()
  let input: HTMLTextAreaElement
  let recognition: any = null
  let recording = false
  let lastSubmitRecorded = false
  let isUploading = false
  let uploadStatus: { progress: number; filename: string | null } = { progress: 0, filename: null }
  let uploadErrorMessage = ''
  let fileUploadButton: any // reference to the FileUploadButton component
  let selectedFile: File | null = null
  let fileUploaded = false // Track if a file has been successfully uploaded
  let isDropdownOpen:boolean = false
  let dropdownElement: HTMLElement

  $: chat = $chatsStorage.find((chat) => chat.id === chatId) as Chat
  $: chatSettings = chat?.settings
  $: if (chat) {
    fileUploaded = chat.hasUploadedFile === true
  }
  let showSettingsModal: () => void
  let promptInput = ''

  $: isSendButtonDisabled = (promptInput === '' && !fileUploaded) || isUploading

  // Define model type for the dropdown
  type DropdownModel = {
    id: string;
    name: string;
    label: string;
    profileKey: string; // Add reference to the profile key
    parameters?: {
      name: string;
      options: {
        value: string;
        name: string;
        selected: boolean;
      }[];
    }[];
  };

  // Get models from profiles
  let profilesData: Record<string, ChatSettings> = {}
  let models: DropdownModel[] = []
  
  const loadModels = async () => {
    profilesData = await getProfiles()
    models = Object.entries(profilesData).map(([profileKey, profile]: [string, ChatSettings]) => {
      const model: DropdownModel = {
        id: profile.modelConfig?.id || '',
        name: profile.modelConfig?.displayName || '',
        label: profile.modelConfig?.displaySubtitle || '',
        profileKey // Store the profile key
      }
  
      // Add reasoning parameters if they exist
      if (profile.modelConfig?.reasoningOptions) {
        const savedReasoning = get(preferredReasoningStorage)
        const preferredReasoningValue = savedReasoning[model.id]

        model.parameters = [
          {
            name: 'Reasoning',
            options: profile.modelConfig.reasoningOptions.map((option: any, index: number) => ({
              value: option.value,
              name: option.displayName,
              selected: preferredReasoningValue ? option.value === preferredReasoningValue : index === 0
            }))
          }
        ]
      }
  
      return model
    }).filter(model => model.id && model.name) // Filter out invalid models

    TestChatController.chatReady = models.length > 0
  }
  
  onMount(() => {
    loadModels()
  })

  // Handle click outside dropdown to close it
  const handleClickOutside = (event: Event) => {
    if (dropdownElement && isDropdownOpen && !dropdownElement.contains(event.target as Node)) {
      isDropdownOpen = false
    }
  }

  // Add event listeners for click outside
  $: if (typeof window !== 'undefined') {
    if (isDropdownOpen) {
      document.addEventListener('click', handleClickOutside)
    } else {
      document.removeEventListener('click', handleClickOutside)
    }
  }

  let selectedModel = models[0]
  let userSelectedModel = false // Track when user manually selects a model

  // Update selectedModel when models are loaded
  $: if (models.length > 0 && !selectedModel) {
    let modelToSelect = models[0] // Default fallback

    // First try to use the current chat's model if it exists
    const currentModel = chat?.settings?.model
    if (currentModel) {
      const matchingModel = models.find(model => model.id === currentModel)
      if (matchingModel) {
        modelToSelect = matchingModel
      }
    } else {
      // If no current chat model, try to use the user's preferred model
      const preferredModelId = get(preferredModelStorage)
      if (preferredModelId) {
        const preferredModel = models.find(model => model.id === preferredModelId)
        if (preferredModel) {
          modelToSelect = preferredModel
        }
      }
    }

    selectedModel = modelToSelect
  }

  // Also update selectedModel when chat changes (but not when user manually selects)
  $: if (models.length > 0 && chat?.settings?.model && selectedModel?.id !== chat.settings.model && !userSelectedModel) {
    const matchingModel = models.find(model => model.id === chat.settings.model)
    if (matchingModel) {
      selectedModel = matchingModel
    }
  }

  // Implement methods of chat controller for automation
  let sendButton: HTMLButtonElement | null = null

  TestChatController.sendMessage = async () => {
    if (sendButton && !sendButton.disabled) {
      sendButton.click()
    } else {
      const error = sendButton ? 'error: send button is disabled' : 'error: send button not found'
      throw new Error(error)
    }
  }
  TestChatController.setMessageInput = async (value: string) => {
    promptInput = value
    // works also with 10ms, so 100 should be safe
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  TestChatController.activateMessageInput = async () => {
    focusInput()
  }
  TestChatController.getLastMessageContent = async (role: string) => {
    // Find the last message in $currentChatMessages matching the given role
    const last = [...$currentChatMessages].reverse().find(m => m.role === role)
    if (last && last.content) {
      return last.content.trim()
    }

    // If no message is found, throw an error and return the last message ignoring the role
    // Do not try to get the content as it might be an error message with no content field
    const lastMessage = $currentChatMessages[$currentChatMessages.length - 1]
    const messageString = JSON.stringify(lastMessage)
    throw new Error(`No message found for role: ${role}; last message: ${messageString}`)
  }

  let scDelay
  const onStateChange = async (...args: any[]) => {
    if (!chat) return
    clearTimeout(scDelay)
    setTimeout(async () => {
      if (chat.startSession) {
        await restartProfile(chatId)
        if (chat.startSession) {
          chat.startSession = false
          saveChatStore()
          // Auto start the session
          submitForm(false, true)
        }
      }
      if ($showSetChatSettings) {
        $showSetChatSettings = false
        showSettingsModal()
      }
      if ($submitExitingPromptsNow) {
        $submitExitingPromptsNow = false
        submitForm(false, true)
      }
      if ($continueMessage) {
        const message = getMessage(chatId, $continueMessage)
        $continueMessage = ''
        if (message && $currentChatMessages.indexOf(message) === ($currentChatMessages.length - 1)) {
          submitForm(lastSubmitRecorded, true, message)
        }
      }
    })
  }

  $: onStateChange($checkStateChange, $showSetChatSettings, $submitExitingPromptsNow, $continueMessage)

  const afterChatLoad = (...args: any[]) => {
    scrollToBottom()
  }

  $: afterChatLoad($currentChatId)

  setCurrentChat(0)
  // Make sure chat object is ready to go
  updateChatSettings(chatId)

  onDestroy(async () => {
    // clean up
    // abort any pending requests.
    chatRequest.controller.abort()
    ttsStop()
  
    // Remove click outside event listener
    if (typeof window !== 'undefined') {
      document.removeEventListener('click', handleClickOutside)
    }
  })

  onMount(async () => {
    if (!chat) return

    setCurrentChat(chatId)

    chatRequest = new ChatRequest()
    await chatRequest.setChat(chat)

    // Check if this chat has any file messages and set hasUploadedFile flag accordingly
    const hasFileMessage = chat.messages.some(m =>
      m.role === 'user' && m.content.startsWith(FILE_MESSAGE_PREFIX)
    )
  
    fileUploaded = hasFileMessage
    chat.hasUploadedFile = hasFileMessage
    chat.lastAccess = Date.now()
    saveChatStore()
    $checkStateChange++

    // Focus the input on mount
    focusInput()

    // Try to detect speech recognition support
    if ('SpeechRecognition' in window) {
      // @ts-ignore
      recognition = new window.SpeechRecognition()
    } else if ('webkitSpeechRecognition' in window) {
      // @ts-ignore
      recognition = new window.webkitSpeechRecognition() // eslint-disable-line new-cap
    }

    if (recognition) {
      recognition.interimResults = false
      recognition.onstart = () => {
        recording = true
      }
      recognition.onresult = (event: any) => {
        // Stop speech recognition, submit the form and remove the pulse
        const last = event.results.length - 1
        const text = event.results[last][0].transcript
        input.value = text
        recognition.stop()
        recording = false
        submitForm(true)
      }
    } else {
      console.log('Speech recognition not supported')
    }
    if (chat.startSession) {
      await restartProfile(chatId)
      if (chat.startSession) {
        chat.startSession = false
        saveChatStore()
        // Auto start the session
        setTimeout(() => { submitForm(false, true) }, 0)
      }
    }
  })

  // Scroll to the bottom of the chat on update
  afterUpdate(() => {
    sizeTextElements()
    // Scroll to the bottom of the page after any updates to the messages array
    // focusInput()
  })

  // Scroll to the bottom of the chat on update
  const focusInput = () => {
    input.focus()
    scrollToBottom()
  }


  const ttsStart = (text:string, recorded:boolean) => {
    // Use TTS to read the response, if query was recorded
    if (recorded && 'SpeechSynthesisUtterance' in window) {
      const utterance = new SpeechSynthesisUtterance(text)
      window.speechSynthesis.speak(utterance)
    }
  }

  const ttsStop = () => {
    if ('SpeechSynthesisUtterance' in window) {
      window.speechSynthesis.cancel()
    }
  }

  // File upload handling functions
  const handleFileSelected = (event: CustomEvent<{file: File}>) => {
    selectedFile = event.detail.file
    console.log('File selected:', selectedFile.name)
  }
  
  const handleUploadStart = (event: CustomEvent<{filename: string}>) => {
    isUploading = true
    uploadErrorMessage = ''
    uploadStatus = {
      progress: 0,
      filename: event.detail.filename
    }
    // Reset the updating state to hide the cancel button
    chatRequest.updating = false
    chatRequest.updatingMessage = ''
    console.log('Upload started:', event.detail.filename)
  }
  
  const handleUploadComplete = (event: CustomEvent<{message: Message[]}>) => {
    isUploading = false
    uploadStatus = { ...uploadStatus, progress: 100 }
    fileUploaded = true

    // Store the fileUploaded state in the chat object
    if (chat) {
      chat.hasUploadedFile = true
      saveChatStore()
    }

    selectedFile = null // Remove the file display since it's now in the chat
    // Add both messages to the chat - user file message and empty assistant message
    const messages = event.detail.message
  
    // Add the user message
    addMessage(chatId, messages[0])
  
    // Add the empty assistant message
    if (messages.length > 1) {
      addMessage(chatId, messages[1])
    }
  
    scrollToBottom()
  }
  
  const handleUploadError = (event: CustomEvent<{error: string}>) => {
    isUploading = false
    console.error('Upload error:', event.detail.error)
  }

  let waitingForCancel:any = 0

  const cancelRequest = () => {
    if (!waitingForCancel) {
      // wait a second for another click to avoid accidental cancel
      waitingForCancel = setTimeout(() => { waitingForCancel = 0 }, 1000)
      return
    }
    clearTimeout(waitingForCancel); waitingForCancel = 0
    chatRequest.controller.abort()
  }

  const submitForm = async (recorded: boolean = false, skipInput: boolean = false, fillMessage: Message|undefined = undefined): Promise<void> => {
    // Compose the system prompt message if there are no messages yet - disabled for now
    if (chatRequest.updating) return

    lastSubmitRecorded = recorded

    if (!skipInput) {
      chat.sessionStarted = true
  
      // Update the chat settings with the selected model and profile before making the request
      if (selectedModel && selectedModel.id && selectedModel.profileKey) {
        // Get the full profile for the selected model
        const selectedProfile = await getProfile(selectedModel.profileKey)
  
        // Apply the profile settings to the chat
        Object.assign(chat.settings, selectedProfile)
  
        // Ensure the model ID is set correctly
        chat.settings.model = selectedModel.id
  
        // Update the system prompt to match the new profile
        setSystemPrompt(chatId)
  
        // Handle reasoning parameters if they exist
        if (selectedModel.parameters) {
          selectedModel.parameters.forEach(param => {
            if (param.name === 'Reasoning') {
              const selectedOption = param.options.find(option => option.selected)
              if (selectedOption) {
                // Add reasoning_effort parameter to chat settings
                chat.settings.reasoning_effort = selectedOption.value
              }
            }
          })
        }
      }
  
      saveChatStore()
      if (input.value !== '') {
        // Compose the input message
        const inputMessage: Message = { role: 'user', content: input.value, uuid: uuidv4() }
        addMessage(chatId, inputMessage)
      } else if (!fillMessage && $currentChatMessages.length &&
        $currentChatMessages[$currentChatMessages.length - 1].role === 'assistant') {
        fillMessage = $currentChatMessages[$currentChatMessages.length - 1]
      }

      // Clear the input value
      input.value = ''
      input.blur()

      // Resize back to single line height
      input.style.height = 'auto'
    }
    focusInput()

    chatRequest.updating = true
    chatRequest.updatingMessage = ''

    let doScroll = true
    let didScroll = false

    const checkUserScroll = (e: Event) => {
      const el = e.target as HTMLElement
      // Check if user has scrolled and adjust auto-scroll behavior accordingly
      if (el && didScroll) {
        // from user
        doScroll = (window.innerHeight + window.scrollY + 10) >= document.body.offsetHeight
      }
    }

    window.addEventListener('scroll', checkUserScroll)

    try {
      const response = await chatRequest.sendRequest($currentChatMessages, {
        chat,
        autoAddMessages: true, // Auto-add and update messages in array
        streaming: chatSettings.stream,
        fillMessage,
        onMessageChange: (messages) => {
          // Hide loading bubble when we get first token from streaming
          if (chatRequest.updating && messages.length > 0 && messages[0]?.content) {
            chatRequest.updating = false
            chatRequest.updatingMessage = ''
          }
          if (doScroll) scrollToBottom(true)
          didScroll = !!messages[0]?.content
        }
      })
      await response.promiseToFinish()
      const message = response.getMessages()[0]
      if (message) {
        ttsStart(message.content, recorded)
      }
    } catch (e) {
      console.error(e)
    }

    window.removeEventListener('scroll', checkUserScroll)

    chatRequest.updating = false
    chatRequest.updatingMessage = ''

    focusInput()
  }

</script>
<style>
  /* Spin animation for the loading icon */
  @keyframes spin {
    0% { transform: rotate(0deg); }
    100% { transform: rotate(360deg); }
  }

  :global(.fa-spinner) {
    animation: spin 1s linear infinite;
  }

  @keyframes spin-centered {
    0% { transform: translate(-50%, -50%) rotate(0deg); }
    100% { transform: translate(-50%, -50%) rotate(360deg); }
  }
  
  /* Upload banner styling */
  .file-upload-banner {
    width: 100%;
    margin-bottom: 8px;
    padding: 8px 12px;
    background-color: white;
    border-radius: 30px;
    display: flex;
    align-items: center;
    box-sizing: border-box;
    overflow: hidden;
  }
  
  .file-upload-icon {
    margin-right: 10px;
    color: rgb(122, 73, 246);
    flex-shrink: 0;
  }
  
  .file-upload-content {
    flex: 1;
    min-width: 0;
    overflow: hidden;
  }
  
  .file-upload-filename {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 0.9rem;
  }
  
  .file-upload-progress-container {
    margin-top: 4px;
    height: 3px;
    background-color: #e0e0e0;
    border-radius: 1.5px;
    overflow: hidden;
  }
  
  .file-upload-progress-bar {
    height: 100%;
    background-color: rgb(122, 73, 246);
    border-radius: 1.5px;
    transition: width 0.3s ease;
  }
  
  .chat-page {
    padding-top: var(--banner-offset, 1.25rem);
    transition: padding-top 0.3s ease;
  }

  :global(.update-banner:not(:empty) + .chat-page) {
    --banner-offset: 3rem;
  }
  
  .control.is-expanded {
    position: relative;
  }

  /* Custom styling for input container with model selector */
  .control.is-expanded {
    background-color: white;
    border-radius: 24px;
    border: 1px solid #dbdbdb;
    padding: 12px;
    overflow: visible; /* Allow dropdown to show outside */
  }

  .control.is-expanded textarea.input {
    background: transparent;
    border: none;
    box-shadow: none;
    margin: 0;
    margin-left: .35rem;
    border-radius: 0;
    padding: 0;
    padding-right: 100px; /* Space for buttons */
  }

  .control.is-expanded textarea.input:focus {
    border: none;
    box-shadow: none;
  }

  /* Ensure dropdown menu appears above other elements */
  .dropdown-menu {
    z-index: 1000;
  }

  /* Chat controls positioned in flex container */
  form .chat-page-controls {
    position: static; /* Override absolute positioning from main CSS */
    margin-bottom: 0;
  }

  .dropdown-button-wrapper::before {
    content: '';
    width: 170%;
    aspect-ratio: 1;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    position: absolute;
    background-image: conic-gradient(
      hsl(265.24deg 89.08% 81.25%) 0%,
      hsl(265.24deg 89.08% 81.25%) 49%,
      hsl(265.24deg 100% 25.29% / 62%) 50%,
      hsl(265.24deg 100% 25.29% / 62%) 100%
    );
    animation: spin-centered 6s linear infinite;
  }
  .dropdown-button-wrapper.is-disabled::before {
    background: var(--thumbBG) ;
  }

  .dropdown-button-wrapper::after {
    content: '';
    inset: 1px;
    background: var(--bgBody);
    position: absolute;
    z-index: 2;
    border-radius: 2rem;
    background-color: white;
  }

  .dropdown-menu {
    background-color: var(--BgColorSidebarDark);
    color: white;
    border-radius: 16px;
  }
  .dropdown-item{
    color: inherit;
  }
  .dropdown-item:hover,
  .dropdown-item.active{
    background-color: rgb(75, 75, 75);
    color: inherit;
  }

  .content-box {
    display: flex;
    flex-direction: column;
    justify-content: space-evenly;
  }

  .model-name {
    font-weight: 600;
    margin: 0;
    margin-bottom: .25rem;
  }

  .model-subtitle {
    font-size: 0.85em;
    opacity: 75%;
  }

  .model-param {
    margin: 0;
    margin-bottom: .25rem;
  }

  .dropdown-item-clickable {
    cursor: pointer;
  }
  
  .dropdown-separator {
    width: 1px;
    height: 100%;
  }
  
  .param-button {
    font-size: 0.875em;
  }
  
  .chat-form {
    margin-bottom: 0;
    overflow: visible;
    max-width: 100%;
  }
  
  .control-expanded {
    overflow: visible;
    max-width: 100%;
  }
  
  .progress-bar-dynamic {
    transition: width 0.3s ease;
  }
  
  .dropdown-button-container {
    width: max-content;
  }
  
  .dropdown-button-text {
    font-size: 0.9rem;
  }
  
  .dropdown-menu-positioned {
    top: unset;
  }
</style>

{#if chat}
<svelte:component this={ChatSettingsModal} chatId={chatId} bind:show={showSettingsModal} />
<div class="chat-page pt-5 px-2" style="--running-totals: {Object.entries(chat.usage || {}).length}">
<div class="chat-content">
<nav class="level chat-header">
  <div class="level-left">
    <div class="level-item">
      <p class="subtitle is-5">
        <span>{chat.name || `Chat ${chat.id}`}</span>
      </p>
    </div>
  </div>
</nav>

<Messages messages={$currentChatMessages} chatId={chatId} chat={chat} />

{#if chatRequest.updating === true || $currentChatId === 0}
  <article class="message is-success assistant-message">
    <div class="message-body content d-flex align-items-center">
      <img src={logoSmall} alt="" width="24" height="24" class="mr-2 flex-shrink-0" />
      <div class="is-loading-dots" ></div>
      <span>{chatRequest.updatingMessage}</span>
    </div>
  </article>
{/if}
</div>
</div>
<Footer class="prompt-input-container" strongMask={true}>
  
  <div class="chat-content mx-auto pt-4 pb-2">
  <form class="field has-addons has-addons-right is-align-items-flex-end chat-form" on:submit|preventDefault={() => submitForm()}>
    <div class="control is-expanded control-expanded">
      <!-- Enhanced loading banner shown during file upload -->
      {#if isUploading}
        <!-- Upload indicator with full width matching the chat input -->
        <div class="file-upload-banner">
          <span class="file-upload-icon">
            <Fa icon={faSpinner} class="fa-spinner" />
          </span>
          <div class="file-upload-content">
            <div class="file-upload-filename" title="{uploadStatus?.filename || 'Uploading document...'}">{uploadStatus?.filename || 'Uploading document...'}</div>
            <div class="file-upload-progress-container">
              <div class="file-upload-progress-bar progress-bar-dynamic" style="width: {uploadStatus?.progress || 0}%;"></div>
            </div>
          </div>
        </div>
      {/if}
      <textarea
        class="input is-info is-focused chat-input auto-size"
        placeholder="Type your message here..."
        bind:value={promptInput}
        rows="1"
        on:keydown={e => {
          // Only send if Enter is pressed, not Shift+Enter
          if (e.key === 'Enter' && !e.shiftKey) {
            e.stopPropagation()
            submitForm()
            e.preventDefault()
          }
        }}
        on:input={e => {
          autoGrowInputOnEvent(e)
          // Reset updating state when user types
          chatRequest.updating = false
          chatRequest.updatingMessage = ''
        }}
        bind:this={input}
      />
      
      <!-- Input bottom row with model selector and controls -->
      <div class="input-bottom-row d-flex align-items-center justify-content-between mt-1">
        <!-- Model selector -->
        <div class="dropdown" bind:this={dropdownElement}>
          <div class="dropdown-button-wrapper dropdown-button-container position-relative overflow-hidden rounded-5 {chat.sessionStarted || models.length === 0 ? 'is-disabled opacity-100' : ''}">
            <button class="btn dropdown-button-text px-4 py-2 d-flex align-items-center justify-content-center gap-2 rounded-5 border-0 position-relative z-3" type="button" on:click|preventDefault={() => { isDropdownOpen = !isDropdownOpen }} disabled={chat.sessionStarted || models.length === 0}>
              <small>{selectedModel?.name || 'Loading...'}</small>
              <Fa icon={faChevronDown} size="xs" />
            </button>
          </div>

          <ul class="dropdown-menu dropdown-menu-positioned d-grid gap-1 p-3 m-0 bottom-100 border-0 mb-2 {isDropdownOpen ? 'd-block' : 'd-none'}">
            {#each models as model}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <li class="dropdown-item-clickable px-2 py-2 rounded-2 d-flex align-items-center gap-3 dropdown-item" class:active={selectedModel === model} on:click={() => {
                userSelectedModel = true
                selectedModel = model
                isDropdownOpen = false
                // Save the user's preferred model for future chats
                preferredModelStorage.set(model.id)
              }}>
                <div class="content-box">
                  <p class="model-name">{model.name}</p>
                  <small class="model-subtitle">{model.label}</small>
                </div>
                {#if model.parameters}
                    <hr class="dropdown-separator bg-white m-0">
                  {#each model.parameters as param}
                    <div class="content-box">
                      <p class="model-param">{param.name}</p>
                      <div class="d-flex gap-1">
                        {#each param.options as option}
                          <button class="btn btn-outline-dark param-button p-0 px-1 text-white border-white border-opacity-25" class:active={option.selected} on:click|preventDefault={() => {
                            param.options.forEach(o => { o.selected = false })
                            option.selected = true
                            isDropdownOpen = false
                            // Save the preferred reasoning option for this model
                            const currentReasoning = get(preferredReasoningStorage)
                            currentReasoning[model.id] = option.value
                            preferredReasoningStorage.set(currentReasoning)
                          }}>
                            {option.name}
                          </button>
                        {/each}
                      </div>
                    </div>
                  {/each}
                {/if}
              </li>
            {/each}
          </ul>
        </div>

        <!-- Chat controls -->
        <div class="chat-page-controls d-flex align-items-center">
    {#if chatRequest.updating}
    <p class="control send">
      <button title="Cancel Response" class="button is-danger" type="button" on:click={cancelRequest}><span class="icon">
        {#if waitingForCancel}
        <Fa icon={faCircleCheck} />
        {:else}
        <Fa icon={faCommentSlash} />
        {/if}
      </span></button>
    </p>
    {:else}
    <FileUploadButton 
      bind:this={fileUploadButton}
      disabled={chatRequest.updating || fileUploaded || isUploading || selectedModel?.name?.toLowerCase().includes('gemma')}
      tooltip={selectedModel?.name?.toLowerCase().includes('gemma') ? 'Gemma cannot handle file uploads' : 'Attach file'}
      on:fileSelected={handleFileSelected}
      on:uploadStart={handleUploadStart}
      on:uploadComplete={handleUploadComplete}
      on:uploadError={handleUploadError} 
    />
    <p class="control send">
      <button title="Send" class="button is-info" type="submit" disabled={isSendButtonDisabled} bind:this={sendButton}><img width="18" height="18" src={send} alt="send icon"/></button>
    </p>
    {/if}
        </div>
      </div>
    </div>
  </form>
    </div>
</Footer>
{/if}
