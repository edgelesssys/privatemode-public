<script lang="ts">
  import Code from './Code.svelte'
  import { afterUpdate, createEventDispatcher, onMount } from 'svelte'
  import { deleteMessage, deleteSummaryMessage, continueMessage, updateMessages } from './Storage.svelte'
  import SvelteMarkdown from 'svelte-markdown'
  import type { Message, Model, Chat } from './Types.svelte'
  import Fa from 'svelte-fa/src/fa.svelte'
  import { faTrash, faDiagramPredecessor, faDiagramNext, faDownload, faClipboard, faFile } from '@fortawesome/free-solid-svg-icons/index'
  import { errorNotice, scrollToMessage } from './Util.svelte'
  import { getImage } from './ImageStore.svelte'
  import { parseFileMessage } from './FileUploadService.svelte'
  import logoSmall from '../assets/logo-small.svg'

  export let message:Message
  export let chatId:number
  export let chat:Chat

  $: chatSettings = chat.settings

  const isError = message.role === 'error'
  const isSystem = message.role === 'system'
  const isUser = message.role === 'user'
  const isAssistant = message.role === 'assistant'
  const isImage = message.role === 'image'

  // change to true to style reasoning output
  const reasoningOutput = false
  
  // Check if this is a file message using the parseFileMessage function
  const fileMessageInfo = parseFileMessage(message)
  const isFileMessage = fileMessageInfo.isFileMessage

  // Marked options
  const markdownOptions = {
    gfm: true, // Use GitHub Flavored Markdown
    breaks: true, // Enable line breaks in markdown
    mangle: false // Do not mangle email addresses
  }

  const getDisplayMessage = ():string => {
    const content = message.content
    if (isSystem && chatSettings.hideSystemPrompt) {
      const result = content.match(/::NOTE::[\s\S]+?::NOTE::/g)
      return result ? result.map(r => r.replace(/::NOTE::([\s\S]+?)::NOTE::/, '$1')).join('') : '(hidden)'
    }
    return content
  }

  const dispatch = createEventDispatcher()
  let editing = false
  let original:string
  let defaultModel:Model
  let imageUrl:string
  let refreshCounter = 0
  let displayMessage = message.content

  onMount(() => {
    defaultModel = chatSettings.model
    if (message?.image) {
      getImage(message.image.id).then(i => {
        imageUrl = 'data:image/png;base64, ' + i.b64image
      })
    }
    displayMessage = getDisplayMessage()
  })

  afterUpdate(() => {
    if (message.streaming && message.content.slice(-5).includes('```')) refreshCounter++
    displayMessage = getDisplayMessage()
  })

  const edit = () => {
    if (message.summarized || message.streaming || editing) return
    editing = true
    original = message.content
    setTimeout(() => {
      const el = document.getElementById('edit-' + message.uuid)
      el && el.focus()
    }, 0)
  }

  let dbnc: ReturnType<typeof setTimeout>
  const update = () => {
    clearTimeout(dbnc)
    dbnc = setTimeout(() => { doChange() }, 250)
  }

  const doChange = () => {
    if (message.content !== original) {
      dispatch('change', message)
      updateMessages(chatId)
    }
  }

  const continueIncomplete = () => {
    editing = false
    $continueMessage = message.uuid
  }

  const exit = () => {
    doChange()
    editing = false
  }

  const keydown = (event:KeyboardEvent) => {
    if (event.key === 'Escape') {
      if (!editing) return
      event.stopPropagation()
      event.preventDefault()
      message.content = original
      editing = false
    }
    if (event.ctrlKey && event.key === 'Enter') {
      if (!editing) return
      event.stopPropagation()
      event.preventDefault()
      exit()
    }
  }

  // Double click for mobile support
  let lastTap: number = 0
  const editOnDoubleTap = () => {
    const now: number = new Date().getTime()
    const timesince: number = now - lastTap
    if ((timesince < 400) && (timesince > 0)) {
      edit()
    }
    lastTap = new Date().getTime()
  }

  const checkDelete = () => {
    if (message.summarized) {
      errorNotice('Sorry, you can\'t delete a summarized message')
      return
    }
    if (message.summary) {
      try {
        deleteSummaryMessage(chatId, message.uuid)
      } catch (e: unknown) {
        errorNotice('Unable to delete summary:', e instanceof Error ? e : undefined)
      }
    } else {
      try {
        deleteMessage(chatId, message.uuid)
      } catch (e: unknown) {
        errorNotice('Unable to delete:', e instanceof Error ? e : undefined)
      }
    }
  }

  const downloadImage = () => {
    const filename = (message?.content || `${chat.name}-image-${message?.image?.id}`)
      .replace(/([^a-z0-9- ]|\.)+/gi, '_').trim().slice(0, 80)
    const a = document.createElement('a')
    a.download = `${filename}.png`
    a.href = imageUrl
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  }

</script>

{#if !isSystem}
<article
  id="{'message-' + message.uuid}"
  class="message chat-message"
  class:is-info={isUser}
  class:is-success={isAssistant || isImage}
  class:is-warning={isSystem}
  class:is-danger={isError}
  class:user-message={isUser || isSystem}
  class:assistant-message={isError || isAssistant || isImage}
  class:summarized={message.summarized}
  class:suppress={message.suppress}
  class:editing={editing}
  class:streaming={message.streaming}
  class:incomplete={message.finish_reason === 'length'}
>
  <div class="message-body content d-flex align-items-start">
    {#if isAssistant}
      <img src={logoSmall} alt="" width="24" height="24" class="mr-2 flex-shrink-0" />
    {/if}

    {#if editing}
      <form class="message-edit" on:submit|preventDefault={update} on:keydown={keydown}>
        <div id={'edit-' + message.uuid} class="message-editor" bind:innerText={message.content} contenteditable
        on:input={update} on:blur={exit} />
      </form>
        {#if imageUrl}
          <img src={imageUrl} alt="">
        {/if}
    {:else}
      <div class="message-display" on:touchend={editOnDoubleTap} on:dblclick|preventDefault={() => edit()}>
        {#if message.summary && !message.summary.length}
          <p><b>Summarizing...</b></p>
        {/if}
        
        {#if isFileMessage}
          <div class="file-message">
            <span class="file-icon"><Fa icon={faFile} /></span>
            <span class="file-name-wrapper">
              <span class="file-name" title="{fileMessageInfo.filename}">{fileMessageInfo.filename}</span>
            </span>
          </div>
          {#if fileMessageInfo.wordCount !== undefined}
            <div class="file-word-count" style="margin-left:2.2em; margin-top:0.1em; color: #888; font-size: 0.85em;">
              {fileMessageInfo.wordCount} words
            </div>
          {/if}
        {:else}
          {#if reasoningOutput && isAssistant}
            <div class="reasoning-output is-streaming">
              {#key refreshCounter}
                <SvelteMarkdown
                  source={displayMessage}
                  options={markdownOptions}
                  renderers={{ code: Code }}
                />
              {/key}
            </div>
          {:else}
            {#key refreshCounter}
              <SvelteMarkdown
                source={displayMessage}
                options={markdownOptions}
                renderers={{ code: Code }}
              />
            {/key}
          {/if}
          
        {/if}
        
        {#if imageUrl}
          <img src={imageUrl} alt="">
        {/if}
        {#if message.finish_reason === 'length' || message.finish_reason === 'abort'}
          <button
            type="button"
            class="continue-button"
            on:click|preventDefault|stopPropagation={() => {
              continueIncomplete()
            }}
          >
            Continue
          </button>
        {/if}
      </div>
    {/if}
  </div>
  <div class="tool-drawer-mask"></div>
  <div class="tool-drawer">
    <div class="button-pack">
      {#if message.summarized}
      <a
        href={'#'}
        title="Jump to summary"
        class="msg-summary button is-small"
        on:click|preventDefault={() => {
          // Using type assertion in a separate function to handle string conversion
          if (message.summarized) scrollToMessage(message.summarized)
        }}
      >
      <span class="icon"><Fa icon={faDiagramNext} /></span>
      </a>
      {/if}
      {#if message.summary}
      <a
        href={'#'}
        title="Jump to summarized"
        class="msg-summarized button is-small"
        on:click|preventDefault={() => {
          // Using type assertion in a separate function to handle string conversion
          if (message.summary) scrollToMessage(message.summary)
        }}
      >
      <span class="icon"><Fa icon={faDiagramPredecessor} /></span>
      </a>
      {/if}
      {#if !message.summarized}
      <a
        href={'#'}
        title="Delete this message"
        class="msg-delete button is-small"
        on:click|preventDefault={() => {
          checkDelete()
        }}
      >
      <span class="icon"><Fa icon={faTrash} /></span>
      </a>
      {/if}
      {#if !isImage}
        <a
          href={'#'}
          title="Copy to Clipboard"
          class="msg-image button is-small"
          on:click|preventDefault={() => {
            navigator.clipboard.writeText(message.content)
          }}
        >
        <span class="icon"><Fa icon={faClipboard} /></span>
        </a>
      {/if}
      {#if imageUrl}
        <a
          href={'#'}
          title="Download Image"
          class="msg-image button is-small"
          on:click|preventDefault={() => {
            downloadImage()
          }}
        >
        <span class="icon"><Fa icon={faDownload} /></span>
        </a>
      {/if}
      </div>

  </div>
</article>
{/if}

<style>
  .continue-button {
    display: inline-block;
    margin-left: 4px;
    margin-top: 1em;
    font-weight: bold;
    background: none;
    border: none;
    padding: 0;
    color: inherit;
    cursor: pointer;
    animation: cursor-blink 1.5s steps(2) infinite;
  }
  
  .file-message {
    display: inline-flex;
    align-items: center;
    padding: 0 12px;
    height: 36px;
    background-color: white;
    border: 1px solid rgba(0, 0, 0, 0.1);
    border-radius: 30px;
    max-width: 100%;
  }
  
  .file-icon {
    margin-right: 8px;
    color: rgb(122, 73, 246);
    font-size: 1.2em;
    display: flex;
    align-items: center;
  }
  
  .file-name-wrapper {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
  }

  .file-name {
    font-weight: normal;
    font-size: 0.9em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
    background: transparent;
    border: none;
    padding: 0;
    margin: 0;
    line-height: 36px;
  }

  .continue-button:hover {
    animation: none;
  }

  .reasoning-output{
   color: #7A49F6;
   font-weight: 700; 
   line-height: 1.3;
   
  }

  .reasoning-output.is-streaming {
    color: transparent;
    background-image: linear-gradient(90deg,rgba(122, 73, 246, 1) 66%, rgba(122, 73, 246, 0.44) 70%, rgba(122, 73, 246, 1) 75%);
    background-size: 200% 100%;
    animation: reasoning-highlight 3s ease-in-out infinite;
    background-clip: text;
  }

  @keyframes reasoning-highlight {
    0% {
      background-position: 200% 100%;
    }
    100% {
      background-position: 0 100%
    }
  }

  .message-display :global(table) {
    width: 100%;
    border-collapse: collapse;
    table-layout: fixed;
  }

  .message-display :global(th),
  .message-display :global(td) {
    text-align: left;
    padding: 8px;
    word-wrap: break-word;
  }
</style>
