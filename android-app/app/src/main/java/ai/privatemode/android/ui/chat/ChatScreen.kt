package ai.privatemode.android.ui.chat

import android.widget.TextView
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.ChatBubbleOutline
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Description
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import ai.privatemode.android.data.model.MODEL_CONFIG
import ai.privatemode.android.data.model.Message
import ai.privatemode.android.data.model.MessageRole
import ai.privatemode.android.data.model.countWords
import ai.privatemode.android.ui.theme.*
import ai.privatemode.android.util.MarkdownRenderer
import kotlin.math.min
import kotlin.math.roundToInt

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    viewModel: ChatViewModel,
    onOpenDrawer: () -> Unit,
    onNavigateToSettings: () -> Unit,
    onNavigateToSecurity: () -> Unit,
) {
    val currentChat by viewModel.currentChat.collectAsState()
    val isGenerating by viewModel.isGenerating.collectAsState()
    val isUploading by viewModel.isUploading.collectAsState()
    val messageText by viewModel.messageText.collectAsState()
    val selectedModel by viewModel.selectedModel.collectAsState()
    val extendedThinking by viewModel.extendedThinking.collectAsState()
    val attachedFiles by viewModel.attachedFiles.collectAsState()
    val modelsLoaded by viewModel.modelsLoaded.collectAsState()

    val messages = currentChat?.messages ?: emptyList()
    val listState = rememberLazyListState()

    // Auto-scroll to bottom when messages change
    LaunchedEffect(messages.size, messages.lastOrNull()?.content) {
        if (messages.isNotEmpty()) {
            listState.animateScrollToItem(messages.size - 1)
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(BackgroundLight)
            .imePadding(),
    ) {
        // Top bar
        TopAppBar(
            title = {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = currentChat?.title ?: "Privatemode AI",
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        style = MaterialTheme.typography.titleLarge,
                    )
                    if (modelsLoaded) {
                        Spacer(modifier = Modifier.width(8.dp))
                        Icon(
                            Icons.Default.Shield,
                            contentDescription = "Secure",
                            modifier = Modifier
                                .size(16.dp)
                                .clickable { onNavigateToSecurity() },
                            tint = SecurityGreen,
                        )
                    }
                }
            },
            navigationIcon = {
                IconButton(onClick = onOpenDrawer) {
                    Icon(Icons.Default.Menu, contentDescription = "Menu")
                }
            },
            colors = TopAppBarDefaults.topAppBarColors(
                containerColor = SurfaceWhite,
            ),
        )

        // Messages area
        if (currentChat == null || messages.isEmpty()) {
            EmptyState(
                hasChat = currentChat != null,
                modifier = Modifier.weight(1f),
            )
        } else {
            LazyColumn(
                modifier = Modifier.weight(1f),
                state = listState,
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                items(messages, key = { it.id }) { message ->
                    MessageBubble(message = message)
                }
            }
        }

        // Input area
        ChatInputBar(
            messageText = messageText,
            onMessageChange = { viewModel.setMessageText(it) },
            selectedModel = selectedModel,
            extendedThinking = extendedThinking,
            isGenerating = isGenerating,
            isUploading = isUploading,
            attachedFiles = attachedFiles,
            onSend = { viewModel.sendMessage() },
            onStop = { viewModel.stopGeneration() },
            onModelSelect = { viewModel.selectModel(it) },
            onToggleThinking = { viewModel.toggleExtendedThinking() },
            onAttachFile = { context, uri -> viewModel.uploadFile(context, uri) },
            onRemoveFile = { viewModel.removeAttachedFile(it) },
            supportsFileUploads = viewModel.supportsFileUploads(),
            supportsExtendedThinking = viewModel.supportsExtendedThinking(),
            wordCount = viewModel.getWordCount(),
            maxWords = viewModel.getMaxWords(),
            messageWordCount = countWords(messageText),
            attachedFilesWordCount = attachedFiles.sumOf { countWords(it.content) },
            filteredModels = viewModel.getFilteredModels(),
        )
    }
}

@Composable
private fun EmptyState(hasChat: Boolean, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            Icons.Default.ChatBubbleOutline,
            contentDescription = null,
            modifier = Modifier.size(64.dp),
            tint = TextTertiary,
        )
        Spacer(modifier = Modifier.height(16.dp))
        Text(
            text = if (hasChat) "Start a conversation" else "No chat selected",
            style = MaterialTheme.typography.headlineMedium,
            color = TextSecondary,
        )
        Spacer(modifier = Modifier.height(8.dp))
        Text(
            text = if (hasChat) "Send a message to begin chatting" else "Start a new conversation to begin",
            style = MaterialTheme.typography.bodyMedium,
            color = TextTertiary,
        )
    }
}

@Composable
private fun MessageBubble(message: Message) {
    val clipboardManager = LocalClipboardManager.current
    val context = LocalContext.current
    val isUser = message.role == MessageRole.USER

    Column(
        modifier = Modifier.fillMaxWidth(),
        horizontalAlignment = if (isUser) Alignment.End else Alignment.Start,
    ) {
        // Assistant header
        if (!isUser) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.padding(bottom = 4.dp),
            ) {
                Icon(
                    Icons.Default.Shield,
                    contentDescription = null,
                    modifier = Modifier.size(16.dp),
                    tint = Purple,
                )
                Spacer(modifier = Modifier.width(4.dp))
                Text(
                    text = "Privatemode",
                    style = MaterialTheme.typography.labelSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = TextSecondary,
                )
                Spacer(modifier = Modifier.weight(1f))
                IconButton(
                    onClick = {
                        clipboardManager.setText(AnnotatedString(message.content))
                    },
                    modifier = Modifier.size(28.dp),
                ) {
                    Icon(
                        Icons.Default.ContentCopy,
                        contentDescription = "Copy",
                        modifier = Modifier.size(14.dp),
                        tint = TextTertiary,
                    )
                }
            }
        }

        // Attached files
        if (!message.attachedFiles.isNullOrEmpty()) {
            Row(
                modifier = Modifier.padding(bottom = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                for (file in message.attachedFiles) {
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = if (isUser) SurfaceWhite.copy(alpha = 0.1f) else BackgroundLight,
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Icon(
                                Icons.Default.Description,
                                contentDescription = null,
                                modifier = Modifier.size(14.dp),
                                tint = TextSecondary,
                            )
                            Spacer(modifier = Modifier.width(4.dp))
                            Text(
                                text = file.name,
                                style = MaterialTheme.typography.bodySmall,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.widthIn(max = 150.dp),
                            )
                        }
                    }
                }
            }
        }

        // Message content
        Surface(
            shape = RoundedCornerShape(12.dp),
            color = if (isUser) SurfaceWhite else androidx.compose.ui.graphics.Color.Transparent,
            modifier = Modifier.widthIn(max = 320.dp),
        ) {
            if (isUser) {
                Text(
                    text = message.content,
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextUser,
                    modifier = Modifier.padding(12.dp),
                )
            } else {
                if (message.content.isEmpty()) {
                    // Streaming indicator
                    Text(
                        text = "...",
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextTertiary,
                        modifier = Modifier.padding(4.dp),
                    )
                } else {
                    // Markdown rendered content
                    MarkdownContent(
                        content = message.content,
                        modifier = Modifier.padding(4.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun MarkdownContent(content: String, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val textColor = TextPrimary.toArgb()
    val linkColor = Purple.toArgb()

    val markwon = remember(context) {
        MarkdownRenderer.create(context)
    }

    AndroidView(
        modifier = modifier.fillMaxWidth(),
        factory = { ctx ->
            TextView(ctx).apply {
                setTextColor(textColor)
                setLinkTextColor(linkColor)
                textSize = 15f
                linksClickable = true
            }
        },
        update = { textView ->
            val spanned = markwon.toMarkdown(content)
            markwon.setParsedMarkdown(textView, spanned)
        },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ChatInputBar(
    messageText: String,
    onMessageChange: (String) -> Unit,
    selectedModel: String?,
    extendedThinking: Boolean,
    isGenerating: Boolean,
    isUploading: Boolean,
    attachedFiles: List<ai.privatemode.android.data.model.AttachedFile>,
    onSend: () -> Unit,
    onStop: () -> Unit,
    onModelSelect: (String) -> Unit,
    onToggleThinking: () -> Unit,
    onAttachFile: (context: android.content.Context, uri: android.net.Uri) -> Unit,
    onRemoveFile: (Int) -> Unit,
    supportsFileUploads: Boolean,
    supportsExtendedThinking: Boolean,
    wordCount: Int,
    maxWords: Int,
    messageWordCount: Int,
    attachedFilesWordCount: Int,
    filteredModels: List<ai.privatemode.android.data.model.ApiModel>,
) {
    val context = LocalContext.current
    val totalWordCount = wordCount + messageWordCount + attachedFilesWordCount
    val usagePercentage = min((totalWordCount.toFloat() / maxWords * 100), 100f)
    val wouldExceedLimit = totalWordCount > maxWords

    val filePickerLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.GetContent()
    ) { uri ->
        if (uri != null) {
            onAttachFile(context, uri)
        }
    }

    Surface(
        modifier = Modifier.fillMaxWidth(),
        color = SurfaceWhite,
        shadowElevation = 8.dp,
    ) {
        Column(
            modifier = Modifier.padding(12.dp),
        ) {
            // Attached files
            AnimatedVisibility(visible = attachedFiles.isNotEmpty()) {
                FlowRow(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(bottom = 8.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    attachedFiles.forEachIndexed { index, file ->
                        Surface(
                            shape = RoundedCornerShape(8.dp),
                            color = BackgroundLight,
                        ) {
                            Row(
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Icon(
                                    Icons.Default.Description,
                                    contentDescription = null,
                                    modifier = Modifier.size(14.dp),
                                    tint = TextSecondary,
                                )
                                Spacer(modifier = Modifier.width(4.dp))
                                Text(
                                    text = file.name,
                                    style = MaterialTheme.typography.bodySmall,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.widthIn(max = 120.dp),
                                )
                                Spacer(modifier = Modifier.width(4.dp))
                                IconButton(
                                    onClick = { onRemoveFile(index) },
                                    modifier = Modifier.size(18.dp),
                                ) {
                                    Icon(
                                        Icons.Default.Close,
                                        contentDescription = "Remove",
                                        modifier = Modifier.size(14.dp),
                                        tint = TextTertiary,
                                    )
                                }
                            }
                        }
                    }
                }
            }

            // Text input
            OutlinedTextField(
                value = messageText,
                onValueChange = onMessageChange,
                modifier = Modifier.fillMaxWidth(),
                placeholder = { Text("Type a message...") },
                enabled = !isGenerating,
                maxLines = 6,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Default),
                shape = RoundedCornerShape(12.dp),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = BorderLight,
                    unfocusedBorderColor = BorderMedium,
                    disabledBorderColor = BorderMedium.copy(alpha = 0.5f),
                ),
            )

            Spacer(modifier = Modifier.height(8.dp))

            // Button row
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                // Left buttons
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    // Attach button
                    IconButton(
                        onClick = { filePickerLauncher.launch("*/*") },
                        enabled = !isGenerating && !isUploading && supportsFileUploads,
                        modifier = Modifier.size(36.dp),
                    ) {
                        Icon(
                            Icons.Default.AttachFile,
                            contentDescription = "Attach file",
                            modifier = Modifier.size(20.dp),
                            tint = if (supportsFileUploads) TextSecondary else TextTertiary,
                        )
                    }

                    if (isUploading) {
                        Text(
                            text = "Uploading...",
                            style = MaterialTheme.typography.bodySmall,
                            color = TextTertiary,
                        )
                    }

                    // Extended thinking toggle
                    AnimatedVisibility(visible = supportsExtendedThinking) {
                        IconButton(
                            onClick = onToggleThinking,
                            enabled = !isGenerating,
                            modifier = Modifier.size(36.dp),
                        ) {
                            Icon(
                                Icons.Default.Timer,
                                contentDescription = "Extended thinking",
                                modifier = Modifier.size(20.dp),
                                tint = if (extendedThinking) Purple else TextSecondary,
                            )
                        }
                    }
                }

                // Right controls
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    // Token usage indicator
                    if (usagePercentage >= 75) {
                        val bgColor = if (usagePercentage >= 100) DangerBg else WarningBg
                        val textColor = if (usagePercentage >= 100) ErrorRed else WarningYellow
                        Surface(
                            shape = RoundedCornerShape(6.dp),
                            color = bgColor,
                        ) {
                            Text(
                                text = "${usagePercentage.roundToInt()}%",
                                modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                                style = MaterialTheme.typography.labelSmall,
                                fontWeight = FontWeight.SemiBold,
                                color = textColor,
                            )
                        }
                    }

                    // Model picker
                    ModelPickerButton(
                        selectedModel = selectedModel,
                        models = filteredModels,
                        onModelSelect = onModelSelect,
                    )

                    // Send/Stop button
                    if (isGenerating) {
                        IconButton(
                            onClick = onStop,
                            modifier = Modifier
                                .size(40.dp)
                                .clip(CircleShape)
                                .background(ErrorRed.copy(alpha = 0.1f)),
                        ) {
                            Icon(
                                Icons.Default.Stop,
                                contentDescription = "Stop",
                                tint = ErrorRed,
                                modifier = Modifier.size(20.dp),
                            )
                        }
                    } else {
                        IconButton(
                            onClick = onSend,
                            enabled = messageText.isNotBlank() && selectedModel != null && !wouldExceedLimit && !isUploading,
                            modifier = Modifier
                                .size(40.dp)
                                .clip(CircleShape)
                                .background(
                                    if (messageText.isNotBlank() && selectedModel != null)
                                        Purple else Purple.copy(alpha = 0.3f)
                                ),
                        ) {
                            Icon(
                                Icons.AutoMirrored.Filled.Send,
                                contentDescription = "Send",
                                tint = TextOnPurple,
                                modifier = Modifier.size(20.dp),
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ModelPickerButton(
    selectedModel: String?,
    models: List<ai.privatemode.android.data.model.ApiModel>,
    onModelSelect: (String) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }

    Box {
        Surface(
            modifier = Modifier.clickable { expanded = true },
            shape = RoundedCornerShape(6.dp),
            color = BackgroundLight,
        ) {
            Text(
                text = selectedModel?.let { MODEL_CONFIG[it]?.displayName } ?: "Select model",
                modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
                style = MaterialTheme.typography.labelMedium,
                color = TextPrimary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }

        DropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
            modifier = Modifier.background(SidebarDark),
        ) {
            models.forEach { model ->
                val config = MODEL_CONFIG[model.id]
                DropdownMenuItem(
                    text = {
                        Column {
                            Text(
                                text = config?.displayName ?: model.id,
                                style = MaterialTheme.typography.bodyMedium,
                                color = TextOnDark,
                                fontWeight = if (model.id == selectedModel) FontWeight.Bold else FontWeight.Normal,
                            )
                            if (config?.subtitle != null) {
                                Text(
                                    text = config.subtitle,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = TextTertiary,
                                )
                            }
                        }
                    },
                    onClick = {
                        onModelSelect(model.id)
                        expanded = false
                    },
                    modifier = Modifier.background(
                        if (model.id == selectedModel) SidebarItemActive else SidebarDark
                    ),
                )
            }
        }
    }
}
