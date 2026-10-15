# Personal media verification

Device documents use the local library database (schema 5); private downloads
use their existing database (schema 2). Both upgrades add notes, favorites and
position bookmarks without replacing media or resume progress. Foreign keys
remove personal rows with the owning item. Serialized writes check that the
owner still exists; offline editors also capture an independent random copy
generation token so an old editor cannot recreate annotations after deletion and redownload.
Relinking a device document preserves personal data independently of the choice
to retain resume progress. Stored identities contain no credentials.

Server personal actions reuse notes and favorites and use the versioned
bookmarks API. A store captures its origin and credential generation, checks
before and after asynchronous operations, and supplies the captured generation
to the credential interceptor. Public shares have no personal actions. Playback
bookmark callbacks also capture the controller/session that opened the sheet.
A previous sheet cannot save the next track's position or seek its controller.

## Automated checks

```sh
flutter analyze
flutter test test/services/personal_media_store_test.dart \
  test/api/bookmarks_api_test.dart \
  test/widgets/personal_media_actions_test.dart \
  test/screens/notes_editor_screen_test.dart \
  test/screens/audio_player_screen_test.dart \
  test/screens/image_navigation_test.dart
flutter test
flutter build apk --debug
```

The database tests use real SQLite files: existing v4/v1 migrations, reopening,
relinking, Unicode labels, invalid and out-of-range positions, owner isolation,
cascading removal and queued/stale writes. Widget tests exercise visible save,
list, jump and delete actions, note edit/Back/reopen, image favorites, a small
landscape viewport with its keyboard shown, failed requests and stale responses.
The existing note-editor suite continues to cover edits made during saves,
failed Back flushes, explicit retries and clearing during a pending save.

## Disposable Android run

`integration_test/personal_media_test.dart` runs the normal app screens and
native decoders on a disposable Android emulator in two process invocations.
Never use it on a personal device/server: it modifies fixture annotations and
removes copies belonging to its loopback fixture origin.

```sh
python3 integration_test/support/offline_fixture.py \
  /absolute/path/to/player-server-binary --personal-media
adb reverse tcp:18765 tcp:18765
flutter test integration_test/personal_media_test.dart -d emulator-5554 --no-uninstall \
  --dart-define-from-file=/tmp/player-offline-FIXTURE/auth.json \
  --dart-define=PERSONAL_E2E_PHASE=online
# Stop the fixture before running the new app process:
flutter test integration_test/personal_media_test.dart -d emulator-5554 --no-uninstall \
  --dart-define=PERSONAL_E2E_PHASE=offline
```

The online phase uses the login form, plays the 60-second MP3 and MP4, saves
named positions, seeks native playback through the bookmark list and deletes a
bookmark. It edits notes and immediately leaves and reopens them, downloads
fixture copies, adds an offline audio bookmark/note and an image favorite/note.
The offline phase first verifies that the server cannot be reached, then checks
those annotations after a real app process restart without signing in. It seeks
and deletes the saved audio position, reopens notes, toggles the image favorite
and removes the fixture downloads. The fixture verifies all original hashes
when stopped. Host SQLite tests cover SAF identity/relink behavior; this run does
not operate Android's document picker or verify a physical device.

A separate `--dart-define=PERSONAL_E2E_PHASE=favorite` invocation seeds one
small private PNG and exercises retained Downloads → image viewer → favorite →
Back → remove favorite, checking the actual persisted value. It needs no server
and removes its own reserved test copy.

## Executed Android results

On 2026-10-15, the Android 14/API 34 x86_64 emulator passed the online and
offline invocations above, plus the separate favorite-return invocation. Audio and video used native decoders; both offline
bookmarks and notes survived a real process restart while the server was stopped.
The image favorite and note also survived. All six pre-existing server fixture
files retained their SHA-256 hashes. The online run encountered an emulator
System UI ANR overlay; its Flutter assertions still completed without an app
exception, and the overlay was dismissed with Wait before the offline run.
No physical-device or native document-picker verification is claimed here.
