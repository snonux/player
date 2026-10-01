package zone.foo.player_android

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.provider.OpenableColumns
import com.ryanheise.audioservice.AudioServiceActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

// AudioServiceActivity (not FlutterActivity) is required by the audio_service
// plugin so background audio sessions, lockscreen controls, and media buttons
// re-attach correctly to this single-Activity Flutter app.  Using the default
// FlutterActivity causes AudioService.init() to throw at startup with
// "The Activity class declared in your AndroidManifest.xml is wrong".
class MainActivity : AudioServiceActivity() {
    private lateinit var documentChannel: MethodChannel
    private var pendingPicker: MethodChannel.Result? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        documentChannel = MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "zone.foo.player_android/document_compatibility"
        )
        documentChannel.setMethodCallHandler { call, result ->
            when (call.method) {
                "pick" -> {
                    if (pendingPicker != null) {
                        result.error("picker_busy", "A document picker is already open", null)
                    } else {
                        val kind = call.argument<String>("kind")
                        if (kind != "audio" && kind != "video") {
                            result.error("invalid_kind", "Choose audio or video", null)
                        } else {
                            pendingPicker = result
                            val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                                addCategory(Intent.CATEGORY_OPENABLE)
                                type = if (kind == "audio") "audio/*" else "video/*"
                                putExtra(Intent.EXTRA_LOCAL_ONLY, true)
                                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or
                                    Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
                            }
                            try {
                                startActivityForResult(intent, DOCUMENT_PICKER_REQUEST)
                            } catch (error: Exception) {
                                pendingPicker = null
                                result.error("picker_unavailable", error.message, null)
                            }
                        }
                    }
                }
                "release" -> {
                    val rawUri = call.argument<String>("uri")
                    if (rawUri == null) {
                        result.error("invalid_uri", "URI is required", null)
                    } else {
                        try {
                            contentResolver.releasePersistableUriPermission(
                                Uri.parse(rawUri), Intent.FLAG_GRANT_READ_URI_PERMISSION
                            )
                            result.success(null)
                        } catch (error: Exception) {
                            result.error("release_failed", error.message, null)
                        }
                    }
                }
                else -> result.notImplemented()
            }
        }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != DOCUMENT_PICKER_REQUEST) return
        val callback = pendingPicker ?: return
        pendingPicker = null
        val uri = data?.data
        if (resultCode != RESULT_OK || uri == null) {
            callback.success(null)
            return
        }
        val readGrant = data.flags and Intent.FLAG_GRANT_READ_URI_PERMISSION
        if (readGrant == 0) {
            callback.error("grant_failed", "Picker did not grant read access", null)
            return
        }
        val alreadyPersisted = contentResolver.persistedUriPermissions.any {
            it.uri == uri && it.isReadPermission
        }
        var acquiredPersistedReadGrant = false
        try {
            if (!alreadyPersisted) {
                contentResolver.takePersistableUriPermission(uri, readGrant)
                acquiredPersistedReadGrant = true
            }
            callback.success(mapOf(
                "uri" to uri.toString(),
                "name" to displayName(uri),
                "mimeType" to contentResolver.getType(uri)
            ))
        } catch (error: Exception) {
            if (acquiredPersistedReadGrant) {
                try {
                    contentResolver.releasePersistableUriPermission(uri, readGrant)
                } catch (_: Exception) {}
            }
            callback.error("grant_failed", error.message, null)
        }
    }

    private fun displayName(uri: Uri): String? = contentResolver.query(
        uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null
    )?.use { cursor ->
        if (cursor.moveToFirst()) cursor.getString(0) else null
    }

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

    private companion object {
        const val DOCUMENT_PICKER_REQUEST = 7214
    }
}
