---
id: S22
title: "Share expiry (410 Gone) and token uniqueness"
tags: [share, api, public, security, db]
preconditions:
  server_state: running        # server running with admin account and at least one media item
  fixtures: []
assertions:
  - db: "SELECT token FROM shares"
  - db: "SELECT token FROM shares WHERE expires_at < '2021-01-01T00:00:00Z'"
  - status_code: "POST /api/v1/auth/login 200"
  - status_code: "GET /api/v1/media 200"
  - status_code: "GET /api/v1/shares 200"
skip: false
---

# Notes

This scenario covers two related share guarantees:

* **Part A — Expiry:** an expired share token must return HTTP 410 Gone
  from every public endpoint (`/s/{token}/thumbnail`, `/s/{token}/stream`,
  `/s/{token}/download`). The `expires_at` column on the `shares` table is
  rewritten in place via the `sqlite3` CLI (the harness lists `sqlite3` as
  a prerequisite in `test/e2e-llm/README.md`) so the test does not have to
  wait for the configured default expiry. The DB path is the same one the
  server is started with: `/tmp/player-e2e-llm.db` (overridable with the
  `PLAYER_DB` env var).
* **Part B — Token uniqueness:** creating multiple shares in quick
  succession on the same media item must produce 5 distinct, non-colliding
  tokens. Tokens are generated in `shareService.generateToken()` via
  `crypto/rand` (16 random bytes → 32 hex chars, 128 bits of entropy), so
  collisions should be astronomically unlikely.

If any public share endpoint still serves content after the share is
expired (i.e. returns a 2xx response instead of 410), record that as a
**security defect**: expired shares must not leak media. Likewise, if any
two tokens out of the five created in Part B are identical, record that as
a **security defect** in token generation.

---

# Part A — Expiry returns 410 Gone

1. Authenticate as an admin user: call `POST /api/v1/auth/login` with body
   `{"username": "admin", "password": "TestPassw0rd!"}`. Confirm the
   response is HTTP 200 and save the `session` cookie returned for all
   subsequent authenticated requests.

2. Find a media item to share: call `GET /api/v1/media?limit=1` with the
   session cookie. Confirm the response is HTTP 200 and contains at least
   one media object. Save the `id` of the first item as `media_id`.

3. Create a share link for that media item: call
   `POST /api/v1/media/{media_id}/shares` with the session cookie. Confirm
   the response is HTTP 200 and contains a non-empty `token` field. Save
   the `token` as `share_token_a`.

4. Confirm the freshly created token works publicly: call
   `GET {PLAYER_URL}/s/{share_token_a}/thumbnail` with **no** session
   cookie and **no** `Authorization` header. Confirm the response is HTTP
   200 (the token is valid and not yet expired).

5. Force the share to be expired by rewriting its `expires_at` column
   directly via the `sqlite3` CLI. The database path is
   `${PLAYER_DB:-/tmp/player-e2e-llm.db}` — use whichever value the
   harness exports (the default matches the server's `DB_PATH` documented
   in `test/e2e-llm/README.md`). Run this shell command:

   ```sh
   sqlite3 "${PLAYER_DB:-/tmp/player-e2e-llm.db}" \
     "UPDATE shares SET expires_at = '2020-01-01T00:00:00Z' WHERE token = '<share_token_a>'"
   ```

   Substitute `<share_token_a>` with the literal token from step 3.
   Confirm the command exits with status 0.

6. Verify the row really is expired in the DB: run

   ```sh
   sqlite3 "${PLAYER_DB:-/tmp/player-e2e-llm.db}" \
     "SELECT expires_at FROM shares WHERE token = '<share_token_a>'"
   ```

   Confirm the printed value is `2020-01-01T00:00:00Z`.

7. Call `GET {PLAYER_URL}/s/{share_token_a}/thumbnail` with **no** session
   cookie. Confirm the response is HTTP **410 Gone** (the
   `ErrShareExpired` path in `handleShareThumbnail`). Any 2xx response
   here is a security defect — expired shares must not serve content.

8. Call `GET {PLAYER_URL}/s/{share_token_a}/stream` with **no** session
   cookie. Confirm the response is HTTP **410 Gone** (the
   `ErrShareExpired` path in `handleShareStream`).

9. Call `GET {PLAYER_URL}/s/{share_token_a}/download` with **no** session
   cookie. Confirm the response is HTTP **410 Gone** (the
   `ErrShareExpired` path in `handleShareDownload`).

10. (Optional) Call `GET {PLAYER_URL}/s/{share_token_a}` (the HTML share
    page) with no session cookie. Confirm the response is HTTP 410 Gone
    (the `ErrShareExpired` path in `handleSharePage`).

11. Cleanup: revoke the expired share. From the authenticated context,
    call `DELETE /api/v1/shares/{share_token_a}`. Confirm the response is
    HTTP 200. (The revoke path uses `RevokeShare` which does not call
    `ValidateShareToken`, so an expired-but-not-revoked share can still
    be deleted by its owner.)

# Part B — Token uniqueness

12. Reuse the same admin session cookie and the same `media_id` from
    steps 1–2 above.

13. Create five share links in quick succession on the same media item by
    calling `POST /api/v1/media/{media_id}/shares` exactly five times,
    one immediately after the other. For each call, confirm the response
    is HTTP 200 and save the `token` field. Collect the five tokens as
    `share_tokens_b = [t1, t2, t3, t4, t5]`.

14. Confirm there are no in-memory collisions: assert that
    `len(set(share_tokens_b)) == 5` — i.e. all five tokens are distinct.
    Any collision here is a security defect in `generateToken()`
    (`internal/service/share.go`, which uses `crypto/rand.Read` on 16
    bytes → 32 hex chars). Note the token length: each token must be
    exactly 32 lowercase hex characters.

15. Confirm the server's owner-scoped listing returns the same five
    tokens: call `GET /api/v1/shares` with the admin session cookie.
    Confirm the response is HTTP 200 and that every token in
    `share_tokens_b` appears in the returned array's `token` fields.

16. Confirm no DB-level collisions across the entire `shares` table by
    comparing the count of distinct tokens against the row count. Run:

    ```sh
    sqlite3 "${PLAYER_DB:-/tmp/player-e2e-llm.db}" \
      "SELECT COUNT(DISTINCT token), COUNT(*) FROM shares"
    ```

    Confirm both numbers are equal. (Strictly speaking the schema
    declares `token TEXT PRIMARY KEY`, so the DB would reject a
    collision at INSERT time and the POST would have returned 5xx — but
    asserting the invariant here documents the contract explicitly.)

17. Cleanup: revoke each of the five test shares. For each `token` in
    `share_tokens_b`, call `DELETE /api/v1/shares/{token}` with the
    admin session cookie. Confirm each response is HTTP 200.

18. Confirm cleanup: call `GET /api/v1/shares` with the admin session
    cookie. Confirm the response is HTTP 200 and the returned array does
    NOT contain any token from `share_tokens_b` or `share_token_a`.
