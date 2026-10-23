package zone.foo.player_android

import android.content.ComponentName
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.support.v4.media.MediaBrowserCompat
import androidx.media.MediaBrowserServiceCompat
import androidx.media.MediaSessionManager
import com.ryanheise.audioservice.AudioService
import java.security.MessageDigest

/**
 * Validates external media hosts while keeping audio_service's one player and
 * session. The plugin explicitly binds/starts AudioService.class, so replacing
 * it with a subclass would split the native service and break media buttons.
 */
class AutoBrowseService : MediaBrowserServiceCompat() {
    private lateinit var browser: MediaBrowserCompat
    private val pending = mutableListOf<() -> Unit>()
    private val subscriptions = linkedMapOf<String, BrowseSubscription>()
    private var unavailable = false
    private val startupTimer = Handler(Looper.getMainLooper())
    private val startupTimeout = Runnable { failConnection() }

    override fun onCreate() {
        super.onCreate()
        browser = MediaBrowserCompat(this, ComponentName(this, AudioService::class.java),
            object : MediaBrowserCompat.ConnectionCallback() {
                override fun onConnected() {
                    unavailable = false
                    startupTimer.removeCallbacks(startupTimeout)
                    if (sessionToken == null) setSessionToken(browser.sessionToken)
                    val waiting = pending.toList()
                    pending.clear()
                    waiting.forEach { it() }
                }

                override fun onConnectionFailed() = failConnection()
                override fun onConnectionSuspended() = failConnection()
            }, null)
        browser.connect()
        startupTimer.postDelayed(startupTimeout, 15000)
    }

    override fun onGetRoot(packageName: String, uid: Int, hints: Bundle?): BrowserRoot? {
        // A package name alone is forgeable; first verify its actual UID.
        if (packageManager.getPackagesForUid(uid)?.contains(packageName) != true) return null
        val trusted = uid == Process.myUid() ||
            MediaSessionManager.getSessionManager(this).isTrustedForMediaControl(
                MediaSessionManager.RemoteUserInfo(packageName, -1, uid)) ||
            isProductionAndroidAuto(packageName)
        if (!trusted) return null
        return BrowserRoot("root", Bundle().apply {
            putBoolean("android.media.browse.SEARCH_SUPPORTED", true)
            putBoolean("android.media.browse.CONTENT_STYLE_SUPPORTED", true)
            putInt("android.media.browse.CONTENT_STYLE_BROWSABLE_HINT", 1)
            putInt("android.media.browse.CONTENT_STYLE_PLAYABLE_HINT", 1)
        })
    }

    @Suppress("DEPRECATION")
    private fun isProductionAndroidAuto(packageName: String): Boolean {
        if (packageName != "com.google.android.projection.gearhead") return false
        return try {
            val signatures = if (Build.VERSION.SDK_INT >= 28) {
                packageManager.getPackageInfo(packageName, PackageManager.GET_SIGNING_CERTIFICATES)
                    .signingInfo?.apkContentsSigners
            } else {
                packageManager.getPackageInfo(packageName, PackageManager.GET_SIGNATURES).signatures
            }
            if (signatures == null || signatures.size != 1) return false
            val digest = MessageDigest.getInstance("SHA-256").digest(signatures[0].toByteArray())
                .joinToString("") { "%02x".format(it.toInt() and 0xff) }
            // Original and rotated production certs from Android's UAMP sample:
            // https://github.com/android/uamp/blob/main/common/src/main/res/xml/allowed_media_browser_callers.xml
            digest in setOf(
                "fdb00c43dbde8b51cb312aa81d3b5fa17713adb94b28f598d77f8eb89daceedf",
                "1ca8dcc0bed3cbd872d2cb791200c0292ca9975768a82d676b8b424fb65b5295")
        } catch (_: PackageManager.NameNotFoundException) {
            false
        }
    }

    override fun onLoadChildren(parentId: String, result: Result<MutableList<MediaBrowserCompat.MediaItem>>) {
        result.detach()
        connected {
            if (!browser.isConnected) {
                result.sendResult(null)
                return@connected
            }
            var subscription = subscriptions[parentId]
            // A new host load requests fresh data (including retry after a
            // network failure). Concurrent loads share the same in-flight read.
            if (subscription == null || subscription.waiting.isEmpty()) {
                if (subscription != null) browser.unsubscribe(parentId, subscription)
                if (subscriptions.size >= 128 && !subscriptions.containsKey(parentId)) {
                    val oldest = subscriptions.entries.first()
                    browser.unsubscribe(oldest.key, oldest.value)
                    oldest.value.finish(null, notify = false)
                    subscriptions.remove(oldest.key)
                }
                subscription = BrowseSubscription(parentId)
                subscriptions[parentId] = subscription
                subscription.waiting.add(result)
                browser.subscribe(parentId, subscription)
            } else if (subscription.waiting.size < 128) {
                subscription.waiting.add(result)
            } else {
                result.sendResult(null)
            }
        }
    }

    private inner class BrowseSubscription(val parentId: String) : MediaBrowserCompat.SubscriptionCallback() {
        val waiting = mutableListOf<Result<MutableList<MediaBrowserCompat.MediaItem>>>()
        fun finish(items: MutableList<MediaBrowserCompat.MediaItem>?, notify: Boolean = true) {
            val requests = waiting.toList()
            waiting.clear()
            requests.forEach { it.sendResult(items) }
            if (notify && requests.isEmpty()) notifyChildrenChanged(parentId)
        }

        override fun onChildrenLoaded(id: String, children: MutableList<MediaBrowserCompat.MediaItem>) {
            if (subscriptions[parentId] === this) finish(children)
        }
        override fun onError(id: String) {
            if (subscriptions[parentId] === this) finish(null)
        }
    }

    override fun onLoadItem(itemId: String, result: Result<MediaBrowserCompat.MediaItem>) {
        result.detach()
        connected {
            if (!browser.isConnected) result.sendResult(null)
            else browser.getItem(itemId, object : MediaBrowserCompat.ItemCallback() {
                override fun onItemLoaded(item: MediaBrowserCompat.MediaItem?) = result.sendResult(item)
                override fun onError(id: String) = result.sendResult(null)
            })
        }
    }

    override fun onSearch(query: String, extras: Bundle?, result: Result<MutableList<MediaBrowserCompat.MediaItem>>) {
        result.detach()
        connected {
            if (!browser.isConnected) result.sendResult(null)
            else browser.search(query, extras, object : MediaBrowserCompat.SearchCallback() {
                override fun onSearchResult(q: String, e: Bundle?, items: MutableList<MediaBrowserCompat.MediaItem>) =
                    result.sendResult(items)
                override fun onError(q: String, e: Bundle?) = result.sendResult(null)
            })
        }
    }

    private fun connected(action: () -> Unit) {
        if (browser.isConnected || unavailable) action()
        else if (pending.size < 128) pending.add(action)
        else {
            // Drain failures instead of holding unlimited detached results.
            failConnection()
            action()
        }
    }

    private fun failConnection() {
        unavailable = true
        val waiting = pending.toList()
        pending.clear()
        waiting.forEach { it() }
        subscriptions.values.toList().forEach { it.finish(null, notify = false) }
    }

    override fun onDestroy() {
        startupTimer.removeCallbacks(startupTimeout)
        failConnection()
        browser.disconnect()
        subscriptions.clear()
        super.onDestroy()
    }
}
