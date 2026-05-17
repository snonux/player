Admin Guide
===========

Admin endpoints are gated by `RequireAdmin` middleware (checks `users.is_admin`). The admin panel is opened via the "Admin" button in the SPA header (shown only when the current user is an admin).

### Bootstrap

On first visit (no users exist), you are redirected to `/bootstrap.html` to create the initial admin account.

### Creating Users

1. Open the admin panel.
2. Enter username, password, and check "Is admin" if desired.
3. Submit — the frontend calls `POST /api/admin/users`.
4. Admins cannot delete themselves via `DELETE /api/admin/users/:id`.

### Managing Set Permissions

- `GET /api/admin/permissions` — list permissions matrix
- `POST /api/admin/permissions` — grant access to a set (`body: { set_id, user_id, role: "owner" | "viewer" }`)
- `DELETE /api/admin/permissions` — revoke access (`body: { set_id, user_id }`)

Roles:
- `owner` — can upload to the set, soft-delete / restore media, regenerate thumbnails
- `viewer` — can browse and play media in the set

Admins implicitly see all sets without explicit permission rows.

### Rescanning the Library

Click **Rescan** in the admin panel, or call:

```bash
curl -X POST -b session=<cookie> http://<host>/api/admin/rescan
```

This triggers `FSScanner.Scan()`, which:
1. Scans immediate subdirectories of `MEDIA_ROOT` as **sets**
2. Recursively walks each set for supported media files
3. Probes new files with `ffprobe`
4. Generates thumbnails for video files
5. Inserts new records into the `media` table

### Managing Trash

- `GET /api/admin/trash` — list soft-deleted media
- `DELETE /api/media/{id}` — soft-delete a media item
- `POST /api/media/{id}/restore` — restore a soft-deleted item

Soft-deleted media remains on disk until garbage collection removes it (see `GC_INTERVAL_MINUTES`).