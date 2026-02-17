package ai.privatemode.android.util

import android.content.Context
import android.text.Spanned
import io.noties.markwon.Markwon
import io.noties.markwon.ext.strikethrough.StrikethroughPlugin
import io.noties.markwon.ext.tables.TablePlugin
import io.noties.markwon.html.HtmlPlugin

/**
 * Markdown renderer using Markwon library.
 * Provides rich text rendering for assistant messages including:
 * - Headers, bold, italic, strikethrough
 * - Code blocks with syntax highlighting
 * - Tables
 * - Links
 * - Lists (ordered and unordered)
 * - Blockquotes
 * - HTML subset
 */
object MarkdownRenderer {

    @Volatile
    private var instance: Markwon? = null

    fun create(context: Context): Markwon {
        return instance ?: synchronized(this) {
            instance ?: Markwon.builder(context)
                .usePlugin(StrikethroughPlugin.create())
                .usePlugin(TablePlugin.create(context))
                .usePlugin(HtmlPlugin.create())
                .build()
                .also { instance = it }
        }
    }

    fun render(context: Context, markdown: String): Spanned {
        return create(context).toMarkdown(markdown)
    }
}
