const API_BASE = '';

async function api(path, options = {}) {
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
  });

  if (res.status === 204) return null;
  if (res.status === 401) {
    let msg = 'Unauthorized';
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {}
    const p = location.pathname;
    if (!p.includes('login') && !p.includes('bootstrap')) {
      location.href = '/login.html';
    }
    throw new Error(msg);
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {}
    throw new Error(msg || `HTTP ${res.status}`);
  }
  const ct = res.headers.get('content-type') || '';
  return ct.includes('application/json') ? res.json() : res.text();
}

export const API = {
  login: (username, password) => api('/api/login', { method: 'POST', body: { username, password } }),
  logout: () => api('/api/logout', { method: 'POST' }),
  bootstrap: (username, password) => api('/api/bootstrap', { method: 'POST', body: { username, password } }),
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
  progress: (mediaId, positionSeconds) => api('/api/progress', { method: 'POST', body: { media_id: mediaId, position_seconds: positionSeconds } }),
  favorite: (id) => api(`/api/media/${id}/favorite`, { method: 'POST' }),
  notes: (id) => api(`/api/media/${id}/notes`),
  saveNote: (id, content) => api(`/api/media/${id}/notes`, { method: 'POST', body: { content } }),
  deleteNote: (id) => api(`/api/media/${id}/notes`, { method: 'DELETE' }),
  share: (id) => api(`/api/media/${id}/shares`, { method: 'POST' }),
  myShares: () => api('/api/shares'),
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
  users: () => api('/api/admin/users'),
  scanProgress: () => api('/api/admin/scan-progress'),
  createUser: (body) => api('/api/admin/users', { method: 'POST', body }),
  deleteUser: (id) => api(`/api/admin/users/${id}`, { method: 'DELETE' }),
  permissions: () => api('/api/admin/permissions'),
  setPermissions: (body) => api('/api/admin/permissions', { method: 'POST', body }),
  delPermissions: (body) => api('/api/admin/permissions', { method: 'DELETE', body }),
  rescan: () => api('/api/admin/rescan', { method: 'POST' }),
  podcasts: () => api('/api/podcasts'),
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
  subscribePodcast: (feedUrl, setName) => api('/api/podcasts', { method: 'POST', body: { feed_url: feedUrl, set_name: setName } }),
  downloadEpisode: (episodeId) => api(`/api/podcasts/episodes/${episodeId}/download`, { method: 'POST' }),
  toggleEpisodeComplete: (episodeId) => api(`/api/podcasts/episodes/${episodeId}/complete`, { method: 'POST' }),
};
