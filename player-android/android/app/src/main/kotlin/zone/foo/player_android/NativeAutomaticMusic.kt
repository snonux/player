package zone.foo.player_android

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.plugin.common.MethodChannel
import java.util.UUID

/** Schedules OS wakes without ever starting an engine or loading credentials. */
class NativeAutomaticMusic : FlutterPlugin {
    private var channel: MethodChannel? = null
    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        val context = binding.applicationContext
        channel = MethodChannel(binding.binaryMessenger, CHANNEL).also { bridge ->
            active = bridge
            bridge.setMethodCallHandler { call, result ->
                if (call.method != "schedule") { result.notImplemented(); return@setMethodCallHandler }
                try {
                    val scheduler = context.getSystemService(Context.JOB_SCHEDULER_SERVICE) as JobScheduler
                    if (call.argument<Boolean>("enabled") != true) {
                        scheduler.cancel(JOB_ID)
                    } else {
                        val hours = call.argument<Int>("hours") ?: 6
                        require(hours in listOf(1, 6, 24))
                        val job = JobInfo.Builder(JOB_ID, ComponentName(context, AutomaticMusicJob::class.java))
                            .setPeriodic(hours * 3600000L)
                            .setPersisted(true)
                            .setRequiredNetworkType(if (call.argument<Boolean>("wifiOnly") != false)
                                JobInfo.NETWORK_TYPE_UNMETERED else JobInfo.NETWORK_TYPE_ANY)
                            .build()
                        check(scheduler.schedule(job) == JobScheduler.RESULT_SUCCESS)
                    }
                    result.success(null)
                } catch (_: RuntimeException) {
                    result.error("scheduling", "Android could not schedule automatic music.", null)
                }
            }
        }
    }
    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        if (active === channel) active = null
        channel?.setMethodCallHandler(null)
        channel = null
    }
    companion object {
        const val CHANNEL = "zone.foo.player_android/automatic_music"
        const val JOB_ID = 19784
        var active: MethodChannel? = null
            private set
    }
}

class AutomaticMusicJob : JobService() {
    private var invocation: String? = null
    private var bridge: MethodChannel? = null
    override fun onStartJob(params: JobParameters): Boolean {
        val channel = NativeAutomaticMusic.active ?: return false
        val token = UUID.randomUUID().toString()
        invocation = token
        bridge = channel
        channel.invokeMethod("wake", token, object : MethodChannel.Result {
            private fun finish() {
                if (invocation != token) return
                invocation = null
                bridge = null
                jobFinished(params, false) // periodic job supplies its own next run
            }
            override fun success(result: Any?) = finish()
            override fun error(code: String, message: String?, details: Any?) = finish()
            override fun notImplemented() = finish()
        })
        return true
    }
    override fun onStopJob(params: JobParameters): Boolean {
        val token = invocation
        invocation = null
        if (token != null) bridge?.invokeMethod("stop", token)
        bridge = null
        return false
    }
}
