package ai.privatemode.android.data.model

import java.util.UUID

data class AttachedFile(
    val name: String,
    val content: String,
)

data class Message(
    val id: String = UUID.randomUUID().toString(),
    val role: MessageRole,
    val content: String,
    val timestamp: Long = System.currentTimeMillis(),
    val attachedFiles: List<AttachedFile>? = null,
)

enum class MessageRole {
    USER,
    ASSISTANT,
    SYSTEM;

    fun toApiString(): String = name.lowercase()

    companion object {
        fun fromApiString(value: String): MessageRole =
            entries.first { it.name.equals(value, ignoreCase = true) }
    }
}

data class Chat(
    val id: String = UUID.randomUUID().toString(),
    val title: String = "New chat",
    val messages: List<Message> = emptyList(),
    val createdAt: Long = System.currentTimeMillis(),
    val updatedAt: Long = System.currentTimeMillis(),
    val lastUserMessageAt: Long = System.currentTimeMillis(),
    val isStreaming: Boolean = false,
    val wordCount: Int = 0,
)

fun countWords(text: String): Int {
    return text.trim().split(Regex("\\s+")).filter { it.isNotEmpty() }.size
}

fun calculateChatWordCount(messages: List<Message>): Int {
    return messages.sumOf { msg ->
        var count = countWords(msg.content)
        msg.attachedFiles?.forEach { file ->
            count += countWords(file.content)
        }
        count
    }
}
