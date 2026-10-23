API Reference
=============

This document is the authoritative contract for the Player HTTP API. It covers
every route registered in `internal/api/server.go`, including request/response
schemas, status codes, and curl examples. An Android developer can read this
document alone, mint a Bearer token, and begin implementing
`player-android/lib/api/player_api_client.dart` against `/api/v1/`.

---

## Table of Contents

1. [Authentication](#authentication)
2. [API Versioning](#api-versioning)
3. [Error Envelope](#error-envelope)
4. [Token Lifecycle](#token-lifecycle)
5. [Public Endpoints](#public-endpoints)
6. [Auth Endpoints](#auth-endpoints)
7. [Configuration](#configuration)
8. [Sets](#sets)
9. [Media](#media)
10. [Notes](#notes)
11. [Bookmarks](#bookmarks)
12. [Progress](#progress)
13. [Shares](#shares)
14. [Tags](#tags)
15. [Admin](#admin)
16. [Podcasts](#podcasts)

---

## Authentication

Every session-required endpoint accepts credentials via **one** of the
mechanisms below, checked in this order:

### 1. Bearer Token (recommended for API clients / mobile apps)

Add an `Authorization` header carrying the token returned by
`POST /api/v1/auth/tokens`:

```
Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx
```

Bearer tokens are long-lived (or non-expiring) and survive server restarts.
They are the correct choice for Android/Flutter clients.

```bash
# Step 1: log in with username/password to get a session cookie
curl -s -c cookies.txt -X POST https://player.example.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "alice", "password": "secret"}'

# Step 2: mint a Bearer token using that session cookie
curl -s -c cookies.txt -b cookies.txt \
  -X POST https://player.example.com/api/v1/auth/tokens \
  -H "Content-Type: application/json" \
  -d '{"name": "android-client", "expires_in_days": 365}'
```

Response:

```json
{
  "id": 7,
  "name": "android-client",
  "token": "pt_xxxxxxxxxxxxxxxxxxxx"
}
```

Store that `token` value. It is the **only time** the plaintext value is
returned — subsequent `GET /api/v1/auth/tokens` responses omit it.

### 2. Session Cookie (browser / web SPA)

A `session=<value>` `HttpOnly` cookie set by `POST /api/login` or
`POST /api/bootstrap`. The cookie is valid for the number of hours configured
via `SESSION_TIMEOUT_HOURS` (default 24 h).

```bash
# Log in and save the session cookie
curl -s -c cookies.txt -X POST https://player.example.com/api/login \
  -H "Content-Type: application/json" \
  -d '{"username": "alice", "password": "secret"}'

# Use the saved cookie on subsequent requests
curl -s -b cookies.txt https://player.example.com/api/v1/media
```

### 3. Trusted reverse-proxy header (optional)

Disabled by default. When `PROXY_AUTH_TRUSTED_SOURCES` is set, requests whose
**immediate peer** (`RemoteAddr`) matches a configured CIDR (or `@` for a
UNIX socket listener) may authenticate with the username in
`PROXY_AUTH_USER_HEADER` (default `Remote-User`).

**Account mapping:** the header value must match an existing Player
`users.username` exactly. Accounts are **not** auto-created — create matching
users in the admin panel (or bootstrap) before enabling the proxy.

**Security:** untrusted clients must not be able to set the user header.
Configure the reverse proxy to strip client-supplied copies of that header and
only set it after successful upstream authentication. Player ignores the
header unless the peer is in `PROXY_AUTH_TRUSTED_SOURCES`; it never trusts
`X-Forwarded-For` for this check. Bearer tokens and session cookies continue
to work (and take precedence when present).

```bash
# Example: trusted proxy at 10.0.0.2 injects Remote-User=alice
curl -s http://player.example.com/api/v1/media \
  -H "Remote-User: alice" \
  # (request must arrive from an address in PROXY_AUTH_TRUSTED_SOURCES)
```

Public `GET /api/v1/auth/status` reports whether proxy auth is enabled (for
the login page note) without exposing trusted CIDRs. Authenticated
`GET /api/v1/config` includes the same `auth` object for the admin panel.

Optional `PROXY_AUTH_LOGOUT_URL` makes `POST /api/logout` return
`{"logout_url":"..."}` (instead of `204`) so the web UI can finish logout at
the identity provider.

Android and other API clients should keep using Bearer tokens; they do not
need reverse-proxy headers.

### Auth precedence in middleware

`RequireSession` (in `internal/api/middleware.go`) tries Bearer first, then a
valid session cookie, then (when configured) a trusted reverse-proxy user
header. If none succeed it returns `401 Unauthorized`. HTML-page requests
(browsers sending `Accept: text/html`) are redirected to `/login.html`
instead.

Endpoints that also require admin status apply `RequireAdmin` on top of
`RequireSession`.

### First-time setup — Bootstrap

Before any user exists the server redirects all requests to `/bootstrap.html`.
Call `POST /api/bootstrap` (or `POST /api/v1/auth/bootstrap`) to create the
first admin account:

```bash
curl -s -X POST https://player.example.com/api/v1/auth/bootstrap \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "changeme"}'
```

Subsequent calls return `403 Forbidden`.

---

## API Versioning

The server registers every session-required and admin route under **both** path
prefixes via the `handleBoth` helper in `server.go`:

| Prefix | Purpose |
|--------|---------|
| `/api/` | Legacy / web-app path; kept for backwards compatibility with the browser SPA |
| `/api/v1/` | Stable contract; use this in new API clients |

The two prefixes are **identical** in behaviour — they share the same handler.
Only the path changes. The `handleBoth` function panics at startup if a path
does not start with `/api/`, so all versioned paths are guaranteed to exist.

Public endpoints (`/api/login`, `/api/bootstrap`) have dedicated v1 aliases
under `/api/v1/auth/login` and `/api/v1/auth/bootstrap` respectively. The
health probes (`/healthz`, `/readyz`) and share viewer (`/s/{token}/*`) have
no v1 alias — they are not API-version-sensitive.

**Recommendation:** Android / Flutter clients should use `/api/v1/` for all
requests and include an `Authorization: Bearer <token>` header on every call.

---

## Error Envelope

All error responses (4xx and 5xx) use a consistent JSON envelope:

```json
{ "error": "<human-readable message>" }
```

Common status codes:

| Code | Meaning |
|------|---------|
| `400` | Bad request — missing or invalid field |
| `401` | Unauthorized — missing or invalid credentials |
| `403` | Forbidden — authenticated but insufficient permission |
| `404` | Not found — resource does not exist or is inaccessible |
| `405` | Method not allowed |
| `410` | Gone — share link has expired, or all its uses are spent |
| `413` | Request entity too large — upload exceeds `MAX_UPLOAD_SIZE_MB` |
| `500` | Internal server error |
| `501` | Not implemented — service dependency is unavailable |

---

## Token Lifecycle

API tokens are stored as bcrypt hashes in the `api_tokens` table.

### Minting

`POST /api/v1/auth/tokens` returns the plaintext token **once**. The server
stores only the hash. If you lose the plaintext, revoke the token and mint a
new one.

### Expiry enforcement

Every `AuthenticateBearer` call checks `expires_at` against the current time.
An expired token yields `401 Unauthorized`. Omit `expires_in_days` (or pass
`null`) to create a non-expiring token.

### last_used_at semantics

Each successful authentication via Bearer updates `last_used_at` on the token
row. The field is `null` if the token has never been used after creation. Use
`GET /api/v1/auth/tokens` to inspect it and audit unused tokens.

### Revocation

`DELETE /api/v1/auth/tokens/{id}` immediately removes the hash from the
database. Any subsequent request carrying that plaintext token returns `401`.
Only the owning user can revoke their own tokens.

---

## Public Endpoints

No credentials required.

---

### `POST /api/bootstrap` · `POST /api/v1/auth/bootstrap`

Create the first admin account. Returns `403` if users already exist.

**Request body:**

```json
{ "username": "admin", "password": "changeme" }
```

**Response `200`:**

```json
{ "id": 1, "username": "admin", "is_admin": true }
```

Sets a `session` cookie.

**Status codes:** `200`, `400`, `403`, `500`

---

### `POST /api/login` · `POST /api/v1/auth/login`

Authenticate with username and password.

**Request body:**

```json
{ "username": "alice", "password": "secret" }
```

**Response `200`:**

```json
{ "id": 3, "username": "alice", "is_admin": false }
```

Sets a `session` cookie valid for `SESSION_TIMEOUT_HOURS` hours.

**Status codes:** `200`, `400`, `401`, `500`

---

### `GET /healthz`

Liveness probe. Returns `200 OK` immediately; no database access.

---

### `GET /readyz`

Readiness probe. Pings the database. Returns `200 OK` or `503 Service
Unavailable`.

---

### Share viewings (how `max_uses` is counted)

A share's `max_uses` counts **viewings**, not HTTP requests. Players fetch one
file with many ranged requests; all requests of one viewing together cost one
use.

**What opens a viewing (one use, claimed atomically; `410` when none is
left)** — a request of a client that presents no valid viewing credential and
is one of:

| Request | Who sends it |
|---------|--------------|
| `POST /s/{token}/view` | the share page's script, before it loads any media |
| `GET /s/{token}` with `Accept: application/json` | apps and API clients |
| `GET /s/{token}/stream`, `/compat`, `/download` | links pasted straight to the media, clients that predate viewings |

**What never opens a viewing and never costs a use:**

- `GET /s/{token}` as HTML — the share page. Link previewers, mail scanners
  and prefetching browsers request exactly this, so a single-use link is not
  spent by being sent through a chat app. The page sets no cookie.
- `HEAD` on any share route.
- `GET /s/{token}/thumbnail`.
- any request that presents a valid viewing credential.

Without a valid credential these free requests are answered while the share
has uses left and `410` once it has none.

**The viewing credential** is bound to its share and valid for 6 hours, or
until the share's own `expires_at` if that comes first. The client gets it

- as a cookie on every response that opened a viewing:
  `share_view=<credential>; Path=/s/{token}; HttpOnly; SameSite=Lax; Max-Age=…`
  (plus `Secure` unless `SECURE_COOKIES=false`);
- in the JSON form of `GET /s/{token}` as `view` (with `view_expires_at`), and
  every URL in that JSON already carries it as the query parameter
  `?view=<credential>`.

**Requests that present a valid credential** — the `view` parameter or the
cookie; when both are sent the first valid one counts — never cost a use and
keep working after `max_uses` is reached, until the viewing expires. That
covers `/s/{token}` (a reload), `POST /s/{token}/view` (idempotent),
`/stream`, `/compat`, `/thumbnail` and `/download`.

**Anything that is not a valid credential** (none, expired, malformed, forged,
or issued for another share) is treated exactly like no credential. A client
that neither keeps cookies nor sends `view` therefore pays one use per media
`GET`, as before viewings existed.

**Revocation and expiry are immediate**: every request checks the share
itself first, so a revoked share answers `404` and an expired one `410` also
inside a viewing.

All share responses carry `Cache-Control: no-store` and
`Referrer-Policy: no-referrer`. A share route called with a method it does
not support answers `405` with an `Allow` header (`GET, HEAD`, or `POST` for
`/view`); any other path below `/s/` answers `404`.

The credential is `<expiry unix seconds>.<base64url HMAC-SHA256>` (54
characters from `[A-Za-z0-9._-]`; anything else is rejected), signed with a
key the server keeps in its database. Viewings therefore survive a server
restart; nothing is stored per viewing. Clients must treat it as opaque.

**Limits of this scheme — know them before relying on `max_uses`:**

- The credential is a **bearer token**. Whoever holds a `?view=` URL (or the
  cookie) can use the share until the viewing expires or the share is
  revoked or expires — a recipient can pass such a URL on, and it then works
  for others without costing a use. Do not log or forward URLs that contain
  `view`; note that a reverse proxy in front of the server records query
  strings in its access log unless told otherwise. `max_uses` limits how many
  viewings are opened, not what the holder of one does with it for 6 hours.
- **`HEAD` discloses metadata for free**: while uses remain, `HEAD` on
  `/stream` and `/download` answers with the file's size, type and (for
  `/download`) name without counting.
- **What can still spend a use unintentionally**: a scanner that executes the
  page's JavaScript (and so sends the `POST`), and any tool that requests the
  JSON form.
- **Cookies disabled or dropped** (for example `SECURE_COOKIES=true` behind
  plain HTTP): the page's media requests then arrive without a credential and
  each opens a viewing of its own.
- The cookie `Path` is `/s/{token}`: it assumes the server is mounted at the
  root of its origin. Behind a reverse proxy that serves it under a path
  prefix the browser does not send the cookie back, with the effect above.
- The signing key lives in the database (`server_secrets`). Anyone who can
  read the database can forge viewings for share tokens they know — which
  are in the same database.

---

### `GET /s/{token}` · `HEAD /s/{token}`

Renders the public share viewer page (HTML). When called with
`Accept: application/json` returns the share metadata as JSON.

The **HTML** form never costs a use and sets no cookie; it embeds the
metadata with plain URLs and without `view`. Its script opens the viewing
with [`POST /s/{token}/view`](#post-stokenview) and loads the media only
after that succeeded. The **JSON** form is for clients that are about to
play: a `GET` without a valid viewing credential opens a viewing (one use,
see [Share viewings](#share-viewings-how-max_uses-is-counted)), sets the
`share_view` cookie and carries the credential in every URL:

**Response `200` (JSON):**

```json
{
  "media": {
    "id": 42,
    "file_name": "holiday.mp4",
    "type": "video",
    "duration": 3612.5,
    "codec": "h264/aac",
    "resolution": "1920x1080",
    "bitrate": 4500,
    "file_size_bytes": 2038431744
  },
  "has_thumb": true,
  "stream_url": "/s/abc123/stream?view=1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU",
  "playback_url": "/s/abc123/stream?view=1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU",
  "transcoded": false,
  "download_url": "/s/abc123/download?view=1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU",
  "thumb_url": "/s/abc123/thumbnail?view=1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU",
  "view": "1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU",
  "view_expires_at": "2026-10-09T18:27:49Z"
}
```

Play `playback_url` exactly as given. It equals `stream_url` unless
`transcoded` is `true` (AVI, WMV, FLV, WMA, MPG/MPEG and other legacy codecs), in which
case it is `/s/{token}/compat?view=…`. `thumb_url` is omitted when the item
has no thumbnail. Fetch this JSON once per viewing: every fetch without a
credential is a new viewing. `view_expires_at` is when the credential stops
working (never later than the share's `expires_at`).

**Status codes:** `200`, `404` (unknown or revoked share), `410` (share
expired, or no use left and no valid viewing credential), `500`

---

### `POST /s/{token}/view`

Opens the share page's viewing. No request body. Without a valid viewing
credential it consumes one use and sets the `share_view` cookie; with one (a
page reload sends the cookie) it changes nothing. The share page sends it
once, before it requests any media, thumbnail or download. If the answer is
`410` the page asks once more after a second: when two tabs of one browser
open a single-use link together, the tab that lost the use finds the
winner's cookie in place by then. Without JavaScript the page cannot open a
viewing and says so.

It is a `POST` because nothing that merely looks at a link sends one.

**Response:** `204 No Content`

**Status codes:** `204`, `404` (unknown or revoked share), `405` (any other
method), `410` (share expired, or no use left and no valid viewing
credential), `500`

---

### `GET /s/{token}/stream` · `HEAD /s/{token}/stream`

Stream shared media. Supports the `Range` header for seeking (HTTP 206 partial
content). See [Range Header Support](#range-header-support) below.

With a valid viewing credential (cookie or `?view=`) no use is counted. A
`GET` without one opens a viewing of its own — one use — and sets the
`share_view` cookie; see
[Share viewings](#share-viewings-how-max_uses-is-counted).

**Status codes:** `200`, `206`, `404`, `410`, `500`

---

### `GET /s/{token}/compat` · `HEAD /s/{token}/compat`

Public equivalent of
[`GET /api/media/{id}/compat`](#get-apimediaidcompat--get-apiv1mediaidcompat):
the transcoded compatibility rendition of the shared media, with `Range`
support and the same `HEAD` readiness probe. Responses carry
`Cache-Control: no-store`.

Share uses are counted as on `/s/{token}/stream`
([Share viewings](#share-viewings-how-max_uses-is-counted)): none with a
valid viewing credential; without one a `GET` opens a viewing (one use, cookie
set). That use is counted only once content is certain to be delivered;
`HEAD` requests never count, and neither do answers without content (`503`
while transcoding or busy, errors). Send the credential with the `HEAD` probe
as well: on a share whose uses are spent, a probe without it answers `410`.
All share links together may have two transcodes pending; beyond that the
answer is `503` with status `busy`.

**Status codes:** `200`, `206`, `304`, `400`, `404`, `410`, `415`, `500`,
`503`, `507` — see the authenticated endpoint for their meaning.

---

### `GET /s/{token}/thumbnail`

Return the thumbnail image for a shared media item. Never costs a use and
never opens a viewing. It is served to a client with a valid viewing
credential, and to anyone while the share has uses left.

**Status codes:** `200`, `404`, `410`, `500`

---

### `GET /s/{token}/download`

Download the original file for a shared media item. Sets
`Content-Disposition: attachment`. Uses are counted as on
`/s/{token}/stream`.

**Status codes:** `200`, `206`, `404`, `410`, `500`

---

## Auth Endpoints

Session required. Use Bearer token or session cookie.

---

### `POST /api/logout` · `POST /api/v1/logout`

Invalidate the current session cookie. Bearer-authenticated clients do not need
to call this — revoke the token instead.

**Response `204 No Content`** (no body).

---

### `POST /api/auth/tokens` · `POST /api/v1/auth/tokens`

Mint a new Bearer API token for the authenticated user.

**Request body:**

```json
{
  "name": "android-client",
  "expires_in_days": 365
}
```

`expires_in_days` is optional. Omit or pass `null` for a non-expiring token.
Pass an integer > 0 to set an expiry.

**Response `200`:**

```json
{
  "id": 7,
  "name": "android-client",
  "token": "pt_xxxxxxxxxxxxxxxxxxxx"
}
```

The `token` field is the **plaintext Bearer value** — it will not appear again.
Store it securely.

**Status codes:** `200`, `400`, `401`, `500`

```bash
curl -s -X POST https://player.example.com/api/v1/auth/tokens \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"name": "ci-token"}'
```

---

### `GET /api/auth/tokens` · `GET /api/v1/auth/tokens`

List API tokens belonging to the authenticated user. Plaintext values are never
returned here.

**Response `200`:**

```json
[
  {
    "id": 7,
    "name": "android-client",
    "last_used_at": "2026-05-17T10:00:00Z",
    "expires_at": "2027-05-17T10:00:00Z",
    "created_at": "2026-05-17T09:00:00Z"
  }
]
```

`last_used_at` and `expires_at` are `null` when not set.

**Status codes:** `200`, `401`, `500`

---

### `DELETE /api/auth/tokens/{id}` · `DELETE /api/v1/auth/tokens/{id}`

Revoke a token by its numeric ID. Only the owning user can revoke their own
tokens. Immediate effect — any in-flight request using the token will fail.

**Response `204 No Content`** (no body).

**Status codes:** `204`, `400`, `401`, `404`, `500`

```bash
curl -s -X DELETE https://player.example.com/api/v1/auth/tokens/7 \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx"
```

---

### `DELETE /api/auth/tokens/current` · `DELETE /api/v1/auth/tokens/current`

Revoke the Bearer token used for this request. This supports clients that have
the token value but did not retain its numeric ID. A session cookie alone cannot
use this endpoint. The token stops authenticating immediately after success.

**Response `204 No Content`** (no body).

**Status codes:** `204`, `401`, `500`

```bash
curl -s -X DELETE https://player.example.com/api/v1/auth/tokens/current \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx"
```

---

## Configuration

### `GET /api/config` · `GET /api/v1/config`

Return client configuration and capabilities: the server-side page size, so
clients can paginate consistently, and `podcast_sync` (`1`): the server
implements `POST /api/v1/podcasts/sync` with subscription and episode
registers. A client that does not find `podcast_sync` (an older server) runs
podcasts standalone for that account, keeps media sync working and shows no
podcast sync error.

**Response `200`:**

```json
{ "media_page_size": 100, "podcast_sync": 1, "user_id": 1 }
```

`user_id` is the authenticated account ID, for locally scoped queue recovery.
It is returned by both prefixes and does not expose credentials or another
account’s queue. Clients without this field leave local queue recovery disabled.

**Status codes:** `200`, `401`

---

## Sets

A **set** is a top-level collection corresponding to a subdirectory of
`MEDIA_ROOT`. Users see only sets they have been granted access to (admins see
all sets).

---

### `GET /api/sets` · `GET /api/v1/sets`

List all sets visible to the authenticated user.

**Response `200`:**

```json
[
  {
    "id": 1,
    "name": "Movies",
    "root_path": "movies",
    "cover_thumbnail_path": "/media/movies/.cover.jpg",
    "is_podcast": false,
    "permissions": [
      { "set_id": 1, "user_id": 3, "role": "viewer", "created_at": "2026-01-01T00:00:00Z" }
    ],
    "created_at": "2026-01-01T00:00:00Z"
  }
]
```

**Status codes:** `200`, `401`, `500`

```bash
curl -s https://player.example.com/api/v1/sets \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx"
```

---

### `GET /api/sets/{id}/browse` · `GET /api/v1/sets/{id}/browse`

Browse the folder tree within a set. Pass `?parent=subfolder` to navigate into
a subdirectory.

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `parent` | Relative folder path within the set (default: root) |

**Response `200`:**

```json
{
  "current_path": "movies/action",
  "folders": [
    { "name": "2023", "has_cover": true }
  ],
  "media": [ { /* Media object — see Media schema */ } ],
  "episodes": []
}
```

`episodes` is present and non-empty only when the set is a podcast set.

**Status codes:** `200`, `400`, `401`, `403`, `500`

---

### `GET /api/sets/{id}/cover` · `GET /api/v1/sets/{id}/cover`

Return the cover image for a set or folder. Returns the image bytes directly.

An explicitly generated `.cover.jpg` takes priority. Otherwise, direct source
artwork is preferred in this order: `cover.jpg`, other `cover` images, `front`,
`folder`, then `album` (case insensitive; JPEG, PNG or GIF). Collection covers
choose among the preferred artwork of their descendant books before using
audio artwork or arbitrary image/video thumbnails. Missing, empty and
non-file fallback candidates are skipped. Photo/video-only sets still use
their media thumbnails.

Responses use `Cache-Control: no-cache` and an ETag identifying the selected
file. Clients should revalidate with `If-None-Match`; modification times alone
cannot validate a URL that may choose a different book's cover.

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `folder` | Subfolder within the set (optional) |

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/sets/{id}/cover` · `POST /api/v1/sets/{id}/cover`

Regenerate the cover image for a set or folder (owner or admin only).

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `folder` | Subfolder to regenerate the cover for (optional) |

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/sets/{id}/upload` · `POST /api/v1/sets/{id}/upload`

Upload a media file to a set. Requires `owner` permission on the set.

**Request:** `multipart/form-data` with a single field named `file`.

Maximum upload size is controlled by `MAX_UPLOAD_SIZE_MB` (default 100 MB).

**Response `200`:** The newly created `Media` object (see [Media schema](#media-schema)).

**Status codes:** `200`, `400`, `401`, `403`, `404`, `413`, `500`

```bash
curl -s -X POST https://player.example.com/api/v1/sets/1/upload \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx" \
  -F file=@/path/to/video.mp4
```

---

## Media

### Media Schema

All endpoints that return a media item use this shape:

```json
{
  "id": 42,
  "set_id": 1,
  "rel_path": "action/movie.mp4",
  "file_name": "movie.mp4",
  "abs_path": "/media/movies/action/movie.mp4",
  "type": "video",
  "duration": 7200.0,
  "codec": "h264/aac",
  "resolution": "1920x1080",
  "bitrate": 4500,
  "file_size_bytes": 4294967296,
  "width": 1920,
  "height": 1080,
  "exif_camera": "",
  "exif_lens": "",
  "exif_date": "",
  "exif_iso": "",
  "exif_f_number": "",
  "exif_exposure": "",
  "exif_focal_length": "",
  "thumbnail_path": "/media/movies/.thumbs/movie.jpg",
  "play_count": 3,
  "deleted_at": null,
  "created_at": "2026-01-15T12:00:00Z",
  "transcoded": false
}
```

`type` is one of `"video"`, `"audio"`, or `"image"`.

`transcoded` is present on every media object returned by the API. When it is
`true` at least one of the clients cannot decode the original file (AVI, WMV,
FLV, WMA containers, or legacy codecs in any container) and the item must be
played from
`/api/v1/media/{id}/compat` instead of `/api/v1/media/{id}/stream`. The
[playback hint](#get-apimediaidplayback--get-apiv1mediaidplayback) returns the
ready-made URL.

---

### `GET /api/media` · `GET /api/v1/media`

List or search media visible to the authenticated user. Supports rich filtering.

**Query parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `search` | string | Plain-text search on file name / path / embedded title, artist and album |
| `set_id` | integer | Filter to a single set |
| `set_ids` | string | Comma-separated set IDs |
| `type` | string | `video`, `audio`, or `image` |
| `favorites` | string | `true` or `1` to show favourites only |
| `tags` | string | Comma-separated tag names (AND match) |
| `min_duration` | float | Minimum duration in seconds |
| `max_duration` | float | Maximum duration in seconds |
| `filesize_min` | integer | Minimum file size in bytes (inclusive) |
| `filesize_max` | integer | Maximum file size in bytes (inclusive) |
| `sort` | string | `name`, `date`, `duration`, `play_count`, or `random` |
| `limit` | integer | Page size (1–1000, default 100) |
| `offset` | integer | Page offset (default 0) |
| `folder` | string | Filter to an exact folder path |
| `parent` | string | Filter to items inside a parent folder |

**Response `200`:** Array of `Media` objects.

**Status codes:** `200`, `401`, `500`

```bash
# List recent videos from set 1, page 2
curl -s "https://player.example.com/api/v1/media?set_id=1&type=video&limit=20&offset=20" \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx"
```

---

### `GET /api/media/{id}` · `GET /api/v1/media/{id}`

Return a single media item with related data (tags, favorite state, note,
and saved progress position).

**Response `200`:**

```json
{
  "media": { /* Media object */ },
  "tags": [ { "id": 3, "name": "documentary" } ],
  "favorite": false,
  "note": null,
  "progress": {
    "user_id": 3,
    "media_id": 42,
    "position_seconds": 1234.5,
    "finished": false,
    "updated_at": "2026-05-10T08:00:00Z"
  }
}
```

`note` and `progress` are `null` when absent.

**Status codes:** `200`, `400`, `401`, `404`, `500`

---

### `GET /api/media/{id}/stream` · `GET /api/v1/media/{id}/stream`

Stream a media file. The server sets `Accept-Ranges: bytes` and handles the
`Range` header natively, so clients can seek without downloading the entire
file.

#### Range Header Support

Standard HTTP range requests are supported on all streaming and download
endpoints:

```
Range: bytes=0-1048575
```

The server responds with `206 Partial Content` when a valid `Range` header is
present, `200 OK` otherwise.

When the file requires remuxing (e.g. `.mkv` files), the server streams the
remuxed output and sets:

```
X-Duration: <seconds as float>
Content-Type: video/mp4
Cache-Control: no-store
```

Remuxed streams do not support range requests.

**Status codes:** `200`, `206`, `400`, `401`, `403`, `404`, `500`

```bash
# Stream from byte offset 10 MB
curl -s -r 10485760- https://player.example.com/api/v1/media/42/stream \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx" \
  -o segment.mp4
```

---

### `GET /api/media/{id}/compat` · `GET /api/v1/media/{id}/compat`

Also answers `HEAD` on both paths (see *Readiness probe* below).

Stream the **compatibility rendition** of a media item: the server transcodes
the original with ffmpeg and caches the result, so formats a client cannot
decode become playable.

| Media | Rendition | `Content-Type` |
|-------|-----------|----------------|
| video with `"transcoded": true` | H.264 video + AAC audio in MP4 | `video/mp4` |
| audio with `"transcoded": true` | AAC audio in MP4 (M4A) | `audio/mp4` |
| audio/video with `"transcoded": false` | not offered — play `/stream` | — (`400`) |
| image | not available | — (`415`) |

Access rules are identical to `/stream`, with one addition: the file must
really be located inside the media root after resolving symbolic links
(`403` otherwise), whereas `/stream` checks the path as written. The
rendition is a complete file, so `Range` requests (seeking) work exactly as
on `/stream`. The response has an `ETag` and no `Last-Modified`. The `ETag`
identifies one particular encode: it changes when the source file changes and
also when the rendition had to be produced again after cache eviction, so an
`If-Range` resume never mixes two encodes.

**First request.** The first request for an item starts the transcode and
waits for it for up to 20 seconds. If it finishes in time the rendition is
served directly. Otherwise the server answers:

```
HTTP/1.1 503 Service Unavailable
Retry-After: 5
X-Transcode-Status: transcoding
Content-Type: application/json

{"error":"transcode in progress","retry_after_seconds":5,"status":"transcoding"}
```

The transcode continues in the background (also if the client disconnects).
Repeat the request until it returns `200`/`206`; concurrent and repeated
requests share the one running transcode. Later requests are served from the
cache immediately. Streams that already are client-compatible (H.264 up to
1080p, stereo AAC-LC) are copied rather than re-encoded, which usually
finishes within the first request.

`X-Transcode-Status: busy` (body `"status":"busy"`,
`"error":"transcoder busy"`) means the transcode could not be queued yet: too
many jobs overall, two jobs of the same user already pending, or the server
is shutting down. Retry the same way. A file that was modified within the
last few seconds (an upload still in progress) also answers `transcoding`
until it has settled.

**Readiness probe.** `HEAD` on the same URL behaves exactly like `GET` —
same access checks, and it starts or joins the transcode — but never returns
a body:

| `HEAD` answer | Meaning |
|---------------|---------|
| `200` with `Content-Type`, `Content-Length`, `ETag`, `Accept-Ranges` | Ready: `GET` the URL now |
| `503` with `Retry-After` and `X-Transcode-Status: transcoding` or `busy` | Not ready: probe again after `Retry-After` seconds |
| any other status | Same terminal status `GET` would return |

Every `503` of this endpoint, for `GET` and `HEAD`, carries
`X-Transcode-Status`. A `HEAD` never consumes a share use (on a share link,
send the viewing credential with it, see
[Share viewings](#share-viewings-how-max_uses-is-counted)).

**Status codes**

| Code | Meaning |
|------|---------|
| `200`, `206`, `304` | Rendition served / range / not modified |
| `400` | Invalid id, or the item is not flagged `transcoded` (`{"error":"media does not need transcoding; play the stream endpoint instead"}`) |
| `401`, `403`, `404` | As on `/stream` (`404` also when the source file is missing on disk, `403` also for a symlink leaving the media root) |
| `415` | Images have no rendition |
| `500` | `{"error":"transcode failed"}` — ffmpeg could not convert the file. Not retried by the server for 1 minute (doubling up to 30 minutes; 24 hours after a job timed out); details are in the server log only |
| `503` | Retry after `Retry-After` seconds; `X-Transcode-Status` is `transcoding` or `busy` |
| `507` | The transcode cache has no room for this rendition: the volume is full, or the rendition would exceed the cache budget (`TRANSCODE_CACHE_MAX_MB`; remembered until the file changes) |

Error bodies contain only the fixed messages above, never file paths.

```bash
# Probe until ready, then fetch the first megabyte
curl -s -I https://player.example.com/api/v1/media/42/compat \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx"
curl -s -r 0-1048575 https://player.example.com/api/v1/media/42/compat \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx" \
  -o head.mp4
```

---

### `GET /api/media/{id}/download` · `GET /api/v1/media/{id}/download`

Download the original file with `Content-Disposition: attachment`. Supports
`Range` header.

**Status codes:** `200`, `206`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/media/{id}/cast` · `POST /api/v1/media/{id}/cast`

Issue a **cast ticket** so a Chromecast (or another Google Cast receiver) can
stream a media item. A cast device fetches media by URL and cannot send the
sender's session cookie or bearer token, so the ticket is the credential it
carries in the URL instead. The caller must be allowed to stream the item.

**Response:**

```json
{
  "ticket": "AAAAAAAAAAEAAAAAAAAABQAAAABo...",
  "expires_at": "2026-10-16T23:51:47Z",
  "stream_content_type": "audio/mpeg",
  "compat_content_type": "audio/mp4"
}
```

The receiver is given absolute URLs built from the server's own base URL:

| URL | Serves |
|-----|--------|
| `{base}/cast/{ticket}/stream` | like `/api/media/{id}/stream` (`stream_content_type`) |
| `{base}/cast/{ticket}/compat` | like `/api/media/{id}/compat` (`compat_content_type`); probe it until ready before casting |
| `{base}/cast/{ticket}/thumbnail` | like `/api/media/{id}/thumbnail` |

These routes need no session, support `Range` and `HEAD`, answer CORS
preflights for any origin, and send `Cache-Control: no-store`. A ticket is
HMAC-signed, valid for 12 hours, bound to one user and one media item, and
checked against that user's current permissions on every request. Invalid,
expired, or no longer permitted tickets answer `404`. The signing key lives
in memory only: after a server restart old tickets stop working and the
sender requests a new one. The cast device must be able to reach the server
at the base URL the sender uses (for example the same LAN address).

**Status codes:** `200`, `400`, `401`, `404`, `500`

---

### DLNA / UPnP output

`GET /api/renderers` (also `/api/v1/renderers`) searches the **server's LAN**
for compatible AVTransport speakers. Requires authentication; returns an array
of `{id, name}` with opaque IDs, never device control URLs. Responses are not
cached. An empty array means no compatible speaker answered the bounded search.
Multicast connectivity between server and speaker is required.

`POST /api/renderers/control` (also `/api/v1/renderers/control`) accepts:

```json
{"device_id":"opaque discovery ID","session_id":"0123456789abcdef0123456789abcdef","revision":1,"action":"load","media_id":42,"position":12.5}
```

`session_id` is a random 16-byte hex output-session ID. Advance `revision` on
each load and stop; other commands carry the current revision. Stale revisions
and retired sessions cannot take over a newer queue entry or restart after stop.
Version checks and complete SOAP sequences are serialized together.

Actions: `load`, `status`, `play`, `pause`, `seek`, `stop`. Only `load` needs
`media_id`; `load` and `seek` accept position in seconds (0 through seven days).
`load` checks current media permission and audio MIME type, claims the speaker
for the account, supplies an existing media-scoped cast ticket, seeks and plays.
It returns `{state:"PLAYING",duration:123.4}`. `status` returns transport `state`
(`PLAYING`, `PAUSED_PLAYBACK`, `STOPPED`, or `TRANSITIONING`), `position`, and
`duration` in seconds. Other commands return `{ok:true}`. `stop` releases
ownership even if the receiver cannot be reached; abandoned ownership can be
reclaimed after two minutes without status/control traffic.

Arbitrary URLs/SOAP commands are refused. Device HTTP URLs must be numeric
private LAN addresses on the discovery response host; redirects, proxies,
loopback and link-local addresses are refused. The receiver gets no cookie or
bearer token. Its original-format audio URL uses this request's scheme and Host,
which must be reachable from the speaker. Codec and network failures return a
readable error; a failed stop can mean the disconnected speaker is still playing.
See [M59 implementation boundaries](music-m59-implementation.md).

Status codes: `200`, `400`, `401`, `404` (media permission/missing), `409` (unknown
or account-owned device on load), `502` (control failure), `503` (discovery/service
unavailable). Browser and native journeys with a compatible receiver remain
required before feature acceptance.

---

### `GET /api/media/{id}/thumbnail` · `GET /api/v1/media/{id}/thumbnail`

Return the thumbnail image for a media item. Generated thumbnails are JPEG;
audio may use an existing album image (including Beets' `cover.jpg`) in its
original format. When no nearby image exists, audio uses its embedded attached
picture, preferring a picture labelled as the front cover. Existing audio picks
this up on an admin rescan. Uses the track's set permissions. Sets `Cache-Control:
no-cache`. An admin rescan updates existing audio tracks when preferred
album artwork appears or the selected image is removed.

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/media/{id}/thumbnail` · `POST /api/v1/media/{id}/thumbnail`

Regenerate a media item's thumbnail using ffmpeg. Audio requires an embedded
attached picture and extracts it without seeking. Requires `owner` permission.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/media/{id}/favorite` · `POST /api/v1/media/{id}/favorite`

Toggle the authenticated user's favourite status for a media item.

**Response `200`:**

```json
{ "favorite": true }
```

`favorite` reflects the **new** state after the toggle.

**Status codes:** `200`, `400`, `401`, `404`, `500`

---

### `DELETE /api/media/{id}` · `DELETE /api/v1/media/{id}`

Soft-delete a media item. The item gains a UTC `deleted_at` timestamp and stays
recoverable for seven days. The source file is kept in place until expiry; the
server sweeps expired trash at startup and every `GC_INTERVAL_MINUTES`.
The item is moved to trash and is no longer returned
by `GET /api/media`. Requires `owner` permission or admin.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/media/{id}/restore` · `POST /api/v1/media/{id}/restore`

Restore a soft-deleted media item from trash. Requires `owner` permission or
admin, and an existing readable regular source confined to `MEDIA_ROOT`. Missing
or non-regular files answer `404`; unsafe or unreadable sources answer `403`.
A rejected restore preserves the trash entry and its timestamp. A read-only set
can be restored when its source is still readable.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `GET /api/media/{id}/playback` · `GET /api/v1/media/{id}/playback`

Return the URL to play plus codec and container metadata of the original
file.

**Response `200`:**

```json
{
  "stream_url": "/api/v1/media/42/stream",
  "playback_url": "/api/v1/media/42/stream",
  "transcoded": false,
  "container": "mp4",
  "video_codec": "h264",
  "audio_codec": "aac",
  "duration_seconds": 7200.0,
  "file_size_bytes": 4294967296,
  "width": 1920,
  "height": 1080,
  "bitrate": 4500,
  "needs_transcode": false
}
```

Clients play `playback_url`. `stream_url` always addresses the original
bytes. `transcoded: true` means `playback_url` is the
[compatibility rendition](#get-apimediaidcompat--get-apiv1mediaidcompat)
because at least one client (web or Android) cannot decode the original. The
decision uses the container and the probed codecs:

- containers `avi`, `wmv`, `wma`, `asf`, `flv`;
- video codecs `wmv1`/`wmv2`/`wmv3`/`vc1`, `msmpeg4v1`–`v3`, `flv1`, `vp6*`,
  `mpeg4` (Xvid/DivX), `mpeg1video`, `mpeg2video`, `h263`, `rv10`–`rv40`,
  `svq1`/`svq3`, `theora` in any container;
- audio codecs `wmav1`/`wmav2`/`wmapro`/`wmalossless`/`wmavoice`, `ac3`,
  `eac3`, `dts`, `truehd`, `cook` in any container.

`codec` is `"video/audio"` for video files (e.g. `"h264/ac3"`) and the audio
codec alone for audio files (embedded cover art is ignored); in the hint an
audio file's codec is reported as `audio_codec`. Audio files are judged by
audio codecs only. Items scanned by older server versions carry the video
codec only; their audio codec is unknown to the rule until the file is probed
again. Example for an AVI file:

```json
{
  "stream_url": "/api/v1/media/43/stream",
  "playback_url": "/api/v1/media/43/compat",
  "transcoded": true,
  "container": "avi",
  "video_codec": "mpeg4",
  "audio_codec": "",
  "duration_seconds": 5400.0,
  "file_size_bytes": 734003200,
  "width": 640,
  "height": 480,
  "bitrate": 1087000,
  "needs_transcode": true
}
```

`needs_transcode` is an older, broader heuristic ("may not play on every
client", also true for e.g. `mkv` and `flac`) and does **not** select the URL;
use `transcoded`. It is always `true` when `transcoded` is `true`. Its native containers include `mp4`, `webm`, `ogg`, `mp3`, `m4a`, `wav`,
`aac`, and `opus`. Native video codecs include `h264`, `vp8`, `vp9`, `av1`,
and `hevc`. Native audio codecs include `aac`, `mp3`, `opus`, and
`vorbis`.

**Status codes:** `200`, `400`, `401`, `404`, `500`

```bash
curl -s https://player.example.com/api/v1/media/42/playback \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx"
```

---

### `POST /api/media/{id}/shares` · `POST /api/v1/media/{id}/shares`

Create a public share link for a media item. The optional JSON body accepts
`expires_at` (an RFC 3339 timestamp in the future) and `max_uses` (a positive
integer). Omit either field to use the defaults: `SHARE_DEFAULT_EXPIRY_DAYS`
(7 days by default) and unlimited uses.

```json
{"expires_at":"2099-05-24T10:00:00Z","max_uses":1}
```

Replace the example timestamp with your intended future expiry.

A use is one **viewing**: one recipient opening the share in one browser or
app, with all the stream, range, thumbnail and download requests that follow,
for up to 6 hours; reloading the page in the same browser is the same
viewing. `max_uses: 1` therefore lets exactly one browser or device play the
file, start to end. Merely fetching the share page does not use the link — a
messenger's link preview or a mail scanner costs nothing — and neither do
`HEAD` requests and thumbnails. See
[Share viewings](#share-viewings-how-max_uses-is-counted) for the details and
for the limits of the scheme (the viewing credential can be passed on while
it is valid; clients that keep no cookies).

Once the expiry is reached, or the use limit is reached for clients without a
running viewing, the public endpoints return `410` while the share record
remains. A deleted share returns `404`.

**Response `200`:**

```json
{
  "token": "abc123xyz",
  "media_id": 42,
  "created_by": 3,
  "created_at": "2026-05-17T10:00:00Z",
  "expires_at": "2026-05-24T10:00:00Z",
  "max_uses": null,
  "used_count": 0
}
```

Share the public URL: `https://player.example.com/s/<token>`

**Status codes:** `200`, `400`, `401`, `404`, `500`

---

### `GET /api/media/{id}/shares` · `GET /api/v1/media/{id}/shares`

List shares for a specific media item, including links that have reached their
use limit or expired but have not been revoked.

**Response `200`:** Array of `Share` objects (same schema as above).

**Status codes:** `200`, `400`, `401`, `404`, `500`

---

## Notes

A note is a per-user, per-media free-text annotation.

---

### `GET /api/media/{id}/notes` · `GET /api/v1/media/{id}/notes`

Return the authenticated user's note for a media item.

**Response `200`:**

```json
{
  "id": 5,
  "media_id": 42,
  "user_id": 3,
  "content": "My viewing notes here.",
  "created_at": "2026-01-20T09:00:00Z",
  "updated_at": "2026-04-01T14:00:00Z"
}
```

**Response `204 No Content`** when no note exists (no body).

**Status codes:** `200`, `204`, `400`, `401`, `500`

---

### `POST /api/media/{id}/notes` · `POST /api/v1/media/{id}/notes`

Create or update (upsert) the authenticated user's note for a media item.

**Request body:**

```json
{ "content": "My viewing notes here." }
```

**Response `200`:** The updated `Note` object.

**Status codes:** `200`, `400`, `401`, `500`

---

### `DELETE /api/media/{id}/notes` · `DELETE /api/v1/media/{id}/notes`

Delete the authenticated user's note for a media item.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `500`

---

## Bookmarks

Bookmarks save additional named playback positions for audio and video. Each
user has their own bookmarks, including administrators; current set access is
required for every operation. Images use the existing favorites API instead.
Bookmarks do not change notes, resume progress, listened status or play counts.

### `GET /api/media/{id}/bookmarks` · `GET /api/v1/media/{id}/bookmarks`

Return the authenticated user's bookmarks for this media, ordered by
`position_seconds` ascending, then `id` ascending for equal positions.

**Response `200`:** An array (empty as `[]`), with this object schema:

```json
[
  {
    "id": 7,
    "media_id": 42,
    "position_seconds": 123.5,
    "label": "Chapter two",
    "created_at": "2026-10-15T09:00:00Z"
  }
]
```

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`.

### `POST /api/media/{id}/bookmarks` · `POST /api/v1/media/{id}/bookmarks`

Create a bookmark belonging to the authenticated user. Multiple bookmarks may
have the same position or label; every POST creates a separate record.

**Request body:**

```json
{ "position_seconds": 123.5, "label": "Chapter two" }
```

`position_seconds` is required, must be a finite number >= 0, and must not
exceed a known positive media duration. Unknown duration (zero) does not impose
an upper bound. `label` is optional (defaults to `""`), is trimmed, and must be
at most 200 Unicode characters after trimming. Unknown fields, malformed JSON,
trailing JSON values and bodies over 4096 bytes are rejected with `400`.

**Response `201`:** The created object, using the schema above. The server
assigns its ID, owner and UTC creation time. No user ID is accepted or returned.

**Status codes:** `201`, `400`, `401`, `403`, `404`, `500`.

### `DELETE /api/media/{id}/bookmarks/{bookmarkId}` · `DELETE /api/v1/media/{id}/bookmarks/{bookmarkId}`

Delete a bookmark only when it belongs to both the authenticated user and the
specified media. A missing bookmark, another user's bookmark, or a bookmark on
another media item returns `404`.

**Response `204`:** No body.

**Status codes:** `204`, `400`, `401`, `403`, `404`, `500`.

All three endpoints reject images with `400`, require authentication (`401`),
and refuse inaccessible sets (`403`) and missing or trashed media (`404`). Path
IDs must be positive integers (`400` otherwise). Soft deletion hides bookmarks
until the media is restored; permanent deletion of the user or media removes
them.

---

## Progress

Playback progress tracks the last known position for each media item per user.
Progress also drives the 60-second accumulator that increments `play_count`.

**Server copies of podcast episodes.** For a media item that is the server
copy of a podcast episode, these routes and
[`playback-sync`](#downloaded-playback-synchronization) edit the caller's
episode state instead (the position and completion registers that
`POST /api/v1/podcasts/sync` synchronizes), so a downloaded episode has one
history across the web, old and new Android clients:

| Call on an episode copy | Episode state edit |
|---|---|
| `POST /progress`, `/progress/batch` (position p at t) | position `(p, t)`; play-count accumulation as for any media |
| `POST /progress/status` `finished` (at t) | manual completion at t; the position is kept |
| `POST /progress/status` `not_started` (at t) | position `(0, t)` and manual uncompletion at t |
| `playback-sync` progress `{p, finished: false, t}` | position `(p, t)` only |
| `playback-sync` progress `{p, finished: true, t}` | automatic completion at t and position `(p, t)` |

Each register keeps the edit with the newest clock (equal clocks keep the
stored value), so a position save is not blocked by a later completion and
the reverse. A clock more than 5 minutes ahead of the server is stored as
the server's time. The progress these routes read back (`GET` progress,
`in-progress`, the `playback-sync` answer) is the projection of that state:
`position_seconds` is the position, `finished` is true only when the episode
is completed and the completion is not older than the position (a completed
episode being replayed reads as unfinished, so it is resumed and listed in
progress), and the edit time is the later of both clocks. An unfinished
position within 5 seconds of the end of the copy (duration known) reads as
`0`, so an episode marked unlistened at its end starts over instead of
completing again at once; the episode state keeps the real position. A `finished` or
`not_started` that changed the state starts a new play-count cycle, as for
any media. A new server download starts from the episode state every client
already shares; deleting the copy removes only its progress, never the
episode state. Bookmarks, notes, favorites and shares stay media-scoped.

---

### `POST /api/progress` · `POST /api/v1/progress`

Save a playback position for a single media item. Call this periodically while
the user is playing media (e.g. every 10–30 seconds).

**Request body:**

```json
{
  "media_id": 42,
  "position_seconds": 1234.5
}
```

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `500`

```bash
curl -s -X POST https://player.example.com/api/v1/progress \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"media_id": 42, "position_seconds": 1234.5}'
```

---

### `POST /api/progress/batch` · `POST /api/v1/progress/batch`

Submit multiple progress updates in one call. Designed for offline clients that
accumulate updates while disconnected and sync on reconnect. Updates are
processed in `observed_at` order — older updates do not overwrite newer ones.

**Request body:**

```json
{
  "updates": [
    {
      "media_id": 42,
      "position_seconds": 500.0,
      "observed_at": "2026-05-17T08:00:00Z"
    },
    {
      "media_id": 43,
      "position_seconds": 120.0,
      "observed_at": "2026-05-17T08:05:00Z"
    }
  ]
}
```

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `500`

---

### `POST /api/progress/status` · `POST /api/v1/progress/status`

Mark a media item as finished or reset its progress.

**Request body:**

```json
{
  "media_id": 42,
  "status": "finished"
}
```

`status` must be one of:
- `"finished"` — mark as fully watched/listened
- `"not_started"` — clear position and playback counters

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `500`

---

### `GET /api/in-progress` · `GET /api/v1/in-progress`

Return media items the authenticated user has started but not finished,
most recently played first.

**Response `200`:** Array of `Media` objects (same schema as `GET /api/media`),
each with an extra `position_seconds` field holding the user's saved playback
position. Clients can show progress and resume without a request per item.
Other media listings omit this field.

**Status codes:** `200`, `401`, `500`

```bash
curl -s https://player.example.com/api/v1/in-progress \
  -H "Authorization: Bearer pt_xxxxxxxxxxxxxxxxxxxx"
```

---

## Shares

These endpoints manage the authenticated user's own share links.

---

### `GET /api/shares` · `GET /api/v1/shares`

List all share links created by the authenticated user.

**Response `200`:**

```json
[
  {
    "token": "abc123xyz",
    "media_id": 42,
    "file_name": "movie.mp4",
    "media_type": "video",
    "created_at": "2026-05-17T10:00:00Z",
    "expires_at": "2026-05-24T10:00:00Z",
    "max_uses": null,
    "used_count": 2
  }
]
```

**Status codes:** `200`, `401`, `500`

---

### `PATCH /api/shares/{token}` · `PATCH /api/v1/shares/{token}`

Edit an existing single-file or music collection link. The creator or an admin
may supply `description` (plain text, at most 1000 Unicode characters; empty
clears it) and/or `expires_at` (RFC 3339 timestamp). Omitted fields retain their
stored values. Null values, unknown fields and empty updates return `400`.
A changed expiry must be in the future; a description-only update preserves the
exact expiry even for an already expired link. An unchanged supplied expiry is
also preserved. Extending an expired link permits new viewings only if its
existing viewing limit still allows them and the source is available.

**Response `200`:** The updated `Share` object. The existing token/URL, creator,
source snapshot, creation time, `max_uses` and `used_count` remain unchanged.
`description` is always present on this object (empty string when cleared) so
clients can replace a previous value without keeping stale text. Nonempty
descriptions also appear in My Shares and public share metadata and are shown
as plain text by guest clients. Editing does not open a viewing, reset uses or
renew existing viewing credentials. Shortening expiry takes effect immediately
on all content requests, including active viewings; extending expiry does not
extend an already-issued credential's expiry. Existing source access, download,
revocation and viewing rules remain in effect.

**Status codes:** `200`, `400`, `401`, `403` (foreign non-admin), `404` (missing or
revoked), `500`.

---

### `DELETE /api/shares/{token}` · `DELETE /api/v1/shares/{token}`

Revoke a share link. The creator or an admin can revoke it, including after its
media item has been moved to trash.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

## Tags

---

### `GET /api/tags` · `GET /api/v1/tags`

Return all tag names visible to the authenticated user.

**Response `200`:**

```json
[
  { "id": 1, "name": "documentary" },
  { "id": 2, "name": "4k" }
]
```

**Status codes:** `200`, `401`, `500`

---

### `POST /api/media/{id}/tags` · `POST /api/v1/media/{id}/tags`

Add a tag to a media item.

**Request body:**

```json
{ "tag": "documentary" }
```

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `404`, `500`

---

### `DELETE /api/media/{id}/tags/{tag}` · `DELETE /api/v1/media/{id}/tags/{tag}`

Remove a tag from a media item. `{tag}` is the URL-encoded tag name.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `404`, `500`

---

## Admin

All admin endpoints require the authenticated user to have `is_admin: true`.
Returns `403 Forbidden` for non-admin users.

---

### `GET /api/admin/users` · `GET /api/v1/admin/users`

List all user accounts.

**Response `200`:**

```json
[
  {
    "id": 1,
    "username": "admin",
    "is_admin": true,
    "created_at": "2026-01-01T00:00:00Z"
  }
]
```

**Status codes:** `200`, `401`, `403`, `500`

---

### `POST /api/admin/users` · `POST /api/v1/admin/users`

Create a new user account.

**Request body:**

```json
{
  "username": "bob",
  "password": "hunter2",
  "is_admin": false
}
```

**Response `200`:** The created `User` object.

**Status codes:** `200`, `400`, `401`, `403`, `500`

---

### `DELETE /api/admin/users/{id}` · `DELETE /api/v1/admin/users/{id}`

Delete a user account. Admins cannot delete themselves.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `500`

---

### `GET /api/admin/permissions` · `GET /api/v1/admin/permissions`

Return the full permission matrix (sets, users, and permission rows).

**Response `200`:**

```json
{
  "sets": [ { /* Set object */ } ],
  "users": [ { /* User object */ } ],
  "permissions": [
    { "set_id": 1, "user_id": 3, "role": "viewer", "created_at": "2026-01-01T00:00:00Z" }
  ]
}
```

**Status codes:** `200`, `401`, `403`, `500`

---

### `POST /api/admin/permissions` · `POST /api/v1/admin/permissions`

Grant a user access to a set.

**Request body:**

```json
{
  "set_id": 1,
  "user_id": 3,
  "role": "viewer"
}
```

`role` is one of `"owner"` or `"viewer"`.

- `owner` — can upload, soft-delete/restore media, and regenerate thumbnails
- `viewer` — can browse and play media

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `500`

---

### `DELETE /api/admin/permissions` · `DELETE /api/v1/admin/permissions`

Revoke a user's access to a set.

**Request body:**

```json
{ "set_id": 1, "user_id": 3 }
```

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `500`

---

### `POST /api/admin/rescan` · `POST /api/v1/admin/rescan`

Trigger a library rescan. The scan runs asynchronously. Poll
`GET /api/admin/scan-progress` to track progress.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `401`, `403`, `500`

---

### `GET /api/admin/scan-progress` · `GET /api/v1/admin/scan-progress`

Return the current or most recent scan state.

**Response `200`:**

```json
{
  "running": true,
  "current_set": "Movies",
  "sets_total": 5,
  "sets_done": 2,
  "files_total": 1200,
  "files_done": 480,
  "last_error": ""
}
```

**Status codes:** `200`, `401`, `403`, `500`

---

### `GET /api/trash` · `GET /api/v1/trash`

Requires a session. Returns the soft-deleted media array, newest deletion first,
restricted to sets where the requesting user has `owner` permission. Admins see
all sets. A viewer or user with no owned trash receives `[]`. Each item includes
`deleted_at`; expiry is seven days after that UTC timestamp. Restore an item with
`POST /api/media/{id}/restore` (or its `/api/v1/` alias); the same owner/admin
permission is checked again at restore time. Missing/non-regular source files
answer `404`; unsafe or unreadable source paths answer `403`. These failures
preserve the deletion timestamp and trash entry.

### `GET /api/admin/trash` · `GET /api/v1/admin/trash`

List all soft-deleted media items.

**Response `200`:** Array of `Media` objects with `deleted_at` set.

**Status codes:** `200`, `401`, `403`, `500`

---

## Podcasts

Podcasts are stored in a special set with `is_podcast: true`. Each feed gets
its own subfolder inside the shared podcast set.

The server keeps two separate things (see
[podcast-sync-contract.md](../../docs/podcast-sync-contract.md)):

- the **catalog**: feeds and episodes the server fetches, shared by all users
  and governed by the podcast set's permissions;
- each user's **subscriptions** and **episode state** (position and
  completion of each episode): per-account registers keyed by stable feed
  and episode keys (`feed:example.com/rss`, `guid:ep-42`), synchronized
  between clients through `POST /api/v1/podcasts/sync`. Episode state is the
  only listening history of an episode; the server copy's media progress is
  a projection of it.

Feeds and episodes carry stable identities: `feed_key` (the canonical feed
URL without scheme, or `legacy-feed:<n>` for a stored URL that has none) and
`episode_key` (`guid:<guid>`, `enc:<canonical enclosure URL>`, or
`legacy-episode:<n>`). Database ids and server media ids are not identities
and never appear in the sync API. A corrected or permanently redirected
legacy feed key and an ingested legacy episode key are retired into aliases;
clients may continue sending them. A server-observed permanent redirect into
another catalog feed merges the source into the target. Episode merges keep
the older catalog identity. Subscription and episode registers move by their
own clocks, with ties keeping the target value; client evidence never creates
a global alias or merge. Ordinary new redirect/GUID aliases are capped at 16
per identity; inherited merge mappings and retired identities are retained.

---

### `POST /api/podcasts/sync` · `POST /api/v1/podcasts/sync`

Synchronize the caller's podcast subscriptions and episode state (position
and completion). Any signed-in user may call it (cookie or Bearer token); it
reads and writes only the caller's rows. Body limit 2 MiB (`413` beyond it:
halve the page and retry); at most 500 register edits per request, where a
subscription item is one edit and an episode item one or two. The server
advertises it as `podcast_sync` in [`GET /api/v1/config`](#configuration).

**Request body:**

```json
{
  "epoch": "4f1c…",
  "since": 1200,
  "limit": 500,
  "subscriptions": [
    {"edit_id": "q7G9xkP2mW4tR8sZ", "feed_key": "feed:example.com/rss",
     "urls": ["https://example.com/rss"], "fetch_url": "https://example.com/rss",
     "title": "Example", "subscribed": true, "observed_ms": 1792065600000}
  ],
  "episodes": [
    {"feed_key": "feed:example.com/rss", "episode_key": "guid:ep-42",
     "enclosure_url": "https://cdn.example.com/42.mp3",
     "position": {"edit_id": "a1b2c3d4e5f6g7h8", "seconds": 812.4, "observed_ms": 1792065601000},
     "completion": {"edit_id": "b2c3d4e5f6g7h8i9", "completed": true, "source": "auto",
                    "observed_ms": 1792065601000}}
  ]
}
```

- `epoch` and `since` come from the previous response (`next_since`); omit
  both, or send `since` 0, for a full pull. A different `epoch` (database
  replaced) or a `since` beyond the server's newest change makes the server
  answer from 0.
- `limit`: rows per page, default 500, at most 1000.
- `edit_id`: 16–64 bytes of `[A-Za-z0-9_-]`, random per local edit and
  reused for every retry of that edit; a new edit of the same register gets
  a new id.
- `feed_key`: canonical `feed:` key, or a `legacy-feed:<n>` key the server
  minted and still knows. `urls`: at most 10, each must have a feed key.
  `title`: at most 500 characters. `observed_ms`: when the user made the
  edit, UTC milliseconds, `1 … 253402300799999`.
- `subscribed: false` is an unsubscribe: a tombstone kept forever, so a
  stale device cannot resurrect the subscription. It never touches files,
  media, catalog rows or episode history.
- `episodes`: `feed_key` resolves like a subscription's key, also through
  the urls of the caller's subscription register for it (edited in the same
  request or stored), so episode state follows a feed the server knows
  under another key. `episode_key` is `guid:<guid>` (trimmed, at most 1024 bytes,
  else its `guid:sha256:` form), a canonical `enc:` key, or a
  `legacy-episode:<n>` key the server knows. `enclosure_url` is optional
  evidence for resolving the key and must canonicalize. `position` and
  `completion` are each optional, at least one is required; `seconds` is
  `0 … 9e12`, `source` is `manual` or `auto`.
- `"scope": {"feed_key": "…", "episode_key": "…"}` instead of `since` asks
  for the current registers of that one episode only (after applying the
  request's edits), without cursor fields: the request a client sends before
  playing an episode. Sending both `scope` and `since` is a `400`.

Merge rule: every register (subscription, position, completion) keeps the
edit with the greatest `observed_ms`; equal clocks keep the stored value, so
a retried edit changes nothing. Position and completion merge independently:
a completion keeps the saved position, a position save never changes
completion. A clock more than 5 minutes ahead of the server is replaced by
the server's time and the edit is reported in `clamped`.

Episode state needs no server copy and no catalog episode: keys the catalog
does not know are kept as sent (state-only keys) and join the catalog
episode once the server ingests one with that key. When the episode has a
server copy, its media progress is rewritten in the same transaction (see
[Server copies of podcast episodes](#progress)).

A subscribe whose key matches no catalog feed creates a catalog row in the
shared podcast set (no folders, no permissions, no audio). The background
checker fetches its metadata and episodes later; nothing ever downloads
episode audio because of a subscription. At most 20 such rows are created
per user and hour; beyond that the subscription is still stored, and a later
subscribe edit of the same key creates the row. Rows created by non-admin
users are fetched only from public unicast addresses (no loopback, private,
link-local, unique-local, multicast or unspecified ranges, checked on every
connection and redirect hop), and so are their enclosures when downloaded to
the server.

**Response `200`:**

```json
{
  "epoch": "4f1c…",
  "acked": ["q7G9xkP2mW4tR8sZ", "a1b2c3d4e5f6g7h8", "b2c3d4e5f6g7h8i9"],
  "rejected": [{"edit_id": "…", "reason": "invalid_key"}],
  "clamped": [],
  "resolved": [{"kind": "feed", "from": "feed:old.example.com/rss", "to": "feed:example.com/rss"},
               {"kind": "episode", "feed_key": "feed:example.com/rss",
                "from": "enc:cdn.example.com/42.mp3", "to": "guid:ep-42"}],
  "subscriptions": [{"feed_key": "feed:example.com/rss", "subscribed": true,
                     "subscription_ms": 1792065600000, "title": "Example",
                     "fetch_url": "https://example.com/rss", "urls": ["https://example.com/rss"]}],
  "episodes": [{"feed_key": "feed:example.com/rss", "episode_key": "guid:ep-42",
                "position": {"seconds": 812.4, "observed_ms": 1792065601000},
                "completion": {"completed": true, "source": "auto", "observed_ms": 1792065601000}}],
  "aliases": [{"kind": "feed", "from": "feed:old.example.com/rss", "to": "feed:example.com/rss"}],
  "next_since": 1290,
  "has_more": false
}
```

- `acked`: every accepted edit, whether it changed the register or was
  superseded. `subscriptions` and `episodes` hold the current registers of
  every edited key plus the pulled rows; a client whose pending edit id was
  acked replaces its register with the returned one (clock included). An
  episode register that was never set is left out of its row.
- `rejected` reasons: `invalid_key`, `invalid_url`, `invalid_position`,
  `invalid_time`, `invalid_edit`, `quota` (more than 5 000 subscription rows,
  tombstones included, or 200 000 episode state rows). A rejected edit can
  never succeed; drop it. A problem with an episode item's keys or enclosure
  rejects both of its edits; a bad position or completion only that edit.
- `resolved`: keys the server mapped to its canonical key (a feed alias, one
  of the sent `urls`, an episode alias, or an `enc:` key of an episode the
  catalog knows under its GUID). A key that matches nothing is stored as sent.
  If server ingest later makes it an alias, the caller's stored registers
  are rekeyed and merged with canonical state under fresh change sequences.
- Pull: the caller's subscription and episode rows changed after `since`, in
  change order, at most `limit`. `aliases` lists alias rows of the caller's
  feeds and episodes, and every alias of each returned row. Persist
  `next_since` after applying a page; repeat while `has_more`. A scoped
  request returns only the scoped episode (and edited rows) and has no
  `next_since` and `has_more`.

Subscribing to an unknown feed creates a catalog row asynchronously fetched
by the checker, subject to 20 new catalog rows per user per rolling hour.
Beyond that creation quota the subscription is still accepted; the checker
creates the deferred row later without another client edit. Deleting a
catalog row does not free a quota slot. No permissions, folders or audio
files are created by synchronization or deferred catalog creation.

**Status codes:** `200` (also with item rejections), `400` (malformed body,
more than 500 edits, invalid `limit` or `since`, an episode item without
edits, a malformed `scope` or `scope` together with `since`), `401`, `413`,
`500`

---

### `GET /api/podcasts` · `GET /api/v1/podcasts`

List the catalog feeds whose set the caller may read, including feeds the
caller has unsubscribed from (this is the catalog view, not the subscription
view). `feed_key` is the feed's identity; `subscribed` is the caller's own
subscription register (`false` for a tombstone or no subscription). Feeds
merged into another catalog feed are left out.

**Response `200`:**

```json
[
  {
    "id": 1,
    "set_id": 4,
    "feed_key": "feed:example.com/feed.rss",
    "subscribed": true,
    "feed_url": "https://example.com/feed.rss",
    "title": "My Podcast",
    "description": "A great podcast.",
    "image_url": "https://example.com/cover.jpg",
    "last_checked_at": "2026-05-17T06:00:00Z",
    "last_etag": "\"abc123\"",
    "check_interval_minutes": 60,
    "auto_download": false,
    "consecutive_failures": 0,
    "next_check_at": "2026-05-17T07:00:00Z",
    "created_at": "2026-01-10T00:00:00Z"
  }
]
```

**Status codes:** `200`, `401`, `500`

---

### `POST /api/podcasts` · `POST /api/v1/podcasts`

Subscribe to a new podcast feed. Admin only.

**Request body:**

```json
{
  "feed_url": "https://example.com/feed.rss",
  "set_name": "Tech Podcasts"
}
```

`set_name` is optional. When omitted the server uses the feed title.

A feed whose URL has the same feed key as an existing catalog feed (another
spelling, `http` instead of `https`, an alias) reuses that row. The call
also writes the admin's own subscription register (`subscribed: true` at
server time), so the feed has an active subscriber, keeps being refreshed and
reaches the admin's other clients through sync.

**Response `200`:** The created or reused `PodcastFeed` object.

**Status codes:** `200`, `400`, `401`, `403`, `500`

---

### `DELETE /api/podcasts/{id}` · `DELETE /api/v1/podcasts/{id}`

Remove a feed from the server. Admin only. `{id}` is the **feed ID**.
Deletes the feed's downloaded episodes (media rows and files), including
copies under merged catalog rows sharing its canonical feed key. All those
catalog rows and their episodes are deleted only when no user has an active
subscription to it; otherwise they stay (without copies) and keep being
refreshed. Subscriptions and episode history of every user are never touched
(this is not a per-user unsubscribe: use `POST /api/v1/podcasts/sync` with
`subscribed: false` for that). Its folders (the current-title folder and any older-title folders
its episodes were stored in) lose the feed artwork (`cover.jpg`,
`.cover.jpg`) and are removed once empty. Anything else is kept: a folder
another remaining feed uses by title, a folder that still holds other
indexed media (uploads, another feed's episodes), and files that were never
indexed.

Deleting media also deletes every user's notes, bookmarks, progress, favorites
and shares for it. A failed file cleanup is reported as `500`; its media row,
episode link and catalog stay so removal can be retried. Other collected
copies are still attempted. The final catalog deletion checks subscriptions
atomically, so a concurrent subscribe keeps its catalog metadata.

**Response `204`:** No body.

**Status codes:** `204`, `400`, `401`, `403`, `404`, `500`

---

### `GET /api/podcasts/episodes` · `GET /api/v1/podcasts/episodes`

List episodes of the caller's **subscribed** feeds (contract §6.3): one page
of catalog episodes joined with the caller's completion and position
registers, newest first by `published_at` then `episode_key`. Identity is
`feed_key` / `episode_key`, not the shared podcast set id. Omit `feed_key`
for the virtual **All podcast episodes** view; set it (canonical key or an
alias) for one subscription folder. Default filter is `uncompleted`
(started episodes included).

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `feed_key` | Limit to one subscribed feed (optional) |
| `filter` | `uncompleted` (default), `completed`, or `all` |
| `limit` | Page size (default 100, max 500) |
| `offset` | Page offset (default 0) |

**Response `200`:**

```json
[
  {
    "id": 10,
    "feed_key": "feed:example.com/feed.rss",
    "episode_key": "guid:episode-guid-001",
    "feed_title": "My Podcast",
    "title": "Episode 1: Introduction",
    "description": "The first episode.",
    "published_at": "2026-01-05T00:00:00Z",
    "duration_seconds": 3600.0,
    "episode_url": "https://example.com/ep1.mp3",
    "completed": false,
    "completion_source": "",
    "position_seconds": 120.5,
    "server_copy": {"media_id": 42}
  }
]
```

`id` is the catalog row id for download/complete routes that still take it;
it is never an identity. `server_copy` is `null` when there is no readable
non-deleted server media row. Downloaded and undownloaded episodes both
appear here so the web podcast views keep podcast actions.

**Status codes:** `200`, `400` (unknown filter or bad page), `401`, `500`

---

### `GET /api/podcasts/{id}/episodes` · `GET /api/v1/podcasts/{id}/episodes`

List episodes for a podcast feed. `{id}` is the **set ID** (not the feed ID).
Prefer `GET /api/podcasts/episodes?feed_key=…` for subscription folders.

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `limit` | Page size (default 50) |
| `offset` | Page offset (default 0) |

**Response `200`:**

```json
[
  {
    "id": 10,
    "feed_id": 1,
    "media_id": null,
    "guid": "episode-guid-001",
    "episode_key": "guid:episode-guid-001",
    "feed_key": "feed:example.com/feed.rss",
    "title": "Episode 1: Introduction",
    "description": "The first episode.",
    "published_at": "2026-01-05T00:00:00Z",
    "episode_url": "https://example.com/ep1.mp3",
    "duration_seconds": 3600.0,
    "file_size": 52428800,
    "file_name": "ep1.mp3",
    "is_downloaded": false,
    "created_at": "2026-01-05T01:00:00Z",
    "is_completed": false,
    "position_seconds": 0.0
  }
]
```

`media_id` is `null` until the episode is downloaded. `is_completed` and
`position_seconds` are the caller's episode state (the completion and
position registers of `POST /api/v1/podcasts/sync`; `false` and `0` when
never set), so they show what any client recorded, also for episodes the
server never downloaded. `guid` is `""` for an episode without one.

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/podcasts/episodes/{episode_id}/download` · `POST /api/v1/podcasts/episodes/{episode_id}/download`

Trigger a server-side download of a podcast episode. The episode is downloaded
to the podcast set directory and a `Media` row is created.

**Response `200`:** The newly created `Media` object.

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/podcasts/episodes/{episode_id}/complete` · `POST /api/v1/podcasts/episodes/{episode_id}/complete`

Toggle the caller's completion register of a podcast episode: a manual
completion edit with the opposite value, at server time (advanced past the
stored clock, so it always wins). The position is kept, and the progress of
the episode's server copy is updated in the same transaction. The caller must
be able to read the podcast set. A toggle is not retry-safe (a retried
toggle undoes itself); new clients write completion edits through
`POST /api/v1/podcasts/sync` instead.

**Response `204 No Content`** (no body).

**Status codes:** `204`, `400`, `401`, `403`, `404`, `500`

---

## Quick Reference

### Route Summary

| Method | Path (`/api/v1/` prefix) | Auth | Description |
|--------|--------------------------|------|-------------|
| `POST` | `auth/bootstrap` | none | Create first admin account |
| `POST` | `auth/login` | none | Login and receive session cookie |
| `GET` | `—` `/healthz` | none | Liveness probe |
| `GET` | `—` `/readyz` | none | Readiness probe |
| `GET` | `—` `/s/{token}` | none | Share viewer page (free) / JSON (opens a viewing: one use) |
| `POST` | `—` `/s/{token}/view` | none | Open the share page's viewing (one use; idempotent with the viewing cookie) |
| `GET` | `—` `/s/{token}/stream` | none (viewing credential) | Stream shared media |
| `GET`, `HEAD` | `—` `/s/{token}/compat` | none (viewing credential) | Stream transcoded rendition of shared media (range); `HEAD` = readiness probe |
| `GET` | `—` `/s/{token}/thumbnail` | none (viewing credential) | Shared media thumbnail |
| `GET` | `—` `/s/{token}/download` | none (viewing credential) | Download shared media |
| `POST` | `logout` | session | Logout |
| `POST` | `auth/tokens` | session | Mint API token |
| `GET` | `auth/tokens` | session | List API tokens |
| `DELETE` | `auth/tokens/{id}` | session | Revoke API token |
| `DELETE` | `auth/tokens/current` | Bearer | Revoke the presented API token |
| `GET` | `config` | session | Client configuration |
| `GET` | `sets` | session | List sets |
| `GET` | `sets/{id}/browse` | session | Browse set folders |
| `GET` | `sets/{id}/cover` | session | Get set cover image |
| `POST` | `sets/{id}/cover` | session | Regenerate set cover |
| `POST` | `sets/{id}/upload` | session | Upload file to set |
| `GET` | `media` | session | List/search media |
| `GET` | `media/{id}` | session | Get media detail |
| `GET` | `media/{id}/stream` | session | Stream media (range) |
| `GET`, `HEAD` | `media/{id}/compat` | session | Stream transcoded compatibility rendition (range); `HEAD` = readiness probe |
| `GET` | `media/{id}/download` | session | Download media file |
| `GET` | `media/{id}/thumbnail` | session | Get thumbnail |
| `POST` | `media/{id}/thumbnail` | session | Regenerate thumbnail |
| `POST` | `media/{id}/favorite` | session | Toggle favourite |
| `DELETE` | `media/{id}` | session | Soft-delete media |
| `POST` | `media/{id}/restore` | session | Restore from trash |
| `GET` | `media/{id}/playback` | session | Playback hints |
| `POST` | `media/{id}/shares` | session | Create share link |
| `GET` | `media/{id}/shares` | session | List shares for media |
| `GET` | `media/{id}/notes` | session | Get note |
| `POST` | `media/{id}/notes` | session | Upsert note |
| `DELETE` | `media/{id}/notes` | session | Delete note |
| `GET` | `media/{id}/bookmarks` | session | List my saved audio/video positions |
| `POST` | `media/{id}/bookmarks` | session | Create a saved position |
| `DELETE` | `media/{id}/bookmarks/{bookmarkId}` | session | Delete my saved position |
| `GET` | `tags` | session | List tags |
| `POST` | `media/{id}/tags` | session | Add tag |
| `DELETE` | `media/{id}/tags/{tag}` | session | Remove tag |
| `POST` | `progress` | session | Save progress |
| `POST` | `progress/batch` | session | Batch save progress |
| `POST` | `progress/status` | session | Mark finished/not-started |
| `GET` | `in-progress` | session | List in-progress media |
| `GET` | `shares` | session | List my shares |
| `PATCH` | `shares/{token}` | session | Edit share description/expiry |
| `DELETE` | `shares/{token}` | session | Revoke share |
| `GET` | `podcasts` | session | List podcast feeds |
| `POST` | `podcasts` | admin | Subscribe to feed |
| `POST` | `podcasts/sync` | session | Synchronize my podcast subscriptions and episode state |
| `DELETE` | `podcasts/{id}` | admin | Remove feed copies (and the feed when unsubscribed by all) |
| `GET` | `podcasts/episodes` | session | Subscribed / all-episodes view (§6.3) |
| `GET` | `podcasts/{id}/episodes` | session | List episodes by set id (legacy) |
| `POST` | `podcasts/episodes/{episode_id}/download` | session | Download episode |
| `POST` | `podcasts/episodes/{episode_id}/complete` | session | Toggle completion |
| `GET` | `admin/users` | admin | List users |
| `POST` | `admin/users` | admin | Create user |
| `DELETE` | `admin/users/{id}` | admin | Delete user |
| `GET` | `admin/permissions` | admin | List permissions |
| `POST` | `admin/permissions` | admin | Grant permission |
| `DELETE` | `admin/permissions` | admin | Revoke permission |
| `POST` | `admin/rescan` | admin | Trigger rescan |
| `GET` | `admin/scan-progress` | admin | Get scan progress |
| `GET` | `trash` | session | List trash the user may restore |
| `GET` | `admin/trash` | admin | List all soft-deleted media |

### Local audio lyrics

`GET /api/media/{id}/lyrics` (also `/api/v1/media/{id}/lyrics`) uses the
same session/Bearer authentication and set permissions as media playback.
`GET /s/{token}/lyrics` is public and follows thumbnail access: it never
opens a viewing, sets a viewing cookie or spends a share use. A valid viewing
cookie or `?view=` credential keeps it accessible after `max_uses` is reached;
expiry, revocation and deleted media still take effect immediately.

```json
{"source":"sidecar","synced":true,"lines":[{"text":"First line","start":0},{"text":"Next line","start":12.5}]}
```

`source` is `sidecar` or `embedded`. `start` is seconds from the start of the
track, and is omitted for plain text. Synced lines are sorted by time; repeated
LRC timestamps expand to separate lines. LRC `[offset:N]` applies milliseconds
(positive means earlier), clamped at zero. Metadata headers are omitted. When
no usable lyrics exist, including for non-audio media, the result is HTTP 200:
`{"source":"","synced":false,"lines":[]}`.

Only local sources are read, in order: matching filename stem `.ttml`, `.yaml`,
`.yml`, `.elrc`, `.lrc`, `.srt`, `.txt`, then embedded lyrics through hardened
ffprobe. The first usable candidate wins; malformed structured candidates fall
through. Embedded format tags precede stream tags in stream order, with keys
sorted lexically within each group. TTML parallel paragraphs and inherited timing,
Lyricsfile YAML 1.0, Enhanced LRC and SRT project onto the same line contract.
Word timing is omitted; sequential TTML containers are rejected. YAML
`metadata.offset_ms` uses the same positive-earlier convention as LRC.
No database
rescan or external lyrics service is needed. The source and sidecars must be
regular files confined to `MEDIA_ROOT`, including resolved symlinks. Sidecars
and expanded lyrics are limited to 1 MiB; structured depth is limited to 32,
YAML nodes and TTML tokens to 32,768, and structured start times to finite
0–1,000,000,000 seconds. YAML aliases are rejected. ffprobe output is limited
to 2 MiB and runtime
to three seconds. Unreadable, unsafe, oversized and unavailable lyrics return
the same empty result. Text is returned as data; clients must render it as text.

### Downloaded playback synchronization

`POST /api/v1/media/{id}/playback-sync` (also `/api/media/{id}/playback-sync`)
requires the usual cookie or Bearer authentication and access to the media.
An empty object pulls the current user's state; an optional progress edit and
bookmark edits are merged atomically before the full snapshot is returned:

```json
{
  "progress": {"position_seconds": 42, "finished": false, "observed_ms": 1792065600000},
  "bookmarks": [{"sync_key": "random-device-key", "position_seconds": 12,
    "label": "Chapter", "observed_ms": 1792065600000, "deleted": false}]
}
```

`observed_ms` is a positive UTC Unix millisecond timestamp captured when edited,
never when retried or reconnected. Progress and each bookmark key independently
keep the edit with the newest timestamp at millisecond resolution. Equal
timestamps keep the server value. A server bookmark deletion in the same
millisecond as creation advances its clock by one millisecond; a deletion
whose server clock precedes a future client edit leaves that newer edit intact.
Keys are stable strings of 1–100 bytes scoped to the authenticated user and media;
clients generate random keys for offline additions and reuse them for retries.
Bookmarks support audio/video; labels allow at most 200 characters. At most
1000 bookmark edits may be submitted in a request, with a 2 MiB JSON body limit.
Positions must be finite and nonnegative; live bookmarks cannot exceed a known
media duration.

The response has the same shape, with every bookmark including deletion
markers. Clients hide deleted bookmarks and retain the markers so a stale
device cannot resurrect them. Existing bookmark create/delete routes participate
in this state. Resetting progress also retains an edit-time barrier while the
existing media-detail API continues to represent reset progress as absent.
The progress status route accepts optional `observed_at` (RFC3339), allowing
queued `finished` and `not_started` commands to preserve their original edit time.
Without it, status actions use server time as before.

For the server copy of a podcast episode the progress edit goes to the
caller's episode state and the answer is its projection; see
[Server copies of podcast episodes](#progress). An episode copy keeps no
reset barrier of its own: the episode registers decide.

Downloaded synchronization preserves resume/completion state without counting
replayed HTTP requests as additional listening time or play counts. It preserves
existing accumulated listening time until completion or reset. Device-picked
files have no server identity and do not use this endpoint. Android retains
pending edits per origin and account through restarts, copy removal, logout and
server changes, and synchronizes only under that matching unlocked account.
Older download resume positions without edit timestamps remain local until
the next playback save or a server snapshot is available; migrated bookmarks
keep their original creation timestamps.

### Embedded audio metadata (`music`)

Media JSON returned by existing list/detail/in-progress/trash/share routes can
include an additive `music` object for audio. Existing `file_name`, paths, IDs,
stream/download URLs and user-added `tags` keep their current meaning.

```json
{"music":{"title":"A Different Music Title","artist":"A & B; C/D","artists":["A & B; C/D"],"album_artist":"Album Ensemble","album_artists":["Album Ensemble"],"album":"Metadata Album","track":4,"track_total":9,"disc":2,"disc_total":3,"date":"2024-02-29","year":2024,"genres":["Jazz"]}}
```

Missing text/numbers/lists are omitted; `{}` means no supported embedded tags.
`music` may be absent for old rows until a rescan, and for non-audio files.
Titles use the tag or filename without its extension in clients; filenames
remain available in Info/details. The existing plain text search also matches
title, artist and album, subject to the same set permissions and filters.

Scanning refreshes existing audio tags in place, including removed tags, and
preserves IDs, filenames, artwork, counts, progress, favorites and bookmarks.
Unreadable files retain their previously indexed tags. Existing libraries need
an admin rescan to populate tags; each rescan probes audio again to detect tag
edits. Uploads use the same prober. Tags are read only; no file-tag editor is
added. Embedded genres are separate from editable user tags.

Container tags take precedence over audio-stream tags. Common track/disc and
album-artist aliases are recognized; `n/total` values become positive numeric
fields and invalid numbers become unknown. Date is retained verbatim; a valid
year prefix takes precedence over a separate year tag, which supplies a
fallback when the date has no valid year. Artist punctuation (including `;`,
`/`, `&`, commas and `feat.`) never creates extra artists. Actual repeated
FLAC and Ogg Vorbis/Opus artist/album-artist/genre comments are preserved with
a bounded comment reader; ffprobe handles other containers. Structured NUL
values are retained when ffprobe supplies them. ffprobe can flatten repeated
ID3/MP4 values, so those containers currently retain its display string rather
than infer boundaries from punctuation. Original display tags and structured
artist arrays remain separate when both are present. No album/artist catalog
identities or extra API endpoints are introduced in this slice.

### Rich release metadata

M05 adds optional read-only fields in each audio `music` object:
`release_date`, `original_date`, `recording_date`, `release_version`,
`release_types[]`, `labels[]`, `catalog_numbers[]`, `media[]`, `moods[]`,
`grouping`, `comment` and `credits[]` (`{"role":"tape op","names":["A; B","東京"]}`).
These use the same existing media permissions, persistence, scan/clear and
offline snapshot behavior; no tag-writing endpoint is provided.

Release date aliases, in order: `release_date`, `releasedate`, `date`, `year`.
Original date: `original_date`, `originaldate`, `originalyear`. Recording date:
`recording_date`, `recordingdate`, with no original/release fallback. Date text
and precision remain verbatim after trimming; legacy `date`/`year` and catalog
year semantics remain unchanged. Clients label a different legacy date
**Tagged date**, and hide Year only when it repeats the release date's leading
four-digit year.

Version aliases: `release_version`, `albumversion`, `album_version`; release
type: `releasetype`, `release_type`, `musicbrainz_albumtype`, `musicbrainz album
type`; label: `label`, `publisher`, `organization`; catalog number:
`catalognumber`, `catalog_number`, `catalogue_number`; media: `media`,
`mediatype`, `media_type`; mood: `mood`; grouping: `grouping`, `contentgroup`;
comment: `comment`, `description`. Keys are case insensitive, first nonempty
alias wins, and container/stream precedence follows the existing tag parser.

Additional descriptive credits use `conductor`, `lyricist`, `arranger`,
`producer`, `director`, `engineer`, `mixer`, `remixer`, `djmixer` or explicit
`credit:<role>` tags. Custom role suffixes are trimmed/lowercased; participant
names keep punctuation and line breaks. Composer/performer fields retain their
existing roles. Repeated FLAC/Vorbis/Opus release types, labels, catalog numbers,
media, moods and credit comments retain actual boundaries through the existing
bounded comment reader. NUL is the other supported structured separator;
commas, semicolons and slashes never split names. Other containers retain what
ffprobe supplies, with no invented repeated-value boundaries. New credits do
not extend artist-role browsing.

### Music source audio information

Optional `music` fields expose indexed original-file information: `bpm` is
positive beats per minute; `sample_rate` is in Hz; `bit_depth` is bits per sample;
`channels` is the channel count. Technical integer fields come from the first
audio stream. Unknown bit depth is omitted rather than inferred from the
decoder's sample format.

`replaygain_track_gain_db` and `replaygain_album_gain_db` are finite signed dB
values, including zero. `replaygain_track_peak` and `replaygain_album_peak` are
finite nonnegative ratios, where 1 is full scale. Missing or invalid values are
omitted. Tempo aliases are `bpm`, `tbpm`, and `tempo`; ReplayGain tags use their
standard `replaygain_*` names, with an optional dB suffix for gain tags.
These informational fields do not change playback volume. Existing file codec,
bitrate, and size fields retain their meaning; no additional endpoint is needed.

### Disc and compilation metadata

M04 adds optional `music.disc_subtitle` and `music.compilation` (true only).
Subtitle aliases: `discsubtitle`, `disc_subtitle`, `discname`, `disc_name`.
Compilation aliases: `compilation`, `tcmp`, `cpil`; explicit `1`, `true`,
`yes` (case insensitive) enable it; absent/false/unknown values do not.
Embedded album/track artist fields remain unchanged. A flagged compilation
without album artists uses derived **Various Artists** for album grouping and
the album-artist role; real structured track credits remain separate links.
Explicit album artists and valid release MBIDs retain the existing identity rules.

### Music albums

`GET /api/v1/music/albums` (also `/api/music/albums`) returns an array of
caller-visible albums. Optional `artist=ar_<hash>` filters by the linked album
artist and `media_id=N` resolves the album containing an accessible song.
`GET /api/v1/music/albums/{album_key}` (also `/api/music/albums/{album_key}`)
returns one album with `songs`, existing Media objects in disc/track order.
Unknown disc/track numbers follow known numbers; filename/path/id break ties.

Album objects contain `album_key`, `title`, `artist`, `artist_key`, optional
`year`, `from_folder`, optional `cover_media_id`, `song_count`, and `duration`
(seconds). The detail adds `songs` and `discs`: `{number, subtitle?, cover_media_id?, song_ids}`.
Disc number 0 means unknown; descriptors follow canonical song order and
first nonempty subtitle/art wins within each visible disc. Summaries omit discs.
All disc fields derive after permission filtering, including subtitle/art choices.
Artwork uses the ordinary authenticated
media thumbnail endpoint. All counts, artwork and songs derive exclusively
from the caller's permitted non-podcast audio. M4B and sources named as
audiobooks are excluded, consistently with Sets music sequencing.

Album keys use a valid indexed MusicBrainz release UUID (`music.release_mbid`)
when present, otherwise whitespace-normalized lowercase album artist and title
(with track artist/Unknown artist fallback). Year and disc folders do not
split a tagged album. Without an album tag, a set/folder identity supplies the
title and `from_folder: true`. Identical unqualified releases can merge until
future mapping support; keys may change on retag. Artist links currently
filter this album catalog. Artist discography and credit roles are available below.

Both routes require cookie/Bearer auth. Empty catalogs return `[]`; missing,
removed or forbidden detail keys all return `404` with an unavailable message,
without revealing whether another account has that album. Internal read errors
return `500`. Album playback remains client-owned, using existing media IDs,
stream URLs and progress routes; no queue editing or persistence API is added.


### Music artists and credit roles

`GET /api/v1/music/artists` (also `/api/music/artists`) returns caller-visible
artist summaries. `role=artists` (the default) includes track and album artists;
`track_artist`, `album_artist`, `composer` and `performer` select a secondary
credit role. Unknown roles return 400. Composer/performer-only names do not
appear in the default Artists list.

`GET /api/v1/music/artists/{artist_key}` (also `/api/music/artists/{artist_key}`)
returns a credited person's discography. Summaries contain `artist_key`, `name`,
`roles`, distinct `album_count` and distinct `song_count`. Detail adds `albums`,
each with the existing album summary fields and that person's `roles` on the
release. This is the complete visible discography, with explicit role labels;
choosing a role in the list does not hide their other release roles in detail.
Missing and inaccessible artist keys return the same 404 response. Counts,
release links and artwork are derived only after normal set permissions.

Artist detail also adds `top_songs: {source, songs}`. `source` is `personal`
when any visible credited song has qualified personal plays; only those
played songs are returned, ordered by descending `personal_play_count`.
Otherwise `global` ranks visible credited songs with positive `play_count`
(the existing server-wide counter). With no counts, `source` is `none` and
`songs` is `[]`. This local fallback does not use an external popularity
provider. Results contain at most ten distinct canonical media objects,
including composer/performer credits consistently with Play artist. Equal
counts sort by normalized title (filename fallback), then media ID. Ranking,
source selection and truncation happen after ACL filtering. Artist list
summaries omit `top_songs`. Playback uses the existing client queue/session.

Audio `music` adds optional structured `composers` and `performers` arrays from
embedded composer/performer tags. Actual repeated FLAC/Vorbis/Opus comments
remain separate names; punctuation in scalar values is never split. Rescan
updates or clears credits on the same media row. Offline snapshots retain them.
Track and album artists use the existing structured arrays, with the existing
scalar fallback; missing album artists fall back to track artists. Untagged
music uses Unknown artist for common browsing, without invented composer or
performer credits. Artist keys are normalized-name `ar_` hashes and may change
when names are retagged; they do not grant permission.

Album responses add `credits`, the union of tagged people and roles on the
release. Album detail also adds `song_credits`, keyed by media ID, containing
`artist_key`, `name`, `roles` for each linked person on that song. Track and album
artist links use these structured credits individually, independently of the
verbatim display artist. Existing album grouping and media identities stay intact.

### Persistent playlists

All routes require a session cookie or Bearer token and have both `/api` and
`/api/v1` aliases. Playlists default to `visibility: "private"`; owners and
administrators can read/edit them. `visibility: "shared"` makes metadata and
caller-accessible songs readable by every signed-in account on this server.
Private foreign/missing IDs return 404; writes to a readable shared playlist
by a non-owner/non-administrator return 403. Sharing never changes media ACLs
and never creates an anonymous link. Lists return owned/shared documents plus
all documents for administrators. Responses include `owner_id`, `owner_name`,
`visibility`, `can_edit` and `can_transfer` (administrator only).

| Method | Route | Body / result |
| --- | --- | --- |
| GET | `/api/v1/playlists` | List metadata and visible song_count, duration (seconds), cover_media_id |
| POST | `/api/v1/playlists` | `{name, comment, visibility?}`; 201 with new empty playlist |
| GET | `/api/v1/playlists/{id}` | Metadata, ordered entries (`entry_id`, `media_id`), ordered songs and aggregates |
| PATCH | `/api/v1/playlists/{id}` | `{name, comment, visibility?, owner_id?}`; metadata replaced, omitted access fields preserved; membership unchanged |
| DELETE | `/api/v1/playlists/{id}` | 204; deletes document and entries, never media files |
| POST | `/api/v1/playlists/{id}/albums` | `{album_key}` appends the caller-visible album songs in album order |

Names are trimmed and require 1–200 Unicode code points; comments allow
0–2000. Invalid metadata returns 400 without changing the document. Names
need not be unique. Repeated album adds preserve duplicate occurrences with
separate entry IDs. Songs no longer in the caller's permitted Music catalog
are omitted from entries, songs, count, duration and cover. Totals reflect
current source metadata; the cover is the first visible song with artwork.
Only administrators may submit `owner_id` to transfer an existing document to
an existing account. Transfer is atomic with metadata/visibility edits; previous
owner loses write access (and private read access) immediately. A nonadministrator
transfer returns 403; an invalid owner or visibility returns 400 without updates.
Existing playlists migrate to private. Older `{name, comment}` clients preserve
visibility and ownership. Song membership editing is described below; public
links, import and offline playlist caching remain separate features. Export is documented below.

### Edit playlist songs

`POST /api/playlists/{playlist_id}/songs` (also `/api/v1`) requires the
playlist owner or an administrator. Body: `{"action":"add","media_ids":[1,2]}` adds 1–1000
currently permitted music songs in supplied order, including duplicates.
Optional `before_entry_id` inserts before that occurrence; an absent anchor
appends (used by Undo). A saved inaccessible anchor is rejected.
`{"action":"remove","entry_id":"pe_…"}` removes only that occurrence.
Actions `up` / `down` with `entry_id` swap with the adjacent currently
accessible occurrence in the full playlist. An endpoint move is a no-op.
Returns the updated playlist, songs, occurrence IDs and derived totals/cover.
Foreign/missing playlists or entries and inaccessible/nonmusic songs return
404; invalid actions/selection sizes return 400. Each change is atomic and
preserves ACL-hidden saved membership and unrelated concurrent additions.
Song edits do not change source files or an already-playing queue snapshot.

### Metadata-aware music search

`GET /api/v1/music/search` (also `/api/music/search`) accepts optional `q`,
`artist` and `album` text parameters and returns
`{"songs":[],"albums":[],"artists":[]}`. Empty parameters return empty groups.
There is no minimum query length or hidden result cap. Songs use existing Media
objects and catalog/disc/track ordering; albums and artists use existing summaries
without embedded detail lists. Existing media IDs and entity keys retain their
meaning. No file tags are changed.

Search derives the authorized Music catalog first, with its existing podcast,
audiobook and trash exclusions. Matching uses whitespace-normalized Unicode
lowercase literal substrings; accents are significant, and `%`/`_` are literal.
Song matching includes title (filename stem when absent), scalar and structured
track/album artists and album. `artist` and `album` constrain those respective
fields with AND semantics. Album results match album title or credited track/album
artist context; artist results match track/album artist names and, when supplied,
the album constraint on their visible discography. Composer/performer-only names
remain available through existing artist-role browsing. Punctuation never splits
an artist name. All counts and links derive exclusively from caller-visible songs.

Both aliases require cookie/Bearer authentication; read failures use normal API
errors. The route requires the server and does not search downloaded-only music.
The generic `/media` search and existing file filter syntax remain available.

### Music metadata facets (M08)

`GET /api/v1/music/facets` (also `/api/music/facets`) returns arrays for
`genre`, `year`, `role`, `release_type`, `compilation`, `mood`, `grouping`,
and `label`, derived from the authenticated caller's complete authorized music
catalog. No restricted metadata or per-option counts are exposed.

Albums, artists and music search list endpoints accept these optional repeated
query parameters. Dimensions combine with AND on the same song; repeated values
within one dimension combine with OR. Text values match normalized exact tags,
including Unicode case folding and preserved punctuation. `year` accepts a
four-digit year or inclusive range `2020-2029`, using indexed `music.year`.
Unknown years do not match. `compilation=true|false` means Marked / Not marked;
absent metadata is not marked. Role values reuse `track_artist`, `album_artist`,
`composer`, `performer` and the existing `artists` alias. Invalid structured
values return 400; unknown text produces empty matches.

Album and artist result counts describe matching songs. Full detail endpoints
continue returning the full authorized collection; opening or playing an album
therefore includes its full songs. Search with empty text and active facets
returns matching songs/albums/artists. Search song queues contain only matching
songs. Existing generic file filters and public share routes are unaffected.


### Server address identity (M53)

Public `GET /api/server-identity` and `GET /api/v1/server-identity` return
`{"server_id":"<64 lowercase hex characters>"}` with `Cache-Control: no-store`.
No credentials are required. The installation ID persists in the database
across restart; different databases generate different IDs. Database backups
and clones retain their ID. Storage failures return 500; stores without this
capability return 503.

Android uses this credential-free probe to test explicitly paired primary and
local addresses before routing account traffic. A different ID blocks routing.
This public ID detects accidental misconfiguration; it is not cryptographic
authentication or an encryption guarantee. Users must trust both origins;
plain HTTP exposes login details and media to the network. Wi-Fi names are
optional routing hints and never authorize an endpoint. Browser clients remain
on their page origin and use the normal session/authentication behavior.

### Export playlist (M3U8)

`GET /api/playlists/{playlist_id}/export.m3u` (also `/api/v1`) requires a
session/Bearer token and the same playlist read access as detail. Private
foreign/missing playlists return 404; readable shared playlists can be exported
without edit access. The response is `application/vnd.apple.mpegurl;
charset=utf-8`, `Cache-Control: no-store`, with attachment filename
`playlist-{playlist_id}.m3u8`.

The extended M3U8 text is UTF-8 without BOM, LF lines, `#EXTM3U`, `#PLAYLIST:`
name, then ordered `#EXTINF:` integer seconds (`-1` unknown) and artist/title
(filename stem fallback), followed by `./{set.root_path}/{media.rel_path}`.
These are literal local paths relative to `MEDIA_ROOT`; save the playlist
beside the matching library folders when moving it to another tool.
Repeated song occurrences are preserved. Only currently caller-accessible
indexed music is included; deleted/restricted tracks and unsafe paths
(traversal, controls, invalid UTF-8 or backslashes) are omitted without leaking
hidden names/counts. Metadata is collapsed to a single line. Leading `./`
prevents `#` and URI-like paths being read as directives or remote URLs.
An empty playlist yields a valid header-only file. Export neither reads audio
bytes nor verifies whether the indexed source still exists on disk.

The file contains no credentials, signed streaming links, absolute host paths
or access grants. The destination tool needs the corresponding local files;
this is not an unauthenticated streaming playlist. Filesystem import/roundtrip
is a separate M31 feature.

## Saved Music session

`GET /api/v1/queue` and `PUT /api/v1/queue` (legacy `/api/queue`) require the
normal session cookie or Bearer token. Each account has one portable Music
session. GET returns revision `0` and an empty queue before the first save.
Both methods return `ETag: "<revision>"` and `Cache-Control: no-store`.

PUT requires `If-Match: "<observed revision>"`; missing/malformed conditions
return 400. A concurrent/stale write returns 409 with the current complete
snapshot and ETag, without changing it. A retry carrying the last stored
`mutation_id` is acknowledged without advancing its revision again.

```json
{"revision":1,"mutation_id":"client-generated-unique-edit-id","entries":[{"id":1,"media_id":42},{"id":2,"media_id":42}],"current_id":2,"position_seconds":7.25,"label":"Album","repeat":"off","shuffle":false,"canonical":[1,2],"up_next":[]}
```

PUT sends this shape; response `revision` is server-controlled. Maximum 10,000
entries; occurrence and media IDs are positive JSON-safe integers. Occurrence
IDs are unique even when media repeats. Current ID must identify an occurrence,
or be 0 for an empty session (whose position must also be 0). Canonical order
is a permutation of occurrence IDs; Up next is a unique subset. Repeat is
`off`, `all` or `one`; position is finite, nonnegative seconds. Labels are
UTF-8 and at most 2,000 bytes, mutation IDs 1–100 bytes. Every saved entry must
be accessible, undeleted audio; ordinary media permission errors apply.
Clients must revalidate available media before applying a saved queue. Remote
sync never grants playback ownership: Continue is explicit and loads paused;
Resume is the separate play action. Local-only playback and per-file/podcast
progress remain independent.


### Filesystem M3U import and following

`POST /api/v1/playlists/import` (also `/api/playlists/import`) accepts
`{path,base,follow,preview,fingerprint}`. `path` is a server media-root-relative
`.m3u`/`.m3u8` file **inside an accessible set**, e.g. `Music/lists/trip.m3u8`.
No URL fetch or device upload is performed. `base` is explicitly `folder`
(ordinary file-relative M3U) or `library` (media root, for Player M3U8 exports).
Use `preview:true` first; the response has `name`, `available`, `unavailable`,
`rejected`, up to 30 `unavailable_lines`, and `fingerprint`. Missing and
inaccessible tracks are indistinguishable and no unavailable names are returned.
Confirm with the same options, `preview:false` and the returned fingerprint;
changed content/accessibility returns 409 requiring another preview. Success
adds `playlist`, a newly created private playlist. `follow:false` is a one-time
editable import; `follow:true` persists a filesystem binding.

M3U input is bounded to 1 MiB and 10,000 entries, UTF-8 (optional BOM),
LF/CRLF. `#PLAYLIST:` sets the initial name; other comments including EXTINF
are descriptive only. Order/repeats and literal Unicode paths survive. Absolute
paths, URL schemes, backslashes, control characters and library escapes are
skipped as rejected; symlinks and nonregular/unindexed/inaccessible files are
unavailable. Relative `..` is permitted within the library. A leading `./`
disambiguates names beginning with `#` or URI-like text. Files are never fetched.

Follow bindings refresh after successful library rescans, on owner playlist
list/detail reads, or `POST /api/v1/playlists/{id}/source` (owner only).
`DELETE /api/v1/playlists/{id}/source` stops following for an owner/admin while
retaining songs. Both have legacy aliases. Editable playlist responses include
`source:{path,base,status,unavailable,rejected}`; shared readers never receive
this binding/status. `status:unavailable` retains the last imported membership;
a later successful refresh recovers. Equal membership preserves occurrence IDs.
Following disables membership writes (409); metadata/sharing remain manual.
Administrator ownership transfer detaches the source atomically. Source changes
are detected at these refresh points, not by a continuous filesystem watcher.

### Music entity favorites (M34)

`PUT /api/v1/music/favorites/{kind}/{entity_key}` favorites an album, artist or
playlist; `DELETE` unfavorites it. `kind` is `album`, `artist` or `playlist`.
Both have the normal `/api/` alias, require authentication and return
`{"is_favorite":true}` or `{"is_favorite":false}`. Operations are idempotent.
The entity must be currently readable: inaccessible/missing entities return 404,
including private foreign playlists; an unknown kind returns 400. Shared playlist
readers can save their own heart without permission to edit the playlist.

Album, artist and playlist catalog/detail responses add `is_favorite` for the
signed-in account. Their list endpoints accept `favorite=true` to return only
currently readable favorites, preserving existing catalog filters. Entity hearts
are independent of existing song/file hearts. Saved keys for hidden entities are
never returned through a separate favorites index. Offline clients retain the
previous heart and may retry after reconnecting; no offline write queue is added.

### Selectable music streaming quality

Authenticated `GET /api/v1/stream-profiles` (also `/api/stream-profiles`)
returns an array of `{id,label,codec,bitrate_kbps}` audio profiles. The server
currently offers AAC 128/256, MP3 192 and Opus 96 kbps stereo encodes.
`GET|HEAD /api/v1/media/{id}/compat?profile=ID` requests that exact profile for
an accessible audio file, including files that can play directly. Unknown IDs
return 400; non-audio requests return 415. Existing readiness (503/Retry-After),
resource limits, auth/ACL and Range/ETag semantics apply. Each profile has its
own cache identity. Missing encoder support fails explicitly (500); clients
can choose Auto or another profile. Omitting `profile` retains the existing
compatibility-only conversion behavior. `/stream` retains original streaming.
Downloads, public share and Cast compatibility endpoints retain their existing
policy; the quality parameter is only supported on authenticated media compat.

### Personal music ratings

`PUT /api/v1/music/ratings/{kind}/{entity_key}` (also `/api/music/...`) sets
`{"rating": 0..5}` for the current account. Kind is `song`, `album` or `artist`;
songs use the canonical decimal media ID, albums/artists use their catalog key.
Zero clears stars. Hearts remain independent. Current music read permission is
required; missing/inaccessible entities return 404. Invalid/missing values return
400. Writes are idempotent and authenticated through the usual Bearer/cookie path.

Album/artist lists and detail responses and music search songs include `rating`
(0 means unrated); album songs and artist albums include the same saved values.
Music album/artist/search lists accept `rated=true` and `sort=rating` (highest
first). Search applies the filter and order independently to songs/albums/artists;
ratings never change album track/disc playback order. Stars persist across restarts,
are account scoped and follow current permissions, with song/user deletion cleanup.

### Personal music listening (M36)

Music songs, albums and artists add `personal_play_count` and optional
`last_played_at` (UTC RFC3339). Counts belong to the authenticated account;
`play_count` remains the existing global file counter. Albums sum visible songs;
artists sum unique visible credited songs across roles. Music filters recompute
collection totals. Existing global history is not attributed/backfilled.

Existing progress and batch-progress saves qualify once per account/session/song
at 60 accumulated seconds, capped at 12 positive position seconds per save.
`last_played_at` is the qualification time, not the latest resume/status edit.
Resume reset, bookmarks and manual completion do not fabricate a play.

`POST /api/v1/media/{id}/playback-sync` additionally accepts `music_plays`, up
to 100 `{id,observed_ms,accumulated_seconds}` events. `id` is a random 32-character
lowercase hex ID retained by the client across retries; `observed_ms` is the
original qualification time and accumulated seconds must be 60..72. Clients
create events only after the same capped listening threshold, never from a
saved final position. Response `accepted_music_plays` lists acknowledged IDs.
Events are idempotent per account/media/ID and merge in the resume/bookmark
transaction; stale resume clocks do not discard genuine listening events.
Podcast and M4B events are acknowledged without adding music statistics.

### Related local music (M41)

Authenticated `GET /api/v1/music/related/{kind}/{entity_key}` (also `/api/music/...`)
returns up to 12 same-kind suggestions. Kind is `song`, `album` or `artist`; keys
are canonical decimal media IDs or catalog album/artist keys. Each entry contains
`{kind,key,title,basis:[string],cover_media_id?,song?}`. Song entries include the
caller-visible media object. `basis` explains shared local tags; it does not claim
acoustic analysis or use an external provider.

Songs/albums match normalized genres or explicitly credited people; artists match
genres on credited songs. More shared values rank first, then title/key for stable
ordering. Results are distinct and exclude the source. All input metadata and
explanations come from the authenticated account's currently readable music
catalog. Missing metadata and synthetic artist names never establish similarity.
A readable source without matches returns `[]`, an inaccessible/missing source
returns 404, invalid kinds return 400, and library read failures retain the normal
server error response. Clients show Retry without discarding primary details.

### Artist radio and Instant Mix (M42)

Authenticated `GET /api/v1/music/mix/{kind}/{entity_key}` (also `/api/music/...`)
returns `{kind,key,title,context,min_rating,songs:[Media]}`. Kind is `song`,
`album` or `artist`; keys use the same canonical media/catalog identities as
related music. Optional `min_rating` is one integer from 0 through 5 (default 0,
including unrated songs). Invalid kinds or rating parameters return 400.

The server first resolves the account's currently readable catalog, then loads
its personal song ratings. Matching uses shared normalized local genres and
explicit artist credits. Album seeds aggregate their songs' tags; artist seeds
use genres on credited songs and their own credit identity. Synthetic unknown
or compilation artist names cannot establish a match. No external provider or
acoustic analysis is required; this is a finite library queue, separate from
internet radio streams.

The seed song comes first if it has meaningful tags and passes the rating
constraint. Other matches rank by the number of shared values, then ascending
canonical media ID. Songs are distinct by media ID. Rating filtering happens
before ranking and the final 50-song limit. An accessible source with no matches
returns 200 with `songs:[]`; an unavailable or forbidden source returns 404.
Catalog/rating read failures retain normal server error responses. No endpoint
call changes a queue. Clients replace their queue only after a complete,
current-account response and preserve it on lookup failure or no matches.

### Bulk original music downloads

`POST /api/music/downloads` · `POST /api/v1/music/downloads` accepts
`{"media_ids":[1,2,3]}` for the signed-in account. Clients expand albums,
playlists or selections through their normal accessible collection endpoints.
The server rechecks each song's access, rejects non-audio items and removes
repeated IDs, preserving first occurrence order. One request accepts at most
500 IDs and produces at most 512 MiB, including ZIP overhead. Invalid inputs
return 400; oversize archives return 413. Select smaller groups for larger
collections.

The response is an `application/zip` attachment, `Player-music.zip`, containing
original bytes named `<media-id>-<basename>`, with `X-Download-Count` and
`Content-Length`. Originals use the same confined source opener as individual
downloads. A temporary archive is completed before serving: access, source,
read or preparation failures return an error without a partial successful ZIP.
Connection interruption during transfer must be retried; browser clients save
only a complete response. Canceling closes the request and removes temporary
archive data. No server download history or credentials are persisted.

Web Download actions save an archive to the browser's downloads folder. Extract
it for offline playback in a compatible local player; this does not populate
the browser's playback cache. Native Keep offline actions instead save unique
songs sequentially using the existing account-isolated offline repository,
originals for supported formats and compatibility copies when required.
Completed native files survive cancel/failure; retry skips already saved files.
Batches are manual foreground work: keep the tab/app open. They resume through
retry, with no background scheduling, automatic collection syncing or partial
byte resumption.


### Artist and album enrichment

`GET /api/v1/music/enrichment/{kind}/{entity_key}` returns optional external
metadata for an accessible `artist` or `album`. `POST` at the same path explicitly
refreshes metadata; only administrators may refresh because the cache is shared.
Both endpoints also exist below `/api/music/`. Missing/inaccessible identities
return 404; other kinds return 400; viewer refresh returns 403. Every request
rechecks current library access before reading the cache or contacting the agent.

The JSON response includes `enabled`, `can_refresh`, `state` (`disabled`,
`unfetched`, `empty`, `ready`, `error`), `stale`, and optional `description`,
`image_url`, `links: [{label,url}]`, `source`, `fetched_at` and `message`. Text is
plain text. Cached descriptions/links/images are attributed to their source;
`fetched_at` is UTC server time and data older than 30 days is marked stale.
GET never contacts the external agent. Failed POST returns state `error` with
last good data retained; it never overwrites tags or local artwork. Successful
no-match POST returns `empty` and replaces any previous enrichment. Persistence
errors retain normal server errors. Responses use `Cache-Control: no-store`.

`MUSIC_ENRICHMENT_URL` configures an optional compatible Player JSON agent.
Unset disables the section. This is an explicit JSON agent contract, not direct
compatibility with arbitrary external provider APIs. See [M10 configuration,
limits and deterministic local fixture](../../docs/music-m10-implementation.md).


### Queue album order

`POST /api/music/queue-albums` (also `/api/v1/music/queue-albums`) accepts
`{"media_ids":[1,2]}`: at most 10,000 positive safe integer media IDs within
200,000 bytes. The response maps requested accessible music song IDs to
`{"album_key":"al_…","order":0}`. Membership and zero-based disc/track rank
reuse the permission-aware catalog, including compilations and folder albums.
Unrequested, unavailable and noncatalog IDs are omitted. Clients keep these as
independent queue occurrences. This endpoint does not edit or start playback.

Portable saved queues optionally include `shuffle_mode: "tracks" | "albums"`;
legacy or absent values mean tracks. `shuffle` remains the enabled boolean.
The saved occurrence order is authoritative on restore; clients do not reshuffle
or fetch the album map until the user explicitly shuffles again.

### Custom artist and playlist artwork

Both `/api` and `/api/v1` provide:

- `GET /music/artwork/{kind}/{entity_key}`: `{has_artwork,can_edit,revision}`.
- `PUT /music/artwork/{kind}/{entity_key}`: raw JPEG, PNG or GIF bytes, up to
  5 MiB, 4096 pixels per side and 8 million pixels. Fully decoded, normalized
  to PNG (GIF first frame), stripped of metadata; invalid input returns 400.
- `DELETE /music/artwork/{kind}/{entity_key}`: remove custom art and restore
  ordinary cover fallback. PUT/DELETE return the status object above.
- `GET /music/artwork/{kind}/{entity_key}/image`: normalized PNG or 404.

`kind` is `artist` or `playlist`. All requests require a current authenticated
session and current entity access. Global artist writes require administrator;
playlist writes use existing owner/admin `can_edit`. Shared viewers may read.
Protected image responses require revalidation and support content digest ETags;
clients use `revision` to invalidate replaced images. Status responses are
`no-store`. Playlist list/detail payloads include optional `artwork_revision`
without loading image BLOBs. Playlist deletion removes its custom art. Artwork
is independent of source files/tags and optional external enrichment images.

### Save a queue as a playlist

`POST /api/v1/playlists` (also `/api/playlists`) optionally accepts
`media_ids` with 1–10000 current, permitted Music song IDs. For example,
`{"name":"Night drive","comment":"","visibility":"private","media_ids":[7,3,7]}`
creates one private playlist owned by the authenticated account, retaining the
supplied order and every repeated occurrence with separate entry IDs. Visibility
must be omitted or `private`; sharing can be changed after saving. The response
is the ordinary playlist detail with status 201. Without `media_ids`, creation
still makes an empty playlist.

Validation precedes one document-and-membership transaction: an empty/oversized
selection or nonprivate visibility returns 400, and any unavailable, deleted,
inaccessible or nonmusic ID returns 409 without creating a partial document.
`media_ids` is rejected on playlist update/album-add requests. Subsequent detail
reads omit songs that later become unavailable using the normal playlist access
rules. Saving never changes the playing queue, current occurrence or position.
Clients capture one snapshot when opening the naming dialog; connection errors
retain that snapshot and name. If a response is lost after creation, check the
playlist list before retrying to avoid creating a second copy.

### Playlist duplicate choices and memberships

`GET /api/v1/playlists/membership` (also `/api`) returns readable playlist views
with ordered `entries` containing only currently accessible music identities;
`songs` is empty. Private foreign lists and inaccessible song/occurrence IDs are
omitted. Source bindings remain visible only to editors. This is a snapshot;
permissions and duplicate membership are checked again when writing.

`POST /api/v1/playlists/songs` (also `/api`) accepts
`{"playlist_ids":["pl_a","pl_b"],"media_ids":[1,2],"duplicate_policy":"skip"}`.
Select 1–100 targets and 1–1000 songs. Policy is required: `skip` omits songs
already saved in each target, including duplicates within the request; `add`
retains every requested occurrence in order. Target IDs are deduplicated.
All target writes commit together; success is 204. The caller must own each target
or be an administrator; shared readers cannot write (403), private foreign/missing
lists or inaccessible/nonmusic songs return 404, source-following or changed
noneditable targets return 409. Bad limits/policy return 400.
Legacy per-playlist song edits, album adds and queue-to-new-playlist creation keep
their occurrence-preserving behavior.

### Public music collection shares

`POST /api/v1/shares/collections` (also `/api/shares/collections`) accepts
`{ "title":"Album title", "media_ids":[7,3,7], "expires_at":"...", "max_uses":1 }`.
Title is 1–200 characters and IDs contain 1–1000 currently accessible audio
occurrences. Expiry and maximum viewings use ordinary share defaults/validation;
success is 201 with the normal share object plus `collection_title` and ordered
`media_ids`. Any unavailable/inaccessible/non-audio source rejects creation.
Clients resolve album, artist, playlist or selection order before creating it.
This is a fixed snapshot retaining repeated occurrences, not a live playlist.

Existing `/s/{token}` HTML/JSON, viewing-cookie and JSON `view` credential rules
apply once for the entire collection. JSON adds `collection_title` and ordered
`items`, each containing the ordinary public display metadata and token-scoped
URLs under `/s/{token}/items/{zero_based_index}/stream|compat|thumbnail|download`.
Every item URL is credentialed in JSON and checks expiry/revocation, bounds and
current creator access. Public IDs are collection-local occurrence numbers;
source/library IDs and paths are excluded. Unavailable sources retain a generic
`unavailable:true` placeholder without title/artwork/content URLs; remaining
occurrences still play. A deleted first source does not delete a collection link.
Original download and compatibility-rendition behavior matches single-file
shares. Collection shares appear in My Shares and use the existing owner/admin
revoke endpoint, even when every source becomes unavailable. Legacy single-file
links retain their existing response shapes and source-deletion lifecycle.

### Listening history

`GET /api/music/history` and `/api/v1/music/history` return this signed-in
account's newly qualified music plays, newest play time first. History starts
with qualifications after this feature is installed; existing personal counters
are not backfilled. A play requires the existing 60 counted listening seconds;
seeks earn at most 12 seconds per observation. Distinct replay occurrences are
separate events. Podcast episodes, audiobooks, public shares and device-only
files are excluded. Current media permissions and trash status are checked before
pagination; session identifiers and other users' history are never returned.
Responses have `Cache-Control: no-store`.

```json
{"settings":{"recording":true,"retention_days":365},"events":[{"id":12,"media_id":7,"played_at":"2026-10-23T12:00:00Z","title":"Song","artist":"Artist"}],"next_before_id":12,"next_before_ms":1792756800000}
```

Pages contain up to 50 events. Pass both returned `before_id` and `before_ms`
for the next page; equal timestamps use event ID as a stable tiebreaker.
`PUT` on the same route requires `recording` (boolean) and `retention_days`
(`30`, `90`, `365`, or `0` for forever). The default is recording enabled,
one year. Shortening retention permanently erases older history; increasing it
cannot restore erased events. `DELETE` clears this account's history (204).
Clear and recording transitions preserve a monotonic cutoff, so delayed offline
plays from before clear or the paused interval cannot appear afterward.
Retention pruning runs on qualification, history reads/settings changes, and
startup. Personal qualification/counter and retry-identity records remain;
history controls do not erase counters or resume positions. Account/media
deletion removes the associated records through the existing ownership cleanup.

Progress saves and batch rows accept optional `listening_session_id`, a
16–80-character ASCII alphanumeric/underscore/hyphen identifier generated once
per playback occurrence and preserved on retries. This affects personal music
qualification only; legacy global counters retain their existing contract.
Explicit occurrence samples can qualify even when a newer resume edit exists.
Clients must send enough sequential position samples to meet the threshold;
resending a single final position does not synthesize a play. Batch bodies may
include `expected_user_id`; a mismatch returns 409 before any writes, protecting
persisted browser samples after an account switch. Old clients without occurrence
IDs retain their original per-authentication-session qualification behavior.
Android downloaded music keeps the existing durable `playback-sync.music_plays`
IDs, so it is not also qualified through streamed-progress IDs.

### Music download quality

`POST /api/music/downloads` (and `/api/v1/music/downloads`) accepts optional
`profile`: `original` (default), `aac128`, `aac256`, `mp3192`, or `opus96`.
The chosen profile is frozen for the archive request independently of streaming
preferences. Each prepared song is copied immediately into the atomic ZIP before
preparing the next; names use the actual output container extension. There is
no fallback to Original for an unavailable selected profile. Existing song-count,
byte, permission and cancellation limits still apply. Preparation is bounded to
five minutes per song; busy or still-preparing work can return 503 for retry,
and no partial archive is served. Android keeps the chosen profile, response
MIME type and source filename with each new offline copy; older copies retain
unknown quality. Changing the preference does not replace existing copies.

### Album discovery collections

`GET /api/music/albums` and `/api/v1/music/albums` accept optional `collection`:
`recently_added`, `recently_played`, `most_played`, `random`, `favorites`, or
`top_rated`; an unknown value returns 400 and an empty collection returns `[]`.
Existing filters still apply; album-detail routes keep their detail response.
Album payloads add `added_at`, the latest creation date among their currently
visible music files. Play-based collections use the account's qualified personal
counts and latest play date; favorites and ratings use its album heart and stars.
No-play and unrated albums are excluded from their corresponding collections.
Ties use normalized title and canonical key. Random shuffles every accessible
album once. ACL filtering and personal-field hydration precede collection
selection. Client previews show a subset while Play/View all use the complete
ordered snapshot; ordinary album queue resolution rechecks current access.

### Internet radio

Authenticated `GET /api/radio` (and `/api/v1/radio`) returns shared stations.
Administrators create with `POST /api/radio`, replace with
`PUT /api/radio/{station_id}`, and remove with `DELETE` on that station route.
Fields are `name`, `stream_url`, `homepage_url`, `artwork_url` and
`local_network` (default false); optional URLs may be empty. HTTP(S) URLs must
not contain userinfo. Local/private access requires the explicit admin opt-in;
link-local, metadata, multicast and reserved destinations remain refused.
Redirects and the actual dial address are revalidated. Player credentials are
never forwarded to stations.

Authenticated `GET /api/radio/{station_id}/stream` relays live MP3 through a
bounded, pipe-only FFmpeg decoder. Playlists cannot fetch secondary resources.
It is a live source: no finite duration, seek, resume history or queue auto-next.
`HEAD` is bodyless and does not reserve a decoder. Four concurrent decoders are
allowed; capacity returns 503. Startup and idle input have a 30-second watchdog,
and output chunks have a renewed bounded write budget rather than an absolute
whole-stream lifetime. `/artwork` returns a bounded decoded JPEG, never active
external markup. Missing stations return 404; unsafe/unreachable/undecodable
sources report a visible failure and can be retried. No decoder/platform support
is inferred from API tests; actual Chrome/Firefox/Android verification is pending.

### Smart playlist rules

`PUT /api/playlists/{playlist_id}/rules` (also `/api/v1`) saves the owner's or
an administrator's dynamic rules. Shared read-only viewers receive 403 on this
write; private foreign/missing playlist IDs receive 404. Ordinary playlist
reads, collection playback, queue actions and M3U8 export evaluate current rules
against the requesting viewer's currently accessible Music songs. Podcast,
audiobook, trashed and inaccessible media stay excluded by the Music catalog.
Rules referencing personal data use the **viewer**, so shared smart playlists
may have different songs and totals for each account.

Example request (all values in conditions are scalar strings):

```json
{
  "rules": {
    "match": "all",
    "conditions": [
      {"field": "genre", "operator": "contains", "value": "Synth"},
      {"field": "rating", "operator": "gte", "value": "4"},
      {"field": "plays", "operator": "lte", "value": "10"}
    ],
    "sort": "added",
    "descending": true,
    "limit": 100
  }
}
```

`match` is `all` (AND) or `any` (OR), with 1–20 conditions and values at most
300 Unicode characters. Unknown fields/operators and invalid numeric/date
values return 400. Every rule applies to the same song.

| Field | Operators | Value and meaning |
| --- | --- | --- |
| `title`, `artist`, `album`, `genre` | `contains`, `is` | Nonblank text; case-insensitive, trimmed; artist/genre match any structured value. Untagged titles use the filename stem. |
| `year` | `eq`, `gte`, `lte` | Integer 0–9999; 0 means unknown embedded year. |
| `plays` | `eq`, `gte`, `lte` | Integer 0–1000000000; viewer's qualified personal plays, distinct from global file play_count. |
| `rating` | `eq`, `gte`, `lte` | Viewer's personal song rating, integer 0–5; 0 means unrated. |
| `starred` | `is` | String `true` or `false`; viewer's file favorite. |
| `added`, `last_played` | `before`, `after` | `YYYY-MM-DD` UTC calendar date. Before excludes that day; after starts at the following UTC midnight. `added` is file CreatedAt, not release date. `last_played` is viewer's last qualified play; never-played songs do not match date rules. |
| `added`, `last_played` | `within_days` | Integer 1–36500; inclusive rolling UTC interval `[now − N days, now]`, excluding future dates. |

`sort` is `title`, `artist`, `album`, `year`, `plays`, `rating`, `added` or
`last_played`; `descending` defaults to false. Unknown last-play dates sort as
missing dates before known dates in ascending order. Equal values use ascending
media ID regardless of direction. `limit` must be 1–1000, applied after ACLs,
matching and sorting. Results refresh on each read; there is no persisted
result snapshot or background refresh delay. Queueing captures the read's
current ordered songs, preserving normal independent queue behavior.

A 200 response is the normal playlist view with additive `rules`, current
ordered songs/entries and viewer-visible totals/cover. Smart occurrence IDs
are stable per playlist/song, not editable saved membership IDs. List responses
retain `rules` and evaluated totals while omitting songs/entries as usual.

`{"rules":null}` disables rules and restores the untouched saved manual
membership. Enabling rules retains existing manual entries; it never overwrites
them. Manual album/song adds, batch adds, duplicate handling and occurrence
removal/reorder return 409 while rules are active, including repository guards
against races. Filesystem-following playlists must stop following before rule
writes (409); filesystem imports create their own playlists and cannot replace
smart results. Name/comment/visibility, owner/admin editing, sharing and delete
keep their ordinary playlist semantics. Changing owner preserves rules and the
new viewer's personal conditions still apply on reads.

## Live music listeners

`GET`, `PUT` and `DELETE /api/v1/music/presence` (also `/api/music/presence`)
require the normal authenticated cookie session or bearer token. Every request
must include `?expected_user_id=<authenticated-user-id>`; missing, malformed or
mismatching identity returns 409. All responses use `Cache-Control: no-store`.
Public share credentials cannot access these routes.

Sharing is an explicit per-client opt-in, off by default. Opening the Listeners
panel only reads current listeners. Clients reset sharing off after account or
server changes and page/app restart. Only current locally played authenticated
server-library music is reported: exclude device files, downloaded/offline
playback, public shares, radio, detached windows, Cast and network speaker
output. Ordinary native and decoded/gapless web library transports are supported.
Presence never writes playback progress, personal/global counts or history.

`PUT` sends a heartbeat while sharing, normally every 10 seconds and after
source/play/pause/seek changes:

```json
{
  "client_id": "d2accfab061ca7d97fa93d557a840009",
  "sequence": 1,
  "client": "web",
  "media_id": 42,
  "state": "playing",
  "position_seconds": 32.5
}
```

`client_id` is an opaque random ID generated for this tab/app instance, 16–80
ASCII letters, digits or hyphens. `sequence` is a positive integer increasing
across that client's heartbeats and removals; equal/older sequences are ignored
with 204. `client` accepts only `web` or `android`; `state` accepts only
`playing` or `paused`. `media_id` must identify a current music song accessible
to the publishing account (normal music scope excludes podcast/audiobook/trash
items). Positions must be finite and nonnegative, and are clamped to current
song duration (zero if duration is unavailable). Invalid input returns 400;
unavailable/inaccessible music returns 404. Successful PUT returns 204.

`DELETE` disables this client's presence and returns 204:

```json
{"client_id":"d2accfab061ca7d97fa93d557a840009","sequence":2}
```

Removals retain a sequence tombstone for 45 seconds so an older delayed
heartbeat cannot restore sharing. Presence and tombstones expire after 45
seconds without an accepted update. Storage is synchronized process-local
memory only; restart clears it. At most eight client records per account and
256 total records, including unexpired tombstones, are retained. A new client
over either limit returns 429 and can retry after expiry. Clients should bound
request timeouts below the expiry interval and never queue offline heartbeats.

`GET` returns an array; an empty result is `[]`:

```json
[
  {
    "username":"alice",
    "client":"Android",
    "media_id":42,
    "title":"First Light",
    "artist":"Aurora Lane",
    "state":"paused",
    "position_seconds":32.5,
    "duration":194,
    "updated_at":"2026-10-23T12:00:00Z"
  }
]
```

The server reads fresh viewer and publisher music permissions, publisher
existence/username, and originating session/API-token validity and expiry.
Only songs currently accessible to both accounts appear, with fresh server
metadata; no hidden-song placeholders or counts expose unauthorized listeners.
Publisher permission loss or credential revocation suppresses and tombstones
that entry until a fresh authorized heartbeat. Repeated clients from the same
publisher reuse that publisher's catalog within one GET only. Raw client IDs,
credential/session IDs and device names are never returned. Progress is the
last reported position, not an estimate of elapsed playback. Clients clear rows
on failed reads, account/server changes and logout, and reject late responses
from a previous identity. Disconnected clients disappear at the 45-second
boundary; stopping/unsupported sources and explicit opt-out send DELETE.
