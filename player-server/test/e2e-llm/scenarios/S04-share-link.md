---
id: S04
title: "Share-link round-trip"
tags: [share, auth, web, visual]
preconditions:
  server_state: running        # server running with admin account and at least one media item
  fixtures: []
assertions:
  - db: "SELECT token FROM shares WHERE token IS NOT NULL"
  - url_contains: /login.html
  - status_code: "GET /api/v1/media 200"
skip: false
---

# Visual check note
Steps 8 and 11 trigger the Haiku screenshot oracle when
`LLM_E2E_SCREENSHOTS=true`. Set that env var and provide `ANTHROPIC_API_KEY`
to enable visual assertions. Without it, the selector assertions in the YAML
front-matter are used instead.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Save the `session`
   cookie for subsequent authenticated requests.

2. Find a media item to share: call `GET /api/v1/media?limit=1` with the
   session cookie. Confirm the response is HTTP 200 and contains at least one
   media object. Save the `id` of the first item as `media_id`.

3. Create a share link for that media item: call
   `POST /api/v1/media/{media_id}/shares` with the session cookie. Confirm the
   response is HTTP 200. Save the `token` field from the response as
   `share_token`. The share URL is `{PLAYER_URL}/s/{share_token}`.

4. Confirm the share is recorded: call
   `GET /api/v1/media/{media_id}/shares` with the session cookie. Confirm the
   response contains a share entry whose `token` matches `share_token`.

5. Open a fresh, unauthenticated Playwright browser context (no cookies). In
   this context, navigate to the share URL `{PLAYER_URL}/s/{share_token}`.
   Confirm the response is HTTP 200 and the browser does NOT redirect to
   `/login.html` (the share page is publicly accessible).

6. Confirm the share page renders the media player: look for a `<audio>` or
   `<video>` element, or a play button element (e.g. a button with aria-label
   "Play" or a class like `.play-btn`). The element must be present and visible.

7. Confirm the share page contains the correct media title by verifying that a
   heading or text element on the page matches the `file_name` or `title`
   of the shared media item (obtained in step 2).

8. **Visual check (Layer 5):** Take a screenshot of the share page and ask
   Claude Haiku: "Is there a media player visible on this page? Answer yes or
   no and give a one-sentence reason." This step requires `LLM_E2E_SCREENSHOTS=true`.

9. From the same unauthenticated browser context, navigate to the root URL
   `{PLAYER_URL}/`. Confirm the browser is redirected to `/login.html`.
   The share token must not grant general access to the application.

10. Confirm the root URL returns HTTP 401 or redirects to `/login.html` for
    unauthenticated requests: call `GET {PLAYER_URL}/api/v1/media` without any
    session cookie. Confirm the response is HTTP 401.

11. **Visual check (Layer 5):** Take a screenshot after the redirect to
    `/login.html` and ask Claude Haiku: "Does this screenshot show a login
    form? Answer yes or no and give a one-sentence reason." This step requires
    `LLM_E2E_SCREENSHOTS=true`.

12. Revoke the share link: from an authenticated context (using the admin
    session cookie), call `DELETE /api/v1/shares/{share_token}`. Confirm the
    response is HTTP 200.

13. Confirm the share is no longer accessible: in the unauthenticated context,
    navigate to `{PLAYER_URL}/s/{share_token}` again. Confirm the response is
    HTTP 404 or HTTP 410 Gone (the token has been revoked).
