---
id: S11
title: "Shares management — list, public thumbnail/download, revoke"
tags: [share, api, public]
preconditions:
  server_state: running        # server running with admin account and at least one media item
  fixtures: []
assertions:
  - db: "SELECT token FROM shares"
  - status_code: "GET /api/v1/shares 200"
skip: false
---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie for subsequent authenticated requests.

2. Find a media item to share: call `GET /api/v1/media?limit=1` with the
   session cookie. Confirm the response is HTTP 200 and contains at least one
   media object. Save the `id` of the first item as `media_id`.

3. Create a share link for that media item: call
   `POST /api/v1/media/{media_id}/shares` with the session cookie. Confirm the
   response is HTTP 200. Save the `token` field from the response as
   `share_token`. The share URL is `{PLAYER_URL}/s/{share_token}`.

4. List shares owned by the current user: call `GET /api/v1/shares` with the
   session cookie. Confirm the response is HTTP 200 and the returned array
   contains an entry whose `token` matches `share_token`.

5. Fetch the public share thumbnail without any authentication: call
   `GET {PLAYER_URL}/s/{share_token}/thumbnail` with no session cookie and no
   Authorization header. Confirm the response is HTTP 200 and the
   `Content-Type` response header starts with `image/`.

6. Fetch the public share download without any authentication: call
   `GET {PLAYER_URL}/s/{share_token}/download` with no session cookie and no
   Authorization header. Confirm the response is HTTP 200 and the
   `Content-Disposition` response header starts with `attachment` (the server
   sets `attachment; filename="<original-file-name>"`).

7. Revoke the share link: from the authenticated context (using the admin
   session cookie), call `DELETE /api/v1/shares/{share_token}`. Confirm the
   response is HTTP 200.

8. Confirm the share no longer appears in the owner's list: call
   `GET /api/v1/shares` with the session cookie. Confirm the response is
   HTTP 200 and the returned array does NOT contain an entry whose `token`
   matches `share_token`.

9. Confirm the public thumbnail endpoint is no longer reachable: call
   `GET {PLAYER_URL}/s/{share_token}/thumbnail` with no session cookie and no
   Authorization header. Confirm the response is HTTP 404 (the token has been
   revoked and the share is no longer resolvable).
