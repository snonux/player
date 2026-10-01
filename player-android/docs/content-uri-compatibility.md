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
