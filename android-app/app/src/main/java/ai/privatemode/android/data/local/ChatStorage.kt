package ai.privatemode.android.data.local

import ai.privatemode.android.data.model.Chat
import ai.privatemode.android.data.model.calculateChatWordCount
import android.content.Context
import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import java.io.File

class ChatStorage(private val context: Context) {

    private val gson = Gson()
    private val chatFile = File(context.filesDir, "chats.json")

    private val _chats = MutableStateFlow<List<Chat>>(emptyList())
    val chats: StateFlow<List<Chat>> = _chats.asStateFlow()

    private val _currentChatId = MutableStateFlow<String?>(null)
    val currentChatId: StateFlow<String?> = _currentChatId.asStateFlow()

    suspend fun load() = withContext(Dispatchers.IO) {
        if (chatFile.exists()) {
            try {
                val json = chatFile.readText()
                val type = object : TypeToken<List<Chat>>() {}.type
                val loaded: List<Chat> = gson.fromJson(json, type) ?: emptyList()
                _chats.value = loaded
            } catch (e: Exception) {
                _chats.value = emptyList()
            }
        }
    }

    private suspend fun save() = withContext(Dispatchers.IO) {
        try {
            val json = gson.toJson(_chats.value)
            chatFile.writeText(json)
        } catch (_: Exception) {
            // Silently fail; next operation will retry
        }
    }

    fun setCurrentChatId(chatId: String?) {
        _currentChatId.value = chatId
    }

    suspend fun createChat(): String {
        val now = System.currentTimeMillis()
        val newChat = Chat(
            createdAt = now,
            updatedAt = now,
            lastUserMessageAt = now,
        )
        _chats.update { it + newChat }
        _currentChatId.value = newChat.id
        save()
        return newChat.id
    }

    suspend fun addMessage(
        chatId: String,
        message: ai.privatemode.android.data.model.Message,
    ): String {
        _chats.update { chats ->
            chats.map { chat ->
                if (chat.id == chatId) {
                    val updatedMessages = chat.messages + message
                    val now = System.currentTimeMillis()
                    chat.copy(
                        messages = updatedMessages,
                        updatedAt = now,
                        lastUserMessageAt = if (message.role == ai.privatemode.android.data.model.MessageRole.USER) now else chat.lastUserMessageAt,
                        wordCount = calculateChatWordCount(updatedMessages),
                        title = if (chat.messages.isEmpty() && message.role == ai.privatemode.android.data.model.MessageRole.USER) {
                            message.content.take(50)
                        } else {
                            chat.title
                        }
                    )
                } else chat
            }
        }
        save()
        return message.id
    }

    suspend fun updateMessage(chatId: String, messageId: String, content: String) {
        _chats.update { chats ->
            chats.map { chat ->
                if (chat.id == chatId) {
                    val updatedMessages = chat.messages.map { msg ->
                        if (msg.id == messageId) msg.copy(content = content) else msg
                    }
                    chat.copy(
                        messages = updatedMessages,
                        updatedAt = System.currentTimeMillis(),
                        wordCount = calculateChatWordCount(updatedMessages),
                    )
                } else chat
            }
        }
        // Note: we don't save on every streaming update for performance.
        // The caller should save after streaming completes.
    }

    fun setStreaming(chatId: String, isStreaming: Boolean) {
        _chats.update { chats ->
            chats.map { chat ->
                if (chat.id == chatId) chat.copy(isStreaming = isStreaming)
                else chat
            }
        }
    }

    suspend fun renameChat(chatId: String, newTitle: String) {
        _chats.update { chats ->
            chats.map { chat ->
                if (chat.id == chatId) chat.copy(title = newTitle)
                else chat
            }
        }
        save()
    }

    suspend fun deleteChat(chatId: String) {
        _chats.update { chats -> chats.filter { it.id != chatId } }
        if (_currentChatId.value == chatId) {
            _currentChatId.value = null
        }
        save()
    }

    suspend fun clearAllChats() {
        _chats.value = emptyList()
        _currentChatId.value = null
        save()
    }

    fun getChat(chatId: String): Chat? {
        return _chats.value.find { it.id == chatId }
    }

    suspend fun saveAfterStreaming() {
        save()
    }
}
