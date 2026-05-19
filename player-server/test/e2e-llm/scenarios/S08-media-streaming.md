---
id: S08
title: "Media streaming, download, thumbnail regen and playback hints"
tags: [media, api, streaming, thumbnail, playback]
preconditions:
  server_state: running        # server running with admin account and at least one media item
  fixtures: []
assertions:
  - status_code: "GET /api/v1/media 200"
skip: false
---

# Notes
This scenario exercises the HTTP-level media endpoints only: thumbnail fetch
and regeneration, the playback-hints endpoint, a ranged byte-range stream
request, and the download endpoint. No actual browser playback is performed —
each step inspects HTTP status codes, response headers and JSON bodies.

The `MEDIA_ROOT` must point at a directory containing at least one media
item (see the harness README — `./testdata/media` is the default for the LLM e2e
suite). If no media items exist the scenario will fail at step 2.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Find a media item to exercise: call `GET /api/v1/media?limit=1` with the
   session cookie. Confirm the response is HTTP 200 and the JSON body contains
   at least one media object. Save the `id` of the first item as `media_id`.
   Save the `type` field (expected to be `audio`, `video` or `image`) for use
   in step 6's Content-Type check.

3. Fetch the thumbnail for the media item: call
   `GET /api/v1/media/{media_id}/thumbnail` with the session cookie. Confirm
   the response is HTTP 200 and the `Content-Type` response header starts with
   `image/` (e.g. `image/jpeg`, `image/png` or `image/webp`).

4. Trigger a thumbnail regeneration: call
   `POST /api/v1/media/{media_id}/thumbnail` with the session cookie and no
   request body. Confirm the response is HTTP 200 or HTTP 202 (the server
   returns HTTP 200 with `{"status": "ok"}` for synchronous regeneration; a
   future async implementation may return 202 Accepted — either is acceptable).

5. Fetch playback hints for the media item: call
   `GET /api/v1/media/{media_id}/playback` with the session cookie. Confirm
   the response is HTTP 200 and the returned JSON object contains a
   `needs_transcode` field whose value is a boolean (either `true` or `false`).

6. Request a byte range of the stream: call
   `GET /api/v1/media/{media_id}/stream` with the session cookie and an
   additional `Range: bytes=0-1023` request header. Confirm the response is
   HTTP 206 (Partial Content) — or HTTP 200 if the server chose to ignore the
   Range header — and that the `Content-Type` response header starts with
   `audio/` or `video/` depending on the media `type` saved in step 2.

7. Download the media item: call
   `GET /api/v1/media/{media_id}/download` with the session cookie. Confirm
   the response is HTTP 200 and the `Content-Disposition` response header is
   present and contains the substring `attachment` (e.g.
   `attachment; filename="track.mp3"`).
