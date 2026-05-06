const CACHE = 'kiss-v9';
const ASSETS = [
  '/',
  '/index.html',
  '/detach.html',
  '/login.html',
  '/bootstrap.html',
  '/css/theme.css',
  '/css/layout.css',
  '/css/components.css',
  '/css/player.css',
  '/css/lightbox.css',
  '/css/login.css',
  '/js/app.js',
  '/js/state.js',
  '/js/api.js',
  '/js/keyboard.js',
  '/js/selection.js',
  '/js/player.js',
  '/js/playback.js',
  '/js/imageViewer.js',
  '/js/detach.js',
  '/js/detachPopup.js',
  '/js/search.js',
  '/js/shuffle.js',
  '/js/themes.js',
  '/js/notes.js',
  '/js/admin.js',
  '/js/dom.js',
  '/js/utils.js',
  '/js/podcasts.js',
  '/js/pwa.js',
  '/js/lightbox.js',
  '/js/views/admin-status.js',
  '/js/views/help.js',
  '/js/views/media-actions.js',
  '/js/views/media-grid.js',
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
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
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
      .catch(() => caches.match(e.request))
  );
});
