package ai.privatemode.android.ui.settings

import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Error
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import ai.privatemode.android.ui.theme.*

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    viewModel: SettingsViewModel,
    onBack: () -> Unit,
) {
    val apiKey by viewModel.apiKey.collectAsState()
    val serverUrl by viewModel.serverUrl.collectAsState()

    var editApiKey by remember(apiKey) { mutableStateOf(apiKey ?: "") }
    var showApiKey by remember { mutableStateOf(false) }
    var apiKeyError by remember { mutableStateOf("") }
    var apiKeySuccess by remember { mutableStateOf(false) }
    var showDeleteConfirm by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        TopAppBar(
            title = { Text("Settings", style = MaterialTheme.typography.displayLarge) },
            navigationIcon = {
                IconButton(onClick = onBack) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                }
            },
            colors = TopAppBarDefaults.topAppBarColors(containerColor = BackgroundLight),
        )

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(24.dp),
        ) {
            // Access Key section
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = SurfaceWhite),
                elevation = CardDefaults.cardElevation(defaultElevation = 1.dp),
            ) {
                Column(modifier = Modifier.padding(20.dp)) {
                    Text(
                        text = "Access key",
                        style = MaterialTheme.typography.headlineMedium,
                    )

                    Spacer(modifier = Modifier.height(8.dp))

                    Text(
                        text = "Set your Privatemode access key from the portal.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextSecondary,
                    )

                    Spacer(modifier = Modifier.height(16.dp))

                    OutlinedTextField(
                        value = editApiKey,
                        onValueChange = {
                            editApiKey = it
                            apiKeyError = ""
                            apiKeySuccess = false
                        },
                        modifier = Modifier.fillMaxWidth(),
                        placeholder = { Text("550e8400-e2...") },
                        visualTransformation = if (showApiKey) VisualTransformation.None else PasswordVisualTransformation(),
                        trailingIcon = {
                            IconButton(onClick = { showApiKey = !showApiKey }) {
                                Icon(
                                    if (showApiKey) Icons.Default.VisibilityOff else Icons.Default.Visibility,
                                    contentDescription = null,
                                )
                            }
                        },
                        isError = apiKeyError.isNotEmpty(),
                        singleLine = true,
                        shape = RoundedCornerShape(8.dp),
                        colors = OutlinedTextFieldDefaults.colors(
                            focusedBorderColor = Purple,
                            unfocusedBorderColor = BorderInput,
                            errorBorderColor = ErrorRed,
                        ),
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                        keyboardActions = KeyboardActions(onDone = {
                            validateAndSaveApiKey(editApiKey, viewModel,
                                onError = { apiKeyError = it },
                                onSuccess = { apiKeySuccess = true; apiKeyError = "" }
                            )
                        }),
                    )

                    if (apiKeyError.isNotEmpty()) {
                        Spacer(modifier = Modifier.height(8.dp))
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Default.Error, null, modifier = Modifier.size(16.dp), tint = ErrorRed)
                            Spacer(modifier = Modifier.width(4.dp))
                            Text(apiKeyError, style = MaterialTheme.typography.bodySmall, color = ErrorRed)
                        }
                    }

                    if (apiKeySuccess) {
                        Spacer(modifier = Modifier.height(8.dp))
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Default.CheckCircle, null, modifier = Modifier.size(16.dp), tint = Purple)
                            Spacer(modifier = Modifier.width(4.dp))
                            Text("Access key updated successfully", style = MaterialTheme.typography.bodySmall, color = Purple)
                        }
                    }

                    Spacer(modifier = Modifier.height(16.dp))

                    Button(
                        onClick = {
                            validateAndSaveApiKey(editApiKey, viewModel,
                                onError = { apiKeyError = it },
                                onSuccess = { apiKeySuccess = true; apiKeyError = "" }
                            )
                        },
                        colors = ButtonDefaults.buttonColors(containerColor = Purple),
                        shape = RoundedCornerShape(8.dp),
                    ) {
                        Text("Update")
                    }
                }
            }

            // Server URL section
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = SurfaceWhite),
                elevation = CardDefaults.cardElevation(defaultElevation = 1.dp),
            ) {
                Column(modifier = Modifier.padding(20.dp)) {
                    Text(
                        text = "Server URL",
                        style = MaterialTheme.typography.headlineMedium,
                    )

                    Spacer(modifier = Modifier.height(8.dp))

                    Text(
                        text = "The proxy connects to this endpoint. Only change if using a custom deployment.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextSecondary,
                    )

                    Spacer(modifier = Modifier.height(16.dp))

                    Text(
                        text = serverUrl,
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextPrimary,
                        fontWeight = FontWeight.W500,
                    )
                }
            }

            // Danger zone
            Card(
                modifier = Modifier
                    .fillMaxWidth()
                    .border(1.dp, ErrorRed.copy(alpha = 0.3f), RoundedCornerShape(12.dp)),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = SurfaceWhite),
                elevation = CardDefaults.cardElevation(defaultElevation = 1.dp),
            ) {
                Column(modifier = Modifier.padding(20.dp)) {
                    Text(
                        text = "Danger zone",
                        style = MaterialTheme.typography.headlineMedium,
                    )

                    Spacer(modifier = Modifier.height(8.dp))

                    Text(
                        text = "Delete all conversations permanently",
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextSecondary,
                    )

                    Spacer(modifier = Modifier.height(16.dp))

                    if (!showDeleteConfirm) {
                        OutlinedButton(
                            onClick = { showDeleteConfirm = true },
                            colors = ButtonDefaults.outlinedButtonColors(contentColor = ErrorRed),
                            border = ButtonDefaults.outlinedButtonBorder(true).copy(
                                brush = androidx.compose.ui.graphics.SolidColor(ErrorRed)
                            ),
                            shape = RoundedCornerShape(8.dp),
                        ) {
                            Icon(Icons.Default.Delete, null, modifier = Modifier.size(20.dp))
                            Spacer(modifier = Modifier.width(8.dp))
                            Text("Delete all chats")
                        }
                    } else {
                        Card(
                            colors = CardDefaults.cardColors(containerColor = DangerBg),
                            shape = RoundedCornerShape(8.dp),
                        ) {
                            Column(modifier = Modifier.padding(16.dp)) {
                                Text(
                                    text = "Are you sure? This action cannot be undone.",
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = ErrorRed,
                                    fontWeight = FontWeight.W500,
                                )
                                Spacer(modifier = Modifier.height(12.dp))
                                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                    Button(
                                        onClick = {
                                            viewModel.clearAllChats()
                                            showDeleteConfirm = false
                                            onBack()
                                        },
                                        colors = ButtonDefaults.buttonColors(containerColor = ErrorRed),
                                        shape = RoundedCornerShape(8.dp),
                                    ) {
                                        Text("Yes, delete all")
                                    }
                                    OutlinedButton(
                                        onClick = { showDeleteConfirm = false },
                                        shape = RoundedCornerShape(8.dp),
                                    ) {
                                        Text("Cancel")
                                    }
                                }
                            }
                        }
                    }
                }
            }

            Spacer(modifier = Modifier.height(40.dp))
        }
    }
}

private fun validateAndSaveApiKey(
    apiKey: String,
    viewModel: SettingsViewModel,
    onError: (String) -> Unit,
    onSuccess: () -> Unit,
) {
    val trimmed = apiKey.trim()
    if (trimmed.isEmpty()) {
        onError("Please enter an access key")
        return
    }

    val uuidV4Regex = Regex(
        "^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-4[0-9A-Fa-f]{3}-[89ABab][0-9A-Fa-f]{3}-[0-9A-Fa-f]{12}$"
    )
    if (!uuidV4Regex.matches(trimmed)) {
        onError("Invalid access key format")
        return
    }

    viewModel.updateApiKey(trimmed)
    onSuccess()
}
