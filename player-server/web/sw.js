const CACHE = 'kiss-v91';
const ASSETS = [
  '/',
  '/index.html',
  '/detach.html',
  '/login.html',
  '/bootstrap.html',
  '/css/theme.css',
  '/css/layout.css',
  '/css/components.css',
  '/css/renderer.css',
  '/css/radio.css',
  '/css/toast.css',
  '/css/player.css',
  '/css/radio.css',
  '/css/login.css',
  '/js/app.js',
  '/js/global-sync-controls.js',
  '/js/global-sync-view.js',
  '/js/global-sync.js',
  '/js/header-menu.js',
  '/js/local-files-store.js',
  '/js/local-files.js',
  '/js/local-podcasts-store.js',
  '/js/local-podcasts.js',
  '/js/music-libraries.js',
  '/js/offline-browse-model.js',
  '/js/offline-browse.js',
  '/js/offline-cache.js',
  '/js/offline-context.js',
  '/js/podcast-detached.js',
  '/js/podcast-discovery.js',
  '/js/podcast-global-sync.js',
  '/js/podcast-identity.js',
  '/js/podcast-playback.js',
  '/js/queue-autoplay.js',
  '/js/i18n.js',
  '/js/i18n-de.js',
  '/js/offline-pins.js',
  '/js/offline-pin-dialog.js',
  '/js/offline-pin-library.js',
  '/js/automatic-offline.js',
  '/js/music-presence.js',
  '/js/music-presence-controller.js',
  '/js/playback-presence.js',
  '/js/state.js',
  '/js/api.js',
  '/js/keyboard.js',
  '/js/keyboard-access.js',
  '/js/navigation.js',
  '/js/navigation-url.js',
  '/js/selection.js',
  '/js/player.js',
  '/js/music.js',
  '/js/music-search.js',
  '/js/music-facets.js',
  '/js/music-layout.js',
  '/js/media-session.js',
  '/js/queue.js',
  '/js/audio-transport.js',
  '/js/gapless-transport.js',
  '/js/gapless.js',
  '/js/queue-prepared.js',
  '/js/queue-storage.js',
  '/js/queue-sync.js',
  '/js/queue-shuffle.js',
  '/js/queue-actions.js',
  '/js/collection-actions.js',
  '/js/collection-resolver.js',
  '/js/queue-panel.js',
  '/js/playback-intent.js',
  '/js/playback.js',
  '/js/playback-speed.js',
  '/js/sleep-timer.js',
  '/js/compactControls.js',
  '/js/audioVisuals.js',
  '/js/replay-gain.js',
  '/js/lyrics.js',
  '/js/saved-lyrics.js',
  '/js/audioFormat.js',
  '/js/seekPreview.js',
  '/js/playback-personal.js',
  '/js/bookmarks.js',
  '/js/modal-focus.js',
  '/js/share-link.js',
  '/js/trash.js',
  '/js/streamSource.js',
  '/js/cast.js',
  '/js/renderer.js',
  '/js/jukebox.js',
  '/js/radio.js',
  '/js/imageViewer.js',
  '/js/detach.js',
  '/js/detachPopup.js',
  '/js/search.js',
  '/js/shuffle.js',
  '/js/themes.js',
  '/js/navigation-preferences.js',
  '/js/equalizer.js',
  '/js/music-favorites.js',
  '/js/music-ratings.js',
  '/js/music-listening.js',
  '/js/listening-history.js',
  '/js/api-tokens.js',
  '/js/listening-progress.js',
  '/js/music-related.js',
  '/js/music-mix.js',
  '/js/music-top-songs.js',
  '/js/music-discovery.js',
  '/js/music-home.js',
  '/js/music-enrichment.js',
  '/js/music-artwork.js',
  '/js/music-downloads.js',
  '/js/download-quality.js',
  '/js/collection-share.js',
  '/js/share-quality.js',
  '/js/share-edit.js',
  '/js/share-collection-player.js',
  '/js/queue-playlist.js',
  '/js/song-notifications.js',
  '/js/playback-cache.js',
  '/js/stream-quality.js',
  '/js/network-policy.js',
  '/js/notes.js',
  '/js/admin.js',
  '/js/dom.js',
  '/js/utils.js',
  '/js/podcasts.js',
  '/js/podcast-views.js',
  '/js/podcast-episode-state.js',
  '/js/podcast-subscriptions.js',
  '/js/radio.js',
  '/js/radio-core.js',
  '/js/pwa.js',
  '/js/views/admin-status.js',
  '/js/views/help.js',
  '/js/views/media-actions.js',
  '/js/views/media-grid.js',
  '/js/views/music-catalog.js',
  '/js/views/music-playlists.js',
  '/js/views/music-playlist-rules.js',
  '/js/views/picture-layout.js',
  '/js/views/thumbnail-size.js',
  '/js/views/media-info.js',
  '/js/views/playback-nav.js',
  '/js/views/sets.js',
  '/js/views/shares.js',
  '/js/views/tags.js',
  '/js/views/upload.js',
  '/manifest.json',
];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(ASSETS)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE && !k.startsWith('player-playback-v1-') && !k.startsWith('player-offline-v1-')).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  if (e.request.method !== 'GET') return;
  const url = new URL(e.request.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;
  e.respondWith(
    fetch(e.request)
      .then((res) => {
        const copy = res.clone();
        if (ASSETS.includes(url.pathname)) {
          caches.open(CACHE).then((c) => c.put(e.request, copy));
        }
        return res;
      })
      .catch(async () => {
        const exact = await caches.match(e.request);
        if (exact) return exact;
        // A saved Offline music link has a query string. Reuse the cached app
        // shell for an offline navigation even if this exact URL was never
        // visited online; the query still reaches the page after it loads.
        if (e.request.mode === 'navigate' && (url.pathname === '/' || url.pathname === '/index.html')) {
          return caches.match('/index.html');
        }
        return undefined;
      })
  );
});
