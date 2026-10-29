const DB = 'player-local-podcasts-v1';
const FEEDS = 'feeds';
const EPISODES = 'episodes';

function openDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(FEEDS)) db.createObjectStore(FEEDS, { keyPath: 'feed_key' });
      if (!db.objectStoreNames.contains(EPISODES)) {
        const store = db.createObjectStore(EPISODES, { keyPath: 'episode_key' });
        store.createIndex('by_feed', 'feed_key', { unique: false });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error || new Error('IndexedDB unavailable'));
  });
}

function txDone(tx) {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error || new Error('IndexedDB transaction failed'));
    tx.onabort = () => reject(tx.error || new Error('IndexedDB transaction aborted'));
  });
}

export function feedKeyFromURL(raw) {
  const url = new URL(raw);
  if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new TypeError('Feed URL must be http or https');
  url.hash = '';
  return `feed:${url.toString()}`;
}

export async function listLocalPodcastFeeds() {
  const db = await openDB();
  try {
    const tx = db.transaction(FEEDS, 'readonly');
    const req = tx.objectStore(FEEDS).getAll();
    const rows = await new Promise((resolve, reject) => {
      req.onsuccess = () => resolve(req.result || []);
      req.onerror = () => reject(req.error);
    });
    await txDone(tx);
    return rows.sort((a, b) => String(a.title || a.feed_url).localeCompare(String(b.title || b.feed_url)));
  } finally {
    db.close();
  }
}

export async function saveLocalPodcastFeed(feed) {
  const db = await openDB();
  try {
    const tx = db.transaction([FEEDS, EPISODES], 'readwrite');
    tx.objectStore(FEEDS).put(feed);
    const episodes = tx.objectStore(EPISODES);
    for (const episode of feed.episodes || []) episodes.put(episode);
    await txDone(tx);
  } finally {
    db.close();
  }
}

export async function removeLocalPodcastFeed(feedKey) {
  const db = await openDB();
  try {
    const tx = db.transaction([FEEDS, EPISODES], 'readwrite');
    tx.objectStore(FEEDS).delete(feedKey);
    const index = tx.objectStore(EPISODES).index('by_feed');
    const req = index.getAllKeys(feedKey);
    const keys = await new Promise((resolve, reject) => {
      req.onsuccess = () => resolve(req.result || []);
      req.onerror = () => reject(req.error);
    });
    for (const key of keys) tx.objectStore(EPISODES).delete(key);
    await txDone(tx);
  } finally {
    db.close();
  }
}

export async function listLocalPodcastEpisodes(feedKey) {
  const db = await openDB();
  try {
    const tx = db.transaction(EPISODES, 'readonly');
    const req = tx.objectStore(EPISODES).index('by_feed').getAll(feedKey);
    const rows = await new Promise((resolve, reject) => {
      req.onsuccess = () => resolve(req.result || []);
      req.onerror = () => reject(req.error);
    });
    await txDone(tx);
    return rows.sort((a, b) => (b.published_at || 0) - (a.published_at || 0));
  } finally {
    db.close();
  }
}

function textOf(node, names) {
  for (const name of names) {
    const el = node.getElementsByTagName(name)[0] || (name.includes(':') ? null : node.querySelector(name));
    if (el?.textContent?.trim()) return el.textContent.trim();
  }
  return '';
}

function imageOf(node) {
 for (const el of [...node.children]) {
  if (el.localName === 'image') {
   const value = el.getAttribute('href') || textOf(el, ['url']);
   if (value) return value;
  }
  if (el.localName === 'thumbnail' && el.getAttribute('url')) return el.getAttribute('url');
 }
 return '';
}

export function parsePodcastFeedXML(xmlText, feedURL) {
  const doc = new DOMParser().parseFromString(xmlText, 'application/xml');
  if (doc.querySelector('parsererror')) throw new Error('Feed XML could not be parsed');
  const channel = doc.querySelector('channel') || doc.querySelector('feed');
  if (!channel) throw new Error('Not a podcast RSS/Atom feed');
  const title = textOf(channel, ['title']) || feedURL;
  const description = textOf(channel, ['description', 'subtitle']);
  const image_url = imageOf(channel);
  const items = [...doc.querySelectorAll('item, entry')];
  const feed_key = feedKeyFromURL(feedURL);
  const episodes = [];
  for (const item of items) {
    const enclosure = item.querySelector('enclosure') || item.querySelector('link[rel="enclosure"]');
    const href = enclosure?.getAttribute('url') || enclosure?.getAttribute('href') || textOf(item, ['link']);
    if (!href) continue;
    const epTitle = textOf(item, ['title']) || href;
    const guid = textOf(item, ['guid', 'id']) || href;
    const pub = textOf(item, ['pubDate', 'published', 'updated']);
    const published_at = pub ? Date.parse(pub) || 0 : 0;
    episodes.push({
      episode_key: `${feed_key}|${guid}`,
      feed_key,
      title: epTitle,
      description: textOf(item, ['content:encoded', 'content', 'description', 'summary']),
      image_url: imageOf(item),
      feed_image_url: image_url,
      enclosure_url: href,
      published_at,
    });
  }
  return { feed_key, feed_url: feedURL, title, description, image_url, episodes, refreshed_at: Date.now() };
}

export async function fetchPodcastFeed(feedURL) {
  feedKeyFromURL(feedURL);
  let response;
  try {
    response = await fetch(feedURL, {
      mode: 'cors',
      credentials: 'omit',
      headers: { Accept: 'application/rss+xml, application/atom+xml, application/xml, text/xml, */*' },
    });
  } catch (error) {
    const err = new Error('This feed blocked browser access (CORS). Sign in to use the server-connected podcast workflow, or pick a feed that allows cross-origin reads.');
    err.cause = error;
    err.code = 'cors';
    throw err;
  }
  if (!response.ok) throw new Error(`Feed HTTP ${response.status}`);
  const xml = await response.text();
  return parsePodcastFeedXML(xml, response.url || feedURL);
}
