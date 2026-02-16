package ai.privatemode.android.ui.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import ai.privatemode.android.data.repository.ChatRepository
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

class SettingsViewModel(
    private val repository: ChatRepository,
) : ViewModel() {

    val apiKey: StateFlow<String?> = repository.apiKey
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), null)

    val serverUrl: StateFlow<String> = repository.serverUrl
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), "")

    fun updateApiKey(key: String) {
        viewModelScope.launch {
            repository.setApiKey(key)
        }
    }

    fun updateServerUrl(url: String) {
        viewModelScope.launch {
            repository.setServerUrl(url)
        }
    }

    fun clearAllChats() {
        viewModelScope.launch {
            repository.clearAllChats()
        }
    }

    class Factory(private val repository: ChatRepository) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            return SettingsViewModel(repository) as T
        }
    }
}
