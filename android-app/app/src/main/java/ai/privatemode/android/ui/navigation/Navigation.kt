package ai.privatemode.android.ui.navigation

import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import ai.privatemode.android.data.repository.ChatRepository
import ai.privatemode.android.proxy.ProxyManager
import ai.privatemode.android.ui.chat.ChatScreen
import ai.privatemode.android.ui.chat.ChatViewModel
import ai.privatemode.android.ui.components.DrawerContent
import ai.privatemode.android.ui.security.SecurityScreen
import ai.privatemode.android.ui.settings.SettingsScreen
import ai.privatemode.android.ui.settings.SettingsViewModel
import kotlinx.coroutines.launch

sealed class Screen(val route: String) {
    data object Chat : Screen("chat")
    data object Settings : Screen("settings")
    data object Security : Screen("security")
}

@Composable
fun MainNavigation(
    repository: ChatRepository,
    proxyManager: ProxyManager,
) {
    val navController = rememberNavController()
    val drawerState = rememberDrawerState(initialValue = DrawerValue.Closed)
    val scope = rememberCoroutineScope()

    val chatViewModel: ChatViewModel = viewModel(factory = ChatViewModel.Factory(repository))
    val settingsViewModel: SettingsViewModel = viewModel(factory = SettingsViewModel.Factory(repository))

    val chats by chatViewModel.chats.collectAsState()
    val currentChatId by chatViewModel.currentChatId.collectAsState()
    val modelsLoaded by chatViewModel.modelsLoaded.collectAsState()

    ModalNavigationDrawer(
        drawerState = drawerState,
        drawerContent = {
            ModalDrawerSheet {
                DrawerContent(
                    chats = chats,
                    currentChatId = currentChatId,
                    modelsLoaded = modelsLoaded,
                    onNewChat = {
                        chatViewModel.createNewChat()
                        scope.launch { drawerState.close() }
                        // Navigate to chat if not already there
                        if (navController.currentDestination?.route != Screen.Chat.route) {
                            navController.navigate(Screen.Chat.route) {
                                popUpTo(Screen.Chat.route) { inclusive = true }
                            }
                        }
                    },
                    onSelectChat = { chatId ->
                        chatViewModel.selectChat(chatId)
                        scope.launch { drawerState.close() }
                        if (navController.currentDestination?.route != Screen.Chat.route) {
                            navController.navigate(Screen.Chat.route) {
                                popUpTo(Screen.Chat.route) { inclusive = true }
                            }
                        }
                    },
                    onRenameChat = { chatId, newTitle ->
                        chatViewModel.renameChat(chatId, newTitle)
                    },
                    onDeleteChat = { chatId ->
                        chatViewModel.deleteChat(chatId)
                    },
                    onNavigateToSettings = {
                        scope.launch { drawerState.close() }
                        navController.navigate(Screen.Settings.route)
                    },
                    onNavigateToSecurity = {
                        scope.launch { drawerState.close() }
                        navController.navigate(Screen.Security.route)
                    },
                )
            }
        },
    ) {
        NavHost(
            navController = navController,
            startDestination = Screen.Chat.route,
            modifier = Modifier.fillMaxSize(),
            enterTransition = {
                slideIntoContainer(
                    AnimatedContentTransitionScope.SlideDirection.Left,
                    tween(300),
                )
            },
            exitTransition = {
                slideOutOfContainer(
                    AnimatedContentTransitionScope.SlideDirection.Left,
                    tween(300),
                )
            },
            popEnterTransition = {
                slideIntoContainer(
                    AnimatedContentTransitionScope.SlideDirection.Right,
                    tween(300),
                )
            },
            popExitTransition = {
                slideOutOfContainer(
                    AnimatedContentTransitionScope.SlideDirection.Right,
                    tween(300),
                )
            },
        ) {
            composable(Screen.Chat.route) {
                ChatScreen(
                    viewModel = chatViewModel,
                    onOpenDrawer = { scope.launch { drawerState.open() } },
                    onNavigateToSettings = { navController.navigate(Screen.Settings.route) },
                    onNavigateToSecurity = { navController.navigate(Screen.Security.route) },
                )
            }

            composable(Screen.Settings.route) {
                SettingsScreen(
                    viewModel = settingsViewModel,
                    onBack = { navController.popBackStack() },
                )
            }

            composable(Screen.Security.route) {
                SecurityScreen(
                    proxyManager = proxyManager,
                    onBack = { navController.popBackStack() },
                )
            }
        }
    }
}
