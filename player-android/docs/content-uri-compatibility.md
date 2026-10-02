# Android content URI compatibility check

This is a manual device check for the resolved media plugins and Android
Storage Access Framework. It does not add or change the default app entrypoint.
The existing server/client startup and share-link handling remain in
`lib/main.dart` and `MainActivity`.

## Run the harness

Build and launch the separate harness entrypoint on an Android device or
emulator:

```sh
cd player-android
flutter run --debug -t lib/content_uri_compatibility_harness.dart
```

For reproducible fixtures, create short audio/video files and copy them onto
the emulator's DownloadProvider:

```sh
ffmpeg -f lavfi -i sine=frequency=440:duration=24 -c:a libmp3lame \
  /tmp/player-compat-test.mp3 -y
ffmpeg -f lavfi -i testsrc=size=320x240:rate=24:duration=24 \
  -f lavfi -i sine=frequency=880:duration=24 -c:v libx264 \
  -pix_fmt yuv420p -c:a aac -shortest /tmp/player-compat-test.mp4 -y
adb push /tmp/player-compat-test.mp3 /sdcard/Download/
adb push /tmp/player-compat-test.mp4 /sdcard/Download/
```

Use **Pick audio** or **Pick video**, select a document, then press **Play** and
**Seek to 10 seconds**. For audio, test pause/resume, send the app to the
background, and use its media notification. Force-stop and relaunch the app to
confirm the selected URI and persisted grant remain usable. For the negative
permission check, press **Revoke persisted access**, force-stop and relaunch,
then press **Play**; playback should fail with a provider permission error.
Picker cancellation should leave the prior selection unchanged. To test offline
playback, disable network on the test device after copying media onto it.

The Android bridge uses `ACTION_OPEN_DOCUMENT`, `CATEGORY_OPENABLE`, an
audio/video MIME filter, `EXTRA_LOCAL_ONLY`, and a persisted read grant. It
returns the opaque content URI and display metadata. The Dart harness passes the
URI directly to `just_audio` and `VideoPlayerController.contentUri`; it does
not add HTTP headers or copy media into app storage. Use disposable test files.

## Result

Validated on 2026-10-01 with the `Player_FDroid_Test_API34` x86_64 Google APIs
emulator (Android 14 / API 34), Flutter 3.47.5, Dart 3.13.4, and these locked
packages: `just_audio` 0.9.46, `video_player` 2.11.1,
`video_player_android` 2.9.5, and `audio_service` 0.18.18.

- The system picker returned DownloadProvider content URIs and names/MIME types.
- Audio loaded and played as an offline `content://` source. Seeking to 10
  seconds and pause/resume worked. The audio service exposed a playing media
  session and its system media notification while the picker/background UI was
  visible.
- VideoPlayerController initialized the offline content URI and reported a
  24-second duration. The rendered H.264 test pattern was visible, and seeking
  to 10 seconds worked.
- The read grant was listed as persisted by `dumpsys activity permissions` and
  playback remained available after force-stop and relaunch.
- After releasing the selected video's persisted grant and force-stopping,
  playback failed with Android `SecurityException: Permission Denial`, as
  expected. The separate audio grant remained present.
- Wi-Fi and mobile data were disabled on the emulator during local playback;
  audio and video sources continued to load from the selected documents.
- `flutter analyze` passed and the Android debug APK built and ran.
- The complete `flutter test` suite passed all 580 tests, including the existing
  server-settings and share-origin regressions. The regular `lib/main.dart`
  debug APK also built successfully; no server API or network client code
  changed.

This verifies the emulator's DocumentsUI/DownloadProvider path. The emulator
does not provide removable storage, so removable-volume behavior remains
unverified and should be checked on physical hardware. Other document providers
may have different seek, offline, or descriptor behavior; these results do not
promise support for every provider or codec. The harness does not exercise the
production server routes; they remain on the existing app entrypoint and were
not changed by this check.

## Production local audio result (2026-10-02)

Tested the regular `lib/main.dart` debug APK on the same API 34 emulator,
using a disposable 180-second MP3 imported through **Add files → Choose audio**.
Airplane mode was enabled and Wi-Fi/mobile data were disabled throughout.

The first production run exposed a difference from the harness: passing an
empty headers map to `AudioSource.uri` enabled just_audio's HTTP proxy, which
failed with `Unsupported scheme content`. Production audio now passes null
when no headers are needed, letting Android open the document URI directly.
Nonempty authenticated server headers are retained and checked by a widget
regression. Local/public sources are checked for null headers.

Actual emulator checks with the updated production APK:

- The imported DownloadProvider URI loaded and played without a network
  connection; Android reported a playing media session with the local filename.
- The on-screen forward control changed a paused position from 55.366 to
  70.366 seconds. Selecting 1.5× speed was reflected in the Android session.
- Playback continued after Back to the local library and Home to the Android
  launcher. Android posted the transport notification with three actions.
- System media-session pause/play commands changed the background session to
  paused/playing. A paused sample of 30.641 seconds was present in the real
  app's `local_progress` SQLite table.
- After another pause, the database held 42.080 seconds. Force-stop/relaunch
  preserved the library record and URI grant; opening that entry again reported
  a playing session at 42.739 seconds, confirming durable resume.
- Removing the playing entry through **Remove from library** stopped its
  Android session (inactive, state NONE), emptied the library, and removed its
  persisted URI grant. The disposable original file stayed in Downloads.

The lifecycle widget regression additionally checks a fractional position save
when the UI pauses, continued periodic saves, no save on resumed alone, and no
stale writes after stop. Existing automated cases cover missing/unreadable
records, failed source loads, stale source ownership, completion/replay, and
local/server progress isolation. Those cases are automated checks, not claims
of physical-device testing. Notification buttons and lock-screen gestures were
not tapped directly; background control was verified through Android's media
session commands. Physical devices and other document providers remain outside
this emulator result.
