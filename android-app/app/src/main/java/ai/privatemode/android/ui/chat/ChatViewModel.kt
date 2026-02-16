package ai.privatemode.android.ui.chat

import android.content.Context
import android.net.Uri
import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import ai.privatemode.android.data.model.AttachedFile
import ai.privatemode.android.data.model.Chat
import ai.privatemode.android.data.model.MODEL_CONFIG
import ai.privatemode.android.data.model.MessageRole
import ai.privatemode.android.data.model.countWords
import ai.privatemode.android.data.remote.ApiException
import ai.privatemode.android.data.repository.ChatRepository
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import java.io.File
import java.io.FileOutputStream

class ChatViewModel(
    private val repository: ChatRepository,
) : ViewModel() {
    private val TAG = "ChatViewModel"

    val chats: StateFlow<List<Chat>> = repository.chats
    val currentChatId: StateFlow<String?> = repository.currentChatId
    val modelsLoaded: StateFlow<Boolean> = repository.modelsLoaded

    private val _selectedModel = MutableStateFlow<String?>(null)
    val selectedModel: StateFlow<String?> = _selectedModel.asStateFlow()

    private val _extendedThinking = MutableStateFlow(false)
    val extendedThinking: StateFlow<Boolean> = _extendedThinking.asStateFlow()

    private val _isGenerating = MutableStateFlow(false)
    val isGenerating: StateFlow<Boolean> = _isGenerating.asStateFlow()

    private val _isUploading = MutableStateFlow(false)
    val isUploading: StateFlow<Boolean> = _isUploading.asStateFlow()

    private val _attachedFiles = MutableStateFlow<List<AttachedFile>>(emptyList())
    val attachedFiles: StateFlow<List<AttachedFile>> = _attachedFiles.asStateFlow()

    private val _messageText = MutableStateFlow("")
    val messageText: StateFlow<String> = _messageText.asStateFlow()

    private var streamingJob: Job? = null

    val currentChat: StateFlow<Chat?> = combine(chats, currentChatId) { chats, chatId ->
        chatId?.let { id -> chats.find { it.id == id } }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), null)

    init {
        viewModelScope.launch {
            repository.selectedModel.collect { model ->
                _selectedModel.value = model
            }
        }
        viewModelScope.launch {
            repository.extendedThinking.collect { enabled ->
                _extendedThinking.value = enabled
            }
        }
    }

    fun loadModels() {
        viewModelScope.launch {
            repository.loadModels()
        }
    }

    fun setMessageText(text: String) {
        _messageText.value = text
    }

    fun selectModel(modelId: String) {
        _selectedModel.value = modelId
        viewModelScope.launch {
            repository.setSelectedModel(modelId)
        }
    }

    fun toggleExtendedThinking() {
        val newValue = !_extendedThinking.value
        _extendedThinking.value = newValue
        viewModelScope.launch {
            repository.setExtendedThinking(newValue)
        }
    }

    fun selectChat(chatId: String) {
        repository.setCurrentChatId(chatId)
    }

    fun createNewChat() {
        viewModelScope.launch {
            val chatId = repository.createChat()
            repository.setCurrentChatId(chatId)
        }
    }

    fun renameChat(chatId: String, newTitle: String) {
        viewModelScope.launch {
            repository.renameChat(chatId, newTitle)
        }
    }

    fun deleteChat(chatId: String) {
        viewModelScope.launch {
            repository.deleteChat(chatId)
        }
    }

    fun removeAttachedFile(index: Int) {
        _attachedFiles.value = _attachedFiles.value.filterIndexed { i, _ -> i != index }
    }

    fun uploadFile(context: Context, uri: Uri) {
        viewModelScope.launch {
            _isUploading.value = true
            try {
                val inputStream = context.contentResolver.openInputStream(uri)
                    ?: throw Exception("Cannot open file")
                val fileName = getFileName(context, uri)

                val tempFile = File(context.cacheDir, "upload_${System.currentTimeMillis()}")
                FileOutputStream(tempFile).use { output ->
                    inputStream.copyTo(output)
                }
                inputStream.close()

                val elements = repository.uploadFile(tempFile, fileName)
                val extractedText = elements.joinToString("\n\n") { it.text }

                _attachedFiles.value = _attachedFiles.value + AttachedFile(
                    name = fileName,
                    content = extractedText,
                )

                tempFile.delete()
            } catch (e: Exception) {
                // The UI will show an error via snackbar
                throw e
            } finally {
                _isUploading.value = false
            }
        }
    }

    fun sendMessage() {
        val model = _selectedModel.value ?: return
        val text = _messageText.value.trim()
        if (text.isEmpty() || _isGenerating.value) return

        val modelInfo = MODEL_CONFIG[model]
        val maxWords = modelInfo?.maxWords ?: 60000
        val currentChat = currentChat.value
        val currentWordCount = currentChat?.wordCount ?: 0
        val messageWordCount = countWords(text)
        val attachedFilesWordCount = _attachedFiles.value.sumOf { countWords(it.content) }

        if (currentWordCount + messageWordCount + attachedFilesWordCount > maxWords) return

        viewModelScope.launch {
            var chatId = currentChatId.value
            if (chatId == null) {
                chatId = repository.createChat()
                repository.setCurrentChatId(chatId)
            }

            val filesToSend = _attachedFiles.value.toList()
            _messageText.value = ""
            _attachedFiles.value = emptyList()

            // Add user message
            repository.addMessage(
                chatId,
                MessageRole.USER,
                text,
                filesToSend.ifEmpty { null },
            )

            // Add empty assistant message
            val assistantMessageId = repository.addMessage(
                chatId,
                MessageRole.ASSISTANT,
                "",
            )

            _isGenerating.value = true
            repository.setStreaming(chatId, true)

            streamingJob = viewModelScope.launch {
                try {
                    val chat = repository.getChat(chatId) ?: throw Exception("Chat not found")
                    val messagesToSend = chat.messages.filter { it.id != assistantMessageId }

                    val reasoningEffort = if (_extendedThinking.value) "high" else "medium"
                    val systemPrompt = modelInfo?.systemPrompt

                    Log.i(TAG, "sendMessage: model=$model messages=${messagesToSend.size} reasoning=$reasoningEffort")

                    var accumulatedContent = ""
                    var lastUpdate = 0L
                    val updateThrottleMs = 100L

                    repository.streamChatCompletion(
                        model = model,
                        messages = messagesToSend,
                        systemPrompt = systemPrompt,
                        reasoningEffort = reasoningEffort,
                    ).collect { chunk ->
                        accumulatedContent += chunk
                        val now = System.currentTimeMillis()
                        if (now - lastUpdate >= updateThrottleMs) {
                            repository.updateMessage(chatId, assistantMessageId, accumulatedContent)
                            lastUpdate = now
                        }
                    }

                    Log.i(TAG, "Stream completed, content length: ${accumulatedContent.length}")
                    // Final update with complete content
                    repository.updateMessage(chatId, assistantMessageId, accumulatedContent)
                } catch (e: Exception) {
                    if (e is kotlinx.coroutines.CancellationException) {
                        Log.i(TAG, "Stream cancelled by user")
                    } else {
                        Log.e(TAG, "Stream error", e)
                        var errorMessage = "Error: ${e.message ?: "Unknown error"}"
                        if (e is ApiException && e.statusCode == 401) {
                            errorMessage += "\n\nYour API key may be invalid or expired. Please update your API key in settings."
                        }
                        repository.updateMessage(chatId, assistantMessageId, errorMessage)
                    }
                } finally {
                    repository.setStreaming(chatId, false)
                    repository.saveAfterStreaming()
                    _isGenerating.value = false
                    streamingJob = null
                }
            }
        }
    }

    fun stopGeneration() {
        streamingJob?.cancel()
        streamingJob = null
    }

    fun getWordCount(): Int {
        return currentChat.value?.wordCount ?: 0
    }

    fun getMaxWords(): Int {
        val model = _selectedModel.value ?: return 60000
        return MODEL_CONFIG[model]?.maxWords ?: 60000
    }

    fun supportsFileUploads(): Boolean {
        val model = _selectedModel.value ?: return true
        return MODEL_CONFIG[model]?.supportsFileUploads ?: false
    }

    fun supportsExtendedThinking(): Boolean {
        val model = _selectedModel.value ?: return false
        return MODEL_CONFIG[model]?.supportsExtendedThinking ?: false
    }

    fun getFilteredModels() = repository.getFilteredModels()

    private fun getFileName(context: Context, uri: Uri): String {
        var name = "file"
        context.contentResolver.query(uri, null, null, null, null)?.use { cursor ->
            val nameIndex = cursor.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME)
            if (nameIndex >= 0 && cursor.moveToFirst()) {
                name = cursor.getString(nameIndex)
            }
        }
        return name
    }

    class Factory(private val repository: ChatRepository) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            return ChatViewModel(repository) as T
        }
    }
}
