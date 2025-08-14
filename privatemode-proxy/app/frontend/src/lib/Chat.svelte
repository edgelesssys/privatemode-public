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
    currentChatId
  } from './Storage.svelte'
  import {
    type Message,
    type Chat
  } from './Types.svelte'
  import Messages from './Messages.svelte'
  import { restartProfile } from './Profiles.svelte'
  import { afterUpdate, onMount, onDestroy } from 'svelte'
  import Fa from 'svelte-fa/src/fa.svelte'
  import {
    faCommentSlash,
    faCircleCheck,
    faSpinner
  } from '@fortawesome/free-solid-svg-icons/index'
  import FileUploadButton from './FileUploadButton.svelte'
  import { v4 as uuidv4 } from 'uuid'
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

  $: chat = $chatsStorage.find((chat) => chat.id === chatId) as Chat
  $: chatSettings = chat?.settings
  $: if (chat) {
    fileUploaded = chat.hasUploadedFile === true
  }
  let showSettingsModal: () => void
  let promptInput = ''

  $: isSendButtonDisabled = (promptInput === '' && !fileUploaded) || isUploading

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
      return last.content
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
  
    if (hasFileMessage) {
      fileUploaded = true
      chat.hasUploadedFile = true
      saveChatStore()
    } else {
      fileUploaded = false
      chat.hasUploadedFile = false
      saveChatStore()
    }

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
  <form class="field has-addons has-addons-right is-align-items-flex-end" style="margin-bottom: 0; overflow: hidden; max-width: 100%;" on:submit|preventDefault={() => submitForm()}>
    <p class="control is-expanded" style="overflow: hidden; max-width: 100%;">
      <!-- Enhanced loading banner shown during file upload -->
      {#if isUploading}
        <!-- Upload indicator with full width matching the chat input -->
        <div class="file-upload-banner">
          <span class="file-upload-icon">
            <Fa icon={faSpinner} style="animation: spin 1s linear infinite;" />
          </span>
          <div class="file-upload-content">
            <div class="file-upload-filename" title="{uploadStatus?.filename || 'Uploading document...'}">{uploadStatus?.filename || 'Uploading document...'}</div>
            <div class="file-upload-progress-container">
              <div class="file-upload-progress-bar" style="width: {uploadStatus?.progress || 0}%;"></div>
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
    </p>
    <div class="chat-page-controls">
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
      disabled={chatRequest.updating || fileUploaded || isUploading}
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
  </form>
    </div>
</Footer>
{/if}
