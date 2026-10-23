package zone.foo.player_android

import android.media.audiofx.AudioEffect
import android.media.audiofx.LoudnessEnhancer
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.plugin.common.MethodChannel

// Engine lifetime, not Activity lifetime: background playback keeps its gain.
// Never install the effect through just_audio's uncaught allocation path.
class NativeLoudness : FlutterPlugin {
    private var channel: MethodChannel? = null
    private var effect: LoudnessEnhancer? = null
    private var sessionId = 0

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel = MethodChannel(binding.binaryMessenger, "zone.foo.player_android/loudness").apply {
            setMethodCallHandler { call, result ->
                if (call.method != "apply") {
                    result.notImplemented()
                    return@setMethodCallHandler
                }
                val session = call.argument<Number>("sessionId")?.toInt() ?: 0
                val gainMb = call.argument<Int>("gainMb") ?: -1
                if (gainMb !in 0..2400) {
                    result.error("invalid_gain", "Loudness gain must be between 0 and 2400 millibels", null)
                    return@setMethodCallHandler
                }
                try {
                    // Off never allocates an effect. A vanished session also
                    // releases the previous attachment without touching audio.
                    if (gainMb == 0 || session <= 0) {
                        release()
                    } else {
                        if (effect == null || session != sessionId) {
                            release()
                            effect = LoudnessEnhancer(session)
                            sessionId = session
                        }
                        val current = checkNotNull(effect)
                        // Android uses millibels: 100 mB equals 1 dB.
                        current.setTargetGain(gainMb)
                        check(current.setEnabled(true) == AudioEffect.SUCCESS)
                    }
                    result.success(null)
                } catch (_: Exception) {
                    release()
                    result.error("loudness_unavailable", "Loudness processing is unavailable on this device", null)
                }
            }
        }
    }

    private fun release() {
        val previous = effect
        effect = null
        sessionId = 0
        try { previous?.release() } catch (_: Exception) { /* Playback continues. */ }
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel?.setMethodCallHandler(null)
        channel = null
        release()
    }
}
