package ai.privatemode.android.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Help
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.SupportAgent
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.privatemode.android.data.model.Chat
import ai.privatemode.android.ui.theme.*
import java.util.Calendar

data class GroupedChats(
    val today: List<Chat> = emptyList(),
    val yesterday: List<Chat> = emptyList(),
    val lastWeek: List<Chat> = emptyList(),
    val older: List<Chat> = emptyList(),
)

fun groupChatsByDate(chats: List<Chat>): GroupedChats {
    val cal = Calendar.getInstance()
    val today = Calendar.getInstance().apply {
        set(Calendar.HOUR_OF_DAY, 0)
        set(Calendar.MINUTE, 0)
        set(Calendar.SECOND, 0)
        set(Calendar.MILLISECOND, 0)
    }.timeInMillis

    val yesterday = today - 86_400_000L
    val lastWeek = today - 7 * 86_400_000L

    val grouped = GroupedChats()
    val todayList = mutableListOf<Chat>()
    val yesterdayList = mutableListOf<Chat>()
    val lastWeekList = mutableListOf<Chat>()
    val olderList = mutableListOf<Chat>()

    for (chat in chats) {
        when {
            chat.lastUserMessageAt >= today -> todayList.add(chat)
            chat.lastUserMessageAt >= yesterday -> yesterdayList.add(chat)
            chat.lastUserMessageAt >= lastWeek -> lastWeekList.add(chat)
            else -> olderList.add(chat)
        }
    }

    return GroupedChats(todayList, yesterdayList, lastWeekList, olderList)
}

@Composable
fun DrawerContent(
    chats: List<Chat>,
    currentChatId: String?,
    modelsLoaded: Boolean,
    onNewChat: () -> Unit,
    onSelectChat: (String) -> Unit,
    onRenameChat: (String, String) -> Unit,
    onDeleteChat: (String) -> Unit,
    onNavigateToSettings: () -> Unit,
    onNavigateToSecurity: () -> Unit,
) {
    val sortedChats = remember(chats) {
        chats.sortedByDescending { it.lastUserMessageAt }
    }
    val grouped = remember(sortedChats) {
        groupChatsByDate(sortedChats)
    }

    Column(
        modifier = Modifier
            .fillMaxHeight()
            .width(280.dp)
            .background(SidebarDark)
            .padding(horizontal = 16.dp),
    ) {
        Spacer(modifier = Modifier.height(48.dp))

        // Logo / Title
        Text(
            text = "Privatemode AI",
            style = MaterialTheme.typography.headlineSmall,
            color = TextOnDark,
            fontWeight = FontWeight.Bold,
        )

        Spacer(modifier = Modifier.height(24.dp))

        // New Chat button
        TextButton(
            onClick = onNewChat,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Icon(Icons.Default.Add, contentDescription = null, tint = TextOnDark, modifier = Modifier.size(18.dp))
            Spacer(modifier = Modifier.width(8.dp))
            Text("New Chat", color = TextOnDark, style = MaterialTheme.typography.bodyLarge)
            Spacer(modifier = Modifier.weight(1f))
        }

        Spacer(modifier = Modifier.height(16.dp))

        // Chat list
        LazyColumn(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            if (grouped.today.isNotEmpty()) {
                item { ChatGroupHeader("TODAY") }
                items(grouped.today, key = { it.id }) { chat ->
                    ChatItem(
                        chat = chat,
                        isActive = chat.id == currentChatId,
                        onSelect = { onSelectChat(chat.id) },
                        onRename = { onRenameChat(chat.id, it) },
                        onDelete = { onDeleteChat(chat.id) },
                    )
                }
            }
            if (grouped.yesterday.isNotEmpty()) {
                item { ChatGroupHeader("YESTERDAY") }
                items(grouped.yesterday, key = { it.id }) { chat ->
                    ChatItem(
                        chat = chat,
                        isActive = chat.id == currentChatId,
                        onSelect = { onSelectChat(chat.id) },
                        onRename = { onRenameChat(chat.id, it) },
                        onDelete = { onDeleteChat(chat.id) },
                    )
                }
            }
            if (grouped.lastWeek.isNotEmpty()) {
                item { ChatGroupHeader("LAST 7 DAYS") }
                items(grouped.lastWeek, key = { it.id }) { chat ->
                    ChatItem(
                        chat = chat,
                        isActive = chat.id == currentChatId,
                        onSelect = { onSelectChat(chat.id) },
                        onRename = { onRenameChat(chat.id, it) },
                        onDelete = { onDeleteChat(chat.id) },
                    )
                }
            }
            if (grouped.older.isNotEmpty()) {
                item { ChatGroupHeader("OLDER") }
                items(grouped.older, key = { it.id }) { chat ->
                    ChatItem(
                        chat = chat,
                        isActive = chat.id == currentChatId,
                        onSelect = { onSelectChat(chat.id) },
                        onRename = { onRenameChat(chat.id, it) },
                        onDelete = { onDeleteChat(chat.id) },
                    )
                }
            }
        }

        // Bottom info section
        HorizontalDivider(color = SidebarItemHover, modifier = Modifier.padding(vertical = 8.dp))

        if (modelsLoaded) {
            DrawerInfoItem(
                icon = { Icon(Icons.Default.Security, null, tint = SecurityGreen, modifier = Modifier.size(18.dp)) },
                text = "Your session is secure",
                textColor = SecurityGreen,
                onClick = onNavigateToSecurity,
            )
        } else {
            DrawerInfoItem(
                icon = { Icon(Icons.Default.Shield, null, tint = TextTertiary, modifier = Modifier.size(18.dp)) },
                text = "Connecting...",
                textColor = TextTertiary,
                onClick = {},
            )
        }

        DrawerInfoItem(
            icon = { Icon(Icons.Default.Settings, null, tint = TextOnDark, modifier = Modifier.size(18.dp)) },
            text = "Settings",
            textColor = TextOnDark,
            onClick = onNavigateToSettings,
        )

        Spacer(modifier = Modifier.height(24.dp))
    }
}

@Composable
private fun ChatGroupHeader(title: String) {
    Text(
        text = title,
        style = MaterialTheme.typography.labelSmall,
        color = TextTertiary,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(vertical = 8.dp),
        letterSpacing = MaterialTheme.typography.labelSmall.letterSpacing,
    )
}

@Composable
private fun ChatItem(
    chat: Chat,
    isActive: Boolean,
    onSelect: () -> Unit,
    onRename: (String) -> Unit,
    onDelete: () -> Unit,
) {
    var isRenaming by remember { mutableStateOf(false) }
    var renameValue by remember { mutableStateOf(chat.title) }

    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .clickable {
                if (!isRenaming) onSelect()
            },
        shape = RoundedCornerShape(6.dp),
        color = if (isActive) SidebarItemActive else Color.Transparent,
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (isRenaming) {
                TextField(
                    value = renameValue,
                    onValueChange = { renameValue = it },
                    modifier = Modifier.weight(1f),
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodySmall.copy(color = TextOnDark),
                    colors = TextFieldDefaults.colors(
                        focusedContainerColor = SidebarItemActive,
                        unfocusedContainerColor = SidebarItemActive,
                        focusedTextColor = TextOnDark,
                        cursorColor = TextOnDark,
                    ),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(
                        onDone = {
                            if (renameValue.isNotBlank()) {
                                onRename(renameValue.trim())
                            }
                            isRenaming = false
                        }
                    ),
                )
            } else {
                Text(
                    text = if (chat.title.length > 30) chat.title.take(30) + "..." else chat.title,
                    style = MaterialTheme.typography.bodySmall,
                    color = TextOnDark,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )

                // Actions (visible on active item)
                if (isActive) {
                    IconButton(
                        onClick = {
                            renameValue = chat.title
                            isRenaming = true
                        },
                        modifier = Modifier.size(24.dp),
                    ) {
                        Icon(Icons.Default.Edit, null, modifier = Modifier.size(14.dp), tint = TextTertiary)
                    }
                    IconButton(
                        onClick = onDelete,
                        modifier = Modifier.size(24.dp),
                    ) {
                        Icon(Icons.Default.Delete, null, modifier = Modifier.size(14.dp), tint = TextTertiary)
                    }
                }
            }
        }
    }
}

@Composable
private fun DrawerInfoItem(
    icon: @Composable () -> Unit,
    text: String,
    textColor: Color,
    onClick: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable { onClick() }
            .padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        icon()
        Spacer(modifier = Modifier.width(10.dp))
        Text(
            text = text,
            style = MaterialTheme.typography.bodyMedium,
            color = textColor,
            fontWeight = FontWeight.W500,
        )
    }
}
