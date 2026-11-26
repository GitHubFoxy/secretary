package ru.scrtrbot.app

import android.os.Bundle
import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import android.webkit.CookieManager
import android.webkit.URLUtil
import android.app.DownloadManager
import android.net.Uri
import android.os.Environment
import android.widget.Toast
import android.webkit.PermissionRequest
import com.getcapacitor.BridgeWebChromeClient
import com.getcapacitor.BridgeActivity
import com.getcapacitor.CapConfig
import org.json.JSONObject

class MainActivity : BridgeActivity() {
    companion object { var resumed: MainActivity? = null }

    public override fun onResume() { super.onResume(); resumed = this }
    public override fun onPause() { resumed = null; super.onPause() }

    public override fun onCreate(savedInstanceState: Bundle?) {
        val host = HostAddress.get(this)
        config = CapConfig.Builder(this).apply {
            setPluginsConfiguration(JSONObject().put("SystemBars", JSONObject().put("style", "DARK")))
            if (host == null) setStartPath("/setup.html") else setServerUrl(host)
        }.create()
        registerPlugin(HostPlugin::class.java)
        registerPlugin(SharePlugin::class.java)
        registerPlugin(DownloadsPlugin::class.java)
        super.onCreate(savedInstanceState)
        bridge.webView.webChromeClient = object : BridgeWebChromeClient(bridge) {
            override fun onPermissionRequest(request: PermissionRequest) {
                val expected = host?.let { Uri.parse(it) }
                val origin = request.origin
                fun port(uri: Uri) = if (uri.port != -1) uri.port else if (uri.scheme == "https") 443 else 80
                if (expected == null || origin.scheme != expected.scheme || origin.host != expected.host || port(origin) != port(expected) ||
                    request.resources.any { it != PermissionRequest.RESOURCE_AUDIO_CAPTURE && it != PermissionRequest.RESOURCE_VIDEO_CAPTURE }) request.deny()
                else super.onPermissionRequest(request)
            }
        }
        if (host != null) {
            ContextCompat.startForegroundService(this, Intent(this, ReplyService::class.java))
            if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
                requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 2)
        }
        // WebView does not download files by itself: the system downloader saves them to Downloads with the login cookie.
        bridge.webView.setDownloadListener { url, userAgent, disposition, mimeType, _ ->
            val name = URLUtil.guessFileName(url, disposition, mimeType)
            val request = DownloadManager.Request(Uri.parse(url))
                .addRequestHeader("Cookie", CookieManager.getInstance().getCookie(url) ?: "")
                .addRequestHeader("User-Agent", userAgent)
                .setMimeType(mimeType)
                .setTitle(name)
                .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, name)
            try {
                val id = getSystemService(DownloadManager::class.java).enqueue(request)
                DownloadsPlugin.record(this, id, name)
                Toast.makeText(this, "Скачиваю $name", Toast.LENGTH_SHORT).show()
            } catch (e: Exception) {
                Toast.makeText(this, "Не удалось начать загрузку: ${e.localizedMessage}", Toast.LENGTH_LONG).show()
            }
        }
        openReply(intent)
        share(intent)
    }

    private fun openReply(intent: Intent?) {
        if (intent?.getBooleanExtra("secretary_reply", false) == true) {
            intent.removeExtra("secretary_reply")
            HostAddress.get(this)?.let { bridge.webView.loadUrl("$it/?secretary") }
        }
    }

    private fun share(intent: Intent?) {
        (bridge.getPlugin("Share")?.instance as? SharePlugin)?.receive(intent)
    }

    override fun onNewIntent(intent: Intent) { super.onNewIntent(intent); openReply(intent); share(intent) }
    override fun onStart() { super.onStart(); ReplyService.visible = true }
    public override fun onStop() {
        ReplyService.visible = false
        CookieManager.getInstance().flush()
        super.onStop()
    }
}
