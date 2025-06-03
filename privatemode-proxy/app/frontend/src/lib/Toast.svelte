<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import Fa from 'svelte-fa/src/fa.svelte'
  import {
    faCircleExclamation,
    faCircleCheck,
    faCircleInfo,
    faTriangleExclamation,
    faXmark
  } from '@fortawesome/free-solid-svg-icons/index'

  export let id: string
  export let type: 'error' | 'success' | 'warning' | 'info' = 'info'
  export let message: string
  export let duration: number = 5000
  export let onRemove: (id: string) => void
  
  let timer: ReturnType<typeof setTimeout>
  
  // Determine the icon and color based on the type
  const getTypeStyles = () => {
    switch (type) {
      case 'error':
        return {
          icon: faCircleExclamation,
          bgColor: 'has-background-danger-light',
          textColor: 'has-text-danger',
          borderColor: 'has-border-danger'
        }
      case 'success':
        return {
          icon: faCircleCheck,
          bgColor: 'has-background-success-light',
          textColor: 'has-text-success',
          borderColor: 'has-border-success'
        }
      case 'warning':
        return {
          icon: faTriangleExclamation,
          bgColor: 'has-background-warning-light',
          textColor: 'has-text-warning',
          borderColor: 'has-border-warning'
        }
      case 'info':
      default:
        return {
          icon: faCircleInfo,
          bgColor: 'has-background-info-light',
          textColor: 'has-text-info',
          borderColor: 'has-border-info'
        }
    }
  }
  
  const typeStyles = getTypeStyles()
  
  const dismiss = () => {
    clearTimeout(timer)
    onRemove(id)
  }
  
  onMount(() => {
    if (duration > 0) {
      timer = setTimeout(dismiss, duration)
    }
  })
  
  onDestroy(() => {
    clearTimeout(timer)
  })
</script>

<div 
  class="toast-notification {typeStyles.bgColor}"
  in:fly={{ y: -20, duration: 300, easing: cubicOut }}
  out:fade={{ duration: 200 }}
  on:mouseenter={() => clearTimeout(timer)}
  on:mouseleave={() => {
    if (duration > 0) {
      timer = setTimeout(dismiss, duration)
    }
  }}
>
  <div class="notification-icon {typeStyles.textColor}">
    <Fa icon={typeStyles.icon} />
  </div>
  <div class="notification-content">
    <p class="notification-message">{message}</p>
  </div>
  <button 
    class="notification-close"
    on:click={dismiss}
    aria-label="Close notification"
  >
    <Fa icon={faXmark} />
  </button>
</div>

<style>
  .toast-notification {
    display: flex;
    align-items: center;
    padding: 12px 15px;
    margin-bottom: 8px;
    border-radius: 8px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
    min-width: 250px;
    max-width: 350px;
    pointer-events: auto;
    position: relative;
    border-left: 4px solid transparent;
  }
  
  .notification-icon {
    flex-shrink: 0;
    margin-right: 12px;
  }
  
  .notification-content {
    flex-grow: 1;
  }
  
  .notification-message {
    margin: 0;
    word-break: break-word;
    font-weight: 500;
  }
  
  .notification-close {
    background: transparent;
    border: none;
    cursor: pointer;
    padding: 5px;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0.6;
    transition: opacity 0.2s;
    margin-left: 10px;
  }
  
  .notification-close:hover {
    opacity: 1;
  }
</style> 
