package ru.scrtrbot.app

import android.os.Bundle
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
        super.onCreate(savedInstanceState)
    }

    public override fun onStop() {
        CookieManager.getInstance().flush()
        super.onStop()
    }
}
