package zone.foo.player_android

import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Toast
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.plugin.common.MethodChannel
import java.util.UUID

/** Small Locale setting boundary. Capabilities are private records, never URLs. */
class NativeMusicAutomation : FlutterPlugin {
    private lateinit var channel: MethodChannel
    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel = MethodChannel(binding.binaryMessenger, CHANNEL)
        active = channel
        channel.setMethodCallHandler { call, result ->
            val prefs = preferences(binding.applicationContext)
            when (call.method) {
                "status" -> result.success(prefs.getString("last-result", "No automation has run yet."))
                "revoke" -> {
                    prefs.edit().clear().putString("last-result", "Saved actions revoked. Save new actions in your automation app.").commit()
                    result.success(null)
                }
                else -> result.notImplemented()
            }
        }
    }
    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        if (active === channel) active = null
        channel.setMethodCallHandler(null)
    }
    companion object {
        const val CHANNEL = "zone.foo.player_android/music_automation"
        const val EDIT = "com.twofortyfouram.locale.intent.action.EDIT_SETTING"
        const val FIRE = "com.twofortyfouram.locale.intent.action.FIRE_SETTING"
        const val BUNDLE = "com.twofortyfouram.locale.intent.extra.BUNDLE"
        const val BLURB = "com.twofortyfouram.locale.intent.extra.BLURB"
        val actions = linkedMapOf("start" to "Start queued music", "shuffle" to "Shuffle and start queued music",
            "offline" to "Use offline music only", "online" to "Allow online music")
        var active: MethodChannel? = null
            private set
        fun preferences(context: Context) = context.getSharedPreferences("music-automation", Context.MODE_PRIVATE)
        fun token(intent: Intent): String? = try {
            @Suppress("DEPRECATION") val bundle = intent.getBundleExtra(BUNDLE)
            if (bundle?.keySet() != setOf("capability")) null
            else bundle.getString("capability")?.takeIf {
                it.matches(Regex("[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}"))
            }
        } catch (_: RuntimeException) { null }
        fun result(context: Context, message: String) {
            preferences(context).edit().putString("last-result", message).apply()
        }
    }
}

class MusicAutomationReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != NativeMusicAutomation.FIRE) return
        // TaskerPlugin.Setting: OK = RESULT_OK; FAILED = RESULT_FIRST_USER + 1.
        val ordered = isOrderedBroadcast
        if (ordered) resultCode = Activity.RESULT_FIRST_USER + 1
        val pending = goAsync()
        val handler = Handler(Looper.getMainLooper())
        val invocation = UUID.randomUUID().toString()
        var finished = false
        fun finish(ok: Boolean, message: String) {
            if (finished) return
            finished = true
            NativeMusicAutomation.result(context, message)
            if (ordered) {
                pending.resultCode = if (ok) Activity.RESULT_OK else Activity.RESULT_FIRST_USER + 1
                pending.setResultExtras(Bundle().apply {
                    putBundle("net.dinglisch.android.tasker.extras.VARIABLES", Bundle().apply {
                        if (!ok) putString("%errmsg", message)
                    })
                })
            }
            if (!ok) Toast.makeText(context, message, Toast.LENGTH_LONG).show()
            pending.finish()
        }
        val capability = NativeMusicAutomation.token(intent)
        val prefs = NativeMusicAutomation.preferences(context)
        val action = capability?.let { prefs.getString("$it-action", null) }
        val scope = capability?.let { prefs.getString("$it-scope", null) }
        val bridge = NativeMusicAutomation.active
        if (action !in NativeMusicAutomation.actions || scope == null || bridge == null) {
            finish(false, if (action == null || scope == null) "Save this Player music action again in your automation app."
                else "Open Player and unlock the configured account before running music automation.")
            return
        }
        val timeout = Runnable {
            bridge.invokeMethod("cancel", invocation)
            finish(false, "Music automation timed out. Open Player and retry.")
        }
        handler.postDelayed(timeout, 8000)
        bridge.invokeMethod("run", mapOf("action" to action, "scope" to scope, "invocation" to invocation),
            object : MethodChannel.Result {
                override fun success(value: Any?) {
                    handler.removeCallbacks(timeout)
                    val response = value as? Map<*, *>
                    finish(response?.get("ok") == true, response?.get("message") as? String ?: "Music automation failed. Open Player and retry.")
                }
                override fun error(code: String, message: String?, details: Any?) {
                    handler.removeCallbacks(timeout)
                    finish(false, "Music automation unavailable. Open Player and retry.")
                }
                override fun notImplemented() = error("unavailable", null, null)
            })
    }
}
