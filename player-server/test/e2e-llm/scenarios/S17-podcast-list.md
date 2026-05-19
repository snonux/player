---
id: S17
title: "Podcast list endpoints and admin-only subscribe"
tags: [podcast, api, list, negative-path]
preconditions:
  server_state: running        # server running with an existing admin account
  fixtures:
    - mock-rss-server          # start the mock RSS server so we have a feed to list if the DB is empty
assertions:
  - status_code: "GET /api/v1/podcasts 200"
  - db: "SELECT id FROM podcasts"
skip: false
---

# Setup note
Before running this scenario, start the mock RSS server fixture:

```sh
node player-server/test/e2e-llm/fixtures/mock-rss-server.js &
# Server listens at http://localhost:8888/feed.xml
```

Stop it after the scenario completes with `kill %1` (or equivalent).

# Purpose

This scenario exercises the podcast *list* endpoints and the admin-only
subscribe boundary. The full subscribe → download → mark-complete happy path
is covered by S02; S17 focuses on:

- `GET /api/v1/podcasts` — list response shape, available to any authenticated
  user.
- `GET /api/v1/podcasts/{id}/episodes` — list response shape and the
  missing-id (404) error case.
- `POST /api/v1/podcasts` — admin-only authorization boundary (non-admin
  must receive 403).

Do NOT re-test the download / complete flow here; that lives in S02.

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response
   is HTTP 200 and save the `session` cookie returned in the response as
   `ADMIN_COOKIE` for all subsequent admin requests.

2. List existing podcast subscriptions: call `GET /api/v1/podcasts` with
   `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the returned JSON body
   is an array (it may be empty if no previous scenario has subscribed a feed,
   or non-empty if S02 has already run in this DB). Record the array length as
   `initial_feed_count`.

3. If `initial_feed_count == 0`, subscribe to the mock RSS feed so that the
   subsequent list assertions have at least one entry: call
   `POST /api/v1/podcasts` with `ADMIN_COOKIE` and body
   `{"feed_url": "http://localhost:8888/feed.xml", "set_name": "S17 Test Podcast"}`.
   Confirm the response is HTTP 200 and the returned JSON object contains a
   non-zero `id` field and a `feed_url` equal to
   `http://localhost:8888/feed.xml`. If `initial_feed_count > 0`, skip this
   step — a feed already exists.

4. List podcast subscriptions again: call `GET /api/v1/podcasts` with
   `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the returned JSON is an
   array of length >= 1. Confirm every entry in the array has the following
   fields (matching `model.PodcastFeed`):
   - `id` (non-zero integer)
   - `set_id` (non-zero integer)
   - `feed_url` (non-empty string)
   - `title` (string; may be empty for a feed whose RSS lacks a title, but the
     field must be present)
   - `check_interval_minutes` (integer)
   - `auto_download` (boolean)
   - `created_at` (ISO-8601 timestamp string)
   Save the `id` of the first entry as `feed_id`.

5. List episodes for that feed: call `GET /api/v1/podcasts/{feed_id}/episodes`
   with `ADMIN_COOKIE`. Confirm the response is HTTP 200 and the returned JSON
   body is an array. The array may be empty if no feed poll has imported
   episodes yet, or non-empty if S02 already populated episodes. If the array
   is non-empty, confirm the first entry contains the fields `id`, `feed_id`,
   `guid`, `title`, `episode_url`, `is_downloaded`, `is_completed`, and
   `position_seconds` (matching `model.PodcastEpisodeWithStatus`).

6. Negative path — nonexistent podcast id must return HTTP 404 (NOT 500): call
   `GET /api/v1/podcasts/999999999/episodes` with `ADMIN_COOKIE`. Confirm the
   response is HTTP 404. The handler returns 404 via `service.ErrNotFound`
   when no feeds exist for the given id; a 500 here would be a real defect
   (file a task).

7. Negative path — invalid (zero) podcast id must return HTTP 400: call
   `GET /api/v1/podcasts/0/episodes` with `ADMIN_COOKIE`. Confirm the response
   is HTTP 400 (the handler short-circuits with `badRequest` when
   `pathID` returns 0).

8. Create a temporary non-admin user so we can exercise the admin-only
   subscribe boundary: call `POST /api/v1/admin/users` with `ADMIN_COOKIE`
   and body
   `{"username": "e2e-podcast-list-user", "password": "TestPassw0rd!", "is_admin": false}`.
   Confirm the response is HTTP 200, the returned JSON has a non-zero `id`,
   `username` equal to `e2e-podcast-list-user`, and `is_admin` equal to
   `false`. Save the `id` as `temp_user_id`.

9. Authenticate as the non-admin user: call `POST /api/v1/auth/login` with no
   prior cookie and body
   `{"username": "e2e-podcast-list-user", "password": "TestPassw0rd!"}`.
   Confirm the response is HTTP 200 and save the returned `session` cookie as
   `USER_COOKIE`. Subsequent admin operations must continue to use
   `ADMIN_COOKIE`; only the 403 step below uses `USER_COOKIE`.

10. Confirm the non-admin user CAN list podcasts (the read endpoint is
    session-only, not admin-only): call `GET /api/v1/podcasts` with
    `USER_COOKIE`. Confirm the response is HTTP 200 and the returned JSON
    body is an array (possibly empty for this user if no permissions have
    been granted, but the status MUST be 200, not 403).

11. Confirm the non-admin user gets HTTP 403 when attempting to subscribe:
    call `POST /api/v1/podcasts` with `USER_COOKIE` and body
    `{"feed_url": "http://localhost:8888/feed.xml", "set_name": "should-not-be-created"}`.
    Confirm the response is HTTP 403 (Forbidden). The `requireAdmin`
    middleware rejects the non-admin session before the handler runs, so no
    new feed must be created. A 200 here is a real authorization defect.

12. Cleanup: delete the temporary non-admin user. Call
    `DELETE /api/v1/admin/users/{temp_user_id}` with `ADMIN_COOKIE`. Confirm
    the response is HTTP 200 and the returned JSON body is
    `{"status": "ok"}`. Do NOT skip this cleanup — leftover users will
    pollute subsequent scenario runs.
