package zone.foo.player_android

import android.media.audiofx.AudioEffect
import android.media.audiofx.Equalizer
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.plugin.common.MethodChannel
import kotlin.math.ln
import kotlin.math.roundToInt

// Engine lifetime preserves equalization during background playback.
class NativeEqualizer : FlutterPlugin {
    private var channel: MethodChannel? = null
    private var effect: Equalizer? = null
    private var sessionId = 0
    private val frequencies = listOf(60.0, 230.0, 910.0, 3600.0, 14000.0)

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel = MethodChannel(binding.binaryMessenger, "zone.foo.player_android/equalizer").apply {
            setMethodCallHandler { call, result ->
                if (call.method != "apply") {
                    result.notImplemented()
                    return@setMethodCallHandler
                }
                val session = call.argument<Number>("sessionId")?.toInt() ?: 0
                val enabled = call.argument<Boolean>("enabled") == true
                val bands = call.argument<List<Number>>("bands")?.map { it.toDouble() }
                if (bands == null || bands.size != 5 || bands.any { !it.isFinite() || it < -24 || it > 0 }) {
                    result.error("invalid_equalizer", "Five valid equalizer levels are required", null)
                    return@setMethodCallHandler
                }
                try {
                    if (!enabled || session <= 0) {
                        release()
                    } else {
                        if (effect == null || sessionId != session) {
                            release()
                            effect = Equalizer(0, session)
                            sessionId = session
                        }
                        val current = checkNotNull(effect)
                        check(current.numberOfBands > 0)
                        val range = current.bandLevelRange
                        for (index in 0 until current.numberOfBands.toInt()) {
                            val band = index.toShort()
                            val hz = current.getCenterFreq(band) / 1000.0
                            val right = frequencies.indexOfFirst { it >= hz }
                            val db = when {
                                right == 0 -> bands.first()
                                right < 0 -> bands.last()
                                else -> {
                                    val left = right - 1
                                    val weight = (ln(hz) - ln(frequencies[left])) / (ln(frequencies[right]) - ln(frequencies[left]))
                                    bands[left] + weight * (bands[right] - bands[left])
                                }
                            }
                            current.setBandLevel(band, (db * 100).roundToInt().coerceIn(range[0].toInt(), range[1].toInt()).toShort())
                        }
                        check(current.setEnabled(true) == AudioEffect.SUCCESS)
                    }
                    result.success(null)
                } catch (_: Exception) {
                    release()
                    result.error("equalizer_unavailable", "Equalizer is unavailable on this device", null)
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
