package ai.privatemode.android.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable

private val LightColorScheme = lightColorScheme(
    primary = Purple,
    onPrimary = TextOnPurple,
    primaryContainer = PurpleLight,
    onPrimaryContainer = TextOnPurple,
    secondary = SidebarDark,
    onSecondary = TextOnDark,
    background = BackgroundLight,
    onBackground = TextPrimary,
    surface = SurfaceWhite,
    onSurface = TextPrimary,
    surfaceVariant = BackgroundLight,
    onSurfaceVariant = TextSecondary,
    error = ErrorRed,
    onError = TextOnPurple,
    outline = BorderLight,
    outlineVariant = BorderMedium,
)

@Composable
fun PrivatemodeTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = LightColorScheme,
        typography = Typography,
        content = content,
    )
}
