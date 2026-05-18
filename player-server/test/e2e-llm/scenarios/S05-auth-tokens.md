---
id: S05
title: "Auth token lifecycle and logout"
tags: [auth, api, tokens, logout]
preconditions:
  server_state: running        # server running with an existing admin account
  fixtures: []
assertions:
  - db: "SELECT id FROM api_tokens"
  - status_code: "GET /api/v1/auth/tokens 200"
skip: false
---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Create an API token for the admin user: call `POST /api/v1/auth/tokens` with
   the session cookie and body
   `{"name": "e2e-token-lifecycle", "expires_in_days": 1}`. Confirm the
   response is HTTP 200 and the returned JSON contains a non-zero `id` field, a
   non-empty `token` field, and a `name` field equal to `e2e-token-lifecycle`.
   Save the `id` as `token_id` and the `token` plaintext as `BEARER_TOKEN`.

3. List the API tokens for the admin user: call `GET /api/v1/auth/tokens` with
   the session cookie. Confirm the response is HTTP 200 and the returned array
   contains an entry whose `id` matches `token_id` and whose `name` is
   `e2e-token-lifecycle`. The plaintext `token` value must NOT be present in
   the list response — only token metadata is exposed after creation.

4. Verify the token can authenticate a request: call `GET /api/v1/media?limit=1`
   with no session cookie and with header
   `Authorization: Bearer {BEARER_TOKEN}`. Confirm the response is HTTP 200.

5. Revoke the API token: call `DELETE /api/v1/auth/tokens/{token_id}` with the
   session cookie. Confirm the response is HTTP 204 (No Content).

6. Confirm the token no longer appears in the list: call
   `GET /api/v1/auth/tokens` with the session cookie. Confirm the response is
   HTTP 200 and the returned array does NOT contain an entry whose `id`
   matches `token_id`.

7. Confirm the revoked token can no longer authenticate a request: call
   `GET /api/v1/media?limit=1` with no session cookie and with header
   `Authorization: Bearer {BEARER_TOKEN}`. Confirm the response is HTTP 401
   (Unauthorized).

8. Logout the admin session: call `POST /api/v1/logout` with the session
   cookie. Confirm the response is HTTP 204 (No Content) and the response
   includes a `Set-Cookie` header that clears the `session` cookie (empty
   value and/or `Max-Age=0`).

9. Confirm the session cookie is no longer valid: call
   `GET /api/v1/auth/tokens` with the original (now-deleted) session cookie.
   Confirm the response is HTTP 401 (Unauthorized) — the server-side session
   has been removed by logout, so the cookie value can no longer authenticate
   subsequent requests.
