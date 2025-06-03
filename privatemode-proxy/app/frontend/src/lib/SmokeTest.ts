import { SmokeTestingActivated, OnSmokeTestCompleted } from '../../wailsjs/go/main/SmokeTestService'

// methods are dynamically implemented by binding to the DOM elements
export const TestChatController = {
    newChat: async (): Promise<void> => { throw new Error('newChat not implemented') },
    activateMessageInput: async (): Promise<void> => { throw new Error('activateMessageInput not implemented') },
    setMessageInput: async (text: string): Promise<void> => { throw new Error('setMessageInput not implemented') },
    sendMessage: async (): Promise<void> => { throw new Error('sendMessage not implemented') },
    getLastMessageContent: async (role: string): Promise<string> => {
        throw new Error('getLastMessageContent not implemented')
    },
    deleteActiveChat: async (): Promise<void> => { throw new Error('deleteActiveChat not implemented') },
}
export const SmokeTest = {
    // throws an error if the test fails
    run: async () => {
        await TestChatController.newChat()
        await TestChatController.activateMessageInput()

        const message = 'This is a smoke test. Answer exactly with "Smoke test completed"'
        await TestChatController.setMessageInput(message)
        await TestChatController.sendMessage()

        // test correct message was sent
        const messageSent = await TestChatController.getLastMessageContent("user")
        if (messageSent !== message) {
            throw new Error('Unexpected message sent: ' + messageSent)
        }

        // test response within at most 5 seconds
        await new Promise(resolve => setTimeout(resolve, 5000))
        const response = await TestChatController.getLastMessageContent("assistant")
        if (response !== 'Smoke test completed') {
            throw new Error('Unexpected response: ' + response)
        }

        // delete the chat
        await TestChatController.deleteActiveChat()

        // wait a bit for debugging to see the final state
        await new Promise(resolve => setTimeout(resolve, 300))
    },

    runIfActivated: async () => {
        if (!await SmokeTestingActivated()) {
            return
        }

        try {
            await SmokeTest.run()
            await OnSmokeTestCompleted(true, "Test completed successfully")
        } catch (error) {
            console.error('Smoke test failed:', error)
            await OnSmokeTestCompleted(false, `Error: ${error instanceof Error ? error.message : String(error)}`)
        }
    }
};
