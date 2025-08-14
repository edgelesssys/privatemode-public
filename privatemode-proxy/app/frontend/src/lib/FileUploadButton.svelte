<script lang="ts">
  import { createEventDispatcher } from 'svelte'
  import Fa from 'svelte-fa/src/fa.svelte'
  import { faPaperclip } from '@fortawesome/free-solid-svg-icons/index'
  import { uploadPdfToUnstructured, createFileMessage, formatUploadError } from './FileUploadService.svelte'
  import type { UploadStatus } from './FileUploadService.svelte'
  import { addErrorToast } from './stores/ToastStore.svelte'
  
  // Used to disable the component after one file has been uploaded
  export let disabled = false
  export let acceptedFileTypes = 'application/pdf, text/plain, application/vnd.openxmlformats-officedocument.wordprocessingml.document, text/markdown'
  
  let fileInput: HTMLInputElement
  let uploadStatus: UploadStatus = {
    isUploading: false,
    progress: 0,
    filename: null
  }

  async function normalizeFile (file: File): Promise<File> {
    const arrayBuffer = await file.arrayBuffer()
    return new File([arrayBuffer], file.name, { type: file.type })
  }
  
  const dispatch = createEventDispatcher()
  
  function handleButtonClick () {
    console.log('File upload button clicked')
    if (fileInput) {
      console.log('Clicking file input element')
      fileInput.click()
    } else {
      console.error('File input element not found')
    }
  }
  
  async function handleFileSelect (event: Event) {
    console.log('File select event triggered', event)
    try {
      const target = event.target as HTMLInputElement
      console.log('File input element:', target)
      console.log('Files:', target.files)
      if (!target.files || target.files.length === 0) {
        console.log('No files selected')
        if (fileInput) fileInput.value = ''
        return
      }

      // Store the file locally and start upload immediately
      let file = target.files[0]
      const isWindows = navigator.platform.startsWith('Win')
      if (isWindows) {
        file = await normalizeFile(file)
      }
      console.log('File selected:', file.name, file.type, file.size)

      // Notify parent component
      dispatch('fileSelected', { file })

      // Start upload process immediately
      await uploadFile(file)
    } catch (error) {
      console.error('Error handling file selection:', error)
      const errorMessage = formatUploadError(error)
      addErrorToast(errorMessage)
      // Reset file input so user can try again
      if (fileInput) fileInput.value = ''
    }
  }
  
  async function uploadFile (file: File): Promise<any> {
    if (!file) {
      console.log('No file selected to upload')
      return null
    }

    console.log('Uploading file:', file.name, file.type, file.size)
  
    // Set status and notify parent
    uploadStatus = {
      isUploading: true,
      progress: 0,
      filename: file.name
    }
    dispatch('uploadStart', { filename: file.name })

    try {
      // Update progress and upload the file
      uploadStatus.progress = 10
      dispatch('uploadProgress', { progress: uploadStatus.progress })
  
      // Process with unstructured API
      const { content, wordCount } = await uploadPdfToUnstructured(file)
      console.log('Received content from API, length:', content.length, 'wordCount:', wordCount)
  
      // Update progress
      uploadStatus.progress = 90
      dispatch('uploadProgress', { progress: uploadStatus.progress })

      // Create message and mark as complete
      const message = createFileMessage(file.name, content, wordCount)
      uploadStatus = {
        isUploading: false,
        progress: 100,
        filename: file.name
      }

      dispatch('uploadComplete', { message })
      return message
    } catch (error) {
      console.error('File upload error:', error)
  
      // Format and display error
      addErrorToast(formatUploadError(error))
  
      // Reset status
      uploadStatus = {
        isUploading: false,
        progress: 0,
        filename: file.name
      }
      // Reset file input so user can try again
      if (fileInput) fileInput.value = ''

      dispatch('uploadError', { error: formatUploadError(error) })
      return null
    }
  }
</script>

<style>
  .file-upload-hidden {
    display: none;
  }
  
  .file-control {
    margin: 0;
  }
  
  .upload-button {
    border: none;
    cursor: pointer;
    background: transparent;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    height: 2.25em;
    width: 2.25em;
  }
</style>

<!-- File input element -->
<input
  type="file"
  accept={acceptedFileTypes}
  class="file-upload-hidden"
  on:change={handleFileSelect}
  bind:this={fileInput}
/>

<!-- Just the upload button -->
<div class="control file-control">
  <button 
    type="button"
    class="button upload-button"
    disabled={disabled}
    on:click={handleButtonClick}
    aria-label="Attach file"
  >
    <span>
      <Fa icon={faPaperclip} />
    </span>
  </button>
</div>
