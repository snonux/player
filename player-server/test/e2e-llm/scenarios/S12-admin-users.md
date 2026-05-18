---
id: S12
title: "Admin user management — create and delete"
tags: [admin, auth, api, users]
preconditions:
  server_state: running        # server running with an existing admin account
  fixtures: []
assertions:
  - db: "SELECT count(*) FROM users"
  - status_code: "GET /api/v1/admin/users 200"
skip: false
---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. List the existing users: call `GET /api/v1/admin/users` with the session
   cookie. Confirm the response is HTTP 200 and the returned JSON is an array
   of user objects. Record the current number of users as `initial_user_count`.

3. Create a new non-admin user: call `POST /api/v1/admin/users` with the
   session cookie and body
   `{"username": "e2e-temp-user", "password": "TempPassw0rd!", "is_admin": false}`.
   Confirm the response is HTTP 200 and the returned JSON contains a non-zero
   `id` field, a `username` field equal to `e2e-temp-user`, and an `is_admin`
   field that is `false` (or `0` if represented as a SQLite-style integer).
   Save the `id` as `new_user_id`.

4. Confirm the new user appears in the list: call `GET /api/v1/admin/users`
   with the session cookie. Confirm the response is HTTP 200, the returned
   array length is exactly `initial_user_count + 1`, and the array contains an
   entry whose `id` matches `new_user_id` and whose `username` is
   `e2e-temp-user`.

5. Confirm the new user can authenticate: call `POST /api/v1/auth/login` with
   body `{"username": "e2e-temp-user", "password": "TempPassw0rd!"}` and no
   session cookie. Confirm the response is HTTP 200 and the response includes a
   `Set-Cookie` header that sets a non-empty `session` cookie. Discard this
   session cookie — subsequent admin operations must continue to use the
   original admin session cookie from step 1.

6. Delete the new user: call `DELETE /api/v1/admin/users/{new_user_id}` with
   the admin session cookie from step 1. Confirm the response is HTTP 200 and
   the returned JSON contains `{"status": "ok"}`.

7. Confirm the deleted user no longer appears in the list: call
   `GET /api/v1/admin/users` with the admin session cookie. Confirm the
   response is HTTP 200, the returned array length is back to
   `initial_user_count`, and the array does NOT contain any entry whose `id`
   matches `new_user_id` or whose `username` is `e2e-temp-user`.

8. Confirm the deleted user can no longer login: call `POST /api/v1/auth/login`
   with body `{"username": "e2e-temp-user", "password": "TempPassw0rd!"}` and
   no session cookie. Confirm the response is HTTP 401 (Unauthorized) — the
   user no longer exists, so authentication must fail.
