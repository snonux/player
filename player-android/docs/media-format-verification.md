# Android server-media verification

Tested on 2026-10-17 with `Player_FDroid_Test_API34` (Android 14, x86_64),
Flutter 3.47.5, and the real Android media plugins.

## Music-video failure and fix

Against `https://player.f3s.buetow.org`, video 3433 (Britney Spears, Toxic)
failed in ExoPlayer's `AacUtil.parseAudioSpecificConfig` with
`ArrayIndexOutOfBoundsException`. The source is MPEG-TS named `.mp4`; the
server's browser-oriented `/stream` remux contains no AAC initialization data.
`ffprobe` of the stream confirmed an AAC track without `extradata_size`.

Android now streams original video bytes from `/download`, whose range
support also permits seeking. Android detects the actual container itself.
The bytes go directly to the native player; the endpoint name does not mean
Player downloads the whole video first. Formats marked `transcoded` still use
`/compat`. Audio, images, device documents and offline files retain their
existing sources. Public video shares use their original endpoint while
preserving the viewing credential and share identity.

The live regression covers videos 3433, 3444 (Dua Lipa, Houdini), 3488
(Calvin Harris, Potion), and 3489 (Ciara, Ecstasy). Each initializes with a
nonzero video size, advances past three seconds, pauses, and resumes.
The first two are MPEG-TS; the latter two are ordinary MP4. Android logs show
both H.264 and AAC decoders running. The live test uses requests without
progress callbacks, preserving the account's existing resume positions.

## Complete extension coverage

The disposable fixture generates one file per supported extension plus a
second MP4-named file containing MPEG-TS. Tests use the normal app, sign in
through its login screen, then navigate to the actual playback/viewer routes.

| Kind | Extensions | Checks |
| --- | --- | --- |
| Video | mp4, mkv, avi, mov, wmv, asf, flv, webm; MPEG-TS named mp4 | Native initialization, advancing position, nonzero picture dimensions, pause, seek, continued playback |
| Audio | mp3, wav, flac, aac, ogg, m4a, wma, m4b, opus | Native playback advances, duration is known, pause and seek |
| Image | jpg, jpeg, png, gif, webp, bmp, avif, svg | The image viewer contains a decoded `RawImage`, without an unavailable-image error |

Legacy AVI, WMV, ASF, FLV and WMA samples exercise the server's actual
compatibility conversion. A single-use MPEG-TS video share additionally checks
playback and seeking with the original endpoint, retention of `?view=`, and
exactly one counted viewing. Host screenshots also confirm the generated MP4
paints changing video frames, with embedded timecodes advancing from 1.08 to
3.04 seconds.

These checks cover the generated codec/container combinations on API 34.
They do not assert that every codec profile or Android device can decode
every file bearing those extensions.

## Results

All 26 fixture media files passed, including all 25 supported extensions and
MPEG-TS named MP4. The single-use video share passed playback and seeking with
one viewing. All four live music videos passed. No additional format defect
was found in these samples.

`flutter analyze` was clean. All 1,312 Flutter unit/widget tests passed with
`flutter test --concurrency=2`. The initial parallel run exceeded one SVG
wall-clock limit while the emulator and server tests were busy; the SVG suite
passed alone and in the complete rerun. Server `go test -race ./...` and
`mage webTest` passed.

## Repeat the checks

Build the server from `player-server/`:

```sh
go build -o /tmp/player-format-server ./cmd/player
```

From `player-android/`, start the disposable fixture in a separate terminal:

```sh
python3 integration_test/support/all_formats_fixture.py /tmp/player-format-server
```

It prints the temporary fixture directory and an `auth_file` path. Pass that
path in place of `/tmp/player-formats-EXAMPLE/auth.json` below. No credentials,
media, database or build products belong in git.

```sh
adb reverse tcp:18765 tcp:18765
flutter test integration_test/all_formats_playback_test.dart -d emulator-5554 \
  --dart-define-from-file=/tmp/player-formats-EXAMPLE/auth.json \
  --dart-define=PLAYER_BASE_URL=http://127.0.0.1:18765
```

Stop the fixture with Ctrl-C. Its generated files remain in the printed
temporary directory for inspection. The test signs out of the disposable
account after checking the formats.

To repeat the live music-video regression, provide a private JSON file with
`username` and `password`. Optional `VIDEO_E2E_ORIGIN` and `VIDEO_E2E_IDS`
select another server and comma-separated video IDs:

```sh
flutter test integration_test/server_video_playback_test.dart -d emulator-5554 \
  --dart-define-from-file=/path/outside/repository/auth.json
```

Integration test APKs contain their supplied credentials. Replace the test
APK with a normal app build after testing.

## Legacy MPEG verification (2026-10-19)

The fixture now includes MPEG-1 video/MP2 audio in `.mpg` and MPEG-2 video/MP2
audio in `.mpeg`. Both are indexed as video and flagged for compatibility
playback. The native API 34 emulator run passed all 28 files (27 supported
extensions plus MPEG-TS named MP4), and all three single-use video shares:
MPEG-TS, MPG and MPEG. MPEG shares use `/compat` with their viewing credential.
Video seeks are checked while paused at the requested position, before resuming,
so ordinary playback cannot satisfy the seek check.

The matching web suite passed both generated MPEG formats in installed Google
Chrome and Playwright Firefox, through library playback and single-use shares,
including seeking and ranged original/compatibility downloads. The real ffmpeg
regressions also cover MP4 content named `.mpg`/`.mpeg`, matching the source
profiles found in the retired Audiobookshelf library. Original source bytes are
preserved. These checks do not cover every Android device or codec profile.
