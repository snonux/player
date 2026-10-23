# Music A01a — Subsonic/OpenSubsonic auth + library browse/search (`el3`)

## Scope

Optional interoperability slice: versioned protocol/auth/error contract and
authenticated music-folder/artist/album/song browse/search with OpenSubsonic
extensions. Reuses Player accounts/ACLs and `MusicAlbums` / `MusicArtists` /
`SearchMusic`. Player custom `/api` and `/api/v1` routes stay intact. Streaming,
downloads, playlists, stars, queue, and lyrics remain later A01 slices.

## UI design (setup / status / errors)

No new music browsing UI. External clients connect to Player; instructions and
status live on existing surfaces:

| Surface | States |
| --- | --- |
| Admin › Sign-in help | Subsonic on: endpoint `/rest`, API version, apiKey/password note, revoke via API Tokens. Off: `SUBSONIC_ENABLED=true` hint. |
| Android Settings › API Tokens | Always-visible **Subsonic clients** note (endpoint path, prefer apiKey, revoke here). Never shows credentials. |
| `GET /api/config` → `subsonic` | `enabled`, `base_path`, `api_version`, `open_subsonic`, extensions/auth when enabled. |
| Subsonic error responses | Protocol `failed` + code (40 bad creds, 41 MD5 token unsupported, 70 not found, 0 disabled/unimplemented). |

Light/dark/narrow: reuses existing admin help text and Material note banner.
Keyboard/vi shortcuts unchanged. Full Chrome/Firefox/DSub/Tempo journeys deferred
to integrated verification.

## Enablement

`SUBSONIC_ENABLED=true` (default off). Routes under `/rest/` are always
registered; when disabled every action returns a Subsonic `failed` envelope.

## Auth

| Method | Support |
| --- | --- |
| `u` + `p` (plain or `enc:` hex) | Yes — Player username/password (bcrypt) |
| `apiKey` | Yes — Player API token (OpenSubsonic extension) |
| `u` + `p` where `p` is an API token | Yes — OpenSubsonic “API key as password” |
| `u` + `t` + `s` (MD5) | No — error **41** (bcrypt-only storage; documented gap) |

Required `c` and `v` parameters. Never log passwords, tokens, or apiKeys.

## Browse / search actions

`ping`, `getLicense`, `getOpenSubsonicExtensions` (public), `getMusicFolders`,
`getIndexes`, `getArtists`, `getArtist`, `getAlbum`, `getSong`, `search2`,
`search3`. Music folders are ACL-visible non-podcast sets. Artist/album/song
identities reuse catalog keys / media ids. Empty `search3` query returns a
paged catalog slice (OpenSubsonic offline sync).

## Layout

| Path | Role |
| --- | --- |
| `internal/subsonic` | Envelope, errors, credential parsing, extensions |
| `internal/service/subsonic.go` | ACL catalog adapter |
| `internal/api/handlers_subsonic.go` | `/rest/{action}` |
| Config `subsonic` + admin/Android notes | Enable/status/instructions |

## Verification (implementation slice)

Targeted Go tests: credential parsing, browse/search ACL filtering, password and
apiKey success, bad credentials (40), MD5 token (41), disabled server, public
extensions, config status. Full DSub2000/Tempo on emulator and Chrome/Firefox
Player regression deferred — honest gap until integrated verification.
