package zone.foo.player_android

import android.app.Activity
import android.content.Intent
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream

/** One-shot bounded image import: no persistent storage permission. */
class ArtworkPicker(private val activity: Activity, messenger: BinaryMessenger) {
    private var pending: MethodChannel.Result? = null
    init {
        MethodChannel(messenger, "zone.foo.player_android/artwork_picker").setMethodCallHandler { call, result ->
            if (call.method != "pick") result.notImplemented()
            else if (pending != null) result.error("busy", "An image picker is already open", null)
            else {
                pending = result
                try {
                    activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                        addCategory(Intent.CATEGORY_OPENABLE)
                        type = "image/*"
                        putExtra(Intent.EXTRA_MIME_TYPES, arrayOf("image/jpeg", "image/png", "image/gif"))
                        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                    }, 7216)
                } catch (error: Exception) {
                    pending = null
                    result.error("picker_unavailable", "Could not open the image picker", null)
                }
            }
        }
    }
    fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        if (requestCode != 7216) return false
        val callback = pending ?: return true
        pending = null
        val uri = data?.data
        if (resultCode != Activity.RESULT_OK || uri == null) { callback.success(null); return true }
        // Provider IO runs off the UI thread; both unknown and false sizes stay bounded.
        Thread {
            try {
                val bytes = activity.contentResolver.openInputStream(uri)?.use { input ->
                    val output = ByteArrayOutputStream()
                    val buffer = ByteArray(8192)
                    while (true) {
                        val n = input.read(buffer)
                        if (n < 0) break
                        if (output.size() + n > 5 * 1024 * 1024) throw IllegalArgumentException("Choose an image up to 5 MB.")
                        output.write(buffer, 0, n)
                    }
                    output.toByteArray()
                } ?: throw IllegalArgumentException("The image could not be read.")
                activity.runOnUiThread { callback.success(bytes) }
            } catch (error: Exception) {
                activity.runOnUiThread { callback.error("image_unavailable", error.message ?: "Could not read image", null) }
            }
        }.start()
        return true
    }
}
