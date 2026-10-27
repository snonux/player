# M61 Internet radio — verified implementation

## Focused acceptance, 2026-10-27

The disposable Player fixture served a paced live MP3 stream through the normal
radio relay and a local JPEG cover through its artwork relay. The focused
Playwright journey passed in installed Google Chrome and Mozilla Firefox,
sequentially with one worker. Both browsers covered station create/edit,
rejected fragment URLs, artwork, viewer write denial, search and keyboard
playback, decoded 440 Hz output, Stop/Play, an off-air stream, browser offline
refresh and reconnect, and narrow dark/light controls. The browser journeys
used the same fixture but deleted their own station after each run.

One API 34 Android emulator ran the release x86_64 APK. Native touch checks
covered viewer search/no-match, live Play/Stop/Back and return, unavailable
station Retry and recovery, admin edit/add/confirmed remove, local artwork,
offline Retry, and saved station persistence after server restart. The Android
media session reported `PLAYING` with the station title; QEMU WAV capture had
4.15 audible seconds at 440.0 Hz. `KEYCODE_MEDIA_PAUSE` changed the session to
`PAUSED` and the station row to Stopped/Play. Logout ended the active session;
the admin subsequently saw management controls. The local artwork file was a
fixture cover, and the stream was a deterministic service double. No public
third-party station or LAN receiver was included in this run.

Evidence is outside git at
`/home/paul/tmp/codex/player-m61-evidence/{chrome,firefox,android}`. The
browser rerun after the offline-message fix passed on both installed browsers;
native code and APK were unchanged. The earlier full task-20 regression gate
passed Go, web, Flutter, Chrome, Firefox and native baseline checks. This
task's focused acceptance did not rerun the full suites; the next broad gate
is due after task 30.

To repeat the native journey, boot one `Player_FDroid_Test_API34` emulator with
`QEMU_AUDIO_DRV=wav` and `QEMU_WAV_PATH`, install the current x86_64 release
APK, map the Player port with `adb reverse`, and sign in using the disposable
fixture's `auth.json`. Seed one station through the admin `POST /api/radio`
using the fixture's `/radio/fixture.mp3` URL and `local_network: true`. Open
**Internet radio** from the Library drawer, then use the visible controls for
the viewer and admin journeys above. Change the station URL to
`/radio/down.mp3` for outage/Retry; use `music_fixture.py restart` for
persistence; temporarily remove and restore the owned `adb reverse` mapping
for offline refresh. Measure QEMU audio and inspect Android `dumpsys
media_session` while playing. Clear the disposable app data and stop only the
owned fixture/emulator when finished.

Internet radio is a separate live source from library artist/genre mixes. Open
**Internet radio** in the web sidebar or Android Library drawer. Search station
names, choose Play, and use Stop or Retry to reconnect at the live edge. Station
websites open explicitly outside the player. Stations do not enter the music
queue or record library progress, ratings, lyrics or listening history.

## Interaction layout and states

Web uses the existing themed modal and playback owner. Its top row has the title,
Close, search, Refresh and (for administrators) Add station. Each station shows
optional artwork, a wrapping name, readable status and labeled Play/Stop/Retry,
Station website, and admin Edit/Remove actions. j/k and arrows select stations;
Enter activates the focused action, p plays the selected station, / searches,
and Escape follows the existing dialog/back hierarchy. Native typing and button
Space activation retain their usual behavior. Dialog close returns focus through
the existing modal-focus manager. Remove confirms its shared effect.

Android uses a native screen with a search field, Refresh/Add app-bar actions,
artwork cards, labeled transport and website buttons and a labeled management
menu. The edit dialog scrolls on narrow screens and uses native text/URL fields
and a local-network checkbox. Back dismisses the editor before the station screen;
an established station keeps playing through the existing background audio
handler. Returning to Internet radio attaches to the existing live source.
An unfinished source load is canceled on Back. No second audio player is created.

Loading stations → list/empty/error+Retry. Search → matching rows/no matches.
Play → Buffering → Live. Network/decoder/end failure → disconnected+Retry;
offline → Offline, reconnect then Retry. Stop releases the live stream; Play
reconnects, without seeking through old buffered radio. Artwork failure falls
back to the station name/icon. A viewer can browse and play but cannot mutate
shared stations. Long names wrap; artwork and actions shrink/reflow using the
existing theme colors in light, dark and narrow contexts. Live surfaces omit
finite duration, seek, speed and song-skip controls.

## API and outbound boundary

Authenticated `GET /api/radio` (also `/api/v1/radio`) lists shared stations.
Admin `POST /api/radio`, `PUT /api/radio/{station_id}` and
`DELETE /api/radio/{station_id}` create/update/delete. JSON fields are `name`,
`stream_url`, optional `homepage_url` and `artwork_url`, and `local_network`
(default false). IDs are generated by the server. GET list returns an array.
Validation rejects non-HTTP(S), embedded credentials and fragments; unsafe or
unreachable endpoints fail playback with a readable retry state.

Authenticated `GET /api/radio/{station_id}/stream` relays live MP3, transcoded by
a pipe-only ffmpeg subprocess. Browser clients therefore fetch same-origin
HTTPS and a common Chrome/Firefox codec regardless of the station's HTTP origin
or original codec. Stations must expose directly decodable live audio; manifests
with secondary network/file references are intentionally refused. There is no
CORS or mixed-content exception or direct credentialed station request.

Every actual dial and redirect is checked. Public unicast is the default;
administrators can explicitly opt a station into private/loopback access for
LAN stations and local verification fixtures. Link-local metadata, multicast and
reserved ranges stay blocked. HTTP proxies, userinfo, Player cookies/tokens and
station cookie jars are absent from outbound fetches. Decoder input receives
already-fetched bytes with only the pipe protocol permitted, so disguised
playlists cannot initiate their own network or file reads. Four live decoders
per server and a 30-second no-output watchdog bound stalled sources. ffmpeg must
be installed; missing decoder and capacity failures are explicit.

Authenticated `/artwork` fetches at most 4 MiB with a 15-second deadline, validates
image dimensions/pixel count, decodes and emits JPEG; active SVG/HTML and oversized
images are refused. Native system artwork uses the existing account-scoped
private file cache. Artwork is optional and never blocks audio.

## Implementation-stage verification (historical)

At the implementation freeze, root-owned review and integrated checks remained.
Cheap targeted tests cover URL/dial/redirect credentials, artwork decoding,
capacity, persistence/restart and admin permissions. Full suites, final builds,
actual Chrome/Firefox keyboard/pointer/audio/theme/narrow journeys and Android
emulator touch/Back/system-controls/audio journeys are deliberately deferred to
the integrated phase. None of those platform journeys is claimed verified.
Required local scenario: administrator create/edit/remove a local-network stream
with art; viewer search/play; verify one audio source, no seek/duration, live
status, disconnect/reconnect and Retry, unsafe/unreachable URLs, logout ownership,
restart persistence, focus and platform controls. Retain versioned evidence
outside the repository. Feature remains pending until all required checks pass.

Focused implementation checks (2026-10-23, shared worktree, no final build):

- `cd player-server && go test ./internal/service ./internal/repository ./internal/api -run 'TestRadio' -count=1` — PASS; service 0.311 s, repository 0.364 s, API 0.021 s. This includes actual FFmpeg WAV-to-MP3 output and a valid HLS manifest refused without contacting its secondary listener.
- `cd player-android && flutter test --no-pub test/services/radio_handler_test.dart test/services/headset_handler_test.dart` — 17 PASS. Live held-load unplug/manual-pause/sleep keep paused controls; explicit Play reconnects; live duration/seek/skip omitted; ordinary controls restore.
- Scoped `flutter analyze --no-pub` of radio screen, handler/request, owner/art/personal providers, notification/router/home composition and radio test — no issues (11 items, 1.4 s). An initial missing optional URL-launch dependency was replaced by the existing native metadata-link launcher.
- `node web/js/tests/radio-media-session.test.js` and `node web/js/tests/media-session.test.js` — PASS. Live actions exclude seek/skip/finite position; normal-source controls restore.

These focused checks do not establish Chrome, Firefox or native Android feature acceptance. Core web route/playback integration is implemented after reviewed M59 commit `c26640b`.


Final focused implementation freeze (2026-10-23):

- Native radio/headset/API tests: **20 PASS**, including held-load unplug/pause/sleep, enabled reconnect from a stopped decoder, explicit live-edge resume, zero finite controls, captured-epoch stale response/mutation rejection and an editor Save after account change. Scoped analysis of 13 changed source/test files reports no issues (1.4 s).
- Go radio service/repository/API subset: **PASS**, 0.312/0.365/0.020 s; HEAD does not acquire a decoder, MP3 conversion succeeds, and manifests cannot contact secondary hosts.
- Production web playback-source regression: **PASS**, including the new live owner/source/timeline/Stop/Play/error/normal-restoration journey. Radio Media Session, ordinary Media Session, renderer protocol and Cast regressions all pass. These use DOM/protocol fakes and are not platform proof.
- Existing shared endpoint/account boundaries are reused. Native list rows and their dialogs retain the exact API client/credential epoch; stale dialogs cannot retarget station IDs on another server. Initial loads keep their original transport/headset/sleep intent. Pause releases live network bytes but publishes paused-ready system controls; explicit Stop clears the session. Headset reconnect reloads live only with an existing eligible ticket.
- Web station selection awaits truthful renderer Stop and refuses replacement if the speaker may still be audible; Google Cast teardown must finish before local audio starts. Library error/ended/seek/skip/personal actions cannot consume a live station. No library-media ID is fabricated.

At that freeze, root-owned work included review, API reference and service-worker asset integration, broad suites/builds, and actual installed Chrome/Firefox/native Android acceptance. No release, version bump, commit or acceptance was performed by that implementation worker. Errcheck and broad regression suites were not run in that focused implementation slot.

Pre-review live HTTP deadline repair: the relay clears the server's absolute
write deadline during bounded source preparation/idle reads, then gives each
actual write plus flush a fresh 10-second budget. Error responses receive the
same bounded write budget. The existing 30-second no-output watchdog still
cancels the station request/decoder; slow-client writes cannot retain a decoder
indefinitely. `TestRadioRelayRenewsHTTPWriteBudget` uses a real HTTP server with
a 30 ms WriteTimeout, an 80 ms station startup, and later 70 ms-separated chunks.
It fails with EOF in 0.288 s when deadline renewal is removed and passes with the
fix; the affected `go test ./internal/service -run '^TestRadio' -count=1` subset
passes in 0.794 s. No native/web code or broad suite was changed/run for this fix.

Round 1 found one outbound policy gap: opted-in private access also allowed
AWS IPv6 metadata `fd00:ec2::254`. The shared dial policy now normalizes mapped
and scoped addresses, denies IPv4/IPv6 metadata unconditionally, then applies
ordinary public or opted-in local allowances. Twenty-eight pure policy cases
cover these boundaries without contacting metadata. The affected Go policy,
URL/redirect, artwork and capacity checks pass; formatting and whitespace checks
pass. Round 2 confirmed the fix with no new blocker. Full regression/build and
actual Chrome/Firefox/native/codec verification remain deferred.
