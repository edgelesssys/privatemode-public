package ai.privatemode.android

import android.app.Application
import ai.privatemode.android.data.local.ChatStorage
import ai.privatemode.android.data.local.PreferencesManager
import ai.privatemode.android.data.repository.ChatRepository
import ai.privatemode.android.proxy.ProxyManager

class PrivatemodeApp : Application() {

    lateinit var preferences: PreferencesManager
        private set
    lateinit var chatStorage: ChatStorage
        private set
    lateinit var proxyManager: ProxyManager
        private set
    lateinit var repository: ChatRepository
        private set

    override fun onCreate() {
        super.onCreate()

        preferences = PreferencesManager(this)
        chatStorage = ChatStorage(this)
        proxyManager = ProxyManager(this)
        repository = ChatRepository(chatStorage, preferences, proxyManager)
    }
}
