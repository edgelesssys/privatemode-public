<script context="module" lang="ts">
  import { writable } from 'svelte/store'
  import { v4 as uuidv4 } from 'uuid'

  export interface ToastItem {
    id: string
    type: 'error' | 'success' | 'warning' | 'info'
    message: string
    duration: number
  }

  // Create a writable store for the toast notifications
  export const toasts = writable<ToastItem[]>([])

  /**
   * Add a new toast notification
   */
  export const addToast = (
    message: string,
    type: 'error' | 'success' | 'warning' | 'info' = 'info',
    duration: number = 5000
  ): string => {
    const id = uuidv4()
    const toast: ToastItem = {
      id,
      type,
      message,
      duration
    }

    toasts.update(items => {
      return [toast, ...items]
    })

    return id
  }

  /**
   * Add an error toast notification
   */
  export const addErrorToast = (message: string, duration: number = 5000): string => {
    return addToast(message, 'error', duration)
  }

  /**
   * Add a success toast notification
   */
  export const addSuccessToast = (message: string, duration: number = 5000): string => {
    return addToast(message, 'success', duration)
  }

  /**
   * Add a warning toast notification
   */
  export const addWarningToast = (message: string, duration: number = 5000): string => {
    return addToast(message, 'warning', duration)
  }

  /**
   * Add an info toast notification
   */
  export const addInfoToast = (message: string, duration: number = 5000): string => {
    return addToast(message, 'info', duration)
  }

  /**
   * Remove a toast notification by id
   */
  export const removeToast = (id: string): void => {
    toasts.update(items => items.filter(item => item.id !== id))
  }

  /**
   * Clear all toast notifications
   */
  export const clearToasts = (): void => {
    toasts.set([])
  }
</script> 
