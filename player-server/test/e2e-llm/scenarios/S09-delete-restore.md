---
id: S09
title: "Soft-delete → admin trash → restore round-trip"
tags: [media, api, admin, trash, restore]
preconditions:
  server_state: running        # server running with an existing admin account
  fixtures: []
assertions:
  - db: "SELECT id FROM media WHERE is_deleted=0"
  - status_code: "GET /api/v1/admin/trash 200"
skip: false
---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Create an API token to authenticate the upload step (Bearer auth): call
   `POST /api/v1/auth/tokens` with the session cookie and body
   `{"name": "e2e-delete-restore", "expires_in_days": 1}`. Confirm the response
   is HTTP 200 and save the `token` plaintext from the response as
   `BEARER_TOKEN`.

3. List the available sets: call `GET /api/v1/sets` with the session cookie.
   Confirm the response is HTTP 200. Save the `id` of the first non-podcast set
   (e.g. `musicvideos`) as `set_id`.

4. Prepare a small disposable test audio file. Create a minimal 1-second silent
   MP3 named `test-delete-restore.mp3` using
   `ffmpeg -f lavfi -i anullsrc=r=44100:cl=mono -t 1 -q:a 9 -acodec libmp3lame /tmp/test-delete-restore.mp3`
   (requires ffmpeg on PATH). Confirm the file exists at
   `/tmp/test-delete-restore.mp3`.

5. Upload the file to the set: call
   `POST /api/v1/sets/{set_id}/upload` as a `multipart/form-data` request with:
   - `Authorization: Bearer {BEARER_TOKEN}` header
   - form field `file` containing the contents of
     `/tmp/test-delete-restore.mp3` with filename `test-delete-restore.mp3`.
   Confirm the response is HTTP 200 and the returned JSON contains a non-zero
   `id` field. Save `media_id` from the response.

6. Confirm the freshly uploaded item appears in the active media list: call
   `GET /api/v1/media` with the session cookie. Confirm the response is HTTP 200
   and the returned list contains an entry whose `id` matches `media_id`.

7. Soft-delete the media item: call `DELETE /api/v1/media/{media_id}` with the
   session cookie. Confirm the response is HTTP 200.

8. Confirm the item no longer appears in the active media list: call
   `GET /api/v1/media` with the session cookie. Confirm the response is HTTP 200
   and the returned list does NOT contain any entry whose `id` matches
   `media_id` (soft-deleted items are filtered out of the active listing).

9. Confirm the item appears in the admin trash list: call
   `GET /api/v1/admin/trash` with the session cookie. Confirm the response is
   HTTP 200 and the returned list contains an entry whose `id` matches
   `media_id`.

10. Restore the soft-deleted media item: call
    `POST /api/v1/media/{media_id}/restore` with the session cookie. Confirm the
    response is HTTP 200.

11. Confirm the item reappears in the active media list: call
    `GET /api/v1/media` with the session cookie. Confirm the response is HTTP
    200 and the returned list contains an entry whose `id` matches `media_id`.

12. Confirm the item is no longer in the admin trash list: call
    `GET /api/v1/admin/trash` with the session cookie. Confirm the response is
    HTTP 200 and the returned list does NOT contain any entry whose `id`
    matches `media_id`.

13. Clean up: delete the test media item by calling
    `DELETE /api/v1/media/{media_id}` with the session cookie. Confirm the
    response is HTTP 200. Also revoke the API token created in step 2 by calling
    `DELETE /api/v1/auth/tokens/{token_id}` with the session cookie.
