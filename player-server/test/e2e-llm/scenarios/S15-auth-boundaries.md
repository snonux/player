---
id: S15
title: "Auth boundary negative-path tests (401/403/404)"
tags: [auth, admin, api, security, negative-path]
preconditions:
  server_state: running        # server running with an existing admin account
  fixtures: []
assertions:
  - status_code: "GET /api/v1/media 401"
  - status_code: "GET /api/v1/admin/users 403"
skip: false
---

# Purpose

This scenario verifies the auth middleware boundaries so future regressions in
`RequireSession` / `RequireAdmin` (see `internal/api/middleware.go`) are caught:

- Unauthenticated API requests must return HTTP 401 (JSON, NOT a redirect to
  `/login.html`). The middleware only redirects to login when the request
  includes `Accept: text/html`. Test these requests WITHOUT an `Accept` header
  (or with `Accept: application/json`) so the server returns 401 as JSON.
- Authenticated requests by a non-admin user must return HTTP 403 on any route
  wrapped by `requireAdmin`.
- Authenticated admin requests for missing resources must return HTTP 404
  (NOT 200 and NOT 500).

If any handler returns a different status code than asserted below, treat it
as a real auth-boundary defect and file a task.

---

## A) Unauthenticated requests must return HTTP 401

1. Send `GET {PLAYER_URL}/api/v1/media` with NO `session` cookie and NO
   `Authorization` header. Do NOT send an `Accept: text/html` header — use
   `Accept: application/json` (or no Accept header). Confirm the response is
   HTTP 401. The body should be JSON-ish text containing `unauthorized`.

2. Send `GET {PLAYER_URL}/api/v1/sets` with no cookie and no Authorization
   header (and no `Accept: text/html`). Confirm the response is HTTP 401.

3. Send `GET {PLAYER_URL}/api/v1/tags` with no cookie and no Authorization
   header (and no `Accept: text/html`). Confirm the response is HTTP 401.

4. Send `POST {PLAYER_URL}/api/v1/auth/tokens` with header
   `Content-Type: application/json`, body `{"name": "should-not-be-created",
   "expires_in_days": 1}`, no `session` cookie, and no `Authorization` header.
   Confirm the response is HTTP 401. Token creation requires an existing
   session and must NOT succeed without one.

## B) Authenticated non-admin user must get HTTP 403 on admin routes

5. Authenticate as the admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie as `ADMIN_COOKIE`.

6. Create a temporary non-admin user: call `POST /api/v1/admin/users` with
   `ADMIN_COOKIE` and body
   `{"username": "e2e-auth-boundaries", "password": "TestPassw0rd!", "is_admin": false}`.
   Confirm the response is HTTP 200 and the returned JSON has a non-zero `id`,
   `username` equal to `e2e-auth-boundaries`, and `is_admin` equal to false.
   Save the `id` as `temp_user_id`.

7. Log in as the non-admin user: call `POST /api/v1/auth/login` with body
   `{"username": "e2e-auth-boundaries", "password": "TestPassw0rd!"}` and no
   prior cookie. Confirm the response is HTTP 200 and save the `session`
   cookie returned as `USER_COOKIE`. From here on, USER_COOKIE and
   ADMIN_COOKIE must NOT be mixed up — admin operations continue to use
   ADMIN_COOKIE; the 403 assertions below use USER_COOKIE.

8. Call `GET /api/v1/admin/users` with `USER_COOKIE` (no Accept: text/html).
   Confirm the response is HTTP 403. The non-admin user is authenticated
   (session is valid) but not authorized for this route.

9. Call `GET /api/v1/admin/trash` with `USER_COOKIE`. Confirm the response is
   HTTP 403.

10. Call `GET /api/v1/admin/permissions` with `USER_COOKIE`. Confirm the
    response is HTTP 403.

11. Call `POST /api/v1/admin/rescan` with `USER_COOKIE` (empty body is fine).
    Confirm the response is HTTP 403. The non-admin user must not be able to
    trigger a media library rescan.

## C) Authenticated admin must get HTTP 404 for missing resources

12. Call `GET /api/v1/media/999999999` with `ADMIN_COOKIE`. Confirm the
    response is HTTP 404. (Admins bypass the per-set permission check, so
    `verifyAccess` returns `ErrNotFound`, which the handler maps to 404 via
    `handleError`.)

13. Call `GET /api/v1/sets/999999999/browse` with `ADMIN_COOKIE`. Confirm the
    response is HTTP 404. (Admins bypass `checkSetPermission`; the service
    then loads the set, finds it nil, and returns `ErrNotFound` which the
    handler maps to 404.)

14. Call `DELETE /api/v1/shares/does-not-exist-token-xyz` with `ADMIN_COOKIE`.
    Confirm the response status code is one of `{404, 500}` and record which
    one was actually returned. NOTE: At the time of writing,
    `shareService.RevokeShare` returns a plain `errors.New("share not found")`
    instead of the sentinel `ErrShareNotFound`, so `handleError` falls into
    its default branch and returns HTTP 500. The "correct" status is 404; if
    you see 500 here, file a task — this is a real auth-boundary / error-
    mapping defect in `internal/service/share.go`.

15. Call `GET {PLAYER_URL}/s/does-not-exist-token-xyz/thumbnail` with no
    cookie (this is a public share route). Confirm the response is HTTP 404.

## D) Cleanup

16. Delete the temporary non-admin user: call
    `DELETE /api/v1/admin/users/{temp_user_id}` with `ADMIN_COOKIE`. Confirm
    the response is HTTP 200 and the body is `{"status": "ok"}`. Do NOT skip
    this cleanup — leftover users will pollute subsequent runs of S12, S13,
    and S15.

17. Confirm cleanup succeeded: call `GET /api/v1/admin/users` with
    `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the array does NOT
    contain any entry whose `id` matches `temp_user_id` or whose `username`
    is `e2e-auth-boundaries`.
