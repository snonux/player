package zone.foo.player_android

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.plugin.common.MethodChannel
import java.io.File

/** Engine lifetime follows audio_service, including background playback. */
class NativePlaybackWidgets : FlutterPlugin {
    private var channel: MethodChannel? = null
    private var context: Context? = null

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        val app = binding.applicationContext
        context = app
        channel = MethodChannel(binding.binaryMessenger, "zone.foo.player_android/widgets").apply {
            setMethodCallHandler { call, result ->
                if (call.method != "update") {
                    result.notImplemented()
                    return@setMethodCallHandler
                }
                val values = call.arguments as? Map<*, *> ?: emptyMap<Any, Any>()
                val path = values["artPath"] as? String
                // Reuse one small bitmap for state-only changes. No artwork or
                // identity is persisted, and protected network URLs never enter here.
                val art = if (path == PlaybackWidgetState.artPath) PlaybackWidgetState.art
                    else loadArtwork(app, path)
                PlaybackWidgetState.update(values, path, art)
                PlaybackWidgetProvider.refresh(app)
                result.success(null)
            }
        }
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel?.setMethodCallHandler(null)
        channel = null
        PlaybackWidgetState.clear()
        context?.let { PlaybackWidgetProvider.refresh(it) }
        context = null
    }

    private fun loadArtwork(context: Context, path: String?): Bitmap? = try {
        val file = path?.let { File(it).canonicalFile }
        val root = context.cacheDir.canonicalPath + File.separator
        if (file == null || !file.path.startsWith(root) || file.length() !in 1L..5_242_880L) null
        else {
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            BitmapFactory.decodeFile(file.path, bounds)
            if (bounds.outWidth <= 0 || bounds.outHeight <= 0) null
            else {
                var sample = 1
                while (bounds.outWidth / sample > 256 || bounds.outHeight / sample > 256) sample *= 2
                BitmapFactory.decodeFile(file.path, BitmapFactory.Options().apply { inSampleSize = sample })
            }
        }
    } catch (_: Exception) { null }
}

/** In-memory only: a new process cannot revive another account's metadata. */
internal object PlaybackWidgetState {
    var values: Map<*, *> = emptyMap<Any, Any>()
        private set
    var artPath: String? = null
        private set
    var art: Bitmap? = null
        private set
    fun update(next: Map<*, *>, path: String?, bitmap: Bitmap?) {
        values = next
        artPath = path
        art = bitmap
    }
    fun clear() = update(emptyMap<Any, Any>(), null, null)
    fun text(key: String) = (values[key] as? String).orEmpty().take(1024)
    fun enabled(key: String) = values[key] == true
}
