package ai.privatemode.android.data.remote

import android.util.Log
import ai.privatemode.android.data.model.ApiModel
import ai.privatemode.android.data.model.Message
import ai.privatemode.android.data.model.ModelsResponse
import com.google.gson.Gson
import com.google.gson.JsonObject
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.asRequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.BufferedReader
import java.io.File
import java.io.IOException
import java.io.InputStreamReader
import java.util.concurrent.TimeUnit

class PrivatemodeClient(
    private val baseUrl: String,
    private val apiKey: String,
) {
    private val TAG = "PrivatemodeClient"
    private val gson = Gson()
    private val client = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(120, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .addInterceptor(Interceptor { chain ->
            val request = chain.request().newBuilder()
                .addHeader("Privatemode-Version", "v1.23.0")
                .addHeader("Privatemode-Client", "App")
                .build()
            chain.proceed(request)
        })
        .build()

    suspend fun fetchModels(): List<ApiModel> = withContext(Dispatchers.IO) {
        val request = Request.Builder()
            .url("$baseUrl/v1/models")
            .addHeader("Authorization", "Bearer $apiKey")
            .addHeader("Content-Type", "application/json")
            .get()
            .build()

        val response = client.newCall(request).execute()
        if (!response.isSuccessful) {
            throw ApiException(
                "Failed to fetch models: ${response.code} ${response.message}",
                response.code
            )
        }

        val body = response.body?.string() ?: throw ApiException("Response body is null")
        val modelsResponse = gson.fromJson(body, ModelsResponse::class.java)
        modelsResponse.data
    }

    fun streamChatCompletion(
        model: String,
        messages: List<Message>,
        systemPrompt: String? = null,
        reasoningEffort: String? = null,
    ): Flow<String> = callbackFlow {
        val apiMessages = mutableListOf<JsonObject>()

        if (systemPrompt != null) {
            apiMessages.add(JsonObject().apply {
                addProperty("role", "system")
                addProperty("content", systemPrompt)
            })
        }

        for (msg in messages) {
            msg.attachedFiles?.forEach { file ->
                apiMessages.add(JsonObject().apply {
                    addProperty("role", msg.role.toApiString())
                    addProperty("content", "[File: ${file.name}]\n\n${file.content}")
                })
            }
            apiMessages.add(JsonObject().apply {
                addProperty("role", msg.role.toApiString())
                addProperty("content", msg.content)
            })
        }

        val requestBody = JsonObject().apply {
            addProperty("model", model)
            add("messages", gson.toJsonTree(apiMessages))
            addProperty("stream", true)
            if (reasoningEffort != null) {
                addProperty("reasoning_effort", reasoningEffort)
            }
        }

        val request = Request.Builder()
            .url("$baseUrl/v1/chat/completions")
            .addHeader("Authorization", "Bearer $apiKey")
            .addHeader("Content-Type", "application/json")
            .post(requestBody.toString().toRequestBody("application/json".toMediaType()))
            .build()

        val call = client.newCall(request)
        Log.i(TAG, "Sending chat completion to $baseUrl/v1/chat/completions model=$model messages=${messages.size}")

        call.enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                Log.e(TAG, "Chat completion network error", e)
                close(e)
            }

            override fun onResponse(call: Call, response: Response) {
                Log.i(TAG, "Chat completion response: ${response.code} ${response.message}")

                if (!response.isSuccessful) {
                    val errorBody = try { response.body?.string() } catch (_: Exception) { null }
                    Log.e(TAG, "Chat completion failed: ${response.code} body=$errorBody")
                    close(
                        ApiException(
                            "Chat completion failed: ${response.code} ${response.message}" +
                                if (errorBody != null) "\n$errorBody" else "",
                            response.code
                        )
                    )
                    return
                }

                val body = response.body ?: run {
                    Log.e(TAG, "Response body is null")
                    close(ApiException("Response body is null"))
                    return
                }

                try {
                    val reader = BufferedReader(InputStreamReader(body.byteStream()))
                    var line: String?
                    var chunkCount = 0

                    while (reader.readLine().also { line = it } != null) {
                        val trimmed = line?.trim() ?: continue
                        if (trimmed.isEmpty()) continue
                        if (trimmed == "data: [DONE]") {
                            Log.d(TAG, "SSE stream done after $chunkCount chunks")
                            continue
                        }

                        if (trimmed.startsWith("data: ")) {
                            val data = trimmed.substring(6)
                            try {
                                val chunk = gson.fromJson(data, JsonObject::class.java)
                                val choices = chunk.getAsJsonArray("choices")
                                if (choices != null && choices.size() > 0) {
                                    val delta = choices[0].asJsonObject
                                        .getAsJsonObject("delta")
                                    val content = delta?.get("content")?.asString
                                    if (content != null) {
                                        chunkCount++
                                        trySend(content)
                                    }
                                } else if (chunkCount == 0) {
                                    // Log first non-content chunk to help debug format issues
                                    Log.w(TAG, "SSE chunk has no choices: $data")
                                }
                            } catch (e: Exception) {
                                Log.e(TAG, "SSE parse error for chunk: $data", e)
                            }
                        } else {
                            Log.w(TAG, "SSE unexpected line: $trimmed")
                        }
                    }

                    Log.i(TAG, "SSE stream completed, total chunks: $chunkCount")
                    close()
                } catch (e: Exception) {
                    Log.e(TAG, "SSE stream read error", e)
                    close(e)
                }
            }
        })

        awaitClose {
            call.cancel()
        }
    }

    suspend fun uploadFile(file: File, fileName: String): List<UnstructuredElement> =
        withContext(Dispatchers.IO) {
            val requestBody = MultipartBody.Builder()
                .setType(MultipartBody.FORM)
                .addFormDataPart("strategy", "fast")
                .addFormDataPart(
                    "files",
                    fileName,
                    file.asRequestBody("application/octet-stream".toMediaType())
                )
                .build()

            val request = Request.Builder()
                .url("$baseUrl/unstructured/general/v0/general")
                .addHeader("Authorization", "Bearer $apiKey")
                .post(requestBody)
                .build()

            val response = client.newCall(request).execute()
            if (!response.isSuccessful) {
                throw ApiException(
                    "File upload failed: ${response.code} ${response.message}",
                    response.code
                )
            }

            val body = response.body?.string() ?: throw ApiException("Response body is null")
            val elements = gson.fromJson(body, Array<UnstructuredElement>::class.java)
            elements.toList()
        }
}

data class UnstructuredElement(
    val type: String = "",
    val element_id: String = "",
    val text: String = "",
)

class ApiException(
    message: String,
    val statusCode: Int = 0,
) : Exception(message)
