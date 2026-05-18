---
id: S14
title: "Admin rescan trigger and scan-progress polling"
tags: [admin, api, scan, rescan]
preconditions:
  server_state: running        # server running with an existing admin account and media library
  fixtures: []
assertions:
  - db: "SELECT count(*) FROM media"
  - status_code: "GET /api/v1/admin/scan-progress 200"
skip: false
---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response as
   `admin_session` for all subsequent admin requests.

2. Trigger a media rescan: call `POST /api/v1/admin/rescan` with the
   `admin_session` cookie and an empty body. Confirm the response is HTTP 200
   and the returned JSON body is `{"status": "ok"}`. The handler starts the
   rescan asynchronously and returns immediately — there is no job id; progress
   is tracked via the next endpoint.

3. Fetch the current scan progress: call `GET /api/v1/admin/scan-progress`
   with the `admin_session` cookie. Confirm the response is HTTP 200 and the
   returned JSON object contains the following fields:
   - `running` (boolean) — `true` if the scan is still in progress, `false` if
     it has already completed
   - `sets_total` (integer, >= 0)
   - `sets_done` (integer, >= 0)
   - `files_total` (integer, >= 0)
   - `files_done` (integer, >= 0)
   The fields `current_set` (string) and `last_error` (string) may be present
   or omitted depending on state. Do not require a specific value for `running`
   — for a small `./testmedia` library the scan can finish before the first
   poll.

4. Poll scan progress a second time: call `GET /api/v1/admin/scan-progress`
   again with the `admin_session` cookie. Confirm the response is HTTP 200 and
   that the same set of fields (`running`, `sets_total`, `sets_done`,
   `files_total`, `files_done`) are present with the same types as in step 3.
   `files_done` must be greater than or equal to the value observed in step 3
   (monotonic non-decreasing within a single rescan), and `last_error` must
   either be absent or be an empty string (no scan error).

5. Poll scan progress a third time to confirm the field shape is stable: call
   `GET /api/v1/admin/scan-progress` once more with the `admin_session`
   cookie. Confirm the response is HTTP 200 and the same field shape as steps 3
   and 4. The harness is asserting API contract stability, not waiting for
   completion.

6. Create a temporary non-admin user to verify the admin-only authorization
   boundary: call `POST /api/v1/admin/users` with the `admin_session` cookie
   and body
   `{"username": "e2e-rescan-user", "password": "TestPassw0rd!", "is_admin": false}`.
   Confirm the response is HTTP 200 and the returned JSON has a non-zero `id`
   field and an `is_admin` field equal to `false`. Save the `id` as
   `temp_user_id`.

7. Authenticate as the non-admin user: call `POST /api/v1/auth/login` with no
   session cookie and body
   `{"username": "e2e-rescan-user", "password": "TestPassw0rd!"}`. Confirm the
   response is HTTP 200 and save the returned `session` cookie as
   `user_session`.

8. Confirm a non-admin user gets 403 on the rescan endpoint: call
   `POST /api/v1/admin/rescan` with the `user_session` cookie and an empty
   body. Confirm the response is HTTP 403 (Forbidden) — the `RequireAdmin`
   middleware rejects non-admin sessions before the handler is reached.

9. Confirm a non-admin user gets 403 on the scan-progress endpoint: call
   `GET /api/v1/admin/scan-progress` with the `user_session` cookie. Confirm
   the response is HTTP 403 (Forbidden).

10. Cleanup: delete the temporary non-admin user. Call
    `DELETE /api/v1/admin/users/{temp_user_id}` with the `admin_session`
    cookie. Confirm the response is HTTP 200 and the returned JSON body is
    `{"status": "ok"}`.
