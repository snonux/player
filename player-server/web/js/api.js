import { offlineMusicMode } from './offline-context.js';
const API_BASE = '';
import { loginDestination } from './navigation-url.js';
import { radioSearchQuery } from './radio-core.js';

export const NO_NOTE = Symbol('no note');

// redirectToLogin sends the browser to the login page after a 401 (expired
// session), unless it already is on the login or bootstrap page. Shared with
// playback.js, whose compat-stream probe does not go through api().
export function redirectToLogin() {
  if (offlineMusicMode()) return;
  const p = location.pathname;
  if (!p.includes('login') && !p.includes('bootstrap')) {
    location.href = loginDestination('/login.html', location.pathname + location.search + location.hash);
  }
}

async function api(path, options = {}) {
  if (offlineMusicMode()) throw new TypeError('Offline music uses saved downloads. Reconnect and sign in for online actions.');
  const url = `${API_BASE}${path}`;
  const isForm = options.body instanceof FormData;
  const headers = { Accept: 'application/json', ...(options.headers || {}) };
  if (!isForm && options.body && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json';
  }

  const res = await fetch(url, {
    credentials: 'include',
    headers,
    method: options.method || 'GET',
    body: isForm ? options.body : (options.body ? JSON.stringify(options.body) : undefined),
    signal: options.signal,
  });

  if (res.status === 204) return 'noContentValue' in options ? options.noContentValue : null;
  if (res.status === 401) {
    let msg = 'Unauthorized';
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {}
    if (!options.quietAuth) redirectToLogin();
    const error = new Error(msg); error.status = 401; throw error;
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {}
    const error = new Error(msg || `HTTP ${res.status}`);
    error.status = res.status;
    throw error;
  }
  const ct = res.headers.get('content-type') || '';
  return ct.includes('application/json') ? res.json() : res.text();
}

export const API = {
  offlineMusicRead: (path,signal) => api(path,{quietAuth:true,signal}),
  progressBatch: (updates,user) => api('/api/progress/batch',{method:'POST',quietAuth:true,body:{updates,expected_user_id:user}}),
  listeningHistory: (params={},user) => api(`/api/music/history?${new URLSearchParams({...params,expected_user_id:user || 0})}`),
  configureListeningHistory: (settings,user) => api(`/api/music/history?expected_user_id=${user || 0}`,{method:'PUT',body:settings}),
  clearListeningHistory: user => api(`/api/music/history?expected_user_id=${user || 0}`,{method:'DELETE'}),
  musicEnrichment: (kind,key,refresh=false) => api(`/api/music/enrichment/${kind}/${encodeURIComponent(key)}`,{method:refresh?'POST':'GET'}),
  login: (username, password) => api('/api/login', { method: 'POST', body: { username, password } }),
  logout: () => api('/api/logout', { method: 'POST' }),
  bootstrap: (username, password) => api('/api/bootstrap', { method: 'POST', body: { username, password } }),
  authStatus: () => api('/api/v1/auth/status'),
  config: () => api('/api/config'),
  syncConfig: () => api('/api/config', { quietAuth: true }),
  playlistRules: (id,rules) => api(`/api/playlists/${encodeURIComponent(id)}/rules`,{method:'PUT',body:{rules}}),
  playlistMembership: () => api('/api/playlists/membership'),
  addToPlaylists: body => api('/api/playlists/songs',{method:'POST',body}),
  editPlaylistSongs: (id,body) => api(`/api/playlists/${encodeURIComponent(id)}/songs`,{method:'POST',body}),
  importPlaylist: body => api('/api/playlists/import',{method:'POST',body}),
  syncPlaylistSource: (id,detach=false) => api(`/api/playlists/${encodeURIComponent(id)}/source`,{method:detach?'DELETE':'POST'}),
  exportPlaylist: id => api(`/api/playlists/${encodeURIComponent(id)}/export.m3u`),
  playlists: (params={}) => api(`/api/playlists?${new URLSearchParams(params)}`),
  musicRating: (kind,key,rating) => api(`/api/music/ratings/${kind}/${encodeURIComponent(key)}`,{method:'PUT',body:{rating}}),
  musicFavorite: (kind,key,value) => api(`/api/music/favorites/${kind}/${encodeURIComponent(key)}`,{method:value?'PUT':'DELETE'}),
  playlist: id => api(`/api/playlists/${encodeURIComponent(id)}`),
  createPlaylist: body => api('/api/playlists', {method:'POST',body}),
  updatePlaylist: (id,body) => api(`/api/playlists/${encodeURIComponent(id)}`,{method:'PATCH',body}),
  deletePlaylist: id => api(`/api/playlists/${encodeURIComponent(id)}`,{method:'DELETE'}),
  addPlaylistAlbum: (id,album_key) => api(`/api/playlists/${encodeURIComponent(id)}/albums`,{method:'POST',body:{album_key}}),
  musicFacets: () => api(`/api/music/facets`),
  musicSearch: params => api(`/api/music/search?${new URLSearchParams(params)}`),
  artists: (params = {}) => api(`/api/music/artists?${new URLSearchParams(params)}`),
  artist: key => api(`/api/music/artists/${encodeURIComponent(key)}`),
  musicMix: (kind, key, minRating = 0) => api(`/api/music/mix/${kind}/${encodeURIComponent(key)}?min_rating=${minRating}`),
  relatedMusic: (kind, key) => api(`/api/music/related/${kind}/${encodeURIComponent(key)}`),
  queueAlbums: media_ids => api('/api/music/queue-albums', {method: 'POST', body: {media_ids}}),
  albums: (params = {}) => api(`/api/music/albums?${new URLSearchParams(params)}`),
  album: key => api(`/api/music/albums/${encodeURIComponent(key)}`),
  sets: () => api('/api/sets'),
  media: (qs) => {
    const params = new URLSearchParams();
    Object.entries(qs).forEach(([k, v]) => {
      if (v !== '' && v !== null && v !== undefined && v !== false) params.set(k, v);
    });
    const s = params.toString();
    return api(`/api/media${s ? '?' + s : ''}`);
  },
  mediaDetail: (id) => api(`/api/media/${id}`),
  castTicket: (id) => api(`/api/media/${id}/cast`, { method: 'POST' }),
  progress: (mediaId, positionSeconds) => api('/api/progress', { method: 'POST', body: { media_id: mediaId, position_seconds: positionSeconds } }),
  progressStatus: (mediaId, status) => {
    const id = typeof mediaId === 'string' && /^\d+$/.test(mediaId) ? Number(mediaId) : mediaId;
    if (!Number.isSafeInteger(id) || id <= 0) {
      throw new Error('Invalid media ID');
    }
    return api('/api/progress/status', { method: 'POST', body: { media_id: id, status } });
  },
  inProgress: () => api('/api/in-progress'),
  favorite: (id) => api(`/api/media/${id}/favorite`, { method: 'POST' }),
  notes: (id) => api(`/api/media/${id}/notes`, { noContentValue: NO_NOTE }),
  saveNote: (id, content) => api(`/api/media/${id}/notes`, { method: 'POST', body: { content } }),
  deleteNote: (id) => api(`/api/media/${id}/notes`, { method: 'DELETE' }),
  bookmarks: (id) => api(`/api/media/${id}/bookmarks`),
  saveBookmark: (id, positionSeconds, label = '') => api(`/api/media/${id}/bookmarks`, {
    method: 'POST', body: { position_seconds: positionSeconds, label },
  }),
  deleteBookmark: (id, bookmarkId) => api(`/api/media/${id}/bookmarks/${bookmarkId}`, { method: 'DELETE' }),
  share: (id) => api(`/api/media/${id}/shares`, { method: 'POST' }),
  myShares: () => api('/api/shares'),
  updateShare: (token, patch) => api(`/api/shares/${encodeURIComponent(token)}`, { method: 'PATCH', body: patch }),
  revokeShare: (token) => api(`/api/shares/${token}`, { method: 'DELETE' }),
  tags: () => api('/api/tags'),
  download: (id) => api(`/api/media/${id}/download`),
  regenThumbnail: (id) => api(`/api/media/${id}/thumbnail`, { method: 'POST' }),
  browse: (setId, parent) => {
    const params = new URLSearchParams();
    if (parent) params.set('parent', parent);
    const s = params.toString();
    return api(`/api/sets/${setId}/browse${s ? '?' + s : ''}`);
  },
  regenCover: (setId, folder = '') => {
    const params = new URLSearchParams();
    if (folder) params.set('folder', folder);
    const s = params.toString();
    return api(`/api/sets/${setId}/cover${s ? '?' + s : ''}`, { method: 'POST' });
  },
  upload: (setId, formData) => api(`/api/sets/${setId}/upload`, { method: 'POST', body: formData }),
  addTag: (id, tag) => api(`/api/media/${id}/tags`, { method: 'POST', body: { tag } }),
  removeTag: (id, tag) => api(`/api/media/${id}/tags/${encodeURIComponent(tag)}`, { method: 'DELETE' }),
  restore: (id) => api(`/api/media/${id}/restore`, { method: 'POST' }),
  trash: () => api('/api/admin/trash'),
  myTrash: () => api('/api/trash'),
  deleteMedia: (id) => api(`/api/media/${id}`, { method: 'DELETE' }),
  users: () => api('/api/admin/users'),
  scanProgress: () => api('/api/admin/scan-progress'),
  cancelScan: () => api('/api/admin/scan-cancel', { method: 'POST' }),
  createUser: (body) => api('/api/admin/users', { method: 'POST', body }),
  deleteUser: (id) => api(`/api/admin/users/${id}`, { method: 'DELETE' }),
  permissions: () => api('/api/admin/permissions'),
  setPermissions: (body) => api('/api/admin/permissions', { method: 'POST', body }),
  delPermissions: (body) => api('/api/admin/permissions', { method: 'DELETE', body }),
  rescan: () => api('/api/admin/rescan', { method: 'POST' }),
  musicLibraries: () => api('/api/music/libraries'),
  adminLibraries: () => api('/api/admin/libraries'),
  musicIdentityConflicts: () => api('/api/admin/music-identity/conflicts'),
  resolveMusicIdentityConflict: (id, body) => api(`/api/admin/music-identity/conflicts/${id}/resolve`, { method: 'POST', body }),
  createLibrary: (body) => api('/api/admin/libraries', { method: 'POST', body }),
  renameLibrary: (id, body) => api(`/api/admin/libraries/${id}`, { method: 'PUT', body }),
  deleteLibrary: (id) => api(`/api/admin/libraries/${id}`, { method: 'DELETE' }),
  grantLibraryAccess: (id, body) => api(`/api/admin/libraries/${id}/access`, { method: 'POST', body }),
  revokeLibraryAccess: (id, body) => api(`/api/admin/libraries/${id}/access`, { method: 'DELETE', body }),
  plugins: () => api('/api/admin/plugins'),
  enablePlugin: (id) => api(`/api/admin/plugins/${encodeURIComponent(id)}/enable`, { method: 'POST' }),
  disablePlugin: (id) => api(`/api/admin/plugins/${encodeURIComponent(id)}/disable`, { method: 'POST' }),
  invokePlugin: (id, capability) => api(`/api/admin/plugins/${encodeURIComponent(id)}/invoke`, {
    method: 'POST',
    body: { capability },
  }),
  podcasts: ({ quietAuth = false } = {}) => api('/api/podcasts', { quietAuth }),
  podcastEpisodes: (setId, pagination, legacyOffset) => {
    let limit = 50;
    let offset = 0;
    if (typeof pagination === 'number') {
      if (typeof legacyOffset === 'number') {
        limit = pagination;
        offset = legacyOffset;
      } else {
        offset = pagination;
      }
    } else if (pagination) {
      limit = pagination.limit ?? limit;
      offset = pagination.offset ?? offset;
    }
    const params = new URLSearchParams({ limit, offset });
    return api(`/api/podcasts/${setId}/episodes?${params}`);
  },
  // Subscribed / all-episodes views (contract §6.3): feed_key identity,
  // completion filter, server pagination.
  subscribedEpisodes: ({ feedKey = '', filter = 'uncompleted', limit = 100, offset = 0 } = {}) => {
    const params = new URLSearchParams({
      filter: filter || 'uncompleted',
      limit: String(limit),
      offset: String(offset),
    });
    if (feedKey) params.set('feed_key', feedKey);
    return api(`/api/podcasts/episodes?${params}`);
  },
  subscribePodcast: (feedUrl, setName) => api('/api/podcasts', { method: 'POST', body: { feed_url: feedUrl, set_name: setName } }),
  // Admin only: deletes the feed's server copies for everyone, and the
  // catalog feed once nobody is subscribed. Not an unsubscribe.
  removePodcastFromServer: (feedId) => api(`/api/podcasts/${feedId}`, { method: 'DELETE' }),
  syncPodcast: (body) => api('/api/podcasts/sync', { method: 'POST', quietAuth: true, body }),
  syncPodcastSubscriptions: (subscriptions) => api('/api/podcasts/sync', { method: 'POST', quietAuth: true, body: { subscriptions } }),
  downloadEpisode: (episodeId, { signal } = {}) => api(`/api/podcasts/episodes/${episodeId}/download`, { method: 'POST', signal }),
  toggleEpisodeComplete: (episodeId) => api(`/api/podcasts/episodes/${episodeId}/complete`, { method: 'POST' }),
  radioStations: (filters) => api(`/api/radio/stations${radioSearchQuery(filters)}`),
  radioTags: (limit = 100) => api(`/api/radio/tags?limit=${limit}`),
  radioCountries: () => api('/api/radio/countries'),
  radioBookmarks: () => api('/api/radio/bookmarks'),
  bookmarkStation: (uuid) => api(`/api/radio/bookmarks/${encodeURIComponent(uuid)}`, { method: 'PUT' }),
  unbookmarkStation: (uuid) => api(`/api/radio/bookmarks/${encodeURIComponent(uuid)}`, { method: 'DELETE' }),
};
