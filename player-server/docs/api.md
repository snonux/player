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

Every session-required endpoint accepts credentials via **either** of two
mechanisms, checked in this order:

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

### Auth precedence in middleware

`RequireSession` (in `internal/api/middleware.go`) tries Bearer first, then
falls back to the session cookie. If neither is present or valid it returns
`401 Unauthorized`. HTML-page requests (browsers sending `Accept: text/html`)
are redirected to `/login.html` instead.

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

### `GET /api/podcasts/{id}/episodes` · `GET /api/v1/podcasts/{id}/episodes`

List episodes for a podcast feed. `{id}` is the **set ID** (not the feed ID).

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
| `DELETE` | `shares/{token}` | session | Revoke share |
| `GET` | `podcasts` | session | List podcast feeds |
| `POST` | `podcasts` | admin | Subscribe to feed |
| `POST` | `podcasts/sync` | session | Synchronize my podcast subscriptions and episode state |
| `DELETE` | `podcasts/{id}` | admin | Remove feed copies (and the feed when unsubscribed by all) |
| `GET` | `podcasts/{id}/episodes` | session | List episodes |
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

Only local sources are read, in order: matching filename stem `.lrc`, `.txt`,
then embedded format/stream lyrics tags through hardened ffprobe. No database
rescan or external lyrics service is needed. The source and sidecars must be
regular files confined to `MEDIA_ROOT`, including resolved symlinks. Sidecars
and expanded lyrics are limited to 1 MiB; ffprobe output to 2 MiB and runtime
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
`/api/v1` aliases. Playlists are private to their owning account, including
from administrators. Foreign or missing IDs return 404.

| Method | Route | Body / result |
| --- | --- | --- |
| GET | `/api/v1/playlists` | List metadata and visible song_count, duration (seconds), cover_media_id |
| POST | `/api/v1/playlists` | `{name, comment}`; 201 with new empty playlist |
| GET | `/api/v1/playlists/{id}` | Metadata, ordered entries (`entry_id`, `media_id`), ordered songs and aggregates |
| PATCH | `/api/v1/playlists/{id}` | `{name, comment}` replaces both metadata fields; membership unchanged |
| DELETE | `/api/v1/playlists/{id}` | 204; deletes document and entries, never media files |
| POST | `/api/v1/playlists/{id}/albums` | `{album_key}` appends the caller-visible album songs in album order |

Names are trimmed and require 1–200 Unicode code points; comments allow
0–2000. Invalid metadata returns 400 without changing the document. Names
need not be unique. Repeated album adds preserve duplicate occurrences with
separate entry IDs. Songs no longer in the caller's permitted Music catalog
are omitted from entries, songs, count, duration and cover. Totals reflect
current source metadata; the cover is the first visible song with artwork.
Song membership editing is described below. Sharing, import/export and an
offline playlist cache remain outside this API slice.

### Edit playlist songs

`POST /api/playlists/{playlist_id}/songs` (also `/api/v1`) requires the
session owner. Body: `{"action":"add","media_ids":[1,2]}` adds 1–1000
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
