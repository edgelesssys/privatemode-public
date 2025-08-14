<script lang="ts">
  import { replace } from 'svelte-spa-router'
  import type { Chat } from './Types.svelte'
  import { deleteChat, saveChatStore } from './Storage.svelte'
  import { onMount } from 'svelte'
  import { hasActiveModels } from './Models.svelte'
  import threeDots from '../assets/three-dots-vertical.svg'

  export let chat:Chat
  export let activeChatId:number|undefined
  export let prevChat:Chat|undefined
  export let nextChat:Chat|undefined

  let editing:boolean = false
  let original:string
  let isDropdownOpen:boolean = false

  const waitingForConfirm:any = 0

  onMount(async () => {
    if (!chat.name) {
      chat.name = `Chat ${chat.id}`
    }
  })

  const keydown = (event:KeyboardEvent) => {
    if (event.key === 'Escape') {
      event.stopPropagation()
      event.preventDefault()
      chat.name = original
      editing = false
    }
    if (event.key === 'Tab' || event.key === 'Enter') {
      event.stopPropagation()
      event.preventDefault()
      update()
    }
  }

  const update = () => {
    editing = false
    if (!chat.name) {
      chat.name = original
      return
    }
    saveChatStore()
  }

  const delChat = () => {
    // if (!waitingForConfirm) {
    //   // wait a second for another click to avoid accidental deletes
    //   waitingForConfirm = setTimeout(() => { waitingForConfirm = 0 }, 1000)
    //   return
    // }
    // clearTimeout(waitingForConfirm)
    // waitingForConfirm = 0
    if (activeChatId === chat.id) {
      const newChat = nextChat || prevChat
      if (!newChat) {
        // No other chats, clear all and go to home
        replace('/').then(() => { deleteChat(chat.id) })
      } else {
        // Delete the current chat and go to the max chatId
        replace(`/chat/${newChat.id}`).then(() => { deleteChat(chat.id) })
      }
    } else {
      deleteChat(chat.id)
    }
  }

  const edit = () => {
    original = chat.name
    editing = true
    setTimeout(() => {
      const el = document.getElementById(`chat-menu-item-${chat.id}`)
      if (el) {
        el.focus()
        const range = document.createRange()
        const sel = window.getSelection()
        range.selectNodeContents(el)
        range.collapse(false) // false means collapse to end
        if (sel) {
          sel.removeAllRanges()
          sel.addRange(range)
        }
      }
    }, 0)
  }
</script>

<li class="chat-menu-item-wrapper position-relative">
  {#if editing}
    <div id="chat-menu-item-{chat.id}" class="chat-menu-item is-active is-editable" on:keydown={keydown} contenteditable bind:innerText={chat.name} on:blur={update} />
  {:else}
    <a href={`#/chat/${chat.id}`} class="chat-menu-item d-flex align-items-center justify-content-between" class:is-waiting={waitingForConfirm} class:is-disabled={!hasActiveModels()} class:is-active={activeChatId === chat.id}>
      <div class="chat-item-name">
        <span>{chat.name || `Chat ${chat.id}`}</span>
      </div>

      <div class="dropdown">
        <button class="border-0 p-0 px-2 bg-transparent d-flex align-items-center justify-content-center" type="button" on:click|preventDefault={() => { isDropdownOpen = !isDropdownOpen }}>
          <img src={threeDots} alt="edit" width="16" height="16" />
        </button>
        
        <ul class="dropdown-menu m-0 {isDropdownOpen ? 'd-block' : 'd-none'}">
          <li class="dropdown-item">
            <button class="p-0 px-1" on:click|preventDefault={() => edit()}>
              Rename
            </button>
          </li>
          <li class="dropdown-item">
            <button class="p-0 px-1" on:click|preventDefault={() => delChat()}>
              Delete
            </button>
          </li>
        </ul>
      </div>
    </a>
  {/if}
</li>

<style>
  .chat-menu-item {
    position: relative;
    width: 100%;
  }

  .chat-menu-item.is-editable {
    outline: none !important;
  }

  .chat-menu-item.is-editable:focus {
    outline: none !important;
    border: none !important;
    box-shadow: none !important;
  }

  .chat-menu-item .dropdown {
    visibility: hidden;
    opacity: 0;
    transition: opacity 0.2s ease-in-out, visibility 0.2s ease-in-out;
  }

  .chat-menu-item:hover .dropdown {
    visibility: visible;
    opacity: 1;
  }

  .dropdown-menu {
    position: absolute;
    right: 0;
    top: 100%;
    z-index: 1000;
  }

  .dropdown-item {
    padding: 8px 12px;
    cursor: pointer;
  }

  .dropdown-item:hover {
    background-color: var(--vt-c-divider-light);
  }

  .dropdown-item button {
    background: none;
    border: none;
    width: 100%;
    text-align: left;
    cursor: pointer;
  }
</style>
