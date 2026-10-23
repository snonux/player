# Music A05 — independent music library roots and access (`sk3`)

## Scope

Optional multi-root music libraries: admin CRUD for named filesystem roots,
per-user access via **existing set permissions** (not a parallel ACL),
aggregate statistics, Music-lens library selection, and cross-library
playlists. Single-library installs stay unchanged (no selector chrome).
Inaccessible music stays hidden the same way set ACL already hides sets.

Does **not** replace Admin › Users or Admin › Permissions. Does **not**
implement A06 / `tk3` music identity preservation.

## UI design (task-specific)

### Admin › Libraries

| State | Presentation |
| --- | --- |
| Section collapsed | Button **Libraries** (`aria-expanded=false`) next to Plugins |
| Empty (only default) | One row: default library name + path + stats; form to add another root |
| Loading | Status “Loading libraries…” (`aria-live=polite`) |
| Row | Name, path, song/set counts + duration, **Access** role selects (same viewer/owner values as Permissions), **Rename**, **Remove** (confirm; blocked if last library) |
| Add | Name + absolute path; primary **Add library**; errors inline |
| Permission | Admin session only |
| Narrow / themes | Existing admin list + theme tokens |

Set-level **Permissions** table remains for fine-grained grants. Library
**Access** grants/revokes the chosen role on every set belonging to that
library (extend, do not replace).

### Music lens — library selection

| State | Presentation |
| --- | --- |
| One accessible library | Selector **hidden** (simple single-library use) |
| Multiple | Compact `<select multiple>` / chip row: **All libraries** (default) or named libraries; `mlibrary` URL param |
| Empty after filter | Existing empty copy; no leak of inaccessible library names |
| Loading / error / offline | Reuse Music status / toast patterns |
| Keyboard | Native select; j/k still move cards; Esc unchanged |

### Android

Drawer/header **Libraries** filter when `GET /music/libraries` returns more
than one accessible library. Touch chips; Back clears filter to All. No vi
keys. Admin library CRUD stays web/admin API (same as Plugins).

## Data model

```
music_libraries(id, name, root_path UNIQUE, created_at)
sets.library_id → music_libraries(id)
UNIQUE(library_id, root_path) on sets
```

- Startup ensures a **default** library whose `root_path` is `MEDIA_ROOT`.
- Existing sets are attached to the default library.
- Additional libraries are independent absolute roots; scanner indexes each
  root’s immediate subdirectories as sets tagged with that `library_id`.
- Streaming / write / compat confinement uses the union of library roots
  (plus thumbnail cache), not a single `MEDIA_ROOT` only.

## API

| Method | Path | Role |
| --- | --- | --- |
| `GET` | `/api/admin/libraries` | Admin list + aggregate stats |
| `POST` | `/api/admin/libraries` | Create `{name, root_path}` |
| `PUT` | `/api/admin/libraries/{id}` | Rename `{name}` |
| `DELETE` | `/api/admin/libraries/{id}` | Delete (refuse default / non-empty policy documented in handler) |
| `POST` | `/api/admin/libraries/{id}/access` | `{user_id, role}` → grant on all sets in library |
| `DELETE` | `/api/admin/libraries/{id}/access` | `{user_id}` → revoke on all sets in library |
| `GET` | `/api/music/libraries` | Caller-visible libraries (+ accessible stats) |
| Music catalog | `library_ids=1,2` query | Filter albums/artists/search/facets/playlists sources |

Existing `/api/admin/permissions` unchanged.

## Cross-library playlists

Playlist entries remain media ids. Songs from any permitted set may be
mixed; listing and play already drop inaccessible media. No second playlist
store.

## Verification (implementation slice)

Targeted Go tests: migration/default library, multi-root scan + confinement,
CRUD, library access → set_permissions, catalog `library_ids` ACL, playlist
cross-library visibility. Web JS unit tests for admin Libraries + selector
visibility. Android widget/API unit tests for filter chips.

**Deferred (honest gaps):** Chrome / Firefox / emulator E2E journeys from the
task scenario (two roots, two accounts, browse/playlist/share/download) until
integrated verification.
