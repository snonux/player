package zone.foo.player_android

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import com.ryanheise.audioservice.AudioServiceActivity

// AudioServiceActivity (not FlutterActivity) is required by the audio_service
// plugin so background audio sessions, lockscreen controls, and media buttons
// re-attach correctly to this single-Activity Flutter app.  Using the default
// FlutterActivity causes AudioService.init() to throw at startup with
// "The Activity class declared in your AndroidManifest.xml is wrong".
class MainActivity : AudioServiceActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        val rejected = isMismatchedShareIntent(intent)
        if (rejected) {
            // Flutter reads Activity.intent during startup. Remove the token
            // before its router or public API client can see the URL.
            intent = Intent(Intent.ACTION_MAIN)
        }
        // A restored Flutter route must not revive a previously opened token.
        super.onCreate(if (rejected) null else savedInstanceState)
        if (rejected) {
            startActivity(Intent(this, ShareLinkMismatchActivity::class.java))
            finish()
        }
    }

    override fun onNewIntent(intent: Intent) {
        if (isMismatchedShareIntent(intent)) {
            startActivity(Intent(this, ShareLinkMismatchActivity::class.java))
            return
        }
        super.onNewIntent(intent)
    }

    private fun isMismatchedShareIntent(incoming: Intent?): Boolean {
        if (incoming?.action != Intent.ACTION_VIEW) return false
        val link = incoming.data ?: return false
        val configured = Uri.parse(getString(R.string.player_share_origin))
        return !link.scheme.equals(configured.scheme, ignoreCase = true) ||
            !link.host.equals(configured.host, ignoreCase = true) ||
            effectivePort(link) != effectivePort(configured)
    }

    private fun effectivePort(uri: Uri): Int = when {
        uri.port >= 0 -> uri.port
        uri.scheme.equals("http", ignoreCase = true) -> 80
        uri.scheme.equals("https", ignoreCase = true) -> 443
        else -> -1
    }
}
