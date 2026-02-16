package ai.privatemode.android.proxy

import android.content.Context
import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import java.io.File

/**
 * Manages the lifecycle of the embedded Privatemode proxy.
 *
 * The proxy runs as a native library loaded into the app process,
 * identical to how the desktop Electron app operates. It:
 * 1. Binds to localhost on a random port
 * 2. Performs remote attestation against the Privatemode backend
 * 3. Establishes HPKE-encrypted communication channel
 * 4. Proxies API requests with field-level encryption
 *
 * The Android app connects to this local proxy for all API calls,
 * ensuring end-to-end encryption from the device to the TEE backend.
 */
class ProxyManager(private val context: Context) {

    companion object {
        private const val TAG = "ProxyManager"
    }

    sealed class ProxyState {
        data object NotStarted : ProxyState()
        data object Loading : ProxyState()
        data class Running(val port: Int) : ProxyState()
        data class Error(val message: String) : ProxyState()
    }

    private val _state = MutableStateFlow<ProxyState>(ProxyState.NotStarted)
    val state: StateFlow<ProxyState> = _state.asStateFlow()

    private var proxyPort: Int = -1

    /**
     * Initialize the proxy: load the native library and start the local server.
     * Must be called before any API requests are made.
     */
    suspend fun initialize() = withContext(Dispatchers.IO) {
        _state.value = ProxyState.Loading

        // Set up workspace directory for manifest cache and logs
        val workspace = File(context.filesDir, "privatemode")
        workspace.mkdirs()

        // The Go code reads UserConfigDir which on Android maps to the app's files dir.
        // We set HOME so the Go code can find its config directory.
        try {
            val env = mapOf(
                "HOME" to context.filesDir.absolutePath,
                "XDG_CONFIG_HOME" to context.filesDir.absolutePath,
            )
            for ((key, value) in env) {
                try {
                    Os_setenv(key, value)
                } catch (_: Exception) {
                    // Best effort
                }
            }
        } catch (_: Exception) {
            // Environment setting is best-effort
        }

        // Load the native library
        if (!NativeProxy.loadLibrary()) {
            val error = NativeProxy.getLoadError() ?: "Unknown error loading native library"
            Log.e(TAG, "Failed to load native proxy library: $error")
            _state.value = ProxyState.Error(
                "Failed to load proxy library. The native library may not be bundled for this device architecture."
            )
            return@withContext
        }

        Log.i(TAG, "Native proxy library loaded successfully")

        // Start the proxy
        try {
            proxyPort = NativeProxy.startProxy()
            Log.i(TAG, "Proxy started on port $proxyPort")
            _state.value = ProxyState.Running(proxyPort)
        } catch (e: ProxyException) {
            Log.e(TAG, "Failed to start proxy: ${e.message}")
            _state.value = ProxyState.Error("Failed to start proxy: ${e.message}")
        }
    }

    /**
     * Get the base URL for connecting to the local proxy.
     */
    fun getBaseUrl(): String {
        return "http://127.0.0.1:$proxyPort"
    }

    /**
     * Get the proxy port, or -1 if not running.
     */
    fun getPort(): Int = proxyPort

    /**
     * Check if the proxy is running and ready.
     */
    fun isRunning(): Boolean = _state.value is ProxyState.Running

    /**
     * Get the current manifest from the proxy.
     */
    fun getCurrentManifest(): String {
        return NativeProxy.getCurrentManifest()
    }

    /**
     * Attempt to set an environment variable via reflection.
     * This is used to configure the Go runtime's view of the filesystem.
     */
    private fun Os_setenv(name: String, value: String) {
        try {
            val processEnvironment = Class.forName("java.lang.ProcessEnvironment")
            val method = processEnvironment.getDeclaredMethod(
                "setenv", String::class.java, String::class.java
            )
            method.isAccessible = true
            method.invoke(null, name, value)
        } catch (_: Exception) {
            // Fallback: try android.system.Os
            try {
                val osClass = Class.forName("android.system.Os")
                val setenvMethod = osClass.getMethod(
                    "setenv", String::class.java, String::class.java, Boolean::class.javaPrimitiveType
                )
                setenvMethod.invoke(null, name, value, true)
            } catch (_: Exception) {
                // Give up silently
            }
        }
    }
}
