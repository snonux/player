# M53 / qj3 — paired server addresses

Implementation stage: expert review round 1 reported no blockers; awaiting integrated verification.
No full suites, server/APK builds, browser or emulator acceptance ran for this
slice, following the implementation-first workflow. Prerequisite nj3 supplies
U01 design and oj3 supplies the existing fixture/acceptance baseline.

## Platform scope and interaction

U01 §11 makes paired addresses and SSID routing an Android profile feature.
Chrome and Firefox retain their page origin, existing session cookies, keyboard
navigation, playback and connection-error behavior. The public identity API is
available at both web and v1 prefixes; browsers do not silently transfer their
cookies or account to another origin. Actual shared behavior in both browsers
still needs integrated verification.

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
Connection controls have native accessible names and touch targets. Light,
dark, narrow, text-scale and screen-reader results remain to be checked on the
actual emulator; these are implementation decisions, not visual proof.

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
- Scoped Dart formatting and Go formatting performed. Full Go static-analysis
  and regression checks are deferred with the integrated verification phase.

## Integrated checks still required

Build the final server and APK; run full suites/static analysis and real Chrome,
Firefox and native Android journeys using the existing disposable fixtures.
Use two addresses for one database, stop the preferred listener, confirm media
bytes/audio and account identity survive fallback, and inspect saved progress,
queue and offline keys. Verify wrong-server pairing and redirect destinations
receive no account credentials, 401/403 never switch, writes are not duplicated,
restart retains pairing while saved login still requires unlock, both addresses
unreachable gives retryable failure, and disposal/account switches stop old
requests. Exercise Wi-Fi name grant/deny/precise-vs-approximate/Location-off and
network transitions on Android, native Back/touch/accessible labels, and narrow
light/dark layouts. Browser keyboard and same-origin playback behavior remain
required shared-platform checks. No platform acceptance is claimed here.
