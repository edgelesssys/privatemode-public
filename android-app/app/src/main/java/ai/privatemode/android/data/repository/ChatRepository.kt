package ai.privatemode.android.data.repository

import ai.privatemode.android.data.local.ChatStorage
import ai.privatemode.android.data.local.PreferencesManager
import ai.privatemode.android.data.model.ApiModel
import ai.privatemode.android.data.model.AttachedFile
import ai.privatemode.android.data.model.Chat
import ai.privatemode.android.data.model.DEFAULT_MODEL_ID
import ai.privatemode.android.data.model.MODEL_CONFIG
import ai.privatemode.android.data.model.Message
import ai.privatemode.android.data.model.MessageRole
import ai.privatemode.android.data.remote.PrivatemodeClient
import ai.privatemode.android.data.remote.UnstructuredElement
import ai.privatemode.android.proxy.ProxyManager
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import java.io.File

class ChatRepository(
    val chatStorage: ChatStorage,
    val preferences: PreferencesManager,
    private val proxyManager: ProxyManager,
) {
    private val _availableModels = MutableStateFlow<List<ApiModel>>(emptyList())
    val availableModels: StateFlow<List<ApiModel>> = _availableModels.asStateFlow()

    private val _modelsLoaded = MutableStateFlow(false)
    val modelsLoaded: StateFlow<Boolean> = _modelsLoaded.asStateFlow()

    private val _modelsError = MutableStateFlow<String?>(null)
    val modelsError: StateFlow<String?> = _modelsError.asStateFlow()

    val chats: StateFlow<List<Chat>> = chatStorage.chats
    val currentChatId: StateFlow<String?> = chatStorage.currentChatId

    val apiKey: Flow<String?> = preferences.apiKey
    val selectedModel: Flow<String?> = preferences.selectedModel
    val extendedThinking: Flow<Boolean> = preferences.extendedThinking
    val serverUrl: Flow<String> = preferences.serverUrl

    private fun createClient(): PrivatemodeClient? {
        val baseUrl = proxyManager.getBaseUrl()
        // The proxy handles auth internally; we pass the user's API key
        // which the proxy forwards to the backend
        val key = try {
            kotlinx.coroutines.runBlocking { preferences.getApiKey() }
        } catch (_: Exception) {
            null
        }
        if (key == null) return null
        return PrivatemodeClient(baseUrl, key)
    }

    suspend fun initialize() {
        chatStorage.load()
    }

    suspend fun loadModels() {
        try {
            _modelsError.value = null
            val client = createClient() ?: run {
                _modelsError.value = "API key not configured"
                return
            }
            val models = client.fetchModels()
            _availableModels.value = models
            _modelsLoaded.value = true

            // Auto-select model if none selected
            val currentModel = preferences.selectedModel.first()
            if (currentModel == null) {
                val filteredModels = MODEL_CONFIG.keys
                    .mapNotNull { id -> models.find { it.id == id } }
                if (filteredModels.isNotEmpty()) {
                    val defaultModel = filteredModels.find { it.id == DEFAULT_MODEL_ID }
                        ?: filteredModels.first()
                    preferences.setSelectedModel(defaultModel.id)
                }
            }
        } catch (e: Exception) {
            _modelsError.value = e.message ?: "Failed to load models"
            _modelsLoaded.value = false
        }
    }

    fun getFilteredModels(): List<ApiModel> {
        return MODEL_CONFIG.keys
            .mapNotNull { id -> _availableModels.value.find { it.id == id } }
    }

    suspend fun createChat(): String {
        return chatStorage.createChat()
    }

    suspend fun addMessage(chatId: String, role: MessageRole, content: String, attachedFiles: List<AttachedFile>? = null): String {
        val message = Message(
            role = role,
            content = content,
            attachedFiles = attachedFiles,
        )
        return chatStorage.addMessage(chatId, message)
    }

    suspend fun updateMessage(chatId: String, messageId: String, content: String) {
        chatStorage.updateMessage(chatId, messageId, content)
    }

    fun setStreaming(chatId: String, isStreaming: Boolean) {
        chatStorage.setStreaming(chatId, isStreaming)
    }

    fun streamChatCompletion(
        model: String,
        messages: List<Message>,
        systemPrompt: String?,
        reasoningEffort: String?,
    ): kotlinx.coroutines.flow.Flow<String> {
        val client = createClient() ?: throw IllegalStateException("API key not configured")
        return client.streamChatCompletion(model, messages, systemPrompt, reasoningEffort)
    }

    suspend fun uploadFile(file: File, fileName: String): List<UnstructuredElement> {
        val client = createClient() ?: throw IllegalStateException("API key not configured")
        return client.uploadFile(file, fileName)
    }

    suspend fun saveAfterStreaming() {
        chatStorage.saveAfterStreaming()
    }

    suspend fun setApiKey(key: String) {
        preferences.setApiKey(key)
    }

    suspend fun setSelectedModel(modelId: String) {
        preferences.setSelectedModel(modelId)
    }

    suspend fun setExtendedThinking(enabled: Boolean) {
        preferences.setExtendedThinking(enabled)
    }

    suspend fun setServerUrl(url: String) {
        preferences.setServerUrl(url)
    }

    fun setCurrentChatId(chatId: String?) {
        chatStorage.setCurrentChatId(chatId)
    }

    suspend fun renameChat(chatId: String, newTitle: String) {
        chatStorage.renameChat(chatId, newTitle)
    }

    suspend fun deleteChat(chatId: String) {
        chatStorage.deleteChat(chatId)
    }

    suspend fun clearAllChats() {
        chatStorage.clearAllChats()
    }

    fun getChat(chatId: String): Chat? {
        return chatStorage.getChat(chatId)
    }

    fun getProxyManager(): ProxyManager = proxyManager
}
