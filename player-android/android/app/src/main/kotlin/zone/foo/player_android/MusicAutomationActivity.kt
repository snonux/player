package zone.foo.player_android

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.LinearLayout
import android.widget.RadioButton
import android.widget.RadioGroup
import android.widget.ScrollView
import android.widget.TextView
import io.flutter.plugin.common.MethodChannel
import java.util.UUID

/** Host-visible configuration; Back and Cancel never save. */
class MusicAutomationActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setResult(RESULT_CANCELED)
        title = "Player music automation"
        val padding = (24 * resources.displayMetrics.density).toInt()
        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(padding, padding, padding, padding)
        }
        setContentView(ScrollView(this).apply { addView(layout) })
        val status = TextView(this).apply {
            text = "Checking selected Player account…"
            textSize = 18f
        }
        layout.addView(status)
        layout.addView(TextView(this).apply {
            text = "Open Player, unlock your server account and queue music first. Start and shuffle use that account’s music queue. Offline only requires every queued song to be downloaded or cached. This preference affects future music automation only. After restarting Player, open and unlock it before running an action."
            setPadding(0, padding, 0, padding)
        })
        val group = RadioGroup(this)
        val choices = NativeMusicAutomation.actions.keys.toList()
        val existing = NativeMusicAutomation.token(intent)
        val prefs = NativeMusicAutomation.preferences(this)
        val selected = existing?.let { prefs.getString("$it-action", null) } ?: "start"
        choices.forEachIndexed { index, key ->
            group.addView(RadioButton(this).apply {
                id = index + 1
                text = NativeMusicAutomation.actions[key]
                minHeight = (48 * resources.displayMetrics.density).toInt()
                isChecked = key == selected
            })
        }
        layout.addView(group)
        val save = Button(this).apply { text = "Save action"; isEnabled = false }
        layout.addView(save)
        layout.addView(Button(this).apply { text = "Cancel"; setOnClickListener { finish() } })
        val bridge = NativeMusicAutomation.active
        if (bridge == null) {
            status.text = "Open Player and unlock the intended account, then configure this action again."
            return
        }
        bridge.invokeMethod("configuration", null, object : MethodChannel.Result {
            override fun success(value: Any?) {
                if (isFinishing || isDestroyed) return
                val config = value as? Map<*, *>
                val scope = config?.get("scope") as? String
                status.text = config?.get("message") as? String ?: "Open Player and unlock the intended account."
                save.isEnabled = scope != null
                save.setOnClickListener {
                    val action = choices.getOrNull(group.checkedRadioButtonId - 1) ?: return@setOnClickListener
                    val token = UUID.randomUUID().toString()
                    val stored = prefs.edit().putString("$token-action", action).putString("$token-scope", scope).commit()
                    if (!stored) { status.text = "Could not save action. Retry."; return@setOnClickListener }
                    val result = Intent().putExtra(NativeMusicAutomation.BUNDLE, Bundle().apply { putString("capability", token) })
                        .putExtra(NativeMusicAutomation.BLURB, "${NativeMusicAutomation.actions[action]} · ${config?.get("message")}")
                        // Tasker extension, bounded below the broadcast lifetime.
                        .putExtra("net.dinglisch.android.tasker.extras.REQUESTED_TIMEOUT", 16000)
                    setResult(RESULT_OK, result)
                    finish()
                }
            }
            override fun error(code: String, message: String?, details: Any?) { status.text = "Open Player and unlock the intended account, then try again." }
            override fun notImplemented() = error("unavailable", null, null)
        })
    }
}
