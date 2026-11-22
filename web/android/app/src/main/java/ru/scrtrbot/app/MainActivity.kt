package ru.scrtrbot.app

import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.view.WindowManager
import android.webkit.CookieManager
import com.getcapacitor.BridgeActivity
import com.getcapacitor.CapConfig

class MainActivity : BridgeActivity() {
    private fun applyAssistIntent(intent: Intent?) {
        val assist = intent?.action == Intent.ACTION_ASSIST
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            setShowWhenLocked(assist)
            setTurnScreenOn(assist)
        } else {
            @Suppress("DEPRECATION")
            val flags = WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED or WindowManager.LayoutParams.FLAG_TURN_SCREEN_ON
            if (assist) window.addFlags(flags) else window.clearFlags(flags)
        }
    }

    public override fun onCreate(savedInstanceState: Bundle?) {
        applyAssistIntent(intent)
        val host = getSharedPreferences("host", MODE_PRIVATE).getString("url", null)
        config = CapConfig.Builder(this).apply {
            if (host == null) setStartPath("/setup.html") else setServerUrl(host)
        }.create()
        registerPlugin(HostPlugin::class.java)
        super.onCreate(savedInstanceState)
    }

    public override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        applyAssistIntent(intent)
    }

    public override fun onStop() {
        CookieManager.getInstance().flush()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            setShowWhenLocked(false)
            setTurnScreenOn(false)
        } else {
            @Suppress("DEPRECATION")
            val flags = WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED or WindowManager.LayoutParams.FLAG_TURN_SCREEN_ON
            window.clearFlags(flags)
        }
        super.onStop()
    }
}
