package ru.scrtrbot.app

import android.app.DownloadManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import org.json.JSONArray
import org.json.JSONObject

@CapacitorPlugin(name = "Downloads")
class DownloadsPlugin : Plugin() {
    companion object {
        fun record(context: Context, id: Long, name: String) {
            val prefs = context.getSharedPreferences("downloads", Context.MODE_PRIVATE)
            val items = JSONArray(prefs.getString("items", "[]"))
            items.put(JSONObject().put("id", id.toString()).put("name", name))
            prefs.edit().putString("items", items.toString()).commit()
        }
    }

    private val manager get() = context.getSystemService(DownloadManager::class.java)
    private fun saved() = JSONArray(context.getSharedPreferences("downloads", Context.MODE_PRIVATE).getString("items", "[]"))

    @PluginMethod
    fun list(call: PluginCall) {
        try {
            val saved = saved()
            val items = JSONArray()
            for (i in saved.length() - 1 downTo 0) {
                val entry = saved.getJSONObject(i)
                val id = entry.getString("id").toLong()
                val item = JSONObject().put("id", id.toString()).put("name", entry.getString("name"))
                manager.query(DownloadManager.Query().setFilterById(id)).use { cursor ->
                    if (cursor != null && cursor.moveToFirst()) {
                        val status = when (cursor.getInt(cursor.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS))) {
                            DownloadManager.STATUS_SUCCESSFUL -> "complete"
                            DownloadManager.STATUS_FAILED -> "failed"
                            DownloadManager.STATUS_RUNNING -> "running"
                            else -> "pending"
                        }
                        val uri = if (status == "complete") manager.getUriForDownloadedFile(id)?.takeIf {
                            try { context.contentResolver.openFileDescriptor(it, "r")?.use { true } ?: false }
                            catch (_: Exception) { false }
                        } else null
                        item.put("status", if (status == "complete" && uri == null) "unavailable" else status)
                        item.put("size", cursor.getLong(cursor.getColumnIndexOrThrow(DownloadManager.COLUMN_TOTAL_SIZE_BYTES)))
                        item.put("progress", cursor.getLong(cursor.getColumnIndexOrThrow(DownloadManager.COLUMN_BYTES_DOWNLOADED_SO_FAR)))
                        item.put("uri", uri?.toString() ?: JSONObject.NULL)
                    } else item.put("status", "unavailable").put("size", -1).put("progress", 0).put("uri", JSONObject.NULL)
                }
                items.put(item)
            }
            call.resolve(JSObject().put("items", items))
        } catch (e: Exception) { call.reject("Не удалось получить загрузки", e) }
    }

    private fun send(call: PluginCall, action: String) {
        try {
            val id = call.getString("id")?.toLongOrNull() ?: return call.reject("Неверный идентификатор")
            val saved = saved()
            if ((0 until saved.length()).none { saved.getJSONObject(it).getString("id") == id.toString() }) return call.reject("Загрузка не найдена")
            val uri: Uri = manager.getUriForDownloadedFile(id) ?: return call.reject("Файл недоступен")
            val mime = manager.getMimeTypeForDownloadedFile(id) ?: "application/octet-stream"
            val intent = Intent(action).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            if (action == Intent.ACTION_VIEW) intent.setDataAndType(uri, mime)
            else intent.setType(mime).putExtra(Intent.EXTRA_STREAM, uri)
            activity.startActivity(Intent.createChooser(intent, if (action == Intent.ACTION_VIEW) "Открыть файл" else "Поделиться файлом"))
            call.resolve()
        } catch (e: Exception) { call.reject("Не удалось открыть файл", e) }
    }

    @PluginMethod fun open(call: PluginCall) = send(call, Intent.ACTION_VIEW)
    @PluginMethod fun share(call: PluginCall) = send(call, Intent.ACTION_SEND)
}
