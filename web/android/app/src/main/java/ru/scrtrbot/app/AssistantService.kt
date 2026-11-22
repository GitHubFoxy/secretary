package ru.scrtrbot.app

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.graphics.Outline
import android.graphics.drawable.ColorDrawable
import android.net.Uri
import android.os.Bundle
import android.service.voice.VoiceInteractionService
import android.service.voice.VoiceInteractionSession
import android.service.voice.VoiceInteractionSessionService
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.ViewOutlineProvider
import android.view.WindowManager
import android.webkit.PermissionRequest
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.webkit.WebViewClient
import android.webkit.WebResourceRequest
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import android.widget.FrameLayout

class AssistantService : VoiceInteractionService()

class AssistantSessionService : VoiceInteractionSessionService() {
    override fun onNewSession(args: Bundle?) = AssistantSession(this)
}

class AssistantSession(private val service: AssistantSessionService) : VoiceInteractionSession(service) {
    private var webView: WebView? = null
    private lateinit var root: FrameLayout

    private fun openApp() {
        startAssistantActivity(Intent(service, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        hide()
    }

    override fun onCreateContentView(): View {
        window?.window?.apply {
            setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
            addFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND)
            setDimAmount(0.35f)
            setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
        }
        root = FrameLayout(service).apply { setOnClickListener { hide() } }
        return root
    }

    override fun onShow(args: Bundle?, showFlags: Int) {
        super.onShow(args, showFlags)
        val host = HostAddress.get(service)
        if (host == null) {
            openApp()
            return
        }
        val origin = Uri.parse(host)
        fun isHost(uri: Uri): Boolean {
            fun port(uri: Uri) = if (uri.port != -1) uri.port else if (uri.scheme == "https") 443 else 80
            return uri.scheme == origin.scheme && uri.host == origin.host && port(uri) == port(origin)
        }
        webView?.let { root.removeView(it); it.destroy() }
        val sheet = WebView(service).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.mediaPlaybackRequiresUserGesture = false
            setBackgroundColor(Color.rgb(10, 10, 10))
            outlineProvider = object : ViewOutlineProvider() {
                override fun getOutline(view: View, outline: Outline) {
                    val radius = 24 * resources.displayMetrics.density
                    outline.setRoundRect(0, 0, view.width, view.height + radius.toInt(), radius)
                }
            }
            clipToOutline = true
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean =
                    !isHost(request.url)
            }
            if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER) &&
                WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
                val origins = setOf(host)
                WebViewCompat.addWebMessageListener(this, "SecretaryOverlayNative", origins) { _, message, _, isMainFrame, _ ->
                    if (isMainFrame) when (message.data) {
                        "close" -> hide()
                        "openApp" -> openApp()
                    }
                }
                WebViewCompat.addDocumentStartJavaScript(this,
                    "if (window === window.top) window.SecretaryOverlay = { close: () => SecretaryOverlayNative.postMessage('close'), openApp: () => SecretaryOverlayNative.postMessage('openApp') };",
                    origins)
            }
            webChromeClient = object : WebChromeClient() {
                override fun onPermissionRequest(request: PermissionRequest) {
                    if (!isHost(request.origin) ||
                        request.resources.any { it != PermissionRequest.RESOURCE_AUDIO_CAPTURE }) {
                        request.deny()
                        return
                    }
                    if (service.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
                        request.grant(request.resources)
                    } else {
                        window?.window?.apply {
                            addFlags(WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE)
                            decorView.visibility = View.INVISIBLE
                        }
                        AudioPermissionActivity.pending = { granted ->
                            window?.window?.apply {
                                clearFlags(WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE)
                                decorView.visibility = View.VISIBLE
                            }
                            if (granted) request.grant(request.resources) else request.deny()
                        }
                        startAssistantActivity(Intent(service, AudioPermissionActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    }
                }
            }
            loadUrl("$host/?overlay")
        }
        webView = sheet
        val margin = (8 * service.resources.displayMetrics.density).toInt()
        root.addView(sheet, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT,
            (service.resources.displayMetrics.heightPixels * 0.5f).toInt(), Gravity.BOTTOM).apply {
            setMargins(margin, 0, margin, margin)
        })
    }

    override fun onBackPressed() { hide() }

    override fun onDestroy() {
        webView?.destroy()
        webView = null
        super.onDestroy()
    }
}

class AudioPermissionActivity : Activity() {
    companion object { var pending: ((Boolean) -> Unit)? = null }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO), 1)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        pending?.invoke(grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED)
        pending = null
        finish()
    }
}

class AssistantRecognitionService : android.speech.RecognitionService() {
    override fun onStartListening(intent: Intent, listener: Callback) { listener.error(android.speech.SpeechRecognizer.ERROR_CLIENT) }
    override fun onStopListening(listener: Callback) {}
    override fun onCancel(listener: Callback) {}
}
