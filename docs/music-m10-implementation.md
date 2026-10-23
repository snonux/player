Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One bounded expert review found no blockers; actual configured-agent/platform acceptance remains pending.

# M10 artist/album enrichment — implementation stage

Prerequisites nj3/oj3/yj3/zj3 are accepted. ak3 implements the M10 scope only.
Full suites/builds and installed Chrome, Firefox and native Android acceptance
are deferred to the user-authorized integrated verification phase. This feature
is not accepted yet. Versions remain 0.8.2/+22.

## Interaction design recorded before coding

Artist/album detail retains local artwork, name, credits, counts and prominent
Play / queue controls. Immediately after these, **About** is collapsed:

```
[local cover] Title / credits / counts
[Play] [Queue actions]
[About ▸]
  Provider image (separate from library cover)
  Biography / description (plain text, wrapped paragraphs)
  External metadata links ↗
  Source · Updated time · May be out of date
  [Refresh metadata] (administrators only)
[songs / albums]
```

The web uses a native button with aria-expanded, Tab/Enter activation and normal
pointer/touch activation, preserving all existing vi playback/navigation keys.
Opening About does not move selection. Refresh preserves its button DOM/focus.
Android uses ExpansionTile, labeled TextButtons, selectable biography and a
browser ACTION_VIEW intent; system Back returns from the browser normally.
External images receive no Player authentication headers. Text uses existing
light/dark typography/tokens; bounded width, wrapping and natural scrolling
handle long content on narrow screens. New controls do not replace artwork or
change playback. Actual light/dark/narrow and keyboard/touch visual assessment
remains part of integrated verification.

States: initial cached-data loading; provider disabled (About hidden); unfetched
(admin refresh prompt, viewer explanation); no match; ready with attribution and
timestamp; age over 30 days marked stale; refresh failure retains last good data
and local art with explanation. Connection failure retains already read metadata
and Retry; HTTP permission denial clears the visible metadata. Refresh is an
explicit admin action because metadata is shared across users. Primary content
and playback remain available independently of provider failures.

## Configuration and external-agent contract

`MUSIC_ENRICHMENT_URL` is optional, disabled by default. Set it to an operator
controlled compatible HTTP(S) JSON endpoint, for example
`http://127.0.0.1:18910/metadata`. Restart the server after configuration changes.
This is the Player JSON external-agent contract; it does not claim direct
compatibility with arbitrary Last.fm or MusicBrainz endpoints. A configured
agent can use such sources and supply attributed links. No plugin framework,
API key manager, artwork upload or tag editor is introduced.

Player sends GET query parameters `kind` (`artist`/`album`), `key` (existing
opaque catalog key), `name`, and for albums available `artist` and verified
embedded `release_mbid`. It sends no Player cookie or bearer token. Operators
are responsible for selecting their provider and its metadata matching/license.
No source is guessed, generated or fabricated by Player.

The agent returns HTTP 204 for no match or HTTP 200 with JSON:

```json
{"description":"Plain text biography or album description", "source":"Provider name", "image_url":"https://example.org/artist.jpg", "links":[{"label":"MusicBrainz", "url":"https://musicbrainz.org/artist/…"}]}
```

Fields are optional except source is required when descriptive content, an image
or links are supplied. Plain text is rendered literally. Limits: 128 KiB JSON,
64,000 bytes description, 200 bytes source/link label, 12 links, 4096-byte URLs.
Only absolute HTTP(S) URLs without user information are accepted. Player does
not follow agent redirects; requests time out after 10 seconds. Returned links
and images are opened by the clients; broken images explain their unavailable
state without replacing local art. Fetched timestamps are server-owned UTC.

The shared SQLite cache uses kind/key plus a SHA-256 endpoint namespace and
survives restart. Changing the configured endpoint selects a fresh namespace.
Every read/refresh first checks the caller's current catalog access. GET only
reads cache; explicit admin POST performs the external request. Successful
refresh replaces the enrichment, including a valid no-match result. Failed
refresh does not overwrite the last successful lookup. Failure messages are
quiet responses for About and do not expose provider URLs/credentials. Disabling
the provider hides old cached content. Local media tags/covers are never changed.

## Deterministic local provider

For integrated acceptance start the disposable agent with
`python3 player-server/test/enrichment/provider.py --port 18910 --mode-file /tmp/player-enrichment-mode`,
set `MUSIC_ENRICHMENT_URL=http://127.0.0.1:18910/metadata` on the disposable Player
server and open a permitted artist/album as admin. A file containing `ready`,
`empty`, or `failure` chooses success/no-match/503. Refresh each state, check
biography/source/time and retained local art, viewer no-refresh permission,
revoked access, network failure/Retry, restart cache, endpoint change and disabled
provider in installed Chrome, Firefox and the actual native Android app. The
fixture description explicitly identifies itself as test metadata. Its PNG is
local and deterministic; use `adb reverse tcp:18910 tcp:18910` for the emulator
or set `--public-url` to a device-reachable address for provider images. Live provider
matching is not covered by fixture tests.

## Checks and remaining verification

Focused implementation checks passed:

- `go test ./internal/service ./internal/repository ./internal/api -run 'TestEnrichment|TestMusicEnrichment' -count=1` (all pass). Real HTTP-agent validation/refresh/failure retention; SQLite restart and endpoint/kind/key isolation; real catalog ACL/admin restrictions/no fetch on GET/revoked access.
- `go vet ./internal/service ./internal/repository ./internal/api` (pass).
- `flutter analyze --no-pub lib/widgets/music_enrichment_section.dart lib/services/external_metadata_links.dart lib/screens/music_albums_screen.dart lib/screens/music_artists_screen.dart lib/api/player_api_client.dart test/widgets/music_enrichment_section_test.dart` (no issues).
- `flutter test --no-pub test/widgets/music_enrichment_section_test.dart` (2 pass).
- `node --experimental-default-type=module player-server/web/js/tests/music-enrichment.test.js` (pass: actual About/refresh listeners, literal text, retained data/error and disabled provider).
- Python fixture compilation, JavaScript syntax and `git diff --check` (pass).

Unit/widget fixtures are implementation checks, not proof of any browser or
emulator feature journey. Full errcheck baseline comparison is deferred.
Kotlin browser-intent compilation, actual browser launch/Back, real image rendering,
all platform acceptance, final builds, full regression and bounded fresh-context
review remain required. No release/push/deployment is authorized.
