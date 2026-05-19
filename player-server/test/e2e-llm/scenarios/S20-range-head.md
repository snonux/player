---
id: S20
title: "HTTP Range and HEAD requests for media stream, download and thumbnail"
tags: [media, api, streaming, range, head, http]
preconditions:
  server_state: running        # server running with admin account and at least one audio media item
  fixtures: []
assertions:
  - status_code: "GET /api/v1/media 200"
  - status_code: "HEAD /api/v1/media/{id}/stream 200"
  - status_code: "GET /api/v1/media/{id}/stream 206"
skip: false
---

# Purpose

This scenario locks in HTTP-level Range and HEAD semantics for the three file
endpoints (`/stream`, `/download`, `/thumbnail`). Correct Range handling is
mandatory for iOS audio playback — Safari issues a `Range: bytes=0-1` probe
before every audio element, and a server that returns 200 instead of 206, or
omits `Accept-Ranges`, causes silent playback failure.

# Server-side notes

The relevant handler is `serveFileResult` in
`player-server/internal/api/handlers.go`. For the non-remuxed (direct file)
path, it does the following before calling `http.ServeContent`:

- Sets `Content-Type` from the file extension via `mediatype.MIMETypeForExt`.
- Sets `Accept-Ranges: bytes` explicitly.
- For the `/download` path only, sets
  `Content-Disposition: attachment; filename="..."`.
- For `/thumbnail` it also sets `Cache-Control: no-cache`.

It then calls `http.ServeContent(w, r, fileName, modTime, file)` which, per
the Go standard library:

- Honors the request method — `HEAD` returns headers (including
  `Content-Length`) and no body, with status 200.
- Sets `Last-Modified` from `stat.ModTime()`.
- Parses the `Range` header. A valid single range returns 206 with
  `Content-Range: bytes <start>-<end>/<size>` and a body of exactly
  `end - start + 1` bytes.
- Multi-range requests (e.g. `bytes=0-99,100-199`) return 206 with
  `Content-Type: multipart/byteranges; boundary=...` and the requested
  ranges concatenated as MIME parts.
- An entirely unsatisfiable Range (start past EOF) returns 416 with
  `Content-Range: bytes */<size>` and an empty body.
- A syntactically malformed `Range` header (e.g. `bytes=garbage`, no `=`,
  no digits) is treated as "no Range" — the server returns the full body
  with status 200, NOT 416. This matches RFC 7233 §3.1.
- `If-Modified-Since` matching the `Last-Modified` returns 304. The handler
  also emits a strong `ETag` header derived from file size and mtime, so
  `If-None-Match` matching that ETag returns 304 too. Both conditional
  paths must work — they're the basis of iOS/podcast-client revalidation.

If any of those server-side facts have changed when this scenario runs,
flag it: every one of them affects iOS / podcast-app playback.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Find an audio media item: call `GET /api/v1/media?type=audio&limit=1` with
   the session cookie. Confirm the response is HTTP 200 and the JSON body
   contains at least one media object whose `type` field is `audio`. Save the
   `id` of the first item as `media_id`. If the response is empty, fail with a
   note that `MEDIA_ROOT` does not contain any audio fixtures — the rest of
   the scenario cannot run.

3. HEAD the stream: issue `HEAD /api/v1/media/{media_id}/stream` with the
   session cookie. Confirm the response is HTTP 200, the `Content-Length`
   response header is present and parses as an integer greater than zero,
   the `Accept-Ranges` response header equals exactly `bytes`, the
   `Content-Type` response header starts with `audio/`, and the response
   body is empty (zero bytes). The presence of `Accept-Ranges: bytes` is
   load-bearing: iOS Safari refuses to play media without it.

4. HEAD the download: issue `HEAD /api/v1/media/{media_id}/download` with the
   session cookie. Confirm the response is HTTP 200, the
   `Content-Disposition` response header is present and contains the
   substring `attachment` (typically `attachment; filename="..."`), the
   `Content-Length` response header is present, and the response body is
   empty. Save the integer value of `Content-Length` as `file_size` — it is
   used in step 9 to construct an out-of-range request.

5. HEAD the thumbnail: issue `HEAD /api/v1/media/{media_id}/thumbnail` with
   the session cookie. Confirm the response is HTTP 200, the `Content-Type`
   response header starts with `image/` (e.g. `image/jpeg`, `image/png`,
   `image/webp`), the `Cache-Control` response header equals `no-cache`,
   and the response body is empty.

6. GET stream with a 100-byte head range: issue
   `GET /api/v1/media/{media_id}/stream` with the session cookie and an
   additional `Range: bytes=0-99` request header. Confirm the response is
   HTTP 206 (Partial Content), the `Content-Range` response header is
   present and matches the pattern `bytes 0-99/<file_size>`, the
   `Content-Length` response header equals `100`, and the response body is
   exactly 100 bytes long. Save the body bytes as `head_bytes` for the next
   step's offset cross-check.

7. GET stream with a 100-byte mid range: issue
   `GET /api/v1/media/{media_id}/stream` with the session cookie and an
   additional `Range: bytes=100-199` request header. Confirm the response is
   HTTP 206, the `Content-Range` response header matches
   `bytes 100-199/<file_size>`, the `Content-Length` response header equals
   `100`, and the response body is exactly 100 bytes long and is NOT equal
   to the `head_bytes` saved in step 6 (otherwise the server is returning
   the file head regardless of the requested offset — a Range bug).

8. GET stream with a suffix range (last 100 bytes): issue
   `GET /api/v1/media/{media_id}/stream` with the session cookie and an
   additional `Range: bytes=-100` request header. Confirm the response is
   HTTP 206, the `Content-Range` response header ends with
   `/<file_size>` and the start offset equals `file_size - 100`
   (i.e. matches `bytes <file_size-100>-<file_size-1>/<file_size>`), and
   the response body is exactly 100 bytes long.

9. GET stream with an open-ended range from offset 100: issue
   `GET /api/v1/media/{media_id}/stream` with the session cookie and an
   additional `Range: bytes=100-` request header. Confirm the response is
   HTTP 206, the `Content-Range` response header matches
   `bytes 100-<file_size-1>/<file_size>`, and the response body length
   equals `file_size - 100`.

10. GET stream with a Range start past EOF: issue
    `GET /api/v1/media/{media_id}/stream` with the session cookie and an
    additional `Range: bytes=999999999-` request header. Confirm the
    response is HTTP 416 (Range Not Satisfiable) and the `Content-Range`
    response header is present and equals `bytes */<file_size>`. This
    proves Go's `http.ServeContent` rejects unsatisfiable ranges instead
    of silently truncating.

11. GET stream with a malformed Range header: issue
    `GET /api/v1/media/{media_id}/stream` with the session cookie and an
    additional `Range: bytes=garbage` request header. Per RFC 7233 §3.1
    and the Go stdlib implementation, an unparseable Range is treated as
    "no Range" — confirm the response is HTTP 200, the `Content-Length`
    response header equals the full `file_size`, the `Accept-Ranges`
    response header equals `bytes`, and no `Content-Range` response
    header is present. If the server returns 416 here instead, flag it as
    a deviation from Go's `http.ServeContent` behaviour (an explicit
    upstream change would have been required).

12. GET stream with a multi-range request: issue
    `GET /api/v1/media/{media_id}/stream` with the session cookie and an
    additional `Range: bytes=0-99,200-299` request header. Confirm the
    response is HTTP 206 and the `Content-Type` response header starts
    with `multipart/byteranges; boundary=`. The body is a MIME multipart
    document containing two parts; full parsing is out of scope, but the
    body MUST be larger than 200 bytes (two 100-byte ranges plus MIME
    boundaries and per-part headers). If the server collapses this to a
    single 200 OK with the full file, flag it — some clients (notably
    iTunes / Music.app) emit multi-range requests.

13. GET stream with `If-Modified-Since` matching the file's
    `Last-Modified`: first issue a plain `GET /api/v1/media/{media_id}/stream`
    with the session cookie and no `Range` header. Confirm the response is
    HTTP 200 and save the `Last-Modified` response header verbatim as
    `last_modified`. Then issue a second
    `GET /api/v1/media/{media_id}/stream` with the session cookie and an
    additional `If-Modified-Since: <last_modified>` request header.
    Confirm the response is HTTP 304 (Not Modified) and the response body
    is empty. This proves the conditional-GET path through
    `http.ServeContent` works.

14. GET stream with `If-None-Match`: first issue a plain
    `GET /api/v1/media/{media_id}/stream` (or HEAD) with the session
    cookie, read the `ETag` response header, and confirm it is a
    quoted string of the form `"<size>-<mtime-nanos>"`. Then re-issue
    `GET /api/v1/media/{media_id}/stream` with the session cookie and
    `If-None-Match: <etag>` and confirm the response is HTTP 304
    (Not Modified) with an empty body. A response missing the `ETag`
    header or returning 200 with a body to the matching `If-None-Match`
    request is a regression of the ETag support added with this
    scenario.

15. Negative case — HEAD on a nonexistent media id: issue
    `HEAD /api/v1/media/999999999/stream` with the session cookie.
    Confirm the response is HTTP 404. This verifies the
    `service.ErrNotFound` → HTTP 404 mapping in `fileHandler`
    (`internal/api/handlers_file.go`) fires for HEAD just as it does for
    GET — `http.ServeContent` is never reached when the upstream service
    returns `ErrNotFound`.

16. Negative case — GET stream on a nonexistent media id: issue
    `GET /api/v1/media/999999999/stream` with the session cookie.
    Confirm the response is HTTP 404. This mirrors step 15 for the GET
    method and confirms there is no method-specific divergence in the
    error path.
