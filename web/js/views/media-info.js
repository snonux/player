import { API } from '../api.js';
import { escapeHtml, fmtDateTime, fmtDur, fmtSize, toast } from '../dom.js';
import { selectedMediaId } from './media-actions.js';

export function initMediaInfo() {
  const modal = document.getElementById('media-info-modal');
  const closeBtn = document.getElementById('media-info-close');
  closeBtn?.addEventListener('click', closeMediaInfo);
  modal?.addEventListener('click', (e) => {
    if (e.target === modal) closeMediaInfo();
  });
}

export function closeMediaInfo() {
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
  body.innerHTML = '<p class="text-muted text-sm">Loading...</p>';
  modal.classList.add('open');
  (document.getElementById('media-info-panel')
    || document.querySelector('#media-info-modal .modal'))?.focus({ preventScroll: true });
  try {
    const detail = await API.mediaDetail(id);
    body.innerHTML = renderMediaInfo(detail);
  } catch (err) {
    body.innerHTML = `<p class="error-message">${escapeHtml(err.message || 'Failed to load media info')}</p>`;
  }
}

function renderMediaInfo(detail) {
  const media = detail?.media || {};
  const tags = Array.isArray(detail?.tags) ? detail.tags.map((t) => t.name).filter(Boolean).join(', ') : '';
  const ext = media.file_name?.includes('.') ? media.file_name.split('.').pop().toUpperCase() : '';
  const progress = detail?.progress;
  const note = detail?.note;
  let rows = [
    ['Title', media.file_name],
    ['Format', ext],
    ['Type', media.type],
    ['File size', media.file_size_bytes ? `${fmtSize(media.file_size_bytes)} (${media.file_size_bytes} bytes)` : ''],
    ['Relative path', media.rel_path],
    ['Absolute path', media.abs_path],
    ['Media ID', media.id],
    ['Set ID', media.set_id],
    ['Play count', media.play_count],
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
  const table = rows
    .filter(([, value]) => value !== undefined && value !== null && value !== '')
    .map(([label, value]) => `<tr><th scope="row">${escapeHtml(label)}</th><td>${escapeHtml(String(value))}</td></tr>`)
    .join('');
  const raw = escapeHtml(JSON.stringify(detail || {}, null, 2));
  return `
    <table class="media-info-table">${table}</table>
    <details class="media-info-raw">
      <summary>Raw API detail</summary>
      <pre>${raw}</pre>
    </details>
  `;
}
