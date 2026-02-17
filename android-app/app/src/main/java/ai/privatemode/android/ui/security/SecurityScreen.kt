package ai.privatemode.android.ui.security

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Build
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Memory
import androidx.compose.material.icons.filled.OpenInNew
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Verified
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.privatemode.android.proxy.ProxyManager
import ai.privatemode.android.ui.theme.*
import com.google.gson.JsonParser
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.security.MessageDigest

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SecurityScreen(
    proxyManager: ProxyManager,
    onBack: () -> Unit,
) {
    val context = LocalContext.current
    val isDirectMode = !proxyManager.isUsingNativeProxy()

    var manifestHash by remember { mutableStateOf("") }
    var trustedMeasurement by remember { mutableStateOf("") }
    var productLine by remember { mutableStateOf("") }
    var minimumTCB by remember { mutableStateOf<Map<String, Int>?>(null) }

    LaunchedEffect(Unit) {
        withContext(Dispatchers.IO) {
            val manifest = proxyManager.getCurrentManifest()
            if (manifest.isNotEmpty()) {
                // Calculate SHA-256 hash
                val digest = MessageDigest.getInstance("SHA-256")
                val hashBytes = digest.digest(manifest.toByteArray(Charsets.UTF_8))
                manifestHash = hashBytes.joinToString("") { "%02x".format(it) }

                // Parse manifest JSON
                try {
                    val parsed = JsonParser.parseString(manifest).asJsonObject
                    val snp = parsed
                        .getAsJsonObject("ReferenceValues")
                        ?.getAsJsonArray("snp")
                        ?.get(0)?.asJsonObject

                    trustedMeasurement = snp?.get("TrustedMeasurement")?.asString ?: ""
                    productLine = snp?.get("ProductName")?.asString ?: ""

                    val tcb = snp?.getAsJsonObject("MinimumTCB")
                    if (tcb != null) {
                        minimumTCB = mapOf(
                            "Bootloader" to (tcb.get("BootloaderVersion")?.asInt ?: 0),
                            "TEE" to (tcb.get("TEEVersion")?.asInt ?: 0),
                            "SNP" to (tcb.get("SNPVersion")?.asInt ?: 0),
                            "Microcode" to (tcb.get("MicrocodeVersion")?.asInt ?: 0),
                        )
                    }
                } catch (_: Exception) {
                    // Invalid JSON, keep defaults
                }
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        TopAppBar(
            title = { Text("Security", style = MaterialTheme.typography.displayLarge) },
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
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            // Secure session highlight
            Card(
                modifier = Modifier
                    .fillMaxWidth()
                    .border(1.dp, SecurityGreen, RoundedCornerShape(12.dp)),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = SurfaceWhite),
            ) {
                Row(
                    modifier = Modifier.padding(20.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(
                        Icons.Default.Security,
                        contentDescription = null,
                        modifier = Modifier.size(28.dp),
                        tint = SecurityGreen,
                    )
                    Spacer(modifier = Modifier.width(12.dp))
                    Column {
                        Text(
                            text = "Your session is secure",
                            style = MaterialTheme.typography.headlineSmall,
                        )
                        Text(
                            text = if (isDirectMode) {
                                "Your connection to Privatemode is encrypted via TLS. The backend runs in a Trusted Execution Environment (TEE) with AMD SEV-SNP."
                            } else {
                                "Your connection to Privatemode is protected by confidential computing technology."
                            },
                            style = MaterialTheme.typography.bodyMedium,
                            color = TextSecondary,
                        )
                    }
                }
            }

            // Remote attestation
            SecuritySection(
                icon = Icons.Default.Verified,
                title = "Remote attestation",
                description = if (isDirectMode) {
                    "Remote attestation verifies the Privatemode deployment before connecting. On Android, attestation details are verified server-side. The desktop app can perform client-side verification using the Contrast SDK."
                } else {
                    "Before establishing a connection, the security of the Privatemode deployment is cryptographically verified. This proves that all components within the deployment run nothing but the expected code."
                },
            ) {
                if (manifestHash.isNotEmpty()) {
                    DataBlock(label = "MANIFEST HASH (SHA-256)", value = manifestHash)
                    Spacer(modifier = Modifier.height(8.dp))
                    LearnMoreLink(
                        text = "Learn how to reproduce this hash",
                        url = "https://docs.privatemode.ai/guides/verify-source",
                    )
                } else if (isDirectMode) {
                    DirectModeNote()
                } else {
                    Text("Loading...", style = MaterialTheme.typography.bodySmall, color = TextTertiary)
                }
            }

            // Reproducible software
            SecuritySection(
                icon = Icons.Default.Build,
                title = "Reproducible software",
                description = "The initial memory contents of each virtual machine running the workloads is cryptographically verified before connecting, proving that the machines have not been tampered with.",
            ) {
                if (trustedMeasurement.isNotEmpty()) {
                    DataBlock(label = "TRUSTED MEASUREMENT", value = trustedMeasurement)
                    Spacer(modifier = Modifier.height(8.dp))
                    LearnMoreLink(
                        text = "Learn how to reproduce this hash",
                        url = "https://docs.privatemode.ai/guides/verify-source",
                    )
                } else if (isDirectMode) {
                    DirectModeNote()
                } else {
                    Text("Loading...", style = MaterialTheme.typography.bodySmall, color = TextTertiary)
                }
            }

            // Hardware-based security
            SecuritySection(
                icon = Icons.Default.Memory,
                title = "Hardware-based security",
                description = "When connecting to Privatemode, the app verifies that all the hardware components are up-to-date and that the latest security updates are available.",
            ) {
                if (productLine.isNotEmpty()) {
                    DataBlock(label = "PRODUCT LINE", value = productLine)
                    Spacer(modifier = Modifier.height(12.dp))
                }
                if (minimumTCB != null) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceEvenly,
                    ) {
                        minimumTCB!!.forEach { (label, value) ->
                            TcbItem(label = label, value = value.toString())
                        }
                    }
                } else if (isDirectMode) {
                    DirectModeNote()
                } else {
                    Text("Loading...", style = MaterialTheme.typography.bodySmall, color = TextTertiary)
                }
            }

            // Learn more
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = BackgroundLight),
            ) {
                LearnMoreLink(
                    text = "Learn more about how Privatemode protects your data",
                    url = "https://docs.privatemode.ai/",
                    modifier = Modifier.padding(20.dp),
                )
            }

            Spacer(modifier = Modifier.height(40.dp))
        }
    }
}

@Composable
private fun DirectModeNote() {
    Surface(
        shape = RoundedCornerShape(8.dp),
        color = BackgroundLight,
    ) {
        Text(
            text = "Client-side attestation details are available on the desktop app. The Android app connects over TLS to the TEE-protected backend.",
            style = MaterialTheme.typography.bodySmall,
            color = TextTertiary,
            modifier = Modifier.padding(12.dp),
        )
    }
}

@Composable
private fun SecuritySection(
    icon: ImageVector,
    title: String,
    description: String,
    content: @Composable () -> Unit,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(12.dp),
        colors = CardDefaults.cardColors(containerColor = SurfaceWhite),
        elevation = CardDefaults.cardElevation(defaultElevation = 1.dp),
    ) {
        Column(modifier = Modifier.padding(20.dp)) {
            // Check badge
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(icon, null, modifier = Modifier.size(24.dp), tint = TextPrimary)
                    Spacer(modifier = Modifier.width(12.dp))
                    Text(title, style = MaterialTheme.typography.headlineSmall)
                }
                Surface(
                    shape = CircleShape,
                    color = SuccessGreen.copy(alpha = 0.1f),
                    modifier = Modifier.size(28.dp),
                ) {
                    Icon(
                        Icons.Default.CheckCircle,
                        null,
                        modifier = Modifier.padding(4.dp),
                        tint = SuccessGreen,
                    )
                }
            }

            Spacer(modifier = Modifier.height(12.dp))

            Text(
                text = description,
                style = MaterialTheme.typography.bodyMedium,
                color = TextSecondary,
                lineHeight = MaterialTheme.typography.bodyMedium.lineHeight,
            )

            Spacer(modifier = Modifier.height(16.dp))

            content()
        }
    }
}

@Composable
private fun DataBlock(label: String, value: String) {
    Surface(
        shape = RoundedCornerShape(8.dp),
        color = BackgroundLight,
    ) {
        Column(modifier = Modifier.padding(12.dp)) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelSmall,
                color = TextTertiary,
                fontWeight = FontWeight.W500,
                letterSpacing = MaterialTheme.typography.labelSmall.letterSpacing,
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = value,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = TextPrimary,
                maxLines = 4,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun TcbItem(label: String, value: String) {
    Surface(
        shape = RoundedCornerShape(8.dp),
        color = BackgroundLight,
    ) {
        Column(
            modifier = Modifier.padding(12.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = label.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                color = TextTertiary,
                fontWeight = FontWeight.W500,
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = value,
                style = MaterialTheme.typography.headlineSmall,
                fontFamily = FontFamily.Monospace,
                fontWeight = FontWeight.SemiBold,
            )
        }
    }
}

@Composable
private fun LearnMoreLink(
    text: String,
    url: String,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    Row(
        modifier = modifier.clickable {
            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        },
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Default.OpenInNew, null, modifier = Modifier.size(16.dp), tint = Purple)
        Spacer(modifier = Modifier.width(6.dp))
        Text(
            text = text,
            style = MaterialTheme.typography.bodySmall,
            color = Purple,
            fontWeight = FontWeight.W500,
        )
    }
}
