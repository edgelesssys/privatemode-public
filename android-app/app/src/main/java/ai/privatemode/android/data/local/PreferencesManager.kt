package ai.privatemode.android.data.local

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.dataStore: DataStore<Preferences> by preferencesDataStore(
    name = "privatemode_preferences"
)

class PreferencesManager(private val context: Context) {

    companion object {
        private val API_KEY = stringPreferencesKey("api_key")
        private val SERVER_URL = stringPreferencesKey("server_url")
        private val SELECTED_MODEL = stringPreferencesKey("selected_model")
        private val EXTENDED_THINKING = booleanPreferencesKey("extended_thinking")

        const val DEFAULT_SERVER_URL = "https://api.privatemode.ai"
    }

    val apiKey: Flow<String?> = context.dataStore.data.map { preferences ->
        preferences[API_KEY]
    }

    val serverUrl: Flow<String> = context.dataStore.data.map { preferences ->
        preferences[SERVER_URL] ?: DEFAULT_SERVER_URL
    }

    val selectedModel: Flow<String?> = context.dataStore.data.map { preferences ->
        preferences[SELECTED_MODEL]
    }

    val extendedThinking: Flow<Boolean> = context.dataStore.data.map { preferences ->
        preferences[EXTENDED_THINKING] ?: false
    }

    suspend fun getApiKey(): String? {
        return context.dataStore.data.first()[API_KEY]
    }

    suspend fun setApiKey(key: String) {
        context.dataStore.data.first()
        context.dataStore.edit { preferences ->
            preferences[API_KEY] = key
        }
    }

    suspend fun clearApiKey() {
        context.dataStore.edit { preferences ->
            preferences.remove(API_KEY)
        }
    }

    suspend fun getServerUrl(): String {
        return context.dataStore.data.first()[SERVER_URL] ?: DEFAULT_SERVER_URL
    }

    suspend fun setServerUrl(url: String) {
        context.dataStore.edit { preferences ->
            preferences[SERVER_URL] = url
        }
    }

    suspend fun setSelectedModel(modelId: String) {
        context.dataStore.edit { preferences ->
            preferences[SELECTED_MODEL] = modelId
        }
    }

    suspend fun setExtendedThinking(enabled: Boolean) {
        context.dataStore.edit { preferences ->
            preferences[EXTENDED_THINKING] = enabled
        }
    }
}
