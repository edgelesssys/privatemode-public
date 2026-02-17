package ai.privatemode.android

import com.google.gson.Gson
import com.google.gson.JsonObject
import org.junit.Assert.*
import org.junit.Test

/**
 * Tests that the API request format matches what the server expects.
 * This catches serialization bugs without needing a live API.
 */
class ApiRequestFormatTest {

    private val gson = Gson()

    @Test
    fun `chat completion request body format`() {
        val apiMessages = mutableListOf<JsonObject>()

        // System prompt
        apiMessages.add(JsonObject().apply {
            addProperty("role", "system")
            addProperty("content", "You are a helpful assistant.")
        })

        // User message
        apiMessages.add(JsonObject().apply {
            addProperty("role", "user")
            addProperty("content", "Hello")
        })

        val requestBody = JsonObject().apply {
            addProperty("model", "openai/gpt-oss-120b")
            add("messages", gson.toJsonTree(apiMessages))
            addProperty("stream", true)
            addProperty("reasoning_effort", "medium")
        }

        val json = requestBody.toString()
        val parsed = gson.fromJson(json, JsonObject::class.java)

        // Verify structure
        assertEquals("openai/gpt-oss-120b", parsed.get("model").asString)
        assertTrue(parsed.get("stream").asBoolean)
        assertEquals("medium", parsed.get("reasoning_effort").asString)

        val messages = parsed.getAsJsonArray("messages")
        assertEquals(2, messages.size())

        val systemMsg = messages[0].asJsonObject
        assertEquals("system", systemMsg.get("role").asString)
        assertEquals("You are a helpful assistant.", systemMsg.get("content").asString)

        val userMsg = messages[1].asJsonObject
        assertEquals("user", userMsg.get("role").asString)
        assertEquals("Hello", userMsg.get("content").asString)
    }

    @Test
    fun `sse chunk parsing with content`() {
        val data = """{"id":"1","object":"text_completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}"""
        val chunk = gson.fromJson(data, JsonObject::class.java)
        val choices = chunk.getAsJsonArray("choices")
        assertNotNull(choices)
        assertEquals(1, choices.size())

        val delta = choices[0].asJsonObject.getAsJsonObject("delta")
        val content = delta?.get("content")?.asString
        assertEquals("Hello", content)
    }

    @Test
    fun `sse chunk parsing with null content does not crash`() {
        // First chunk often has role but no content
        val data = """{"id":"1","object":"text_completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}"""
        val chunk = gson.fromJson(data, JsonObject::class.java)
        val choices = chunk.getAsJsonArray("choices")
        val delta = choices[0].asJsonObject.getAsJsonObject("delta")

        // delta.get("content") returns null when key is absent - OK
        val content = delta?.get("content")?.asString
        assertNull(content)
    }

    @Test
    fun `sse chunk parsing with explicit json null content`() {
        // If server sends explicit null content
        val data = """{"id":"1","object":"text_completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":null},"finish_reason":null}]}"""
        val chunk = gson.fromJson(data, JsonObject::class.java)
        val choices = chunk.getAsJsonArray("choices")
        val delta = choices[0].asJsonObject.getAsJsonObject("delta")

        val contentElement = delta?.get("content")
        // contentElement is JsonNull, not null!
        assertNotNull(contentElement)
        assertTrue(contentElement!!.isJsonNull)

        // This is what the current code does - it will throw!
        try {
            val content = contentElement.asString
            fail("Should have thrown UnsupportedOperationException")
        } catch (e: UnsupportedOperationException) {
            // Expected - this is a bug in the current code
        }
    }

    @Test
    fun `sse chunk parsing with empty string content`() {
        val data = """{"id":"1","object":"text_completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":""},"finish_reason":null}]}"""
        val chunk = gson.fromJson(data, JsonObject::class.java)
        val choices = chunk.getAsJsonArray("choices")
        val delta = choices[0].asJsonObject.getAsJsonObject("delta")
        val content = delta?.get("content")?.asString
        assertEquals("", content)
    }

    @Test
    fun `models response parsing`() {
        val json = """{"object":"list","data":[{"id":"openai/gpt-oss-120b","object":"model","created":1234,"owned_by":"Edgeless Systems"}]}"""

        // Simulating what GSON does with ModelsResponse
        val parsed = gson.fromJson(json, JsonObject::class.java)
        val data = parsed.getAsJsonArray("data")
        assertEquals(1, data.size())
        assertEquals("openai/gpt-oss-120b", data[0].asJsonObject.get("id").asString)
    }

    @Test
    fun `attached files are serialized as separate messages before main message`() {
        val apiMessages = mutableListOf<JsonObject>()

        // Simulate a user message with an attached file
        val role = "user"
        val content = "Summarize this"

        // Attached file added first (matching the client code)
        apiMessages.add(JsonObject().apply {
            addProperty("role", role)
            addProperty("content", "[File: test.pdf]\n\nFile contents here")
        })
        // Then the main message
        apiMessages.add(JsonObject().apply {
            addProperty("role", role)
            addProperty("content", content)
        })

        val requestBody = JsonObject().apply {
            addProperty("model", "openai/gpt-oss-120b")
            add("messages", gson.toJsonTree(apiMessages))
            addProperty("stream", true)
        }

        val json = requestBody.toString()
        val parsed = gson.fromJson(json, JsonObject::class.java)
        val messages = parsed.getAsJsonArray("messages")
        assertEquals(2, messages.size())
        assertTrue(messages[0].asJsonObject.get("content").asString.startsWith("[File:"))
        assertEquals("Summarize this", messages[1].asJsonObject.get("content").asString)
    }
}
