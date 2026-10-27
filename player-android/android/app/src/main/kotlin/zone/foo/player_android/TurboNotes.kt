package zone.foo.player_android

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

/**
 * Opens a note in TurboNotes (org.buetow.turbonotes), which creates it when
 * it does not exist yet. The intent is TurboNotes' NoteLinks contract.
 */
class TurboNotes(private val activity: Activity, messenger: BinaryMessenger) {
    init {
        MethodChannel(messenger, CHANNEL).setMethodCallHandler { call, result ->
            if (call.method != "openNote") {
                result.notImplemented()
                return@setMethodCallHandler
            }
            val path = call.argument<String>("path")
            if (path.isNullOrBlank()) {
                result.error("invalid_path", "A note path is required", null)
                return@setMethodCallHandler
            }
            val intent = Intent(ACTION_OPEN_NOTE).apply {
                setPackage(PACKAGE)
                putExtra(EXTRA_PATH, path)
                call.argument<String>("title")?.let { putExtra(EXTRA_TITLE, it) }
                // Into TurboNotes' own task, so Back there and the recents
                // screen keep the two apps apart.
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            try {
                activity.startActivity(intent)
                result.success(null)
            } catch (e: ActivityNotFoundException) {
                result.error("not_installed", "TurboNotes is not installed", null)
            }
        }
    }

    private companion object {
        const val CHANNEL = "zone.foo.player_android/turbonotes"
        const val PACKAGE = "org.buetow.turbonotes"
        const val ACTION_OPEN_NOTE = "org.buetow.turbonotes.OPEN_NOTE"
        const val EXTRA_PATH = "org.buetow.turbonotes.extra.PATH"
        const val EXTRA_TITLE = "org.buetow.turbonotes.extra.TITLE"
    }
}
