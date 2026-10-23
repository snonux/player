package zone.foo.player_android

import android.app.Activity
import android.content.Intent
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

/** A user-selected document destination, without storage permission or media credentials. */
class PlaylistExport(private val activity: Activity, messenger: BinaryMessenger) {
    private val channel = MethodChannel(messenger, "zone.foo.player_android/playlist_export")
    private var pending: MethodChannel.Result? = null
    private var content: ByteArray? = null

    init {
        channel.setMethodCallHandler { call, result ->
            if (call.method != "save") {
                result.notImplemented()
            } else if (pending != null) {
                result.error("save_busy", "A save dialog is already open", null)
            } else {
                val filename = call.argument<String>("filename")
                val text = call.argument<String>("content")
                if (filename == null || text == null) {
                    result.error("invalid_document", "Filename and content are required", null)
                } else {
                    pending = result
                    content = text.toByteArray(Charsets.UTF_8)
                    val intent = Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                        addCategory(Intent.CATEGORY_OPENABLE)
                        type = "application/vnd.apple.mpegurl"
                        putExtra(Intent.EXTRA_TITLE, filename)
                    }
                    try {
                        activity.startActivityForResult(intent, REQUEST_CODE)
                    } catch (error: Exception) {
                        pending = null
                        content = null
                        result.error("save_unavailable", error.message, null)
                    }
                }
            }
        }
    }

    fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        if (requestCode != REQUEST_CODE) return false
        val callback = pending ?: return true
        val bytes = content
        pending = null
        content = null
        val uri = data?.data
        if (resultCode != Activity.RESULT_OK || uri == null || bytes == null) {
            callback.success(false)
            return true
        }
        // Document providers may block. Keep the UI thread responsive while writing.
        Thread {
            try {
                val output = activity.contentResolver.openOutputStream(uri, "wt")
                    ?: throw java.io.IOException("Could not open the selected destination")
                output.use { it.write(bytes) }
                activity.runOnUiThread { callback.success(true) }
            } catch (error: Exception) {
                activity.runOnUiThread { callback.error("save_failed", error.message, null) }
            }
        }.start()
        return true
    }

    fun close() {
        channel.setMethodCallHandler(null)
        pending?.error("save_cancelled", "Save was interrupted", null)
        pending = null
        content = null
    }

    companion object {
        private const val REQUEST_CODE = 7152
    }
}
