package ai.privatemode.android.proxy

import android.content.Context
import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext

/**
 * Manages the connection to the Privatemode backend.
 *
 * Connection modes:
 *
 * 1. **Proxy mode** (preferred): Loads the native proxy library (libprivatemode.so)
 *    via JNI and connects through a local proxy, identical to the desktop app.
 *    This enables:
 *    - Client-side TEE attestation (AMD SEV-SNP) via the Contrast SDK
 *    - HPKE field-level E2E encryption on top of TLS
 *    - Manifest verification with hash display in the Security screen
 *
 * 2. **Direct mode** (fallback): Connects directly to the Privatemode API over
 *    HTTPS when the native proxy library is not present. Transport encryption
 *    via TLS only; no client-side attestation verification.
 */
class ProxyManager(private val context: Context) {

    companion object {
        private const val TAG = "ProxyManager"
        const val DEFAULT_API_URL = "https://api.privatemode.ai"
    }

    sealed class ProxyState {
        data object NotStarted : ProxyState()
        data object Loading : ProxyState()
        data class Running(val port: Int) : ProxyState()
        data class DirectMode(val baseUrl: String) : ProxyState()
        data class Error(val message: String) : ProxyState()
    }

    private val _state = MutableStateFlow<ProxyState>(ProxyState.NotStarted)
    val state: StateFlow<ProxyState> = _state.asStateFlow()

    private var baseUrl: String = DEFAULT_API_URL
    private var proxyPort: Int = -1
    private var usingNativeProxy = false

    /**
     * Initialize the connection.
     * Attempts to load the native proxy library; falls back to direct HTTPS mode.
     */
    suspend fun initialize() = withContext(Dispatchers.IO) {
        _state.value = ProxyState.Loading

        // Try native proxy first
        if (tryNativeProxy()) {
            return@withContext
        }

        // Fall back to direct HTTPS connection
        Log.i(TAG, "Using direct HTTPS connection to $baseUrl")
        _state.value = ProxyState.DirectMode(baseUrl)
    }

    /**
     * Attempt to load and start the native proxy.
     * Returns true if successful.
     */
    private fun tryNativeProxy(): Boolean {
        if (!NativeProxy.loadLibrary()) {
            Log.i(TAG, "Native proxy library not available: ${NativeProxy.getLoadError()}")
            return false
        }

        Log.i(TAG, "Native proxy library loaded, starting proxy...")
        return try {
            val dataDir = context.filesDir.absolutePath
            proxyPort = NativeProxy.startProxy(dataDir)
            baseUrl = "http://127.0.0.1:$proxyPort"
            usingNativeProxy = true
            Log.i(TAG, "Native proxy started on port $proxyPort")
            _state.value = ProxyState.Running(proxyPort)
            true
        } catch (e: ProxyException) {
            Log.w(TAG, "Native proxy failed to start: ${e.message}")
            false
        }
    }

    /**
     * Get the base URL for API requests.
     * Returns the local proxy URL if running, otherwise the direct API URL.
     */
    fun getBaseUrl(): String = baseUrl

    /**
     * Set a custom API URL (for direct mode).
     */
    fun setBaseUrl(url: String) {
        if (!usingNativeProxy) {
            baseUrl = url
        }
    }

    /**
     * Whether the native proxy is running (vs direct HTTPS mode).
     */
    fun isUsingNativeProxy(): Boolean = usingNativeProxy

    /**
     * Whether the manager is ready to handle requests.
     */
    fun isReady(): Boolean {
        val s = _state.value
        return s is ProxyState.Running || s is ProxyState.DirectMode
    }

    /**
     * Get the current manifest from the native proxy.
     * Returns empty string in direct mode or if proxy hasn't completed attestation.
     */
    fun getCurrentManifest(): String {
        if (!usingNativeProxy) return ""
        return NativeProxy.getCurrentManifest()
    }
}
