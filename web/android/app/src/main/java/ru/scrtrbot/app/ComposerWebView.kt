package ru.scrtrbot.app

import android.content.Context
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.AttributeSet
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputMethodManager
import android.widget.Toast
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import java.io.ByteArrayInputStream
import java.util.concurrent.ConcurrentHashMap
import androidx.core.view.inputmethod.EditorInfoCompat
import androidx.core.view.inputmethod.InputConnectionCompat
import androidx.webkit.JavaScriptReplyProxy
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import com.getcapacitor.CapacitorWebView
import org.json.JSONObject
import java.io.File
import java.util.UUID
import java.util.concurrent.Executors

class ComposerWebView(context: Context, attrs: AttributeSet?) : CapacitorWebView(context, attrs) {
    private val worker = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private var focused = false
    private var reply: JavaScriptReplyProxy? = null
    private var origin: Uri? = null
    private val files = ConcurrentHashMap<String, Pair<File, String>>()
    private var session = ""

    fun enableImages(host: String) {
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) return
        origin = Uri.parse(host)
        WebViewCompat.addWebMessageListener(this, "SecretaryIme", setOf(host)) { _, message, _, isMainFrame, proxy ->
            if (!isMainFrame || !trusted()) return@addWebMessageListener
            val data = try { JSONObject(message.data ?: "") } catch (_: Exception) { return@addWebMessageListener }
            when (data.optString("action")) {
                "focus" -> {
                    focused = true
                    session = data.optString("session")
                    reply = proxy
                    context.getSystemService(InputMethodManager::class.java).restartInput(this)
                }
                "blur" -> { focused = false; reply = null }
                "done" -> files.remove(data.optString("id"))?.first?.delete()
            }
        }
    }

    private fun sameOrigin(actual: Uri): Boolean {
        val expected = origin ?: return false
        fun port(uri: Uri) = if (uri.port != -1) uri.port else if (uri.scheme == "https") 443 else 80
        return actual.scheme == expected.scheme && actual.host == expected.host && port(actual) == port(expected)
    }

    private fun trusted() = url?.let { sameOrigin(Uri.parse(it)) } == true

    fun imageRequest(request: WebResourceRequest): WebResourceResponse? {
        val uri = request.url
        if (!sameOrigin(uri) || !uri.path.orEmpty().startsWith("/.secretary-ime/")) return null
        val id = uri.path.orEmpty().removePrefix("/.secretary-ime/")
        val entry = if (request.method == "GET" && uri.query == null) files[id] else null
        val stream = try { entry?.first?.inputStream() } catch (_: Exception) { null }
        val headers = mapOf("Cache-Control" to "no-store", "Content-Security-Policy" to "sandbox; default-src 'none'", "X-Content-Type-Options" to "nosniff")
        return if (stream == null) WebResourceResponse("text/plain", "UTF-8", 404, "Not Found", headers, ByteArrayInputStream(ByteArray(0)))
        else WebResourceResponse(entry!!.second, null, 200, "OK", headers, stream)
    }

    override fun onCreateInputConnection(outAttrs: EditorInfo): InputConnection? {
        val connection = super.onCreateInputConnection(outAttrs) ?: return null
        if (!focused || !trusted()) return connection
        EditorInfoCompat.setContentMimeTypes(outAttrs, (EditorInfoCompat.getContentMimeTypes(outAttrs).toList() + "image/*").distinct().toTypedArray())
        return InputConnectionCompat.createWrapper(connection, outAttrs) { content, flags, _ ->
            val uri = content.contentUri
            if (!focused || !trusted() || uri.scheme != "content" || !content.description.hasMimeType("image/*")) return@createWrapper false
            val target = reply ?: return@createWrapper false
            val targetSession = session
            val granted = flags and InputConnectionCompat.INPUT_CONTENT_GRANT_READ_URI_PERMISSION != 0
            try { if (granted) content.requestPermission() } catch (_: Exception) { return@createWrapper false }
            try { worker.execute {
                var file: File? = null
                try {
                    val mime = context.contentResolver.getType(uri) ?: throw IllegalArgumentException("Неизвестный формат изображения")
                    require(mime.startsWith("image/") && !mime.contains('*')) { "Нужен файл изображения" }
                    file = File.createTempFile("ime-", ".image", context.cacheDir)
                    context.contentResolver.openInputStream(uri)?.use { input ->
                        file.outputStream().use { output ->
                            val buffer = ByteArray(65536)
                            var size = 0L
                            while (true) {
                                val count = input.read(buffer)
                                if (count < 0) break
                                size += count
                                require(size <= 100L * 1024 * 1024) { "Файл превышает 100 МБ" }
                                output.write(buffer, 0, count)
                            }
                            require(size > 0) { "Пустое изображение" }
                        }
                    } ?: throw IllegalArgumentException("Файл недоступен")
                    val ready = file
                    val id = UUID.randomUUID().toString()
                    main.post {
                        if (!focused || reply !== target || session != targetSession || !trusted()) ready.delete()
                        else {
                            files[id] = ready to mime
                            main.postDelayed({ files.remove(id)?.first?.delete() }, 60000)
                            try { target.postMessage(JSONObject().put("id", id).put("session", targetSession).put("type", mime).toString()) }
                            catch (_: Exception) { files.remove(id)?.first?.delete() }
                        }
                    }
                    file = null
                } catch (e: Exception) {
                    main.post { Toast.makeText(context, e.message ?: "Не удалось прочитать изображение", Toast.LENGTH_LONG).show() }
                } finally {
                    file?.delete()
                    if (granted) content.releasePermission()
                }
            } } catch (_: java.util.concurrent.RejectedExecutionException) {
                if (granted) content.releasePermission()
                return@createWrapper false
            }
            true
        }
    }

    override fun destroy() {
        worker.shutdown()
        super.destroy()
    }

    override fun onDetachedFromWindow() {
        focused = false
        reply = null
        files.values.forEach { it.first.delete() }
        files.clear()
        super.onDetachedFromWindow()
    }
}
