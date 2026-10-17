// Library links carry IDs and view state, never credentials or stream URLs.
const filterKeys = ['type', 'search', 'favorites', 'tags', 'sort', 'minDuration', 'maxDuration', 'minFileSize', 'maxFileSize'];
const positiveID = value => /^[1-9]\d*$/.test(String(value)) && Number.isSafeInteger(Number(value)) ? Number(value) : null;

export function readLibraryURL(search) {
  const params = new URLSearchParams(search);
  const sets = [...new Set((params.get('set') || '').split(',').map(positiveID).filter(Boolean))];
  const folder = params.get('folder') || '';
  const safeFolder = !folder.split('/').some(part => part === '.' || part === '..') && !folder.startsWith('/') && !folder.includes('\\') && !folder.includes('\0');
  const filters = Object.fromEntries(filterKeys.map(key => [key, key === 'favorites' ? params.get(key) === '1' : params.get(key) || '']));
  return {
    sets, folder: safeFolder && sets.length === 1 ? folder : '', filters,
    virtual: params.get('view') === 'in-progress' ? 'in-progress' : '',
    page: /^\d+$/.test(params.get('page') || '') && Number.isSafeInteger(Number(params.get('page'))) ? Number(params.get('page')) : undefined,
    media: positiveID(params.get('media')),
    shuffle: params.get('shuffle') === '1',
  };
}

export function libraryURL(state, media = null, shuffle = false) {
  const params = new URLSearchParams();
  if (state.virtualSet) params.set('view', state.virtualSet);
  if (state.selectedSetIds.length) params.set('set', state.selectedSetIds.join(','));
  if (state.folderPath && state.selectedSetIds.length === 1) params.set('folder', state.folderPath);
  for (const key of filterKeys) if (state.filters[key]) params.set(key, key === 'favorites' ? '1' : state.filters[key]);
  if (shuffle) params.set('shuffle', '1');
  if (state.mediaPage > 0) params.set('page', String(state.mediaPage));
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
