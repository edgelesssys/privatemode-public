package ai.privatemode.android.proxy

/**
 * JNI bridge to the native libprivatemode library.
 *
 * This class loads the cross-compiled Go library that implements the
 * Privatemode proxy. The proxy handles:
 * - Remote attestation of the confidential computing backend (AMD SEV-SNP)
 * - HPKE secret exchange for end-to-end encryption
 * - Request encryption and response decryption
 * - Manifest verification
 *
 * The native library exports the same C functions as the desktop version:
 * - PrivatemodeStartProxy() -> (port: int, error: *char)
 * - CurrentManifest() -> *char
 */
object NativeProxy {

    private var loaded = false
    private var loadError: String? = null

    /**
     * Attempt to load the native library.
     * Returns true if successful, false otherwise.
     */
    @Synchronized
    fun loadLibrary(): Boolean {
        if (loaded) return true
        return try {
            System.loadLibrary("privatemode")
            loaded = true
            loadError = null
            true
        } catch (e: UnsatisfiedLinkError) {
            loadError = e.message
            false
        }
    }

    fun isLoaded(): Boolean = loaded
    fun getLoadError(): String? = loadError

    /**
     * Start the proxy server. Returns the port number on success,
     * or throws ProxyException on failure.
     *
     * @param dataDir The app's internal files directory. This is passed to the
     *   Go proxy so it can set HOME/XDG_CONFIG_HOME for os.UserConfigDir(),
     *   which is used to cache attestation data (KDS certs, manifests).
     *
     * The proxy binds to 127.0.0.1 on a random available port and
     * begins the attestation + secret exchange in a background goroutine.
     */
    fun startProxy(dataDir: String): Int {
        if (!loaded) throw ProxyException("Native library not loaded")
        val result = nativeStartProxy(dataDir)
        if (result.port < 0) {
            throw ProxyException(result.error ?: "Unknown proxy error")
        }
        return result.port
    }

    /**
     * Get the current manifest JSON from the proxy.
     * Returns empty string if the proxy hasn't completed attestation yet.
     */
    fun getCurrentManifest(): String {
        if (!loaded) return ""
        return nativeGetCurrentManifest()
    }

    // JNI native methods
    private external fun nativeStartProxy(dataDir: String): ProxyStartResult
    private external fun nativeGetCurrentManifest(): String
}

/**
 * Result from starting the native proxy.
 */
data class ProxyStartResult(
    val port: Int,
    val error: String?,
)

class ProxyException(message: String) : Exception(message)
