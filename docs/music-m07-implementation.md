# M07 metadata-aware search

Pre-code contract approved by root on 2026-10-22 for batch 9k3 + uj3.

Web `/` retains the existing search field. Plain text and `artist:`/`album:`
queries show Songs, Albums, Artists and compatible Files groups. Existing
file-only modifiers retain ordinary Sets filtering. Songs show title, track
artist, album and duration; album and artist rows open existing details. Files
show filename and path. Existing theme tokens, rows, focus and action menus
apply at desktop and narrow widths in light and dark themes.

Opening search captures the previous view, filters, selection and scroll.
Typing debounces without moving focus; Enter flushes the query and focuses the
first result; Escape cancels pending requests and restores the previous view.
Opening an entity and returning preserves the query and selected row. Native
text entry and composition own keys. j/k/arrows navigate all result groups;
Enter opens entities or plays a song. Existing playback shortcuts remain.
Song playback snapshots only the displayed Songs group; Files keep ordinary
file playback and resume, and must never enter that song queue. Browsing or
searching leaves an already-playing queue alone.

Android Music AppBars expose Search music. The new route has a focused search
field, Clear, All/Songs/Albums/Artists chips, grouped rows, mini player and queue
access. Song taps play the result-song queue; entity taps open existing routes.
Back dismisses the keyboard normally, returns from details to retained search,
then returns to the originating screen. Labels and 48 dp touch targets apply.
There is no playlist search, new facet system or search history.

`GET /api/v1/music/search` and `/api/music/search` return existing Media songs
and album/artist summaries in `{songs:[],albums:[],artists:[]}`. q is a literal
Unicode lowercase substring over title, track/album artists and album. artist
and album are AND constraints; real structured artist names remain intact.
Artist entities include track/album artists, not composer-only credits. Album
results match title/artist context; artist results match names and constrained
release membership. Existing ACL-filtered catalog derivation and exclusions
precede matching. Existing IDs, keys and deterministic catalog/disc/track order
remain. Empty query/constraints returns empty groups. No minimum, silent cap,
truncation, schema/index or new framework is introduced. Accents remain significant.

Empty input prompts Search songs, albums and artists. Loading is visible without
stealing typing focus. No results shows Nothing matches these filters and Clear
filters. Connection failures show Could not search music with Retry retaining
query; stale responses never replace current state. Fresh requests reflect ACL
changes; unavailable entity details use existing messages. Same-database server
restart retains IDs and search results. Web search URLs restore the query on
refresh. Offline catalog search remains M49; this route requires the server.

Acceptance plan: focused Go matching/API ACL tests, JS parser/state/debounce/URL
and playback dispatch checks, native loading/empty/error/race/navigation widgets;
then root-owned actual installed Chrome, Firefox and native emulator journeys
for title/artist/album/Unicode/no results, entities and actual source/audio,
keyboard typing/focus or touch/Back, light/dark/narrow/long names, queue isolation,
permission changes, real offline Retry and same-database restart. Extend current
component-local synthetic fixture and runners, preserve old fixtures/evidence.
Root owns combined reviews, full gates/builds, live runtime, commit and closures.

## Implementation and verification

Implemented ACL-derived search without schema/index changes, grouped web search
with existing file syntax and separate queue/file playback ownership, restorable
search URLs, and a native Music search route using existing entity/player routes.
The service worker precaches the new module under kiss-v39. Focused service/API,
parser/state/IME/URL/playback and Flutter navigation/error/race tests passed.
Both expert review rounds are complete; the second confirmed the blocking fixes.

Combined batch gates passed: Go build/vet and full race/coverage suite with
required FFmpeg, all web unit tests, Flutter analysis and 1,424 Flutter tests,
native Python oracle tests, browser filter-oracle tests and 36 fixture-support
tests. Errcheck retains 256 existing findings, with zero new findings; it is not
a clean errcheck run. The full web suite was rerun after the actual tag-focus and
Firefox runner fixes. No native product change followed the final APK build.

Actual acceptance used one disposable 49-file fixture at
`/home/paul/tmp/9k3-uj3-fixture`, one local server at port 18815, and one reused
Android emulator. All M07 platform scenarios passed:

| Platform | Accepted evidence under `/home/paul/tmp/9k3-uj3-evidence/` |
|---|---|
| Google Chrome 144.0.7559.132 | `browser-attempt2-chrome`: core entities, source/audio, Unicode/query/state, offline/ACL/restart; `browser-attempt4-chrome-touch`: trusted touch |
| Mozilla Firefox 156.0 | `browser-attempt5-firefox`: core entities, source/audio, Unicode/query/state, offline/ACL/restart; `browser-attempt6-firefox-touch`: trusted touch |
| Android 14 / API 34, Player_FDroid_Test_API34 | `native-attempt1-m07`: offline/ACL/restart; `native-attempt4-m07-journey`: complete touch/Back/entities/source-audio/Unicode/theme journey |

Root inspected browser light 390/dark 1366 screenshots and native long-title
light/dark and Unicode screenshots: labels wrap, selection/focus remain visible,
and native controls remain usable. Source captures identify the selected fixture
tone, rather than merely checking that a player appears. Native Unicode uses
actual metadata Select all/Copy, Paste and cursor/delete input, then asserts the
exact editable query and both matching songs.

Retained earlier failures led to actual Sets Clear routing, visible Files open
control and shared tag-dialog focus fixes. Runner corrections covered a held
request race, already-minimized player state, empty/loading oracles, and query
fields being mistaken for native result rows. Native selectors now exclude
editable fields and reject ambiguous clickable results. Firefox's installed
Playwright BiDi touch method is empty; the runner uses genuine standard touch
pointer actions through its existing owned BiDi session, preserving actionability,
trusted-event and actual source/menu assertions. Chrome retains normal taps.
No vendor patch, synthetic DOM input or relaxed autoplay policy is used.

## Artifacts and commands

Server and app stay at 0.8.2, Android +22 (x86_64 APK code 4022). Builds used
baseline `89c8ad6123285ed8ccaaa7870e557d5c7f41e311` plus this batch's changes.
External artifacts are `player-final` and `app-x86_64-release.apk` under the
evidence root. The installed APK hash was checked against the final file in
`native-install.json`; `artifacts.json` records both build hashes. The server
serves the repository web directory at runtime; web fixes were tested from that
directory. The later normal server rebuild produced identical backend bytes.

Full gate commands, logs and results are in `run-full-gates.py` and
`full-gates-results.json`; final commands/results are in `build-results.json`:

```sh
# player-server/
go build ./...
go vet ./...
GOMAXPROCS=2 PLAYER_REQUIRE_FFMPEG=1 go test -race -cover -count=1 ./...
mage webTest
go build -o /home/paul/tmp/9k3-uj3-evidence/player-final ./cmd/player
# player-android/
flutter analyze --no-pub
flutter test --no-pub --concurrency=1 --reporter expanded
flutter build apk --release --target-platform android-x64 --split-per-abi
# player-server/test/e2e-web/, with PLAYER_MUSIC_FIXTURE set to the fixture
npx playwright test -c playwright.music.config.ts tests/music-search.test.ts tests/music-filters.test.ts --project=chrome --headed --workers=1 --retries=0
npx playwright test -c playwright.music.config.ts tests/music-search.test.ts tests/music-filters.test.ts --project=firefox --headed --workers=1 --retries=0
# Affected browser continuations use --grep; each attempt's command.json is exact.
# player-android/
python3 test/e2e-live/music_search_e2e.py /home/paul/tmp/9k3-uj3-fixture --emulator-wav /home/paul/tmp/9k3-uj3-evidence/emulator-output.wav
python3 test/e2e-live/music_search_e2e.py /home/paul/tmp/9k3-uj3-fixture --emulator-wav /home/paul/tmp/9k3-uj3-evidence/emulator-output.wav --scenario journey
```

Each attempt retains command/version records, evidence JSONL, failures and
screenshots/audio externally. Both batch features are accepted on all three platforms. No release/version
bump/push/deployment is authorized.
