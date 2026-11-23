package ru.scrtrbot.app

import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID
import java.util.concurrent.Executors

@CapacitorPlugin(name = "Share")
class SharePlugin : Plugin() {
    private val worker = Executors.newSingleThreadExecutor()
    private val root get() = File(context.filesDir, "shared")

    fun receive(intent: Intent?) {
        if (intent?.action != Intent.ACTION_SEND && intent?.action != Intent.ACTION_SEND_MULTIPLE) return
        val uris = mutableListOf<Uri>()
        @Suppress("DEPRECATION")
        if (intent.action == Intent.ACTION_SEND_MULTIPLE) {
            intent.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM)?.let { uris.addAll(it) }
        } else {
            intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM)?.let { uris.add(it) }
        }
        val clip = intent.clipData
        if (uris.isEmpty() && clip != null) for (i in 0 until clip.itemCount) clip.getItemAt(i).uri?.let { uris.add(it) }
        val text = intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString() ?: ""
        intent.action = null
        worker.execute {
            val dir = File(root, "${System.currentTimeMillis()}-${UUID.randomUUID()}")
            dir.mkdirs()
            val files = JSONArray()
            val errors = mutableListOf<String>()
            uris.forEachIndexed { index, uri ->
                try {
                    val name = context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
                        if (it.moveToFirst()) it.getString(0) else null
                    } ?: uri.lastPathSegment ?: "file"
                    val mime = context.contentResolver.getType(uri) ?: intent.type?.takeUnless { it.contains('*') } ?: "application/octet-stream"
                    val file = File(dir, index.toString())
                    context.contentResolver.openInputStream(uri)?.use { input -> file.outputStream().use { input.copyTo(it) } }
                        ?: throw IllegalStateException("Файл недоступен")
                    files.put(JSONObject().put("path", file.absolutePath).put("name", name).put("type", mime))
                } catch (e: Exception) { errors.add("Не удалось прочитать файл: ${e.localizedMessage}") }
            }
            val data = JSONObject().put("id", dir.name).put("text", text).put("files", files).put("error", errors.joinToString("\n"))
            File(dir, "share.json").writeText(data.toString())
            notifyListeners("received", JSObject())
        }
    }

    @PluginMethod
    fun pending(call: PluginCall) {
        worker.execute {
            val items = JSONArray()
            root.listFiles()?.sortedBy { it.name }?.forEach { dir ->
                val metadata = File(dir, "share.json")
                if (metadata.exists()) items.put(JSONObject(metadata.readText()))
            }
            call.resolve(JSObject().put("items", items))
        }
    }

    @PluginMethod
    fun acknowledge(call: PluginCall) {
        val id = call.getString("id") ?: return call.reject("Missing share id")
        worker.execute {
            if (File(id).name != id) return@execute call.reject("Invalid share id")
            File(root, id).deleteRecursively()
            call.resolve()
        }
    }
}
