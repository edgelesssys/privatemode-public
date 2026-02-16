package ai.privatemode.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import ai.privatemode.android.proxy.ProxyManager
import ai.privatemode.android.ui.navigation.MainNavigation
import ai.privatemode.android.ui.setup.SetupScreen
import ai.privatemode.android.ui.theme.BackgroundLight
import ai.privatemode.android.ui.theme.PrivatemodeTheme
import ai.privatemode.android.ui.theme.Purple
import ai.privatemode.android.ui.theme.TextSecondary
import ai.privatemode.android.ui.theme.TextTertiary
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        val app = application as PrivatemodeApp

        setContent {
            PrivatemodeTheme {
                AppContent(app)
            }
        }
    }
}

@Composable
private fun AppContent(app: PrivatemodeApp) {
    val scope = rememberCoroutineScope()

    val apiKey by app.preferences.apiKey.collectAsState(initial = null)
    var initialized by remember { mutableStateOf(false) }
    var apiKeyChecked by remember { mutableStateOf(false) }

    val proxyState by app.proxyManager.state.collectAsState()

    // Initialize storage and proxy on first composition
    LaunchedEffect(Unit) {
        app.repository.initialize()
        app.proxyManager.initialize()
        apiKeyChecked = true
    }

    // When proxy is running and we have an API key, load models
    LaunchedEffect(proxyState, apiKey) {
        if (proxyState is ProxyManager.ProxyState.Running && apiKey != null) {
            app.repository.loadModels()
            initialized = true
        }
    }

    when {
        // Still checking API key
        !apiKeyChecked -> {
            LoadingScreen("Starting Privatemode...")
        }
        // No API key -> show setup
        apiKey == null || apiKey?.isEmpty() == true -> {
            SetupScreen(
                onApiKeySubmitted = { key ->
                    scope.launch {
                        app.repository.setApiKey(key)
                    }
                },
            )
        }
        // Proxy loading
        proxyState is ProxyManager.ProxyState.Loading || proxyState is ProxyManager.ProxyState.NotStarted -> {
            LoadingScreen("Connecting to secure backend...")
        }
        // Proxy error
        proxyState is ProxyManager.ProxyState.Error -> {
            ErrorScreen(
                message = (proxyState as ProxyManager.ProxyState.Error).message,
                onRetry = {
                    scope.launch {
                        app.proxyManager.initialize()
                    }
                },
            )
        }
        // Ready
        else -> {
            MainNavigation(
                repository = app.repository,
                proxyManager = app.proxyManager,
            )
        }
    }
}

@Composable
private fun LoadingScreen(message: String) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(BackgroundLight),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Icon(
                Icons.Default.Shield,
                contentDescription = null,
                modifier = Modifier.size(64.dp),
                tint = Purple,
            )
            Spacer(modifier = Modifier.height(24.dp))
            CircularProgressIndicator(color = Purple)
            Spacer(modifier = Modifier.height(16.dp))
            Text(
                text = message,
                style = MaterialTheme.typography.bodyMedium,
                color = TextSecondary,
            )
        }
    }
}

@Composable
private fun ErrorScreen(
    message: String,
    onRetry: () -> Unit,
) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(BackgroundLight),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Icon(
                Icons.Default.Shield,
                contentDescription = null,
                modifier = Modifier.size(64.dp),
                tint = Purple.copy(alpha = 0.5f),
            )
            Spacer(modifier = Modifier.height(24.dp))
            Text(
                text = "Connection Error",
                style = MaterialTheme.typography.headlineMedium,
                color = TextSecondary,
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = message,
                style = MaterialTheme.typography.bodyMedium,
                color = TextTertiary,
            )
            Spacer(modifier = Modifier.height(24.dp))
            androidx.compose.material3.Button(
                onClick = onRetry,
                colors = androidx.compose.material3.ButtonDefaults.buttonColors(containerColor = Purple),
            ) {
                Text("Retry")
            }
        }
    }
}
