package ru.scrtrbot.app

import android.os.Bundle
import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import android.webkit.CookieManager
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
        super.onCreate(savedInstanceState)
        if (host != null) {
            ContextCompat.startForegroundService(this, Intent(this, ReplyService::class.java))
            if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
                requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 2)
        }
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
