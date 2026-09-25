import java.net.URI
import java.util.Base64

plugins {
    id("com.android.application")
    id("kotlin-android")
    id("dev.flutter.flutter-gradle-plugin")
}

// Keep Android's VIEW filter on the same server origin used by the Dart API
// client. Flutter passes --dart-define values as base64-encoded key/value pairs.
val playerBaseUrl = providers.gradleProperty("dart-defines").orNull
    ?.split(",")
    ?.mapNotNull { encoded ->
        runCatching { String(Base64.getDecoder().decode(encoded)) }.getOrNull()
    }
    ?.firstOrNull { it.startsWith("PLAYER_BASE_URL=") }
    ?.substringAfter("=")
    ?: "http://10.0.2.2:8080"
val playerShareOrigin = URI(playerBaseUrl)
require(playerShareOrigin.scheme == "http" || playerShareOrigin.scheme == "https") {
    "PLAYER_BASE_URL must use http or https"
}
require(!playerShareOrigin.host.isNullOrEmpty()) {
    "PLAYER_BASE_URL must contain a host"
}
require(playerShareOrigin.rawUserInfo == null &&
    (playerShareOrigin.path.isNullOrEmpty() || playerShareOrigin.path == "/") &&
    playerShareOrigin.rawQuery == null && playerShareOrigin.rawFragment == null) {
    "PLAYER_BASE_URL must be a server origin without credentials, path, query, or fragment"
}
// Dart's Uri.origin omits the scheme's default port. The VIEW filter must
// therefore match URLs without an explicit :80 or :443 as well.
val playerSharePort = when {
    playerShareOrigin.scheme == "http" && playerShareOrigin.port == 80 -> -1
    playerShareOrigin.scheme == "https" && playerShareOrigin.port == 443 -> -1
    else -> playerShareOrigin.port
}

android {
    namespace = "zone.foo.player_android"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        applicationId = "zone.foo.player_android"
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        manifestPlaceholders["playerShareScheme"] = playerShareOrigin.scheme
        manifestPlaceholders["playerShareHost"] = playerShareOrigin.host
        // Android treats port -1 as unspecified. Non-default explicit ports
        // are matched exactly by the intent filter.
        manifestPlaceholders["playerSharePort"] = playerSharePort.toString()
        resValue("string", "player_share_origin", playerBaseUrl)
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

flutter {
    source = "../.."
}
