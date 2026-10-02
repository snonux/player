# Standalone playback verification

Verified on 2026-10-02 for tasks `b03`–`i03`. These results apply to the current
source and normal `lib/main.dart` debug APK; they do not establish that an
F-Droid release containing this feature has been published.

## Platform and fixtures

- Android 14/API 34, x86_64 Google APIs emulator `Player_FDroid_Test_API34`.
- Flutter 3.47.5, Dart 3.13.4; `just_audio` 0.9.46, `audio_service` 0.18.18,
  `video_player` 2.11.1, `video_player_android` 2.9.5.
- Android DocumentsUI and Downloads/DownloadProvider; real persisted
  `content://` read grants, no copied-file playback fallback.
- Disposable 180-second MP3 and 90-second H.264/AAC MP4 generated with ffmpeg.
  The video visibly rendered a moving test pattern. A 31-byte invalid MP4 was
  used for a decode failure. Fixtures and database snapshots stayed outside git.
- A disposable Player server with a separate database/media root, exposed to
  the emulator at the debug build's origin `http://10.0.2.2:8080`. A local
  reverse proxy recorded method, redacted path, status, and whether credentials
  were present; it did not log credential values or request bodies.

## Android journeys

| Journey | Actual result |
| --- | --- |
| Fresh install with networking enabled | Uninstall/reinstall of the normal APK opened the empty **On this device** library with Add files/Connect to server. The proxy request count stayed at 41 before and after launch: zero new server requests. |
| Airplane-mode cold start | The normal app opened **On this device** and retained its library. The isolated server proxy received zero requests before Connect to server. |
| Local-only playback with network available | After enabling Wi-Fi/mobile data, importing and playing the MP3, Back/Home, and lock-screen control still left the proxy at zero requests. Subsequent successful server requests established that the test origin was reachable. |
| Persisted access and audio resume | Import through the system picker, pause at 42.080 seconds, force-stop/relaunch, then reopen the same entry: Android reported playing at 42.739 seconds. See the detailed [audio evidence](content-uri-compatibility.md#production-local-audio-result-2026-10-02). |
| Audio seek/speed/background | Forward moved 55.366 → 70.366 seconds; 1.5× appeared in the native media session. Back and Home kept audio playing and exposed its notification. |
| Lock-screen controls | With a temporary emulator PIN set and the device locked, directly tapping Play produced native PLAYING at 165.037 seconds; tapping Pause produced PAUSED at 165.794 seconds. The PIN was cleared after the check. No direct notification-button tap is claimed. |
| Video lifecycle/resume | The actual MP4 rendered with Chewie controls; periodic and Home-transition progress reached SQLite. A repeatable saved position of 23.898 seconds became 26.600 after force-stop/relaunch and brief playback. See [video evidence](content-uri-compatibility.md#production-local-video-result-2026-10-02). |
| Library switching | Server audio streamed, then selecting **On this device** stopped the remote session and a local entry played. Reconnecting to the same configured server restored its library. |
| Delayed server 401 during switch | A server-library refresh was held for 12 seconds and returned 401 after switching to local audio. Android still reported the local filename playing; SQLite retained both local records and continued saving fractional progress (74.487 seconds sampled). The local player stayed visible. |
| Bootstrap, login, logout | The Android UI created the first admin account, minted a token, browsed the scanned fixture set, and played authenticated audio. Later login minted a new token. Explicit logout returned 204 for token revocation and session logout; returning to local mode retained its records. |
| Public share | An explicit Android VIEW intent at the build origin loaded share metadata and played audio. Both metadata and stream requests had no bearer or cookie. A same-host link with port 8089 showed the native server-mismatch screen. |
| Corrupt media | Importing the invalid MP4 succeeded; playback showed a Source error with **Retry**. Back opened the library; its menu offered **Choose file again** and **Remove from library**, with confirmation that the original file stays. |
| Removed document | Deleting the disposable MP4 after leaving its player and reopening the entry showed Source error with **Retry**. The fixture was restored afterward. |
| Removal and grant release | Removing a playing audio entry stopped the native session, removed its local history and persisted read grant, and retained the original document. |
| Revoked grant | The separate compatibility harness released a video grant; after force-stop/relaunch, Android rejected access with SecurityException. This is a harness result, not a claim about the production error UI. |

The zero-request results apply to local-only journeys before connecting to a
server. Previously queued server progress can be finalized during an explicit
library transition; it is server progress, and local progress never enters that
queue. The delayed-401 journey included previously initiated server requests.

## Durable automated coverage

The full suite exercises production Dart repositories, routing, source
resolution, session ownership, and auth cleanup. Native decoder/controller
stubs in widget tests do not replace the Android checks above.

- `test/screens/local_playback_route_test.dart`: a real SQLite file is closed
  and reopened in a new provider container/handler. The durable local ID restores
  the URI and 12.3-second resume position, with no server API, queue, or token
  provider allowed. Removing the record then rejects the route and leaves no
  playback.
- `test/services/local_library_repository_test.dart`: logout preserves a local
  session lease and SQLite progress; explicitly changing the server stops the
  local lease while retaining the record and position. Other cases cover failed
  writes, grant ownership, relink, duplicates, and database migrations.
- `test/providers/server_settings_test.dart`: fresh settings/router start
  locally without auth/count-users work; legacy auth origin survives expired
  credentials, invalid saved origins recover locally, and server/public origins
  remain distinct. Legacy migration is automated, not an actual APK-upgrade
  journey on the emulator.
- `test/providers/auth_persistence_test.dart` and
  `test/services/playback_session_coordinator_test.dart`: late 401s, logout,
  token/restore races, suspended server progress, and local/public session
  isolation. The local settings UI omits account/logout controls, so logout
  during local playback is exercised through the cleanup APIs in tests rather
  than a native UI action.
- Audio/video screen and handler tests cover rapid competing source loads,
  delayed initialization/disposal, stale completions, and source failures.
  Rapid switching while a decoder load is pending is automated; the native
  switch above used an already playing stream plus a pending server refresh.
- Lifecycle tests save fractional audio/video positions on backgrounding,
  avoid zero writes while video resume is loading, and reject stale writes.
  Completion/replay and missing/unreadable records also have regression tests.

## Final checks

From `player-android/`:

```sh
flutter analyze
flutter test --concurrency=2 --reporter expanded
flutter build apk --debug
```

Analyzer: no issues. Full Flutter suite: **642 tests passed**. Normal debug APK:
built successfully; its SHA-256 matched the APK installed for the native
journeys. From `player-server/`, `mage test` passed the complete Go race suite
and web JavaScript tests; the fixture server also built with `go build`.

No physical device or removable volume was available. Other providers, codecs,
Android versions, direct notification-button taps, actual upgrade migration,
and interrupted removable-storage access remain unverified. No general codec
or provider compatibility claim is made. Folder scanning, downloads, playlists,
local tags/notes, and cross-device local-history sync remain outside scope.
