package ru.scrtrbot.app

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.util.Base64
import java.io.ByteArrayOutputStream
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.net.Uri
import android.os.Bundle
import android.service.voice.VoiceInteractionService
import android.service.voice.VoiceInteractionSession
import android.service.voice.VoiceInteractionSessionService
import android.view.View
import android.view.ViewGroup
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
    private var screenshot: String? = null
    private var screenshotTime = 0L

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
        root = FrameLayout(service)
        return root
    }

    override fun onHandleScreenshot(bitmap: Bitmap?) {
        super.onHandleScreenshot(bitmap)
        screenshot = bitmap?.let {
            val scale = minOf(1.0, 1568.0 / maxOf(it.width, it.height))
            val scaled = Bitmap.createScaledBitmap(it, maxOf(1, (it.width * scale).toInt()), maxOf(1, (it.height * scale).toInt()), true)
            val output = ByteArrayOutputStream()
            scaled.compress(Bitmap.CompressFormat.JPEG, 80, output)
            if (scaled !== it) scaled.recycle()
            Base64.encodeToString(output.toByteArray(), Base64.NO_WRAP)
        }
        screenshotTime = android.os.SystemClock.elapsedRealtime()
    }

    override fun onShow(args: Bundle?, showFlags: Int) {
        super.onShow(args, showFlags)
        if (MainActivity.resumed != null) {
            hide()
            return
        }
        ReplyService.overlayVisible = true
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
            // Шторку рисует веб-интерфейс, вокруг неё должно быть видно приложение под ней.
            setBackgroundColor(Color.TRANSPARENT)
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean =
                    !isHost(request.url)
            }
            if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER) &&
                WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
                val origins = setOf(host)
                WebViewCompat.addWebMessageListener(this, "SecretaryOverlayNative", origins) { _, message, _, isMainFrame, reply ->
                    if (isMainFrame) when (message.data) {
                        "close" -> hide()
                        "openApp" -> openApp()
                        "screenshot" -> {
                            val value = screenshot?.takeIf { android.os.SystemClock.elapsedRealtime() - screenshotTime < 60000 }
                            screenshot = null
                            reply.postMessage(value ?: "")
                        }
                    }
                }
                WebViewCompat.addDocumentStartJavaScript(this,
                    "if (window === window.top) window.SecretaryOverlay = { close: () => SecretaryOverlayNative.postMessage('close'), openApp: () => SecretaryOverlayNative.postMessage('openApp'), screenshot: () => new Promise(resolve => { SecretaryOverlayNative.onmessage = e => resolve(e.data); SecretaryOverlayNative.postMessage('screenshot'); }) };",
                    origins)
            }
            webChromeClient = object : WebChromeClient() {
                override fun onPermissionRequest(request: PermissionRequest) {
                    if (!isHost(request.origin) ||
                        request.resources.any { it != PermissionRequest.RESOURCE_AUDIO_CAPTURE && it != PermissionRequest.RESOURCE_VIDEO_CAPTURE }) {
                        request.deny()
                        return
                    }
                    val permissions = request.resources.map { if (it == PermissionRequest.RESOURCE_AUDIO_CAPTURE) Manifest.permission.RECORD_AUDIO else Manifest.permission.CAMERA }.toTypedArray()
                    if (permissions.all { service.checkSelfPermission(it) == PackageManager.PERMISSION_GRANTED }) {
                        request.grant(request.resources)
                    } else {
                        window?.window?.apply {
                            addFlags(WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE)
                            decorView.visibility = View.INVISIBLE
                        }
                        AudioPermissionActivity.permissions = permissions
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
        root.addView(sheet, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
    }

    override fun onHide() { screenshot = null; ReplyService.overlayVisible = false; super.onHide() }

    override fun onBackPressed() { hide() }

    override fun onDestroy() {
        ReplyService.overlayVisible = false
        webView?.destroy()
        webView = null
        super.onDestroy()
    }
}

class AudioPermissionActivity : Activity() {
    companion object { var pending: ((Boolean) -> Unit)? = null; var permissions = emptyArray<String>() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestPermissions(permissions, 1)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        pending?.invoke(grantResults.isNotEmpty() && grantResults.all { it == PackageManager.PERMISSION_GRANTED })
        pending = null
        finish()
    }
}

class AssistantRecognitionService : android.speech.RecognitionService() {
    override fun onStartListening(intent: Intent, listener: Callback) { listener.error(android.speech.SpeechRecognizer.ERROR_CLIENT) }
    override fun onStopListening(listener: Callback) {}
    override fun onCancel(listener: Callback) {}
}
