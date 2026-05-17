Podcast Support
===============

Podcasts are **special sets** (`sets.is_podcast = 1`). They reuse set permissions, browsing, and cover images, while adding feed management and episode tracking.

### Subscribing

Admin opens the **Podcasts** button in the admin panel (or calls `POST /api/podcasts`):
- Submit an RSS/Atom feed URL and optional folder name.
- Server creates a set folder, parses the feed, downloads the cover image, and inserts episodes into `podcast_episodes`.

### Episode Management

Episodes are stored in `podcast_episodes` and rendered in the browse grid for podcast sets:
- **Undownloaded** episodes show a **Download to server** button (calls `POST /api/podcasts/episodes/{id}/download`).
- **Downloaded** episodes become regular `media` rows and appear as normal media cards.
- Users can mark episodes as listened/unlistened via the checkmark button.

### Background Feed Checker

A background goroutine (`CheckFeeds`) refreshes feeds every hour (configurable via `PODCAST_CHECK_INTERVAL_MINUTES`). It uses conditional GET (`If-None-Match`, `If-Modified-Since`) to avoid re-downloading unchanged feeds.

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/podcasts` | List podcast sets |
| `POST` | `/api/podcasts` | Subscribe to a new feed (admin) |
| `GET` | `/api/podcasts/{id}/episodes` | List episodes with status |
| `POST` | `/api/podcasts/episodes/{id}/download` | Server-side download |
| `POST` | `/api/podcasts/episodes/{id}/complete` | Toggle completion |