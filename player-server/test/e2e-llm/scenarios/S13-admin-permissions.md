---
id: S13
title: "Admin permissions: grant and revoke set access"
tags: [admin, permissions, api]
preconditions:
  server_state: running        # server running with admin account and at least one set
  fixtures: []
assertions:
  - db: "SELECT count(*) FROM set_permissions"
  - status_code: "GET /api/v1/admin/permissions 200"
skip: false
---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Create a non-admin user that will receive the permission grant: call
   `POST /api/v1/admin/users` with the session cookie and body
   `{"username": "e2e-perm-user", "password": "TestPassw0rd!", "is_admin": false}`.
   Confirm the response is HTTP 200 and the returned JSON object has a non-zero
   `id` field, a `username` field equal to `e2e-perm-user`, and an `is_admin`
   field equal to `false`. Save the `id` as `user_id`.

3. List the current permissions matrix: call `GET /api/v1/admin/permissions`
   with the session cookie. Confirm the response is HTTP 200 and the returned
   JSON object has three array fields: `sets`, `users`, and `permissions`.
   Confirm `users` contains an entry whose `id` matches `user_id`, and that
   `sets` contains at least one entry. Save the `id` of the first set in `sets`
   as `set_id`, and remember the existing entries in `permissions` (the count
   may be zero or greater) as `initial_perm_count`.

4. Grant the new user the `viewer` role on the chosen set: call
   `POST /api/v1/admin/permissions` with the session cookie and body
   `{"user_id": <user_id>, "set_id": <set_id>, "role": "viewer"}`. Confirm the
   response is HTTP 200 and the returned JSON body is `{"status": "ok"}`. Note
   that the request body also accepts `"owner"` as the role; this scenario uses
   `"viewer"`.

5. Re-fetch the permissions matrix: call `GET /api/v1/admin/permissions` with
   the session cookie. Confirm the response is HTTP 200 and the `permissions`
   array now contains an entry whose `user_id` equals `user_id`, `set_id`
   equals `set_id`, and `role` equals `viewer`. The array length must be
   greater than `initial_perm_count`.

6. Revoke the permission: call `DELETE /api/v1/admin/permissions` with the
   session cookie and body `{"user_id": <user_id>, "set_id": <set_id>}`.
   Confirm the response is HTTP 200 and the returned JSON body is
   `{"status": "ok"}`. Note that the revoke handler ignores the `role` field
   and removes any role the user has on that set.

7. Re-fetch the permissions matrix one more time: call
   `GET /api/v1/admin/permissions` with the session cookie. Confirm the
   response is HTTP 200 and the `permissions` array no longer contains any
   entry whose `user_id` equals `user_id` and `set_id` equals `set_id`. The
   array length must equal `initial_perm_count` again.

8. Cleanup: delete the temporary user by calling
   `DELETE /api/v1/admin/users/{user_id}` with the session cookie. Confirm the
   response is HTTP 200 and the returned JSON body is `{"status": "ok"}`.
