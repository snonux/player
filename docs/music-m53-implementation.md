# M53 / qj3 — paired server addresses

Implementation and expert review round 1 are complete; no review blockers or
second round were required. Integrated acceptance ran on 2026-10-24, as recorded
below. Prerequisite nj3 supplies U01 design and oj3 supplies the existing
fixture/acceptance baseline.

## Platform scope and interaction

U01 §11 makes paired addresses and SSID routing an Android profile feature.
Chrome and Firefox retain their page origin, existing session cookies, keyboard
navigation, playback and connection-error behavior. The public identity API is
available at both web and v1 prefixes; browsers do not silently transfer their
cookies or account to another origin. Actual shared behavior in both browsers
is covered by the integrated checks below.

Android Settings → Servers → **Advanced connection settings** is collapsed by
default. It uses existing Material theme typography, ExpansionTile, outlined
text fields, labeled switches/checkboxes and a full-width primary button. The
column scrolls with Settings, uses available width without fixed dimensions,
and inherits light/dark colors and text scaling. Native Back remains Settings
navigation; no new gesture or keyboard requirement is introduced.

```
Servers
  Advanced connection settings                              ▾
    Optional local address · on this device
    Primary address: https://player.example
    [ Local server address (optional)                       ]
    Prefer the local address                           [on]
    [ Only on Wi-Fi named (optional)                        ]
    [ Use current Wi-Fi name ]
    Reading Wi-Fi names requires precise location…
    [✓] I trust both addresses for this account
        HTTP sends login details and media without encryption…
    [ Test and save addresses                               ]
    Connected using http://192.168.1.2:8080
```

The primary address/profile is the existing server choice. Local address is
optional. Test and save probes both addresses without credentials, requires
matching installation IDs and explicit trust, and leaves existing settings
intact on failure. Blank local address removes the pair. The button disables
while testing and displays a spinner and live status. Invalid addresses,
different installation, unreachable endpoints, permission denial and unknown
Wi-Fi names have human-readable outcomes. Long addresses wrap in status text.
Connection controls have native accessible names and touch targets. Narrow
light/dark results were reviewed on the actual emulator during integrated
acceptance. Operated screen-reader and enlarged text-scale checks are not
claimed.

Prefer-local chooses the local address first, then the primary when unreachable.
Switching it off prefers the primary and permits local fallback. An exact Wi-Fi
name restricts local routing to that network; unknown/denied/unmatched SSID
uses only the primary. Blank name permits local routing on any network. Use
current Wi-Fi name is the only action requesting location permission. Android
also requires Location enabled; unavailable names produce an explanation.
SSID is a routing hint, never an identity or authorization signal.

## Identity, transport and security

A database-persistent 256-bit public server ID is exposed by
`GET /api/server-identity` and `/api/v1/server-identity`, without cookies or
credentials and with no-store. The normal store generates it once and retains
it across restarts. Copies/backups of a database retain the same ID. Public IDs
catch accidental pairing mistakes; they do not authenticate a hostile endpoint
that copies the ID. Explicit origin trust and TLS are still required. HTTP's
lack of encryption is explained before pairing.

The primary origin stays the canonical account, credential-storage, progress,
offline-download, cache and queue identity. Paired settings live under that
profile; they do not add a separate saved server or unlock an account. Existing
cold-start saved-login locking is unchanged. Same-profile setting edits update
the router without destroying a playing queue; changing profiles closes the
old native gateway. In-flight selections reject disposed profiles or restart
selection after pairing changes.

Dio authentication sees canonical URLs throughout the existing generation and
cookie guards. A transport adapter resolves only explicitly paired origins,
probes without credentials, then sends to the verified origin with redirects
disabled. Read requests can retry the other verified address once after a
transport failure. Writes and request bodies are never replayed. Authentication
and HTTP errors are returned without endpoint switching. Different IDs block
routing rather than falling through to another address. Selection is cached for
five seconds and invalidated by connectivity changes/failures/settings edits.
Direct artwork/download socket or timeout failures invalidate their cached route
for the next attempt; HTTP/authentication/status errors do not. The status keeps
its preferred-address failure explanation after selecting the alternate.

Native audio/video/image plugins follow redirects with caller headers, so
paired sources use an app-owned loopback streaming gateway with a random
256-bit path. It accepts only GET/HEAD for known media/cover routes on the
canonical origin, forwards Range and relevant response metadata, and never
forwards redirects or Location to native clients. Credentials remain request
headers and are never embedded in source URLs. It probes/selects the paired
upstream before forwarding. If a cached upstream fails before responding, it
tries the verified alternate once; response/stream/auth failures are never
replayed. Profile disposal closes gateway connections. Artwork/offline file
transfers use verified paired origins directly and already disable redirects.
Offline files retain canonical keys; the downloader accepts only its explicitly
resolved transport origin in addition to the canonical origin.

## Cheap checks performed

- `flutter analyze --no-pub` over the 17 changed/new Dart source/test paths:
  no issues. Final query/image-invalidation and review nits were checked with
  scoped analysis over the five affected router/provider/test paths, also clean.
- `flutter test --no-pub test/services/server_endpoint_router_test.dart`:
  10 passed. Includes preferred failure, different-ID blocking, exact/unknown
  SSID policy, canonical Dio identity/redirect disabling, no write replay,
  one read retry, query parameters forwarded exactly once without modifying the
  canonical request, actual loopback Range bytes, redirect rejection with no sink
  credential/request leakage, forged cross-origin gateway route rejection,
  profile disposal, cached native-address failure fallback with explanatory
  status, and saved profile
  persistence/canonical separation.
- `go test ./internal/repository -run '^TestServerIdentityPersistsAndSeparatesDatabases$' -count=1`:
  passed.
- `go test ./internal/api -run '^TestServerIdentityPublicAliases$' -count=1`:
  passed, both aliases work without authentication and are no-store.
- Scoped Dart formatting and Go formatting performed. At implementation time,
  full Go static-analysis and regression checks were deferred to the integrated
  verification phase; final gate reuse is recorded below.

## Integrated acceptance — 2026-10-24

Evidence is outside git at `/home/paul/tmp/qj3-evidence`. The unchanged product
reuses the integrated passing Go race/coverage and web checks, clean Flutter
analysis and 1676 passing Flutter tests. The normal installed Android release
is 0.8.2/versionCode4022 from `c2650c1`, running on API34 x86_64; the disposable
18771 server reports `c7b6aea-dirty`. Its web assets reflect the current checkout.
No product fix, release/version change, extra review round or rebuild was needed.

Chrome 144.0.7559.132 and Firefox 156 both passed
`music-endpoints.test.ts`. Each uses the fixture viewer, keyboard login, actual
Three Tones playback and captured output, p/p playback, `/` focus and text
entry/Escape, actual browser-offline search failure and Retry recovery, and
narrow light/dark screenshots. Account identity and API origins remain the page
origin. Opening the separately hosted same-server address on `localhost` shows
login rather than transferring the `127.0.0.1` account. Distinct hostnames are
intentional: cookies are not isolated by TCP port. Browser pairing/SSID controls
are outside U01's supported browser scope.

From `player-server/test/e2e-web`, the final command was:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/player-quality-fixture \
PLAYER_M53_ALTERNATE=http://localhost:18773 \
PLAYER_M53_EVIDENCE=/home/paul/tmp/qj3-evidence \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
npx playwright test --config playwright.music.config.ts \
  tests/music-endpoints.test.ts --workers=1
```

Result: both projects passed, 21.3 seconds. `m53-chrome/result.json` and
`m53-firefox/result.json` retain exact browser/server versions, sanitized request
origins, account IDs and audio analysis.

Native acceptance used actual touch/Back and the existing `adrv`, `app_ui`,
`music_loudness_e2e.Run` visibility/touch helpers and `music_e2e.EmulatorAudio`,
without clearing data, installing an APK or writing preferences. The owned
transparent proxy source is retained as `proxy.py` in the evidence directory;
it proxies aliases to the one 18771 backend, records only paths/methods/status,
byte counts and credential-presence booleans, and preserves upstream HEAD
Content-Length. Primary device port18771 temporarily mapped to the recording
alias18776; preferred local port18773 mapped to alias18773. Ports18774/18775
were a wrong-ID endpoint and redirect sink. No second server shared its DB.

The native results were:

- Test/save probes carry no Authorization or Cookie. The optional trust control
  and matching installation IDs allow saving; a wrong ID is rejected without
  sending credentials, leaving the previous saved pair intact.
- Healthy local Afterglow produces 3.11 seconds at 698.5 Hz. Closing preferred
  connections forces fresh Café Ñoño API/media requests through the verified
  primary, with 3.61 seconds at 349.2 Hz and settled paused position 3.618 seconds.
  The status explains the preferred-address failure. Sources are distinct, so
  buffered playback cannot satisfy fallback.
- API403 and API302 produce visible error/Retry and do not retry the same
  request on the primary. API401 returns to sign-in. One failed favourite POST
  is observed once on the preferred address, is not forwarded upstream or
  replayed on the primary, and shows a failure message.
- A separate media-only redirect mode returns actual302 responses for fresh
  Neon Rain `/media/38/stream` requests. The native player displays its error,
  Quality and Retry; the redirect sink receives zero requests. Restoring the
  endpoint and touching Retry yields the actual200 source and 2.73 seconds of
  394.8 Hz output. Earlier API-resolution-only redirect attempts are retained
  separately and are not used as gateway proof.
- Actual Android permission dialogs cover deny, approximate grant and upgrade
  to precise. Deny/approximate explain unavailable Wi-Fi names; precise reads
  `AndroidWifi`. Saving that exact name routes fresh catalog requests locally.
  Location disabled and a saved nonmatching `OtherWifi` each route fresh
  requests only through the primary. Location is restored afterwards.
- Cold recreation retains the paired profile and requires explicit server
  choice and account sign-in. The401 case had correctly invalidated saved
  login, so both username and password were supplied again. A first immediate
  launcher attempt stayed on the launcher; a subsequent explicit launch reached
  the chooser. This driver attempt is not presented as an app launch failure.
- Cache rows retain canonical18771 origin keys. A newly kept-offline Three
  Tones copy downloads through the preferred address and retains canonical
  origin/user/media identity in SQLite. With both adb endpoint mappings removed,
  Downloads plays 2.74 seconds of output with measured100/1000/8000Hz bands.
  The newly created copy is then removed through its confirmation dialog.
  A fresh online-library request with both endpoints unreachable shows Retry;
  restoring mappings and touching Retry returns the actual catalog.
- Native settings are scrollable and use labeled native touch controls, with
  narrow light/dark screenshots and ordinary Back navigation. No operated
  TalkBack, physical-device or physical-network roaming claim is made.

`requests.jsonl`, scenario time windows, PCM/WAV/analysis files, screenshots,
`persisted-pair.json`, `download-canonical.json`, `canonical-cache.json`,
`gateway-trace.json` and `restored-state.json` retain the underlying evidence.
The alias proxy was stopped after verification. The18771 pair and new offline
copy were removed through UI, permission grants revoked, Location enabled,
System theme restored, and the original18766 admin Library restored. Its exact
queue is entries3/1/2 with media3/3/5, index2, paused4.69seconds, repeatOff,
shufflefalse and canonical3/1/2. Only the original18766 adb reverse remains;
the shared server/emulator and original backup were not restarted or cleared.
