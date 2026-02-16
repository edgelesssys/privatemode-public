/*
 * JNI bridge between the Android app and the Go-compiled libprivatemode.
 *
 * The Go library exports these C functions:
 *   int PrivatemodeStartProxy(void) -> returns port (>0) or -1 on error
 *   char* CurrentManifest(void) -> returns JSON string (caller must free)
 *
 * This JNI layer translates between Java/Kotlin types and the C FFI.
 */

#include <jni.h>
#include <string.h>
#include <stdlib.h>

/*
 * Forward declarations of the Go-exported functions.
 * These are defined in the cross-compiled libprivatemode.so.
 *
 * Note: The Go function PrivatemodeStartProxy returns a struct with two fields
 * (int port, char* error). In cgo, this becomes a struct with r0 and r1 fields.
 */
struct PrivatemodeStartProxy_return {
    long r0; /* port or -1 */
    char* r1; /* error message (NULL on success) */
};

extern struct PrivatemodeStartProxy_return PrivatemodeStartProxy(void);
extern char* CurrentManifest(void);

/*
 * JNI method: nativeStartProxy
 *
 * Returns a ProxyStartResult object with port and error fields.
 * Matches: ai.privatemode.android.proxy.NativeProxy.nativeStartProxy(String)
 *
 * Note: HOME and XDG_CONFIG_HOME must be set BEFORE loading the Go library
 * (in Kotlin via android.system.Os.setenv) because Go caches the environment
 * at runtime init. The dataDir parameter is accepted for API compatibility
 * but env setup is handled on the Kotlin side.
 */
JNIEXPORT jobject JNICALL
Java_ai_privatemode_android_proxy_NativeProxy_nativeStartProxy(
    JNIEnv *env, jobject thiz, jstring dataDir) {

    /* Call the Go function */
    struct PrivatemodeStartProxy_return result = PrivatemodeStartProxy();

    /* Find the ProxyStartResult class */
    jclass resultClass = (*env)->FindClass(env,
        "ai/privatemode/android/proxy/ProxyStartResult");
    if (resultClass == NULL) return NULL;

    /* Find the constructor: ProxyStartResult(int port, String? error) */
    jmethodID constructor = (*env)->GetMethodID(env, resultClass,
        "<init>", "(ILjava/lang/String;)V");
    if (constructor == NULL) return NULL;

    /* Convert error string if present */
    jstring errorStr = NULL;
    if (result.r0 == -1 && result.r1 != NULL) {
        errorStr = (*env)->NewStringUTF(env, result.r1);
        free(result.r1);
    }

    /* Create and return the result object */
    return (*env)->NewObject(env, resultClass, constructor,
        (jint)result.r0, errorStr);
}

/*
 * JNI method: nativeGetCurrentManifest
 *
 * Returns the current manifest JSON string.
 * Matches: ai.privatemode.android.proxy.NativeProxy.nativeGetCurrentManifest()
 */
JNIEXPORT jstring JNICALL
Java_ai_privatemode_android_proxy_NativeProxy_nativeGetCurrentManifest(
    JNIEnv *env, jobject thiz) {

    char *manifest = CurrentManifest();
    if (manifest == NULL) {
        return (*env)->NewStringUTF(env, "");
    }

    jstring result = (*env)->NewStringUTF(env, manifest);
    free(manifest);
    return result;
}
