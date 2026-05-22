package zone.foo.player_android

import com.ryanheise.audioservice.AudioServiceActivity

// AudioServiceActivity (not FlutterActivity) is required by the audio_service
// plugin so background audio sessions, lockscreen controls, and media buttons
// re-attach correctly to this single-Activity Flutter app.  Using the default
// FlutterActivity causes AudioService.init() to throw at startup with
// "The Activity class declared in your AndroidManifest.xml is wrong".
class MainActivity : AudioServiceActivity()
