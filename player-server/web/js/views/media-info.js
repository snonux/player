import { mountRelatedMusic } from '../music-related.js';
import { mediaTitle, musicRows, musicDetailGroups } from '../music.js';
import { API } from '../api.js';
import { fmtDateTime, fmtSize } from '../dom.js';
import { escapeHtml, fmtDur, toast } from '../utils.js';
import { selectedMediaId } from './media-actions.js';

let callbacks = {};
let infoGeneration = 0;

export function initMediaInfo(options = {}) {
  callbacks = options;
  const modal = document.getElementById('media-info-modal');
  const closeBtn = document.getElementById('media-info-close');
  if (modal) {
    const home = modal.parentNode;
    const nextSibling = modal.nextSibling;
    // Fullscreen paints only the fullscreen element's descendants. Keep the
    // existing dialog inside that element while open, then restore its place.
    const syncMount = () => {
      const host = modal.classList.contains('open') && document.fullscreenElement;
      const parent = host || home;
      if (!parent || modal.parentNode === parent) return;
      const active = modal.contains(document.activeElement) ? document.activeElement : null;
      if (parent === home && nextSibling?.parentNode === home) home.insertBefore(modal, nextSibling);
      else parent.appendChild(modal);
      if (active && modal.classList.contains('open')) active.focus({ preventScroll: true });
    };
    document.addEventListener('fullscreenchange', syncMount);
    new MutationObserver(syncMount).observe(modal, { attributes: true, attributeFilter: ['class'] });
  }
  closeBtn?.addEventListener('click', closeMediaInfo);
  modal?.addEventListener('click', (e) => {
    if (e.target === modal) closeMediaInfo();
  });
  modal?.addEventListener('click', async (e) => {
    const button = e.target.closest('[data-media-info-action]');
    if (!button) return;
    const id = button.dataset.mediaId;
    if (!id) return;
    let updated = false;
    if (button.dataset.mediaInfoAction === 'mark-finished') {
      updated = await callbacks.markAsFinished?.(id);
    } else if (button.dataset.mediaInfoAction === 'mark-not-started') {
      updated = await callbacks.markAsNotStarted?.(id);
    }
    if (updated) await openMediaInfo(id);
  });
}

export function closeMediaInfo() {
  infoGeneration++;
  document.getElementById('media-info-modal')?.classList.remove('open');
}

export function isMediaInfoOpen() {
  return document.getElementById('media-info-modal')?.classList.contains('open');
}

export function scrollMediaInfo(delta) {
  const panel = document.getElementById('media-info-panel')
    || document.querySelector('#media-info-modal .modal');
  if (!panel) return;
  const step = Math.max(96, Math.round(panel.clientHeight * 0.25));
  panel.scrollTop += delta * step;
}

export async function toggleMediaInfo() {
  const modal = document.getElementById('media-info-modal');
  if (!modal) return;
  if (modal.classList.contains('open')) {
    closeMediaInfo();
    return;
  }
  const id = selectedMediaId();
  if (!id) {
    toast('No media selected', 'error');
    return;
  }
  await openMediaInfo(id);
}

async function openMediaInfo(id) {
  const modal = document.getElementById('media-info-modal');
  const body = document.getElementById('media-info-body');
  if (!modal || !body) return;
  const generation = ++infoGeneration;
  body.innerHTML = '<p class="text-muted text-sm">Loading...</p>';
  modal.classList.add('open');
  if (document.fullscreenElement && modal.parentNode !== document.fullscreenElement) {
    document.fullscreenElement.appendChild(modal);
  }
  (document.getElementById('media-info-panel')
    || document.querySelector('#media-info-modal .modal'))?.focus({ preventScroll: true });
  try {
    const detail = await API.mediaDetail(id);
    if (generation !== infoGeneration || !isMediaInfoOpen()) return;
    body.innerHTML = renderMediaInfo(detail);
    if (detail?.media?.type === 'audio') {
      const button = document.createElement('button'); button.className='btn btn-ghost'; button.textContent='Open album';
      button.onclick = () => { closeMediaInfo(); document.dispatchEvent(new CustomEvent('music:open-song-album',{detail:id})); };
      body.prepend(button);
      mountRelatedMusic(body,'song',String(id),()=>generation===infoGeneration && isMediaInfoOpen(),item=>openMediaInfo(item.key));
    }
  } catch (err) {
    if (generation !== infoGeneration || !isMediaInfoOpen()) return;
    body.innerHTML = `<p class="error-message">${escapeHtml(err.message || 'Failed to load media info')}</p>`;
  }
}

function renderMediaInfo(detail) {
  const media = detail?.media || {};
  const tags = Array.isArray(detail?.tags) ? detail.tags.map((t) => t.name).filter(Boolean).join(', ') : '';
  const ext = media.file_name?.includes('.') ? media.file_name.split('.').pop().toUpperCase() : '';
  const progress = detail?.progress;
  const note = detail?.note;
  const basicRows = [
    ['Title', mediaTitle(media)],
 ['Filename', media.file_name],
 ...musicRows(media),
  ];
  let rows = [
    ['Format', ext],
    ['Type', media.type],
    ['File size', media.file_size_bytes ? `${fmtSize(media.file_size_bytes)} (${media.file_size_bytes} bytes)` : ''],
    ['Relative path', media.rel_path],
    ['Absolute path', media.abs_path],
    ['Media ID', media.id],
    ['Set ID', media.set_id],
    ['Global play count', media.play_count],
    ...(media.type === 'audio' ? [['Your plays', media.personal_play_count || 0], ['Your last played', media.last_played_at ? new Date(media.last_played_at).toLocaleString() : 'Not played yet']] : []),
    ['Added', fmtDateTime(media.created_at)],
    ['Thumbnail', media.thumbnail_path],
    ['Favorite', detail?.favorite ? 'Yes' : 'No'],
    ['Tags', tags],
  ];
  if (media.type === 'image') {
    rows = rows.concat([
      ['Dimensions', media.resolution],
      ['Width', media.width],
      ['Height', media.height],
      ['Camera', media.exif_camera],
      ['Lens', media.exif_lens],
      ['Date Taken', media.exif_date],
      ['ISO', media.exif_iso],
      ['F-Number', media.exif_f_number],
      ['Exposure', media.exif_exposure],
      ['Focal Length', media.exif_focal_length],
    ]);
  } else {
    rows = rows.concat([
      ['Duration', media.duration ? `${fmtDur(media.duration)} (${Math.round(media.duration)} seconds)` : ''],
      ['Bitrate', media.bitrate ? `${Math.round(media.bitrate / 1000)} kbps (${media.bitrate} bps)` : ''],
      ['Codec', media.codec],
      ['Resolution', media.resolution],
      ['Saved position', progress ? `${fmtDur(progress.position_seconds)} (${Math.round(progress.position_seconds || 0)} seconds)` : ''],
      ['Progress updated', fmtDateTime(progress?.updated_at)],
    ]);
  }
  rows = rows.concat([
    ['Note updated', fmtDateTime(note?.updated_at)],
    ['Note length', note?.content ? `${note.content.length} characters` : ''],
  ]);
  const renderRows = entries => entries
    .filter(([, value]) => value !== undefined && value !== null && value !== '')
    .map(([label, value]) => `<tr><th scope="row">${escapeHtml(label)}</th><td>${escapeHtml(String(value))}</td></tr>`)
    .join('');
  const table = renderRows(rows);
  const basicTable = renderRows(basicRows);
  const raw = escapeHtml(JSON.stringify(detail || {}, null, 2));
  const groups = musicDetailGroups(media).map(([title, entries]) => `<details class="media-info-music"><summary tabindex="0">${escapeHtml(title)}</summary>${title === 'Advanced audio' ? '<p class="text-muted text-sm">Indexed original file. ReplayGain tags are informational; playback volume is unchanged.</p>' : ''}<table class="media-info-table">${entries.map(([label, value]) => `<tr><th scope="row">${escapeHtml(label)}</th><td>${escapeHtml(String(value))}</td></tr>`).join('')}</table></details>`).join('');
  return `
    <div class="flex flex-wrap gap-2 mt-1 mb-2">
      <button class="btn btn-ghost btn-sm" type="button" data-media-info-action="mark-finished" data-media-id="${escapeHtml(String(media.id || ''))}">✓ Finished</button>
      <button class="btn btn-ghost btn-sm" type="button" data-media-info-action="mark-not-started" data-media-id="${escapeHtml(String(media.id || ''))}">↺ Not started</button>
    </div>
    <table class="media-info-table">${basicTable}</table>
    ${groups}
    <table class="media-info-table">${table}</table>
    <details class="media-info-raw">
      <summary>Raw API detail</summary>
      <pre>${raw}</pre>
    </details>
  `;
}
