# Offline downloads verification

Verified on 2026-10-15 with an Android 14/API 34 x86_64 emulator: both live
integration phases passed, including native MP3, H.264/AAC MP4, MPEG-TS stored
under an `.mp4` name, PNG and SVG playback. All five downloaded hashes matched
the actual Player server's original files. After deleting every offline copy,
all six server source files (including the lyrics sidecar) remained unchanged.
The app restarted between phases, the server was stopped before offline
playback, and the offline phase needed no login. Physical devices were not used.

Downloads use a separate app-private SQLite index and files under
`getDatabasesPath()/offline-downloads`. Names are SHA-256 identities of the
server origin, account ID and media ID; remote names are display metadata only.
Bearer/cookie headers are captured through the existing account credential gate,
never stored in download metadata, and never followed across redirects. Switching
servers/accounts or cancelling stops an active download. One download runs at a
time, with awaited chunk writes, size checks and incomplete-file cleanup. Audio
and video hand file URIs to the normal players with a distinct local identity;
their progress callbacks use only the offline database. Images read the same
private file, and SVGs retain the existing parser and raster budgets.

The manager has no remote deletion API. It stops a file's playback before
deleting the file and its index row. Late progress saves update existing rows
only. Android document-picker files use the existing separate library and are
never removed by this manager.

Podcast episodes use a separate device podcast store and direct publisher
downloads, described in [standalone podcasts](standalone-podcasts.md). They do
not enter this server-media Downloads index, require a server media ID, or
invoke the server episode-download endpoint. The installed two-device podcast
journey has verified independent copies, removal and redownload with listening
history retained; the remaining cross-platform sync boundaries are tracked in
the [podcast verification plan](../../docs/podcast-management-plan.md#integrated-verification-snapshot-2026-10-26).

## Host checks

```sh
flutter analyze
flutter test test/services/offline_downloads_test.dart
flutter test
```

The service test runs an actual HTTP server and private filesystem/SQLite
repository. It downloads a WAV, closes the server and database, reopens persisted
metadata, builds a local playback request, reads its real file, saves progress,
and removes the copy while checking the original file is unchanged. It also
covers credential/origin refusal, HTTP failures and redirects, cancellation,
account changes, metadata size mismatches, missing/altered files, empty responses,
streaming responses without a content length, filesystem write failure, duplicate
downloads, restart cleanup, invalid file keys and invalid/late progress.

## Actual Android playback

`integration_test/offline_downloads_test.dart` runs the real app and native
decoders on Android in two invocations. It needs a disposable emulator and a
real Player server containing `offline-audio.mp3`, `offline-video.mp4`,
`offline-image.png`, `offline-vector.svg`, and an MPEG-TS file named
`offline-transport.mp4`. Use
`integration_test/support/offline_fixture.py` to create and run that fixture.
The fixture generates account credentials into a mode-600 `auth.json` outside
the repository; pass that file by path, never inline secrets.

```sh
python3 integration_test/support/offline_fixture.py /absolute/path/to/player-server-binary
# In another terminal, use the directory printed by the fixture:
adb reverse tcp:18765 tcp:18765
flutter test integration_test/offline_downloads_test.dart -d emulator-5554 --no-uninstall \
  --dart-define-from-file=/tmp/player-offline-FIXTURE/auth.json \
  --dart-define=OFFLINE_E2E_ORIGIN=http://127.0.0.1:18765 \
  --dart-define=OFFLINE_E2E_PHASE=online
# Stop the fixture with Ctrl-C. It checks the remote file hashes on exit.
flutter test integration_test/offline_downloads_test.dart -d emulator-5554 --no-uninstall \
  --dart-define=OFFLINE_E2E_ORIGIN=http://127.0.0.1:18765 \
  --dart-define=OFFLINE_E2E_PHASE=offline
```

The online phase signs in through the app form and taps **Keep offline** for
all five server files, checks their private bytes and lists the downloads. The
offline phase starts a new app process with those files still present, first
checks that the server cannot be contacted, opens Downloads without signing in,
asserts advancing native audio/video positions (including the MPEG-TS file) and decoded PNG/SVG images, then
deletes each copy through the confirmation dialog. This is separate from the
host playback-request test; only an executed Android run verifies native media
decoding and platform storage. Native-compatible downloads use the original
`/download` bytes: `/stream` may remux MPEG-TS even when `transcoded` is false,
so it cannot be compared with the original file-size metadata. Transcoded files
use `/compat`; both paths retain complete-response and declared-size checks.

Do not point these tests at a personal server or app install. They create a test
login and offline copies, and remove fixture downloads from the tested app.
