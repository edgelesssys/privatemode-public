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
  import Prompts from './Prompts.svelte'
  import Messages from './Messages.svelte'
  import { restartProfile } from './Profiles.svelte'
  import { afterUpdate, onMount, onDestroy } from 'svelte'
  import Fa from 'svelte-fa/src/fa.svelte'
  import {
    faArrowUpFromBracket,
    faPaperPlane,
    faGear,
    faPenToSquare,
    faMicrophone,
    faLightbulb,
    faCommentSlash,
    faCircleCheck
  } from '@fortawesome/free-solid-svg-icons/index'
  import { v4 as uuidv4 } from 'uuid'
  import { getPrice } from './Stats.svelte'
  import { autoGrowInputOnEvent, scrollToBottom, sizeTextElements } from './Util.svelte'
  import ChatSettingsModal from './ChatSettingsModal.svelte'
  import Footer from './Footer.svelte'
  import { openModal } from 'svelte-modals'
  import PromptInput from './PromptInput.svelte'
  import { ChatRequest } from './ChatRequest.svelte'
  import { getModelDetail } from './Models.svelte'
  import microphone from '../assets/microphone.svg'
  import send from '../assets/send.svg'
  import logoSmall from '../assets/logo-small.svg'

  export let params = { chatId: '' }
  const chatId: number = parseInt(params.chatId)

  let chatRequest = new ChatRequest()
  let input: HTMLTextAreaElement
  let recognition: any = null
  let recording = false
  let lastSubmitRecorded = false

  $: chat = $chatsStorage.find((chat) => chat.id === chatId) as Chat
  $: chatSettings = chat?.settings
  let showSettingsModal
  let promptInput = ''

  $: isSendButtonDisabled = promptInput === ''

  let scDelay
  const onStateChange = async (...args:any) => {
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

  const afterChatLoad = (...args:any) => {
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
      recognition.onresult = (event) => {
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

  const addNewMessage = () => {
    if (chatRequest.updating) return
    let inputMessage: Message
    const lastMessage = $currentChatMessages[$currentChatMessages.length - 1]
    const uuid = uuidv4()
    if ($currentChatMessages.length === 0) {
      inputMessage = { role: 'system', content: input.value, uuid }
    } else if (lastMessage && lastMessage.role === 'user') {
      inputMessage = { role: 'assistant', content: input.value, uuid }
    } else {
      inputMessage = { role: 'user', content: input.value, uuid }
    }
    addMessage(chatId, inputMessage)

    // Clear the input value
    input.value = ''
    // input.blur()
    focusInput()
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
      if (el && e.isTrusted && didScroll) {
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

  const suggestName = async (): Promise<void> => {
    const suggestMessage: Message = {
      role: 'user',
      content: "Using appropriate language, please tell me a short 6 word summary of this conversation's topic for use as a book title. Only respond with the summary.",
      uuid: uuidv4()
    }

    const suggestMessages = $currentChatMessages.slice(0, 10) // limit to first 10 messages
    suggestMessages.push(suggestMessage)

    chatRequest.updating = true
    chatRequest.updatingMessage = 'Getting suggestion for chat name...'
    const response = await chatRequest.sendRequest(suggestMessages, {
      chat,
      autoAddMessages: false,
      streaming: false,
      summaryRequest: true
    })

    try {
      await response.promiseToFinish()
    } catch (e) {
      console.error('Error generating name suggestion', e, e.stack)
    }
    chatRequest.updating = false
    chatRequest.updatingMessage = ''
    if (response.hasError()) {
      addMessage(chatId, {
        role: 'error',
        content: `Unable to get suggested name: ${response.getError()}`,
        uuid: uuidv4()
      })
    } else {
      response.getMessages().forEach(m => {
        const name = m.content.split(/\s+/).slice(0, 8).join(' ').replace(/^[^a-z0-9!?]+|[^a-z0-9!?]+$/gi, '').trim()
        if (name) chat.name = name
      })
      saveChatStore()
    }
  }

  function promptRename () {
    openModal(PromptInput, {
      title: 'Enter Name for Chat',
      label: 'Name',
      value: chat.name,
      class: 'is-info',
      onSubmit: (value) => {
        chat.name = (value || '').trim() || chat.name
        saveChatStore()
        $checkStateChange++
      }
    })
  }

  const recordToggle = () => {
    ttsStop()
    if (chatRequest.updating) return
    // Check if already recording - if so, stop - else start
    if (recording) {
      recognition?.stop()
      recording = false
    } else {
      recognition?.start()
    }
  }

</script>
<style>
  .chat-page {
    padding-top: var(--banner-offset, 1.25rem);
    transition: padding-top 0.3s ease;
  }

  :global(.update-banner:not(:empty) + .chat-page) {
    --banner-offset: 3rem;
  }
</style>

{#if chat}
<ChatSettingsModal chatId={chatId} bind:show={showSettingsModal} />
<div class="chat-page pt-5 px-2" style="--running-totals: {Object.entries(chat.usage || {}).length}">
<div class="chat-content">
<nav class="level chat-header">
  <div class="level-left">
    <div class="level-item">
      <p class="subtitle is-5">
        <span>{chat.name || `Chat ${chat.id}`}</span>
        <!-- <a href={'#'} class="greyscale ml-2 is-hidden has-text-weight-bold editbutton" title="Rename chat" on:click|preventDefault={promptRename}><Fa icon={faPenToSquare} /></a> -->
        <!-- <a href={'#'} class="greyscale ml-2 is-hidden has-text-weight-bold editbutton" title="Suggest a chat name" on:click|preventDefault={suggestName}><Fa icon={faLightbulb} /></a> -->
        <!-- <a href={'#'} class="greyscale ml-2 is-hidden has-text-weight-bold editbutton" title="Copy this chat" on:click|preventDefault={() => { copyChat(chatId) }}><Fa icon={faClone} /></a> -->
        <!-- <a href={'#'} class="greyscale ml-2 is-hidden has-text-weight-bold editbutton" title="Delete this chat" on:click|preventDefault={deleteChat}><Fa icon={faTrash} /></a> -->
      </p>
    </div>
  </div>

  <div class="level-right">
    <div class="level-item">
      <!-- <button class="button is-warning" on:click={() => { clearMessages(chatId); window.location.reload() }}><span class="greyscale mr-2"><Fa icon={faTrash} /></span> Clear messages</button> -->
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

<!--{#if $currentChatId !== 0 && ($currentChatMessages.length === 0 || ($currentChatMessages.length === 1 && $currentChatMessages[0].role === 'system'))}
  <Prompts bind:input />
{/if}-->
</div>
</div>
<Footer class="prompt-input-container" strongMask={true}>
  <div class="chat-content mx-auto pt-4 pb-2">
  <form class="field has-addons has-addons-right is-align-items-flex-end" on:submit|preventDefault={() => submitForm()}>
    <p class="control is-expanded">
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
        on:input={e => autoGrowInputOnEvent(e)}
        bind:this={input}
      />
    </p>
    <div class="chat-page-controls">
    <!-- <p class="control mic" class:is-hidden={!recognition}>
      <button class="button" class:is-disabled={chatRequest.updating} class:is-pulse={recording} on:click|preventDefault={recordToggle}
      ><img width="12" height="18" src={microphone} alt="microphone icon"/></button
      >
    </p> -->
    <!-- <p class="control settings">
      <button title="Chat/Profile Settings" class="button" on:click|preventDefault={showSettingsModal}><span class="icon"><Fa icon={faGear} /></span></button>
    </p>
    <p class="control queue">
      <button title="Queue message, don't send yet" class:is-disabled={chatRequest.updating} class="button is-ghost" on:click|preventDefault={addNewMessage}><span class="icon"><Fa icon={faArrowUpFromBracket} /></span></button>
    </p> -->
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
    <p class="control send">
      <button title="Send" class="button is-info" type="submit" disabled={isSendButtonDisabled}><img width="18" height="18" src={send} alt="send icon"/></button>
    </p>
    {/if}
    </div>
  </form>
  <!-- <p class="chat-page-demo">
    The Privatemode preview is serving open-source models and brought to you by
    <a
      href="https://www.edgeless.systems/"
      target="_blank"
      rel="noopener noreferrer">Edgeless Systems</a
    >.
  </p> -->
      <!-- a target to scroll to -->
    <!-- <div class="content has-text-centered running-total-container">
      {#each Object.entries(chat.usage || {}) as [model, usage]}
      <p class="is-size-7 running-totals">
        <em>{getModelDetail(model || '').label || model}</em> total <span class="has-text-weight-bold">{usage.total_tokens}</span>
        tokens ~= <span class="has-text-weight-bold">${getPrice(usage, model).toFixed(6)}</span>
      </p>
      {/each}
    </div> -->
    </div>
</Footer>
{/if}
