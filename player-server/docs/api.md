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
11. [Progress](#progress)
12. [Shares](#shares)
13. [Tags](#tags)
14. [Admin](#admin)
15. [Podcasts](#podcasts)

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
`Referrer-Policy: no-referrer`.

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
`transcoded` is `true` (AVI, WMV, FLV, WMA and other legacy codecs), in which
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
once, before it requests any media, thumbnail or download.

It is a `POST` because nothing that merely looks at a link sends one.

**Response:** `204 No Content`

**Status codes:** `204`, `404` (unknown or revoked share), `410` (share
expired, or no use left and no valid viewing credential), `500`

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

Return client configuration. Currently exposes the server-side page size so
clients can paginate consistently.

**Response `200`:**

```json
{ "media_page_size": 100 }
```

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
| `search` | string | Plain-text search on file name / path |
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

### `GET /api/media/{id}/thumbnail` · `GET /api/v1/media/{id}/thumbnail`

Return the thumbnail image for a media item (JPEG). Sets `Cache-Control:
no-cache`.

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/media/{id}/thumbnail` · `POST /api/v1/media/{id}/thumbnail`

Regenerate a media item's thumbnail using ffmpeg. Requires `owner` permission.

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

Soft-delete a media item. The item is moved to trash and is no longer returned
by `GET /api/media`. Requires `owner` permission or admin.

**Response `200`:**

```json
{ "status": "ok" }
```

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/media/{id}/restore` · `POST /api/v1/media/{id}/restore`

Restore a soft-deleted media item from trash. Requires `owner` permission or
admin.

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

## Progress

Playback progress tracks the last known position for each media item per user.
Progress also drives the 60-second accumulator that increments `play_count`.

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

### `GET /api/admin/trash` · `GET /api/v1/admin/trash`

List all soft-deleted media items.

**Response `200`:** Array of `Media` objects with `deleted_at` set.

**Status codes:** `200`, `401`, `403`, `500`

---

## Podcasts

Podcasts are stored in a special set with `is_podcast: true`. Each feed gets
its own subfolder inside the shared podcast set.

---

### `GET /api/podcasts` · `GET /api/v1/podcasts`

List all subscribed podcast feeds visible to the authenticated user.

**Response `200`:**

```json
[
  {
    "id": 1,
    "set_id": 4,
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

**Response `200`:** The created `PodcastFeed` object.

**Status codes:** `200`, `400`, `401`, `403`, `500`

---

### `DELETE /api/podcasts/{id}` · `DELETE /api/v1/podcasts/{id}`

Unsubscribe from a podcast feed. Admin only. `{id}` is the **feed ID**.
Deletes the feed, its episodes, and its downloaded episodes (media rows and
files). Its folders (the current-title folder and any older-title folders
its episodes were stored in) lose the feed artwork (`cover.jpg`,
`.cover.jpg`) and are removed once empty. Anything else is kept: a folder
another remaining feed uses by title, a folder that still holds other
indexed media (uploads, another feed's episodes), and files that were never
indexed.

Deleting media also deletes every user's notes, progress, favorites and
shares for it. After the feed is deleted, the remaining steps are
best-effort: a failure to remove a file is reported as `500`, but the feed is
already gone (a retry returns `404`) and the other items were still
processed.

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
`position_seconds` are per-user.

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/podcasts/episodes/{episode_id}/download` · `POST /api/v1/podcasts/episodes/{episode_id}/download`

Trigger a server-side download of a podcast episode. The episode is downloaded
to the podcast set directory and a `Media` row is created.

**Response `200`:** The newly created `Media` object.

**Status codes:** `200`, `400`, `401`, `403`, `404`, `500`

---

### `POST /api/podcasts/episodes/{episode_id}/complete` · `POST /api/v1/podcasts/episodes/{episode_id}/complete`

Toggle the per-user completion state of a podcast episode.

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
| `GET` | `admin/trash` | admin | List soft-deleted media |
