<script context="module" lang="ts">
  import type { Message } from './Types.svelte'
  import { v4 as uuidv4 } from 'uuid'
  import { get } from 'svelte/store'
  import { apiKeyStorage } from './Storage.svelte'

  export interface UploadStatus {
    isUploading: boolean
    progress: number
    filename: string | null
  }

  export interface UnstructuredElement {
    type: string
    text: string
    element_id: string
  }

  export const FILE_MESSAGE_PREFIX = '[FILE]'
  export const MAX_FILE_WORD_LIMIT = 30000 // 40k tokens * 0.75
  

  function isBelowMaxTokenLimit (wordCount: number): boolean {
    return wordCount <= MAX_FILE_WORD_LIMIT
  }

  /**
   * Formats an error message for upload failures
   */
  export function formatUploadError (error: any): string {
    // Check if the error is an Error object or a string
    const errorMessage = error instanceof Error ? error.message : String(error)
  
    // Handle specific error cases with more user-friendly messages
    if (errorMessage.includes('Unauthorized')) {
      return 'Unauthorized: Check your API key for file processing'
    } else if (errorMessage.includes('Network Error') || errorMessage.includes('Failed to fetch')) {
      return 'Network error: Upload failed, please try again'
    } else if (errorMessage.includes('429')) {
      return 'Rate limit exceeded: Too many requests, please try again later'
    } else if (errorMessage.includes('413')) {
      return 'File too large: Please upload a smaller file'
    } else {
      return `Upload failed: ${errorMessage}`
    }
  }

  /**
   * Uploads a file to the unstructured API and returns the parsed text content and word count
   */
  export async function uploadPdfToUnstructured (file: File, apiUrl = ''): Promise<{ content: string, wordCount: number }> {
    console.log('uploadFileToUnstructured called with file:', file.name, file.size, file.type)
    const isLocalTest = import.meta.env.VITE_TEST_UNSTRUCTURED_API_BASE !== undefined
    apiUrl = isLocalTest
      ? `${import.meta.env.VITE_TEST_UNSTRUCTURED_API_BASE}/general/v0/general`
      : `${import.meta.env.VITE_API_BASE}/unstructured/general/v0/general`
  
    // Create form data for the upload
    const formData = new FormData()
    formData.append('files', file)
    formData.append('strategy', 'fast')

    try {
      const apiKey = isLocalTest ? undefined : get(apiKeyStorage) // for local testing no auth is required and setting the header would trigger CORS issues


      // Prepare headers - don't include Content-Type as it will be set automatically with the boundary
      const headers: Record<string, string> = {
        // Add Origin header to help with CORS
        Origin: window.location.origin
      }

      // Only add Authorization if we have an API key
      if (apiKey) {
        headers.Authorization = `Bearer ${apiKey}`
      }

      const response = await fetch(apiUrl, {
        method: 'POST',
        body: formData,
        mode: 'cors',
        credentials: 'omit',
        headers
      })

      if (!response.ok) {
        const errorText = await response.text().catch(() => response.statusText)
        throw new Error(`${response.status} - ${errorText || response.statusText}`)
      }

      const data = await response.json() as UnstructuredElement[]
  
      // Combine all text chunks from the response
      const textContent = data
        .filter(element => element.text && element.text.trim().length > 0)
        .map(element => element.text.trim())
        .join('\n\n')

      // Calculate word count
      const wordCount = textContent.trim().split(/\s+/).filter(Boolean).length
      if (wordCount === 0) {
        throw new Error('No text could be extracted for this document.')
      }
      if (!isBelowMaxTokenLimit(wordCount)) {
        throw new Error(`This document has ${wordCount} words and exceeds the maximum word limit of ${MAX_FILE_WORD_LIMIT}. Please upload a shorter document.`)
      }
      return { content: textContent, wordCount }
    } catch (error) {
      console.error('Error uploading file:', error)
      throw error
    }
  }

  /**
   * Creates a chat message for the uploaded file
   */
  /**
   * Creates a chat message for the uploaded file, now including word count in the content metadata
   */
  export function createFileMessage (filename: string, content: string, wordCount?: number): Message[] {
    const userMessage: Message = {
      role: 'user' as 'user', // Using a literal type to satisfy TypeScript
      content: `${FILE_MESSAGE_PREFIX}${filename}\n\n${content}`,
      uuid: uuidv4(),
      // Add metadata for word count if provided
      ...(wordCount !== undefined ? { wordCount } : {})
    }
  
    const assistantMessage: Message = {
      role: 'assistant' as 'assistant',
      content: '',
      uuid: uuidv4()
    }
  
    return [userMessage, assistantMessage]
  }

  /**
   * Parses a message to extract file information if it's a file message
   */
  /**
   * Parses a message to extract file information if it's a file message, including word count if available
   */
  export function parseFileMessage (message: Message): { isFileMessage: boolean, filename: string, content: string, wordCount?: number } {
    // Not a file message if it's not from a user or doesn't start with the prefix
    if (message.role !== 'user' || !message.content.startsWith(FILE_MESSAGE_PREFIX)) {
      return {
        isFileMessage: false,
        filename: '',
        content: message.content,
        wordCount: (message as any).wordCount
      }
    }
  
    // Extract filename and content
    const filenameEndIndex = message.content.indexOf('\n\n')
    const filename = filenameEndIndex > 0
      ? message.content.substring(FILE_MESSAGE_PREFIX.length, filenameEndIndex)
      : 'file.pdf'

    const content = filenameEndIndex > 0
      ? message.content.substring(filenameEndIndex + 2)
      : message.content.substring(FILE_MESSAGE_PREFIX.length)

    // Try to extract wordCount from the message object if present
    const wordCount = (message as any).wordCount
    return {
      isFileMessage: true,
      filename,
      content,
      wordCount
    }
  }
</script>
