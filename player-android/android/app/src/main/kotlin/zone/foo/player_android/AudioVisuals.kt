package zone.foo.player_android

import android.Manifest
import android.app.Activity
import android.content.pm.PackageManager
import android.media.audiofx.Visualizer
import android.os.Handler
import android.os.Looper
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel

// Observe only our player's session, never the device output mix or microphone.
// Android requires RECORD_AUDIO even for Visualizer's low-resolution playback data.
class AudioVisuals(private val activity: Activity, messenger: BinaryMessenger) :
    EventChannel.StreamHandler {
    private val methods = MethodChannel(messenger, "zone.foo.player_android/audio_visuals")
    private val events = EventChannel(messenger, "zone.foo.player_android/audio_visual_frames")
    private val handler = Handler(Looper.getMainLooper())
    private var visualizer: Visualizer? = null
    private var permissionResult: MethodChannel.Result? = null

    init {
        events.setStreamHandler(this)
        methods.setMethodCallHandler { call, result ->
            if (call.method != "permission") {
                result.notImplemented()
            } else if (hasPermission() || call.argument<Boolean>("request") != true) {
                result.success(hasPermission())
            } else if (permissionResult != null) {
                result.error("permission_pending", "Audio permission is already being requested", null)
            } else {
                permissionResult = result
                ActivityCompat.requestPermissions(activity,
                    arrayOf(Manifest.permission.RECORD_AUDIO), PERMISSION_REQUEST)
            }
        }
    }

    private fun hasPermission() = ContextCompat.checkSelfPermission(
        activity, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED

    fun onPermissionResult(requestCode: Int) {
        if (requestCode != PERMISSION_REQUEST) return
        permissionResult?.success(hasPermission())
        permissionResult = null
    }

    override fun onListen(arguments: Any?, sink: EventChannel.EventSink) {
        stop()
        val session = ((arguments as? Map<*, *>)?.get("sessionId") as? Number)?.toInt() ?: 0
        if (session <= 0 || !hasPermission()) {
            sink.error("visuals_unavailable", "Music visuals need audio access and active playback", null)
            return
        }
        try {
            val capture = Visualizer(session)
            visualizer = capture
            val range = Visualizer.getCaptureSizeRange()
            check(capture.setCaptureSize(512.coerceIn(range[0], range[1])) == Visualizer.SUCCESS)
            check(capture.setEnabled(true) == Visualizer.SUCCESS)
            val wave = ByteArray(capture.captureSize)
            val fft = ByteArray(capture.captureSize)
            val poll = object : Runnable {
                override fun run() {
                    if (visualizer !== capture) return
                    try {
                        check(capture.getWaveForm(wave) == Visualizer.SUCCESS)
                        check(capture.getFft(fft) == Visualizer.SUCCESS)
                        sink.success(mapOf("waveform" to wave, "fft" to fft))
                        handler.postDelayed(this, 33)
                    } catch (_: Exception) {
                        stop()
                        sink.error("visuals_unavailable", "Music visuals are unavailable on this device", null)
                    }
                }
            }
            handler.post(poll)
        } catch (_: Exception) {
            stop()
            sink.error("visuals_unavailable", "Music visuals are unavailable on this device", null)
        }
    }

    override fun onCancel(arguments: Any?) = stop()

    fun stop() {
        handler.removeCallbacksAndMessages(null)
        val old = visualizer
        visualizer = null
        try { old?.release() } catch (_: Exception) {}
    }

    fun close() {
        stop()
        permissionResult?.error("activity_closed", "Player closed", null)
        permissionResult = null
        methods.setMethodCallHandler(null)
        events.setStreamHandler(null)
    }

    companion object {
        const val PERMISSION_REQUEST = 7215
    }
}
