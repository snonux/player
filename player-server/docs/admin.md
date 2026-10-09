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

### Thumbnail Naming and Upgrades

Generated thumbnails live in a hidden `.thumbnails` directory and are named
after the source file's full name plus `.jpg`, so every media file has its
own thumbnail:

| Source | Created by | Thumbnail |
|--------|------------|-----------|
| `set/holiday.mp4` | rescan or upload | `set/.thumbnails/holiday.mp4.jpg` |
| `set/holiday.png` | rescan or upload | `set/.thumbnails/holiday.png.jpg` |
| `set/a/clip.mp4` | rescan | `set/.thumbnails/a/clip.mp4.jpg` |
| `set/a/clip.mp4` | upload | `set/a/.thumbnails/clip.mp4.jpg` |

Releases up to v0.2.2 named thumbnails after the stem only (`holiday.jpg`)
and the scanner kept them flat in the set's `.thumbnails` directory. Files
sharing a stem (`holiday.mp4` and `holiday.png`), or a name in two folders of
one set, overwrote each other's thumbnail.

After upgrading, **run one Rescan**. Until then nothing breaks: stored
thumbnail paths keep working, and affected cards simply keep showing the
shared thumbnail. The rescan then, for every already indexed video and image:

- renames a thumbnail used by a single media item to its new name (no
  re-encoding, a manually regenerated frame is kept);
- generates a fresh thumbnail for each item that shared one with another item;
- deletes old thumbnail files nothing refers to any more.

An item whose migration fails keeps its old thumbnail and is retried on the
next rescan. Later rescans find nothing left to migrate.

### Managing Trash

- `GET /api/admin/trash` — list soft-deleted media
- `DELETE /api/media/{id}` — soft-delete a media item
- `POST /api/media/{id}/restore` — restore a soft-deleted item

Soft-deleted media remains on disk until garbage collection removes it (see `GC_INTERVAL_MINUTES`).