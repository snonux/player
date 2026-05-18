---
id: S03
title: "Upload via API → verify on web"
tags: [upload, api, web, visual]
preconditions:
  server_state: running        # server running with an existing admin account
  fixtures: []
assertions:
  - db: "SELECT id FROM media WHERE file_name='test-audio-e2e.mp3'"
  - selector_visible: ".media-card"
  - status_code: "GET /api/v1/media 200"
skip: false
---

# Visual check note
Step 10 triggers a Haiku screenshot oracle when `LLM_E2E_SCREENSHOTS=true`.
Set that env var and provide `ANTHROPIC_API_KEY` to enable the visual assertion.
If the env var is not set, the step is skipped and the selector assertion in the
YAML front-matter is used instead.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Save the `session`
   cookie for subsequent requests.

2. Create an API token for the upload step (Bearer auth): call
   `POST /api/v1/auth/tokens` with body
   `{"name": "e2e-upload-test", "expires_in_days": 1}` and the session cookie.
   Save the `token` value from the response as `BEARER_TOKEN`.

3. List the available sets: call `GET /api/v1/sets`. Confirm the response is
   HTTP 200. Save the `id` of the first set (e.g. the set named `musicvideos`
   or any non-podcast set) as `set_id`.

4. Prepare a small test audio file to upload. Use the fixture file at
   `player-server/testmedia/podcast/` or create a minimal 1-second silent MP3
   named `test-audio-e2e.mp3` using `ffmpeg -f lavfi -i anullsrc=r=44100:cl=mono -t 1 -q:a 9 -acodec libmp3lame /tmp/test-audio-e2e.mp3`
   (requires ffmpeg on PATH). Confirm the file exists at `/tmp/test-audio-e2e.mp3`.

5. Upload the file to the set: call
   `POST /api/v1/sets/{set_id}/upload` as a `multipart/form-data` request with:
   - `Authorization: Bearer {BEARER_TOKEN}` header
   - form field `file` containing the contents of `/tmp/test-audio-e2e.mp3`
   with filename `test-audio-e2e.mp3`.
   Confirm the response is HTTP 200 and the returned JSON contains a non-zero
   `id` field. Save `media_id` from the response.

6. Confirm the media is in the database: call
   `GET /api/v1/media/{media_id}` with the session cookie. Confirm the response
   is HTTP 200, the `file_name` field is `test-audio-e2e.mp3`, and the
   `type` field is `audio`.

7. Trigger a media rescan so the server reindexes the upload:
   call `POST /api/v1/admin/rescan` with the session cookie. Confirm the
   response is HTTP 200.

8. Wait for the scan to complete: poll `GET /api/v1/admin/scan-progress` until
   the response indicates the scan is done (e.g. `"scanning": false` or an
   empty progress object). Poll up to 30 seconds with 2-second intervals.

9. Open the web UI in a Playwright browser context: navigate to
   `{PLAYER_URL}/index.html` and authenticate by injecting the session cookie.
   Navigate to the set page that contains the uploaded file (use the set
   browsing UI or navigate directly to the URL for the set).

10. Confirm the uploaded file's media card is visible in the media grid.
    Look for a `.media-card` element (or equivalent) whose title or filename
    contains `test-audio-e2e`.
    **Visual check (Layer 5):** Take a screenshot of the media grid and ask
    Claude Haiku: "Is there a media card visible in the grid? Answer yes or no
    and give a one-sentence reason." This step requires `LLM_E2E_SCREENSHOTS=true`.

11. Clean up: delete the test media item by calling
    `DELETE /api/v1/media/{media_id}` with the session cookie. Confirm the
    response is HTTP 200.
