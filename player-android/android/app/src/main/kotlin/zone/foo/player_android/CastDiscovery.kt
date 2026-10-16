package zone.foo.player_android

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Build
import android.os.Handler
import android.os.Looper
import io.flutter.plugin.common.EventChannel
import java.net.Inet4Address
import java.net.InetAddress

// Finds Google Cast receivers (Chromecast, Cast-enabled TVs and speakers) on
// the local network with Android's own mDNS service, so the app needs no
// Google Play Services. Each listen() streams "found" and "lost" events to
// Dart until the stream is cancelled.
class CastDiscovery(context: Context) : EventChannel.StreamHandler {
    private val nsd = context.getSystemService(Context.NSD_SERVICE) as NsdManager
    private val main = Handler(Looper.getMainLooper())
    private var sink: EventChannel.EventSink? = null
    private var listener: NsdManager.DiscoveryListener? = null

    // Before Android 14 NsdManager resolves one service at a time and fails
    // a second concurrent resolve, so found services wait in this queue.
    private val pending = ArrayDeque<NsdServiceInfo>()
    private var resolving = false

    override fun onListen(arguments: Any?, events: EventChannel.EventSink) {
        stop()
        sink = events
        val discovery = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(serviceType: String) {}
            override fun onDiscoveryStopped(serviceType: String) {}
            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                main.post { sink?.error("discovery_failed", "Discovery failed ($errorCode)", null) }
            }
            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {}
            override fun onServiceFound(info: NsdServiceInfo) {
                main.post {
                    if (listener !== this) return@post
                    pending.addLast(info)
                    resolveNext()
                }
            }
            override fun onServiceLost(info: NsdServiceInfo) {
                main.post {
                    if (listener !== this) return@post
                    sink?.success(mapOf("event" to "lost", "service" to info.serviceName))
                }
            }
        }
        listener = discovery
        try {
            nsd.discoverServices(SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, discovery)
        } catch (error: Exception) {
            listener = null
            events.error("discovery_failed", error.message, null)
        }
    }

    override fun onCancel(arguments: Any?) {
        stop()
    }

    private fun stop() {
        val current = listener
        listener = null
        sink = null
        pending.clear()
        if (current != null) {
            try {
                nsd.stopServiceDiscovery(current)
            } catch (_: Exception) {}
        }
    }

    private fun resolveNext() {
        if (resolving || listener == null) return
        val info = pending.removeFirstOrNull() ?: return
        resolving = true
        val owner = listener
        @Suppress("DEPRECATION")
        nsd.resolveService(info, object : NsdManager.ResolveListener {
            override fun onResolveFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
                main.post { finishResolve() }
            }

            override fun onServiceResolved(serviceInfo: NsdServiceInfo) {
                main.post {
                    if (listener === owner) report(serviceInfo)
                    finishResolve()
                }
            }
        })
    }

    private fun finishResolve() {
        resolving = false
        resolveNext()
    }

    private fun report(info: NsdServiceInfo) {
        val host = address(info) ?: return
        val attributes = info.attributes
        fun text(key: String): String? = attributes[key]?.let { String(it, Charsets.UTF_8) }
        sink?.success(mapOf(
            "event" to "found",
            "service" to info.serviceName,
            "id" to (text("id") ?: info.serviceName),
            "name" to (text("fn") ?: info.serviceName),
            "model" to text("md"),
            "host" to host,
            "port" to info.port,
        ))
    }

    // An IPv4 address when there is one: link-local IPv6 needs a scope that
    // a plain address string does not carry.
    private fun address(info: NsdServiceInfo): String? {
        val addresses: List<InetAddress> = if (Build.VERSION.SDK_INT >= 34) {
            info.hostAddresses
        } else {
            @Suppress("DEPRECATION")
            listOfNotNull(info.host)
        }
        val chosen = addresses.firstOrNull { it is Inet4Address } ?: addresses.firstOrNull()
        return chosen?.hostAddress
    }

    companion object {
        const val CHANNEL = "zone.foo.player_android/cast_discovery"
        private const val SERVICE_TYPE = "_googlecast._tcp."
    }
}
