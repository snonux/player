import { escapeHtml } from './utils.js';

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

export function bindPodcastMetadata(root) {
 bindEpisodeArtwork(root);
 root.querySelectorAll('.episode-description').forEach(notes => {
  notes.addEventListener('click', event => event.stopPropagation());
  notes.addEventListener('keydown', event => {
   if (event.key === 'Enter' || event.key === ' ') event.stopPropagation();
  });
 });
}
