API Reference
=============

### Public

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/bootstrap` | Create the first admin account |
| `POST` | `/api/login` | Login |
| `GET` | `/healthz` | Liveness probe (no DB) |
| `GET` | `/readyz` | Readiness probe (DB ping) |
| `GET` | `/s/{token}` | View a shared media item |
| `GET` | `/s/{token}/stream` | Stream shared media |
| `GET` | `/s/{token}/thumbnail` | Thumbnail for shared media |
| `GET` | `/s/{token}/download` | Download shared media |

### Session-required

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/logout` | Logout |
| `GET` | `/api/sets` | List sets |
| `GET` | `/api/sets/{id}/browse` | Browse a set (folder navigation) |
| `GET` | `/api/sets/{id}/cover` | Get set cover image |
| `POST` | `/api/sets/{id}/cover` | Update set cover image |
| `POST` | `/api/sets/{id}/upload` | Upload file to set |
| `GET` | `/api/media` | List/search media (with filters) |
| `GET` | `/api/media/{id}` | Get media details |
| `GET` | `/api/media/{id}/stream` | Stream media (range support) |
| `GET` | `/api/media/{id}/download` | Download original file |
| `GET` | `/api/media/{id}/thumbnail` | Get thumbnail |
| `POST` | `/api/media/{id}/thumbnail` | Regenerate thumbnail |
| `POST` | `/api/media/{id}/favorite` | Toggle favorite |
| `POST` | `/api/media/{id}/tags` | Add tag |
| `DELETE` | `/api/media/{id}/tags/{tag}` | Remove tag |
| `POST` | `/api/media/{id}/shares` | Create share link |
| `GET` | `/api/media/{id}/shares` | List shares for a media item |
| `GET` | `/api/media/{id}/notes` | Get note |
| `POST` | `/api/media/{id}/notes` | Upsert note |
| `DELETE` | `/api/media/{id}/notes` | Delete note |
| `POST` | `/api/progress` | Save playback progress |
| `DELETE` | `/api/media/{id}` | Soft-delete media |
| `POST` | `/api/media/{id}/restore` | Restore soft-deleted media |
| `GET` | `/api/shares` | List my shares |
| `DELETE` | `/api/shares/{token}` | Revoke share |
| `GET` | `/api/podcasts` | List podcasts |
| `GET` | `/api/podcasts/{id}/episodes` | List episodes |
| `POST` | `/api/podcasts/episodes/{id}/download` | Download episode to server |
| `POST` | `/api/podcasts/episodes/{id}/complete` | Toggle episode listened |

### Admin-only

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/admin/users` | List users |
| `POST` | `/api/admin/users` | Create user |
| `DELETE` | `/api/admin/users/{id}` | Delete user |
| `GET` | `/api/admin/permissions` | List permissions |
| `POST` | `/api/admin/permissions` | Grant set permission |
| `DELETE` | `/api/admin/permissions` | Revoke set permission |
| `POST` | `/api/admin/rescan` | Trigger library rescan |
| `GET` | `/api/admin/scan-progress` | Get scan progress |
| `GET` | `/api/admin/trash` | List soft-deleted media |
| `POST` | `/api/podcasts` | Subscribe to podcast feed |

### Query parameters for `GET /api/media`

| Parameter | Description |
|-----------|-------------|
| `search` | Plain text search on file name |
| `set_id` | Filter by a single set ID |
| `set_ids` | Filter by comma-separated set IDs |
| `type` | Filter by media type (e.g. `video`, `audio`, `image`) |
| `favorites` | `true` or `1` to show favorites only |
| `tags` | Comma-separated tag names |
| `min_duration` | Minimum duration in minutes |
| `max_duration` | Maximum duration in minutes |
| `filesize_min` | Minimum file size in MB |
| `filesize_max` | Maximum file size in MB |
| `sort` | Sort order (e.g. `random`) |
| `limit` | Page size |
| `offset` | Page offset |
| `folder` | Filter to a specific folder path |
| `parent` | Filter to items within a parent folder |