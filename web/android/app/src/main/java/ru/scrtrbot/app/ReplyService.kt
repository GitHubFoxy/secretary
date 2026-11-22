package ru.scrtrbot.app

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.webkit.CookieManager
import androidx.core.app.NotificationCompat
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.util.concurrent.TimeUnit

class ReplyService : Service() {
    companion object { @Volatile var visible = false; @Volatile var overlayVisible = false }
    private val handler = Handler(Looper.getMainLooper())
    private val client = OkHttpClient.Builder().pingInterval(30, TimeUnit.SECONDS).build()
    private var socket: WebSocket? = null
    private var stopped = false

    override fun onCreate() {
        super.onCreate()
        val manager = getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= 26) {
            manager.createNotificationChannel(NotificationChannel("connection", "Подключение к Host", NotificationManager.IMPORTANCE_LOW))
            manager.createNotificationChannel(NotificationChannel("replies", "Ответы Secretary", NotificationManager.IMPORTANCE_DEFAULT))
        }
        startForeground(1, NotificationCompat.Builder(this, "connection")
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle("Secretary работает в фоне")
            .setOngoing(true).build())
        connect()
    }

    private fun connect() {
        val host = HostAddress.get(this) ?: return stopSelf()
        val cookie = CookieManager.getInstance().getCookie(host)
        if (cookie.isNullOrEmpty()) { retry(); return }
        val request = Request.Builder().url("$host/ws").header("Cookie", cookie).build()
        socket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                val message = try { JSONObject(text) } catch (_: Exception) { return }
                if (message.optString("type") != "secretary_reply" || visible || overlayVisible) return
                val answer = message.optString("text").trim()
                if (answer.isEmpty()) return
                val intent = Intent(this@ReplyService, MainActivity::class.java)
                    .putExtra("secretary_reply", true).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
                val pending = PendingIntent.getActivity(this@ReplyService, 0, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
                val notification = NotificationCompat.Builder(this@ReplyService, "replies")
                    .setSmallIcon(android.R.drawable.stat_notify_chat)
                    .setContentTitle("Secretary")
                    .setContentText(answer.take(200))
                    .setStyle(NotificationCompat.BigTextStyle().bigText(answer.take(500)))
                    .setContentIntent(pending).setAutoCancel(true).build()
                getSystemService(NotificationManager::class.java).notify(2, notification)
            }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) { retry() }
            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) { webSocket.close(code, reason) }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) { retry() }
        })
    }

    private fun retry() { if (!stopped) handler.postDelayed({ if (!stopped) connect() }, 10_000) }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int) = START_STICKY
    override fun onBind(intent: Intent?): IBinder? = null
    override fun onDestroy() {
        stopped = true
        handler.removeCallbacksAndMessages(null)
        socket?.cancel()
        client.dispatcher.executorService.shutdown()
        super.onDestroy()
    }
}
