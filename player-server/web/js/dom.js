export { escapeHtml, fmtDur, toast } from './utils.js';

export function fmtDate(d) {
  if (!d) return '';
  const dt = typeof d === 'string' ? new Date(d) : d;
  return dt.toLocaleDateString();
}

export function fmtDateTime(d) {
  if (!d) return '';
  const dt = typeof d === 'string' ? new Date(d) : d;
  if (Number.isNaN(dt.getTime())) return '';
  return dt.toLocaleString();
}

export function fmtSize(bytes) {
  if (!bytes || bytes <= 0) return '';
  const kb = bytes / 1024;
  if (kb < 1024) return Math.round(kb) + ' KB';
  const mb = kb / 1024;
  if (mb < 1024) return Math.round(mb * 10) / 10 + ' MB';
  return Math.round((mb / 1024) * 10) / 10 + ' GB';
}

export function closeAllModals() {
  document.querySelectorAll('.modal-overlay.open').forEach((m) => m.classList.remove('open'));
}
