---
id: S01
title: "Bootstrap → first user"
tags: [auth, web, bootstrap]
preconditions:
  server_state: fresh          # harness must start the server against an empty temp DB
  fixtures: []
assertions:
  - db: "SELECT id FROM users WHERE is_admin=1"   # at least one admin row
  - db: "SELECT id FROM users"                    # at least one user row
  - url_contains: /login.html
skip: false
---

1. Confirm the server is reachable: `GET /healthz` must return HTTP 200.

2. Navigate to the root URL (`/`). Confirm the browser is redirected to
   `/bootstrap.html` (the BootstrapRedirect middleware redirects unauthenticated
   requests when the database is empty).

3. Confirm the bootstrap form is visible: look for a `<form>` that contains
   inputs for "username" and "password".

4. Fill in the username field with `admin` and the password field with
   `TestPassw0rd!`. Fill in the confirm-password field (if present) with the
   same value.

5. Submit the form by clicking the submit button (or pressing Enter). The
   browser should be redirected to `/login.html` after a successful bootstrap.

6. Confirm the browser URL now contains `/login.html`.

7. Fill in the login form with username `admin` and password `TestPassw0rd!`
   and submit it.

8. Confirm the browser lands on the home page (`/` or `/index.html`) and that
   an authenticated session cookie named `session` is present in the browser.

9. Confirm the admin panel link is accessible: navigate to the web UI and
   verify that an element indicating admin access (e.g. a settings or admin
   menu item) is visible.

10. Via the API, call `GET /api/v1/admin/users` with the session cookie. Confirm
    the response is HTTP 200 and contains exactly one user object whose
    `is_admin` field is `true` (SQLite stores this as 1) and whose `username`
    is `admin`.
