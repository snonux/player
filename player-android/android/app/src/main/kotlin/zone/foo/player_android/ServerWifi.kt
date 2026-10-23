package zone.foo.player_android

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.LocationManager
import android.net.wifi.WifiManager
import android.os.Build
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

// SSID is only a routing hint. It never authorizes an address or an account.
class ServerWifi(private val activity: MainActivity, messenger: BinaryMessenger) {
    private val channel = MethodChannel(messenger, "zone.foo.player_android/server_wifi")
    private var pending: MethodChannel.Result? = null

    init {
        channel.setMethodCallHandler { call, result ->
            if (call.method != "wifiName") {
                result.notImplemented()
            } else if (hasPermission()) {
                result.success(readName())
            } else if (call.argument<Boolean>("requestPermission") == true) {
                if (pending != null) {
                    result.error("busy", "Permission request already open", null)
                } else {
                    pending = result
                    ActivityCompat.requestPermissions(activity,
                        arrayOf(Manifest.permission.ACCESS_FINE_LOCATION,
                            Manifest.permission.ACCESS_COARSE_LOCATION), PERMISSION_REQUEST)
                }
            } else {
                result.success(null)
            }
        }
    }

    fun onPermissionResult(requestCode: Int) {
        if (requestCode != PERMISSION_REQUEST) return
        val result = pending
        pending = null
        result?.success(if (hasPermission()) readName() else null)
    }

    fun close() {
        pending?.success(null)
        pending = null
        channel.setMethodCallHandler(null)
    }

    private fun hasPermission() = ContextCompat.checkSelfPermission(activity,
        Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    @Suppress("DEPRECATION")
    private fun readName(): String? {
        return try {
            val location = activity.getSystemService(Context.LOCATION_SERVICE) as LocationManager
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && !location.isLocationEnabled) return null
            val wifi = activity.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            val name = wifi.connectionInfo?.ssid?.removeSurrounding("\"")
            name?.takeIf { it.isNotBlank() && it != "<unknown ssid>" && it != "0x" }
        } catch (_: SecurityException) { null }
    }

    companion object { const val PERMISSION_REQUEST = 5301 }
}
