---
id: S06
title: "Set browse + cover regeneration + config"
tags: [api, sets, browse, cover, config]
preconditions:
  server_state: running        # server running with admin account and at least one media item
  fixtures: []
assertions:
  - status_code: "GET /api/v1/config 200"
  - status_code: "GET /api/v1/sets 200"
skip: false
---

# Note on the cover endpoint
`POST /api/v1/sets/{id}/cover` does **not** ingest an uploaded image. It
triggers a server-side regeneration that picks a candidate file (artwork,
video frame, image, or existing thumbnail) from the set and writes
`.cover.jpg` into the set's directory. The PNG sent as multipart/form-data
in step 5 is a benign payload — the server ignores it but accepts the
request. The endpoint returns HTTP 200 only when the set contains at least
one media file the server can derive a cover from; the chosen set in
step 3 must therefore be a non-empty set such as `musicvideos` or any
seeded testmedia set.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Fetch the authenticated client configuration: call `GET /api/v1/config`
   with the session cookie. Confirm the response is HTTP 200 and the returned
   JSON object contains a numeric `media_page_size` field (this field is
   sourced from the server's `MediaPageSize` config and defaults to
   `internal.DefaultMediaPageSize` when unset).

3. List the available sets: call `GET /api/v1/sets` with the session cookie.
   Confirm the response is HTTP 200 and the returned array contains at least
   one set. Prefer a non-podcast set seeded from `testmedia/` (for example
   `musicvideos`). Save the `id` of the chosen set as `set_id`.

4. Browse the contents of the chosen set: call
   `GET /api/v1/sets/{set_id}/browse` with the session cookie. Confirm the
   response is HTTP 200. The returned JSON is a `BrowseResult` object with
   the following fields: `current_path` (string), `folders` (array of
   `{name, has_cover}` objects), and `media` (array of media objects).
   Confirm the `folders` and `media` fields are present (either may be an
   empty array, but both keys must exist).

5. Fetch the current set cover: call `GET /api/v1/sets/{set_id}/cover` with
   the session cookie. Confirm the response is HTTP 200 (a cover already
   exists for the set) **or** HTTP 404 (no cover has been generated yet for
   this set). Either outcome is acceptable at this stage.

6. Trigger a cover regeneration: call `POST /api/v1/sets/{set_id}/cover`
   with the session cookie as a `multipart/form-data` request containing a
   single form field `file` whose value is a minimal valid PNG (for example
   the 67-byte 1x1 transparent PNG produced by
   `printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB\x60\x82' > /tmp/s06-pixel.png`).
   The server does not read the file body — it picks a candidate file
   already inside the set's directory and writes `.cover.jpg` — but the
   request must still be a well-formed multipart request. Confirm the
   response is HTTP 200 and the returned JSON is `{"status":"ok"}`.

7. Fetch the set cover again: call `GET /api/v1/sets/{set_id}/cover` with
   the session cookie. Confirm the response is HTTP 200 and the `Content-Type`
   header indicates an image (typically `image/jpeg` since the regenerated
   cover is `.cover.jpg`). The cover is now guaranteed to exist after the
   successful regeneration in step 6.
