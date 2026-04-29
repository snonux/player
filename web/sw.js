const CACHE = 'kiss-v1';
const ASSETS = [
  '/',
  '/index.html',
  '/login.html',
  '/bootstrap.html',
  '/css/theme.css',
  '/css/layout.css',
  '/css/components.css',
  '/css/player.css',
  '/css/login.css',
  '/js/app.js',
  '/js/state.js',
  '/js/api.js',
  '/js/keyboard.js',
  '/js/selection.js',
  '/js/player.js',
  '/js/search.js',
  '/js/shuffle.js',
  '/js/themes.js',
  '/js/notes.js',
  '/js/admin.js',
  '/js/pwa.js',
  '/manifest.json',
];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(ASSETS)));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
    )
  );
});

self.addEventListener('fetch', (e) => {
  e.respondWith(caches.match(e.request).then((res) => res || fetch(e.request)));
});
