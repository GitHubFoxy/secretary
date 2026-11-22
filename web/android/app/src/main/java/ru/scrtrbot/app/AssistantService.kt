package ru.scrtrbot.app

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.graphics.Outline
import android.graphics.drawable.ColorDrawable
import android.graphics.drawable.GradientDrawable
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
import android.widget.FrameLayout

class AssistantService : VoiceInteractionService()

class AssistantSessionService : VoiceInteractionSessionService() {
    override fun onNewSession(args: Bundle?) = AssistantSession(this)
}

class AssistantSession(private val service: AssistantSessionService) : VoiceInteractionSession(service) {
    private var webView: WebView? = null

    override fun onCreateContentView(): View {
        window?.window?.apply {
            setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
            addFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND)
            setDimAmount(0.35f)
            setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
        }
        val root = FrameLayout(service)
        root.setOnClickListener { hide() }
        val sheet = WebView(service).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.mediaPlaybackRequiresUserGesture = false
            background = GradientDrawable().apply {
                setColor(Color.WHITE)
                cornerRadius = 24 * resources.displayMetrics.density
            }
            outlineProvider = object : ViewOutlineProvider() {
                override fun getOutline(view: View, outline: Outline) {
                    outline.setRoundRect(0, 0, view.width, view.height, 24 * resources.displayMetrics.density)
                }
            }
            clipToOutline = true
            webChromeClient = object : WebChromeClient() {
                override fun onPermissionRequest(request: PermissionRequest) {
                    if (request.origin.toString() != "https://app.scrtrbot.ru/" ||
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
            loadUrl("https://app.scrtrbot.ru")
        }
        webView = sheet
        root.addView(sheet, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT,
            (service.resources.displayMetrics.heightPixels * 0.45f).toInt(), Gravity.BOTTOM))
        return root
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
