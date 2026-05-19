---
id: S23
title: "User deletion cascades to tokens, sessions, notes, favorites, progress, shares, permissions"
tags: [admin, auth, cascade, users, security]
preconditions:
  server_state: running        # server running with admin account and at least one media item
  fixtures: []
assertions:
  - db: "SELECT count(*) FROM users WHERE username='e2e-cascade-user'"
  - status_code: "DELETE /api/v1/admin/users 200"
skip: false
---

# Scenario note

This scenario verifies that deleting a non-admin user via
`DELETE /api/v1/admin/users/{id}` removes ALL rows the user owns across every
related table. The schema in `internal/repository/schema.go` declares
`ON DELETE CASCADE` on every user-owned FK:

- `api_tokens.user_id` → CASCADE
- `sessions.user_id` → CASCADE
- `set_permissions.user_id` → CASCADE
- `favorites.user_id` → CASCADE
- `playback_progress.user_id` → CASCADE
- `media_notes.user_id` → CASCADE
- `shares.created_by` → CASCADE
- `podcast_status.user_id` → CASCADE

`playback_accumulator` references `sessions(id)` and so is cleaned up
transitively when the user's session row cascades. `tags` and `media_tags` are
global (not user-scoped) so a deleted user's tag associations remain — that is
expected. Foreign key enforcement is enabled at startup via
`PRAGMA foreign_keys = ON` (`enableForeignKeys` in `schema.go`).

Any non-zero row count in the post-delete DB checks below indicates a real
defect: orphaned rows accumulate over time, and for `shares.created_by` it is a
security issue because public share links would continue to resolve for a user
that no longer exists. Treat the API token / session invalidation steps the
same way — a successful authenticated call with the deleted user's credentials
means the cascade did not propagate.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie returned in the response as
   `ADMIN_COOKIE`. Use `ADMIN_COOKIE` for every admin call below.

2. Create the target non-admin user `U`: call `POST /api/v1/admin/users` with
   `ADMIN_COOKIE` and body
   `{"username": "e2e-cascade-user", "password": "CascadePass!1", "is_admin": false}`.
   Confirm the response is HTTP 200 and the returned JSON contains a non-zero
   `id` field, a `username` field equal to `e2e-cascade-user`, and an
   `is_admin` field of `false` (or `0`). Save the `id` as `U_id`.

3. Find a media item that will be touched by `U`: call
   `GET /api/v1/media?limit=1` with `ADMIN_COOKIE`. Confirm the response is
   HTTP 200 and the body contains at least one media object. Save the `id` of
   the first item as `media_id` and the `set_id` field as `set_id`.

4. Grant `U` viewer access on the chosen set so that share creation and media
   access are allowed: call `POST /api/v1/admin/permissions` with `ADMIN_COOKIE`
   and body `{"user_id": <U_id>, "set_id": <set_id>, "role": "viewer"}`.
   Confirm the response is HTTP 200 and the returned JSON body is
   `{"status": "ok"}`. This step is the precondition for `U` to create a
   `shares` row in step 11.

5. Log in as `U`: call `POST /api/v1/auth/login` with body
   `{"username": "e2e-cascade-user", "password": "CascadePass!1"}` and no
   session cookie. Confirm the response is HTTP 200 and the response includes
   a `Set-Cookie` header that sets a non-empty `session` cookie. Save that
   cookie value as `U_COOKIE`. Subsequent steps that act AS `U` must use
   `U_COOKIE`; admin steps must continue to use `ADMIN_COOKIE`.

6. As `U`, create the first API token: call `POST /api/v1/auth/tokens` with
   `U_COOKIE` and body
   `{"name": "e2e-cascade-token-a", "expires_in_days": 1}`. Confirm the
   response is HTTP 200, the returned JSON contains a non-zero `id`, a
   non-empty `token`, and a `name` of `e2e-cascade-token-a`. Save the `id` as
   `token_a_id` and the plaintext `token` value as `U_BEARER`.

7. As `U`, create the second API token: call `POST /api/v1/auth/tokens` with
   `U_COOKIE` and body
   `{"name": "e2e-cascade-token-b", "expires_in_days": 1}`. Confirm the
   response is HTTP 200 and the returned JSON contains a non-zero `id` and a
   `name` of `e2e-cascade-token-b`. Save the `id` as `token_b_id`. The
   plaintext `token` value is not needed for this scenario.

8. As `U`, tag the media item: call `POST /api/v1/media/{media_id}/tags` with
   `U_COOKIE` and body `{"tag": "e2e-cascade-tag"}`. Confirm the response is
   HTTP 200 and the returned JSON body is `{"status": "ok"}`. Note: `tags` is
   a global table and `media_tags` is keyed by `(media_id, tag_id)` only —
   neither references `users`, so this row will survive the user delete
   (expected behaviour, not a bug).

9. As `U`, favorite the media item: call
   `POST /api/v1/media/{media_id}/favorite` with `U_COOKIE`. Confirm the
   response is HTTP 200 and the returned JSON body is `{"favorite": true}`
   (the toggle starts unset, so the first call sets it).

10. As `U`, create a note on the media item: call
    `POST /api/v1/media/{media_id}/notes` with `U_COOKIE` and body
    `{"content": "e2e cascade note"}`. Confirm the response is HTTP 200 and
    the returned JSON object has a `content` field equal to `e2e cascade note`
    and a `media_id` field equal to `media_id`.

11. As `U`, record progress on the media item: call `POST /api/v1/progress`
    with `U_COOKIE` and body
    `{"media_id": <media_id>, "position_seconds": 15.0}`. Confirm the response
    is HTTP 200 and the returned JSON body is `{"status": "ok"}`. This writes
    a row into `playback_progress` keyed by `(user_id, media_id)` and also a
    row into `playback_accumulator` keyed by `(session_id, media_id)`.

12. As `U`, create a share for the media item: call
    `POST /api/v1/media/{media_id}/shares` with `U_COOKIE`. Confirm the
    response is HTTP 200 and the returned JSON contains a non-empty `token`
    field. Save the `token` value as `share_token`. The new row in `shares`
    has `created_by = U_id`.

13. Snapshot the per-table row counts owned by `U` before deletion. The runner
    must run the following SQL via the `sqlite3` CLI against the server's
    database (path comes from `PLAYER_DB` or the runner's default). For each
    query, confirm the result is the expected non-zero value listed in the
    comment — this proves the test setup actually wrote the rows that the
    cascade will need to delete. If any pre-delete count is zero, the scenario
    must FAIL at this step rather than at the post-delete check, because a
    cascade test only has meaning if there is something to cascade.

    - `SELECT count(*) FROM api_tokens WHERE user_id = <U_id>` — expect `2`
    - `SELECT count(*) FROM sessions WHERE user_id = <U_id>` — expect `>= 1`
    - `SELECT count(*) FROM set_permissions WHERE user_id = <U_id>` — expect `1`
    - `SELECT count(*) FROM favorites WHERE user_id = <U_id>` — expect `1`
    - `SELECT count(*) FROM media_notes WHERE user_id = <U_id>` — expect `1`
    - `SELECT count(*) FROM playback_progress WHERE user_id = <U_id>` — expect `1`
    - `SELECT count(*) FROM shares WHERE created_by = <U_id>` — expect `1`

14. As admin, delete `U`: call `DELETE /api/v1/admin/users/{U_id}` with
    `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the returned JSON
    body is `{"status": "ok"}`.

15. Confirm `U` is gone from the admin user list: call
    `GET /api/v1/admin/users` with `ADMIN_COOKIE`. Confirm the response is
    HTTP 200 and the returned array does NOT contain any entry whose `id`
    equals `U_id` or whose `username` equals `e2e-cascade-user`.

16. Verify cascade on every user-owned table. Run each query via `sqlite3`
    against the server database and confirm the result is exactly `0`. Any
    non-zero result is a real bug — either the schema is missing `ON DELETE
    CASCADE` on that FK or `PRAGMA foreign_keys = ON` is not in effect.

    - `SELECT count(*) FROM api_tokens WHERE user_id = <U_id>` — expect `0`
    - `SELECT count(*) FROM sessions WHERE user_id = <U_id>` — expect `0`
    - `SELECT count(*) FROM set_permissions WHERE user_id = <U_id>` — expect `0`
    - `SELECT count(*) FROM favorites WHERE user_id = <U_id>` — expect `0`
    - `SELECT count(*) FROM media_notes WHERE user_id = <U_id>` — expect `0`
    - `SELECT count(*) FROM playback_progress WHERE user_id = <U_id>` — expect `0`
    - `SELECT count(*) FROM shares WHERE created_by = <U_id>` — expect `0`
    - `SELECT count(*) FROM podcast_status WHERE user_id = <U_id>` — expect `0`
      (this scenario does not create podcast status rows, so the count was
      already zero pre-delete; the assertion still belongs here so a future
      schema regression is caught)

17. Confirm `U`'s previous session cookie no longer authenticates: call
    `GET /api/v1/auth/tokens` with `U_COOKIE` (and no `Authorization`
    header). Confirm the response is HTTP 401 (Unauthorized). The
    `sessions` row that backed `U_COOKIE` was removed by the cascade in
    step 16, so the cookie value cannot resolve to a user.

18. Confirm `U`'s API token no longer authenticates: call
    `GET /api/v1/media?limit=1` with no session cookie and with header
    `Authorization: Bearer {U_BEARER}`. Confirm the response is HTTP 401
    (Unauthorized). The `api_tokens` row was removed by the cascade in
    step 16, so the bearer token has no associated user.

19. Confirm `U` can no longer log in: call `POST /api/v1/auth/login` with body
    `{"username": "e2e-cascade-user", "password": "CascadePass!1"}` and no
    session cookie. Confirm the response is HTTP 401 (Unauthorized) — the
    `users` row is gone.

20. Confirm the public share link `U` created is no longer reachable: call
    `GET {PLAYER_URL}/s/{share_token}/thumbnail` with no session cookie and no
    `Authorization` header. Confirm the response is HTTP 404. If this returns
    HTTP 200 then the `shares` row was NOT cascade-deleted — this is a
    security defect (a deleted user's public shares remain live) and the
    scenario must FAIL.
