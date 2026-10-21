// Library links carry IDs and view state, never credentials or stream URLs.
const filterKeys = ['type', 'search', 'favorites', 'tags', 'sort', 'minDuration', 'maxDuration', 'minFileSize', 'maxFileSize'];
const positiveID = value => /^[1-9]\d*$/.test(String(value)) && Number.isSafeInteger(Number(value)) ? Number(value) : null;

export function readLibraryURL(search) {
  const params = new URLSearchParams(search);
  const music = ['albums','album','artists','artist','playlists','playlist'].includes(params.get('music')) ? { album: params.get('music') === 'album' ? params.get('id') || '' : '', artist: params.get('martist') || '', name: params.get('martist_name') || '' } : null;
  if (music && ['playlists','playlist'].includes(params.get('music'))) { music.playlists=true; music.playlist=params.get('music')==='playlist' ? params.get('id') || '' : ''; }
  if (music && ['artists','artist'].includes(params.get('music'))) { music.artists=true; music.artist=params.get('music')==='artist' ? params.get('id') || '' : ''; music.role=params.get('mrole') || 'artists'; }
  if (music && params.get('mfrom')==='artists') { music.artists=true; music.role=params.get('mrole') || 'artists'; }
  const sets = music ? [] : [...new Set((params.get('set') || '').split(',').map(positiveID).filter(Boolean))];
  const folder = params.get('folder') || '';
  const safeFolder = !folder.split('/').some(part => part === '.' || part === '..') && !folder.startsWith('/') && !folder.includes('\\') && !folder.includes('\0');
  const filters = Object.fromEntries(filterKeys.map(key => [key, key === 'favorites' ? !music && params.get(key) === '1' : music ? '' : params.get(key) || '']));
  return {
    music,
    sets, folder: safeFolder && sets.length === 1 ? folder : '', filters,
    virtual: !music && params.get('view') === 'in-progress' ? 'in-progress' : '',
    page: !music && /^\d+$/.test(params.get('page') || '') && Number.isSafeInteger(Number(params.get('page'))) ? Number(params.get('page')) : undefined,
    media: positiveID(params.get('media')),
    shuffle: params.get('shuffle') === '1',
  };
}

export function libraryURL(state, media = null, shuffle = false) {
  const params = new URLSearchParams();
  if (state.music) { params.set('music',state.music.playlists ? state.music.playlist ? 'playlist' : 'playlists' : state.music.album ? 'album' : state.music.artists ? state.music.artist ? 'artist' : 'artists' : 'albums'); if (state.music.playlist) params.set('id',state.music.playlist); else if (state.music.album) params.set('id',state.music.album); else if (state.music.artists && state.music.artist) params.set('id',state.music.artist); if(state.music.artists) { params.set('mrole',state.music.role || 'artists'); if(state.music.album) params.set('mfrom','artists'); } if (state.music.artist) params.set('martist',state.music.artist); if (state.music.name) params.set('martist_name',state.music.name); }
  if (!state.music) {
  if (state.virtualSet) params.set('view', state.virtualSet);
  if (state.selectedSetIds.length) params.set('set', state.selectedSetIds.join(','));
  if (state.folderPath && state.selectedSetIds.length === 1) params.set('folder', state.folderPath);
  for (const key of filterKeys) if (state.filters[key]) params.set(key, key === 'favorites' ? '1' : state.filters[key]);
  if (state.mediaPage > 0) params.set('page', String(state.mediaPage));
  }
  if (shuffle) params.set('shuffle', '1');
  if (positiveID(media)) params.set('media', String(media));
  return '/' + (params.size ? `?${params}` : '');
}

export function safeReturnTo(value) {
  if (!value || !value.startsWith('/') || value.startsWith('//') || /[\\\r\n]/.test(value)) return '/';
  try {
    const url = new URL(value, 'https://player.invalid');
    if (url.origin !== 'https://player.invalid' || !['/', '/index.html'].includes(url.pathname)) return '/';
    return url.pathname + url.search + url.hash;
  } catch { return '/'; }
}

export function loginDestination(page, returnTo) {
  const next = safeReturnTo(returnTo);
  return page + (next === '/' ? '' : '?return_to=' + encodeURIComponent(next));
}
