import { escapeHtml, fmtDur } from './utils.js';

export function publicationDate(value) {
 const date = value ? new Date(value) : null;
 return date && Number.isFinite(date.getTime()) ? `Published ${date.toLocaleDateString()}` : 'Publication date unavailable';
}

function webURL(value) {
 try {
  const url = new URL(value);
  return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : '';
 } catch { return ''; }
}

// Notes are text, never publisher HTML. Preserve paragraphs and link addresses
// before escaping every character that could become markup. No remote resources
// or executable attributes from the feed are inserted into the document.
export function showNotesText(raw) {
 const entities = {amp:'&',lt:'<',gt:'>',quot:'"',apos:"'",nbsp:' '};
 return String(raw || '')
  .replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1\s*>/gi, '')
  .replace(/<a\b[^>]*href\s*=\s*(["'])(.*?)\1[^>]*>([\s\S]*?)<\/a\s*>/gi, (_, q, url, label) => `${label} ${url}`)
  .replace(/<\/(p|div|h[1-6]|ul|ol|li|blockquote)\s*>|<br\s*\/?\s*>/gi, '\n')
  .replace(/<[^>]*>/g, '')
  .replace(/&(#x[0-9a-f]+|#\d+|amp|lt|gt|quot|apos|nbsp);/gi, (match, name) => {
   if (name[0] !== '#') return entities[name.toLowerCase()] || match;
   const n = name[1].toLowerCase() === 'x' ? parseInt(name.slice(2),16) : Number(name.slice(1));
   return n > 0 && n <= 0x10ffff && !(n >= 0xd800 && n <= 0xdfff) ? String.fromCodePoint(n) : '\uFFFD';
  }).replace(/[\t ]+/g,' ').replace(/ *\n */g,'\n').replace(/\n{3,}/g,'\n\n').trim();
}

export function showNotesHTML(raw) {
 const text = showNotesText(raw);
 if (!text) return '';
 const linked = text.split(/(https?:\/\/[^\s<>]+)/g).map(part => {
  const url = webURL(part);
  return url ? `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(part)}</a>` : escapeHtml(part);
 }).join('');
 return `<details class="episode-description"><summary>Show notes</summary><p class="subtitle">${linked}</p></details>`;
}

export function episodeArtworkHTML(episode, { thumbnailURL = '' } = {}) {
 const item = webURL(episode.image_url), feed = webURL(episode.feed_image_url);
 const copy = episode.server_copy?.media_id || (episode.is_downloaded ? episode.media_id : null);
 const candidate = thumbnailURL || (Number.isSafeInteger(Number(copy)) && Number(copy) > 0 ? `/api/media/${Number(copy)}/thumbnail` : '');
 // Authenticated thumbnail fallback is always a same-origin API path.
 const thumbnail = /^\/api\/(?:v1\/)?media\/[1-9]\d*\/thumbnail$/.test(candidate) ? candidate : '';
 const url = item || feed || thumbnail;
 return `<span class="placeholder" aria-hidden="true">🎙️</span>${url ? `<img class="podcast-episode-cover" data-podcast-artwork src="${escapeHtml(url)}" data-feed-artwork="${escapeHtml(feed && feed !== url ? feed : '')}" data-thumbnail-artwork="${escapeHtml(thumbnail && thumbnail !== url ? thumbnail : '')}" alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer">` : ''}`;
}

export function bindEpisodeArtwork(root) {
 root.querySelectorAll('[data-podcast-artwork]').forEach(image => {
  image.addEventListener('error', () => {
   const fallback = image.dataset.feedArtwork;
   if (fallback) { image.dataset.feedArtwork = ''; image.src = fallback; }
   else if (image.dataset.thumbnailArtwork) { image.src = image.dataset.thumbnailArtwork; image.dataset.thumbnailArtwork = ''; }
   else image.hidden = true;
  });
 });
}

// One confirmation for every way to delete an episode: the row action, the
// list's right-to-left swipe, the Delete key and the player's Delete episode.
export function confirmEpisodeDelete(title) {
 return confirm(`Delete "${title || 'this episode'}" from your podcast lists on all your devices? It stays deleted after feed refresh and sync. Listening history and downloaded files stay saved. Removing a downloaded file is a separate action.`);
}

// Device-only podcasts (no account) live in this browser's lists only.
export function confirmLocalEpisodeDelete(title) {
 return confirm(`Delete "${title || 'this episode'}" from this browser’s podcast lists?`);
}

export function bindPodcastMetadata(root) {
 bindEpisodeArtwork(root);
 root.querySelectorAll('.episode-description').forEach(notes => {
  notes.addEventListener('click', event => event.stopPropagation());
  notes.addEventListener('keydown', event => {
   if (event.key === 'Enter' || event.key === ' ') event.stopPropagation();
  });
 });
}

// episodeCardMarkup renders the compact episode card shared by subscription
// and set-folder views: title, status badge, Play/Download (+ Cancel while
// transferring), More for completion/restart/delete; description expands.
// More stays available during transfer so completion remains reachable;
// Cancel is the primary control. Progress shows without a server copy
// (Android 1t3 parity). feedTitle adds a subtitle line; downloadLabel names
// the download button; deletable adds the "Delete episode" action and
// showListened the "✓ Listened" line.
export function episodeCardMarkup(ep, { completed, downloaded, downloading = false, feedTitle = '', downloadLabel, deletable = false, showListened = false }) {
  const dateStr = publicationDate(ep.published_at);
  const duration = ep.duration_seconds ? fmtDur(ep.duration_seconds) : '';
  const hasProgress = Number(ep.position_seconds) > 0;
  const canResume = downloaded && hasProgress;
  const position = hasProgress
    ? (canResume ? `Resume ${fmtDur(ep.position_seconds)}` : `In progress · ${fmtDur(ep.position_seconds)}`)
    : '';
  const downloadState = downloading
    ? 'Downloading…'
    : downloaded ? 'Downloaded on server' : 'Not downloaded';
  const completeLabel = completed ? 'Mark uncompleted' : 'Mark completed';
  const feedLine = feedTitle ? `<div class="subtitle">${escapeHtml(feedTitle)}</div>` : '';
  const metaBits = [dateStr, duration, position, downloadState].filter(Boolean).join(' · ');
  const playLabel = hasProgress ? 'Resume' : 'Play';
  const primary = `${downloading
    ? '<button type="button" class="icon-btn btn-sm" data-action="cancel-download" title="Cancel download" aria-label="Cancel download">✕</button>'
    : downloaded ? '' : `<button type="button" class="icon-btn btn-sm btn-download-episode" data-action="download-episode" title="${downloadLabel.title}" aria-label="${downloadLabel.aria}">↓</button>`}
    <button type="button" class="icon-btn btn-sm" data-action="play" title="${playLabel}" aria-label="${playLabel} episode">▶</button>`;
  const extras = `
        <button type="button" class="icon-btn btn-sm card-action-extra btn-complete${completed ? ' active' : ''}" data-action="complete"
          title="${completeLabel}" aria-label="${completeLabel}" aria-pressed="${completed}">✓</button>
        ${downloaded ? '<button type="button" class="icon-btn btn-sm card-action-extra" data-action="play-beginning" title="Play from beginning" aria-label="Play from beginning">↺</button>' : ''}
        ${downloaded ? '<button type="button" class="icon-btn btn-sm card-action-extra" data-action="delete-download" title="Delete downloaded file" aria-label="Delete downloaded file">🗑</button>' : ''}${deletable ? `
        <button type="button" class="icon-btn btn-sm card-action-extra" data-action="delete-episode" title="Delete episode" aria-label="Delete episode">🗑</button>` : ''}`;
  return `
    <div class="thumb-wrap">
      ${episodeArtworkHTML(ep)}
      <span class="badge">${escapeHtml(metaBits)}</span>
      <div class="card-actions">
        ${primary}
        <button type="button" class="icon-btn btn-sm card-more" data-action="more" title="More episode actions" aria-label="More episode actions" aria-haspopup="menu">☰</button>
        ${extras}
      </div>
    </div>
    <div class="meta">
      <div class="title" title="${escapeHtml(ep.title || 'Untitled')}">${escapeHtml(ep.title || 'Untitled')}</div>
      ${feedLine}
      ${showListened && completed ? '<div class="episode-listened">✓ Listened</div>' : ''}
      ${showNotesHTML(ep.description)}
    </div>`;
}
