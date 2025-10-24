import { SmokeTestingActivated, OnSmokeTestCompleted } from '../../wailsjs/go/main/SmokeTestService'

// methods are dynamically implemented by binding to the DOM elements
export const TestChatController = {
  newChat: async (): Promise<void> => { throw new Error('newChat not implemented') },

  chatReady: false,
  activateMessageInput: async (): Promise<void> => { throw new Error('activateMessageInput not implemented') },
  setMessageInput: async (text: string): Promise<void> => { throw new Error('setMessageInput not implemented') },
  sendMessage: async (): Promise<void> => { throw new Error('sendMessage not implemented') },
  uploadFile: async (event: Event): Promise<void> => { throw new Error('uploadFile not implemented') },
  getLastMessageContent: async (role: string): Promise<string> => {
    throw new Error('getLastMessageContent not implemented')
  },
  deleteActiveChat: async (): Promise<void> => { throw new Error('deleteActiveChat not implemented') }
}

export const SmokeTest = {
  // Generic waiter: pass a predicate getter to poll until true
  waitFor: async (predicate: () => boolean, timeoutMs: number = 10000, pollIntervalMs: number = 100) => {
    const deadline = Date.now() + timeoutMs
    while (!predicate()) {
      if (Date.now() > deadline) {
        throw new Error('Timeout waiting for condition')
      }
      await new Promise(resolve => setTimeout(resolve, pollIntervalMs))
    }
  },
  // Wrapper to start a new chat and wait for the chat UI to become ready
  newChat: async () => {
    await TestChatController.newChat()
    await SmokeTest.waitFor(() => TestChatController.chatReady)

    // After the chat is loaded, we still need to wait for the
    // UI elements to be ready
    await new Promise(resolve => setTimeout(resolve, 200))
  },
  run: async () => {
    await SmokeTest.newChat()
    await TestChatController.activateMessageInput()

    let message = 'This is a smoke test. Answer exactly with "Smoke test completed"'
    await TestChatController.setMessageInput(message)
    await TestChatController.sendMessage()

    // test correct message was sent
    await new Promise(resolve => setTimeout(resolve, 1000))
    const messageSent = await TestChatController.getLastMessageContent('user')
    if (messageSent !== message) {
      throw new Error(`Unexpected message sent: ${messageSent}`)
    }

    // test response within at most 5 seconds
    await new Promise(resolve => setTimeout(resolve, 5000))
    let response = await TestChatController.getLastMessageContent('assistant')
    // Strip the dot at the end of response.
    response = response.replace(/\.$/, '');
    if (response !== 'Smoke test completed') {
      throw new Error(`Unexpected response: ${response}`)
    }

    // delete the chat
    await TestChatController.deleteActiveChat()

    // wait a bit for debugging to see the final state
    await new Promise(resolve => setTimeout(resolve, 300))

    // FormData with created Files, or Blobs is fundamentally broken on Safari/Webkit,
    // meaning we can't run the test on Mac
    // The issue is known since 2016: https://bugs.webkit.org/show_bug.cgi?id=165081
    // The suggested workaround doesn't work, as is already shown in the testcase attached to the report
    if (navigator.platform.startsWith('Mac')) {
      return
    }

    // test file upload
    await SmokeTest.newChat()
    await TestChatController.activateMessageInput()

    // Create a mock Event that imitates a file upload
    const fileContent = 'File upload smoke test'
    const targetFile = new File([fileContent], 'testFile.txt', { type: 'text/plain' })
    const mockFileList = {
      0: targetFile,
      length: 1,
      item: (index: number) => index === 0 ? targetFile : null,
      [Symbol.iterator]: function * () {
        yield targetFile
      }
    }
    const mockInput = {
      files: mockFileList,
      type: 'file',
      value: ''
    }
    const mockEvent = {
      target: mockInput,
      type: 'change',
      preventDefault: () => {},
      stopPropagation: () => {}
    }

    await TestChatController.uploadFile(mockEvent as unknown as Event)

    message = 'Repeat the content of the uploaded file'
    await TestChatController.setMessageInput(message)
    await TestChatController.sendMessage()

    await new Promise(resolve => setTimeout(resolve, 5000))
    response = await TestChatController.getLastMessageContent('assistant')
    if (!response.includes(fileContent)) {
      throw new Error('Unexpected response: ' + response)
    }

    // wait a bit for debugging to see the final state
    await new Promise(resolve => setTimeout(resolve, 300))
  },

  runIfActivated: async () => {
    if (!(await SmokeTestingActivated())) {
      return
    }

    try {
      await SmokeTest.run()
      await OnSmokeTestCompleted(true, 'Test completed successfully')
    } catch (error) {
      console.error('Smoke test failed:', error)
      await OnSmokeTestCompleted(false, `Error: ${error instanceof Error ? error.message : String(error)}`)
    }
  }
}
