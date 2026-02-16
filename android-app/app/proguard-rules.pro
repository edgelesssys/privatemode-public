# OkHttp
-dontwarn okhttp3.**
-dontwarn okio.**
-keep class okhttp3.** { *; }

# Gson
-keepattributes Signature
-keepattributes *Annotation*
-keep class ai.privatemode.android.data.model.** { *; }
-keep class ai.privatemode.android.data.remote.** { *; }

# Native proxy (JNI)
-keep class ai.privatemode.android.proxy.** { *; }

# Markwon
-keep class io.noties.markwon.** { *; }
-keep class io.noties.prism4j.** { *; }
