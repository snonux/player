---
id: S02
title: "Podcast subscribe → download → mark complete"
tags: [podcast, api, progress]
preconditions:
  server_state: running        # server running with an existing admin account
  fixtures:
    - mock-rss-server          # start the mock RSS server before running steps
assertions:
  - db: "SELECT id FROM podcast_feeds WHERE feed_url='http://localhost:8888/feed.xml'"
  - db: "SELECT id FROM podcast_episodes WHERE feed_id=(SELECT id FROM podcast_feeds WHERE feed_url='http://localhost:8888/feed.xml')"
  - db: "SELECT episode_id FROM podcast_status WHERE is_completed=1"
skip: false
---

# Setup note
Before running this scenario, start the mock RSS server fixture:

```sh
node player-server/test/e2e-llm/fixtures/mock-rss-server.js &
# Server listens at http://localhost:8888/feed.xml
```

Stop it after the scenario completes with `kill %1` (or equivalent).

---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Save the `session`
   cookie returned in the response for all subsequent requests.

2. Subscribe to the mock podcast feed: call `POST /api/v1/podcasts` with body
   `{"feed_url": "http://localhost:8888/feed.xml", "set_name": "Test Podcast"}`.
   Confirm the response is HTTP 200 and the returned JSON object contains a
   non-zero `id` field. Save `feed_id` and `set_id` from the response. Episode listing takes the shared podcast set ID, not the feed ID.

3. Confirm `GET /api/v1/podcasts` returns HTTP 200 and the list contains an
   entry whose `feed_url` is `http://localhost:8888/feed.xml`.

4. Retrieve the list of episodes for the new feed: call
   `GET /api/v1/podcasts/{set_id}/episodes`. Confirm the response is HTTP 200
   and contains at least one episode object. Save the `id` of the first episode
   as `episode_id`.

5. Confirm the first episode has a non-empty `title` field and a non-null
   `episode_url` field.

6. Download the episode: call
   `POST /api/v1/podcasts/episodes/{episode_id}/download`.
   Confirm the response is HTTP 200 and the returned JSON is a media object with
   a non-zero `id` field and a non-empty `file_name` field. Save `id` as
   `media_id`.

7. Confirm the episode now appears in the list returned by
   `GET /api/v1/podcasts/{set_id}/episodes` with `is_downloaded: true`.

8. Record playback progress for the downloaded media using one session.
   Call `POST /api/v1/progress` with `media_id` and successively increasing
   `position_seconds`: 12, 24, 36, 48, 60, 72, then 120. Confirm each
   response is HTTP 200. A single seek to 120 is insufficient to cross the
   60-second playback accumulator threshold.

9. Confirm `GET /api/v1/in-progress` contains the downloaded `media_id`.

10. Mark the episode as complete: call
    `POST /api/v1/podcasts/episodes/{episode_id}/complete`. Confirm the response
    is HTTP 204 (No Content).

11. Retrieve the episode list again:
    `GET /api/v1/podcasts/{set_id}/episodes`. Confirm the target episode now
    has `is_completed: true` in its status fields.
