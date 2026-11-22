package ru.scrtrbot.app

import android.app.role.RoleManager
import android.content.ComponentName
import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.activity.result.ActivityResult
import com.getcapacitor.JSObject
import com.getcapacitor.annotation.ActivityCallback
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
        HostAddress.set(activity, address)
        call.resolve()
        activity.runOnUiThread { activity.recreate() }
    }

    @PluginMethod
    fun requestAssistantRole(call: PluginCall) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            call.resolve(JSObject().put("held", false))
            return
        }
        val roles = activity.getSystemService(RoleManager::class.java)
        if (!roles.isRoleAvailable(RoleManager.ROLE_ASSISTANT)) {
            call.resolve(JSObject().put("held", false))
            return
        }
        val intent = if (roles.isRoleHeld(RoleManager.ROLE_ASSISTANT) && !assistantServiceSelected())
            Intent(Settings.ACTION_MANAGE_DEFAULT_APPS_SETTINGS)
        else roles.createRequestRoleIntent(RoleManager.ROLE_ASSISTANT)
        startActivityForResult(call, intent, "assistantRoleResult")
    }

    @ActivityCallback
    private fun assistantRoleResult(call: PluginCall?, result: ActivityResult) {
        val held = activity.getSystemService(RoleManager::class.java).isRoleHeld(RoleManager.ROLE_ASSISTANT)
        call?.resolve(JSObject().put("held", held && assistantServiceSelected()))
    }

    private fun assistantServiceSelected(): Boolean {
        val selected = Settings.Secure.getString(activity.contentResolver, "voice_interaction_service")
        return ComponentName.unflattenFromString(selected ?: "") == ComponentName(activity, AssistantService::class.java)
    }

    @PluginMethod
    fun reset(call: PluginCall) {
        HostAddress.set(activity, null)
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
