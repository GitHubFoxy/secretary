package ru.scrtrbot.app

import android.content.Context

object HostAddress {
    private fun preferences(context: Context) = context.getSharedPreferences("host", Context.MODE_PRIVATE)
    fun get(context: Context): String? = preferences(context).getString("url", null)
    fun set(context: Context, address: String?) = preferences(context).edit().putString("url", address).apply()
}
