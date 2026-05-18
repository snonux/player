---
id: S07
title: "Favorites, tags, and notes lifecycle"
tags: [favorites, tags, notes, api]
preconditions:
  server_state: running        # server running with admin account and at least one media item
  fixtures: []
assertions:
  - db: "SELECT id FROM tags WHERE name='e2e-test'"
  - status_code: "GET /api/v1/tags 200"
skip: false
---

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the response is
   HTTP 200 and save the `session` cookie returned in the response for all
   subsequent authenticated requests.

2. Find a media item to operate on: call `GET /api/v1/media?limit=1` with the
   session cookie. Confirm the response is HTTP 200 and contains at least one
   media object. Save the `id` of the first item as `media_id`.

3. Toggle the favorite flag on the media item for the first time: call
   `POST /api/v1/media/{media_id}/favorite` with the session cookie. Confirm
   the response is HTTP 200 and the returned JSON body is `{"favorite": true}`
   (the item is now marked as a favorite).

4. Toggle the favorite flag a second time on the same item: call
   `POST /api/v1/media/{media_id}/favorite` again with the session cookie.
   Confirm the response is HTTP 200 and the returned JSON body is
   `{"favorite": false}` (the favorite has been cleared).

5. List existing tags for the user: call `GET /api/v1/tags` with the session
   cookie. Confirm the response is HTTP 200. The body is a JSON array which
   may be empty. Save the current tag count as `initial_tag_count`.

6. Add the tag `e2e-test` to the media item: call
   `POST /api/v1/media/{media_id}/tags` with the session cookie and body
   `{"tag": "e2e-test"}`. Confirm the response is HTTP 200 and the returned
   JSON body is `{"status": "ok"}`.

7. List tags again: call `GET /api/v1/tags` with the session cookie. Confirm
   the response is HTTP 200 and the returned array now contains an entry whose
   `name` field equals `e2e-test`.

8. Remove the tag from the media item: call
   `DELETE /api/v1/media/{media_id}/tags/e2e-test` with the session cookie.
   Confirm the response is HTTP 200 and the returned JSON body is
   `{"status": "ok"}`.

9. Retrieve the note for the media item before any note exists: call
   `GET /api/v1/media/{media_id}/notes` with the session cookie. Confirm the
   response status code is either HTTP 200 (with an empty or null body) or
   HTTP 204 (No Content) — both indicate that no note is currently stored.

10. Create the note: call `POST /api/v1/media/{media_id}/notes` with the session
    cookie and body `{"content": "e2e note"}`. Confirm the response is HTTP 200
    and the returned JSON object has a `content` field equal to `e2e note` and
    a `media_id` field equal to `media_id`.

11. Retrieve the note again: call `GET /api/v1/media/{media_id}/notes` with the
    session cookie. Confirm the response is HTTP 200 and the returned JSON
    object has a `content` field equal to `e2e note`.

12. Delete the note: call `DELETE /api/v1/media/{media_id}/notes` with the
    session cookie. Confirm the response is HTTP 200 and the returned JSON
    body is `{"status": "ok"}`.

13. Retrieve the note one more time after deletion: call
    `GET /api/v1/media/{media_id}/notes` with the session cookie. Confirm the
    response is either HTTP 404 (Not Found) or HTTP 204 (No Content) or HTTP
    200 with an empty/null body — any of these indicates that the note has
    been removed.
