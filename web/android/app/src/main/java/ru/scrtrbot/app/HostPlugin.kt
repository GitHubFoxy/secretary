package ru.scrtrbot.app

import android.webkit.CookieManager
import android.webkit.WebStorage
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import java.net.URI

@CapacitorPlugin(name = "Host")
class HostPlugin : Plugin() {
    @PluginMethod
    fun connect(call: PluginCall) {
        val address = call.getString("url")?.trim()?.trimEnd('/') ?: ""
        val uri = try { URI(address) } catch (_: Exception) { null }
        if (uri == null || uri.host == null || uri.userInfo != null || uri.query != null || uri.fragment != null ||
            (uri.path != null && uri.path.isNotEmpty()) ||
            !(uri.scheme == "https" || (uri.scheme == "http" && uri.host == "localhost"))) {
            call.reject("Введите HTTPS-адрес Host, например https://host.tailnet.ts.net")
            return
        }
        activity.getSharedPreferences("host", 0).edit().putString("url", address).apply()
        call.resolve()
        activity.runOnUiThread { activity.recreate() }
    }

    @PluginMethod
    fun reset(call: PluginCall) {
        activity.getSharedPreferences("host", 0).edit().remove("url").apply()
        activity.runOnUiThread {
            CookieManager.getInstance().removeAllCookies {
                CookieManager.getInstance().flush()
                WebStorage.getInstance().deleteAllData()
                bridge.webView.clearCache(true)
                call.resolve()
                activity.recreate()
            }
        }
    }
}
