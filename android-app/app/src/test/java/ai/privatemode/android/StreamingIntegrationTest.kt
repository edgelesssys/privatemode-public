package ai.privatemode.android

import ai.privatemode.android.data.model.Message
import ai.privatemode.android.data.model.MessageRole
import ai.privatemode.android.data.remote.ApiException
import ai.privatemode.android.data.remote.PrivatemodeClient
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test

/**
 * Integration tests using MockWebServer to verify the full streaming flow.
 */
class StreamingIntegrationTest {

    private lateinit var server: MockWebServer
    private lateinit var client: PrivatemodeClient

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        client = PrivatemodeClient(server.url("/").toString().trimEnd('/'), "test-key")
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun `streaming chat completion collects all chunks`() = runTest {
        val sseBody = """
            data: {"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

            data: {"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

            data: {"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

            data: {"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

            data: [DONE]

        """.trimIndent()

        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "text/event-stream")
                .setBody(sseBody)
        )

        val messages = listOf(Message(role = MessageRole.USER, content = "Hi"))
        val chunks = client.streamChatCompletion("test-model", messages).toList()

        assertEquals(listOf("Hello", " world"), chunks)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/v1/chat/completions", request.path)
        assertTrue(request.getHeader("Authorization")!!.contains("test-key"))
    }

    @Test
    fun `streaming handles explicit null content without crash`() = runTest {
        val sseBody = """
            data: {"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":null},"finish_reason":null}]}

            data: {"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}

            data: [DONE]

        """.trimIndent()

        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "text/event-stream")
                .setBody(sseBody)
        )

        val messages = listOf(Message(role = MessageRole.USER, content = "Hi"))
        val chunks = client.streamChatCompletion("test-model", messages).toList()

        assertEquals(listOf("ok"), chunks)
    }

    @Test
    fun `streaming error returns parsed API error message`() = runTest {
        server.enqueue(
            MockResponse()
                .setResponseCode(400)
                .setBody("""{"error":{"message":"bad request details","type":"invalid_request_error"}}""")
        )

        val messages = listOf(Message(role = MessageRole.USER, content = "Hi"))
        try {
            client.streamChatCompletion("test-model", messages).toList()
            fail("Should have thrown")
        } catch (e: ApiException) {
            assertEquals("bad request details", e.message)
            assertEquals(400, e.statusCode)
        }
    }

    @Test
    fun `streaming error detects HPKE proxy requirement`() = runTest {
        server.enqueue(
            MockResponse()
                .setResponseCode(400)
                .setBody("""{"error":{"message":"invalid message format: expected format 'id:nonce:iv:cipher'"}}""")
        )

        val messages = listOf(Message(role = MessageRole.USER, content = "Hi"))
        try {
            client.streamChatCompletion("test-model", messages).toList()
            fail("Should have thrown")
        } catch (e: ApiException) {
            assertTrue(e.message!!.contains("secure proxy"))
        }
    }

    @Test
    fun `fetchModels parses response`() = runTest {
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"object":"list","data":[{"id":"model-1","object":"model","created":1234,"owned_by":"test"}]}""")
        )

        val models = client.fetchModels()
        assertEquals(1, models.size)
        assertEquals("model-1", models[0].id)
    }

    @Test
    fun `fetchModels error returns parsed message`() = runTest {
        server.enqueue(
            MockResponse()
                .setResponseCode(401)
                .setBody("""{"error":{"message":"Invalid API key"}}""")
        )

        try {
            client.fetchModels()
            fail("Should have thrown")
        } catch (e: ApiException) {
            assertEquals("Invalid API key", e.message)
            assertEquals(401, e.statusCode)
        }
    }
}
