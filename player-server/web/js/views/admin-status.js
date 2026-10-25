import { API } from '../api.js';
import { toast } from '../utils.js';

let scanProgressTimer = null;
let progressRequestRunning = false;

export function showAdmin() {
  const btn = document.getElementById('admin-toggle');
  if (btn) btn.classList.remove('hidden');
  startScanProgressPolling();
}

export async function triggerRescan() {
  try {
    await API.rescan();
    await refreshScanProgress();
    toast('Rescan triggered');
  } catch (err) {
    toast(err.message || 'Rescan failed', 'error');
  }
}

export function startScanProgressPolling() {
  if (scanProgressTimer) return;
  refreshScanProgress();
  scanProgressTimer = setInterval(refreshScanProgress, 2000);
}

export async function refreshScanProgress() {
  if (progressRequestRunning) return;
  progressRequestRunning = true;
  try {
    renderScanProgress(await API.scanProgress());
  } catch {
    const status = document.getElementById('admin-scan-status');
    if (status) status.textContent = 'Scan status unavailable. Check the connection and admin access; status retries automatically.';
  } finally { progressRequestRunning = false; }
}

let lastIdentityNotice = '';

export function renderScanProgress(progress) {
  renderAdminScanStatus(progress);
  const indicator = document.getElementById('scan-indicator');
  const text = document.getElementById('scan-indicator-text');
  if (!indicator || !text || !progress) return;

  if (!progress.running) {
    indicator.classList.add('hidden');
    const moved = progress.identity_moved || 0;
    const conflicts = progress.identity_conflicts || 0;
    if (moved || conflicts) {
      const notice = `${moved} song${moved === 1 ? '' : 's'} kept across moves` +
        (conflicts ? ` · ${conflicts} identity conflict${conflicts === 1 ? '' : 's'} (Admin › Music identity)` : '');
      if (notice !== lastIdentityNotice) {
        lastIdentityNotice = notice;
        toast(notice);
      }
    }
    return;
  }

  const setPart = progress.current_set ? ` ${progress.current_set}` : '';
  const setsTotal = progress.sets_total || 0;
  const setsPart = setsTotal ? ` ${progress.sets_done || 0}/${setsTotal} sets` : '';
  const filesTotal = progress.files_total || 0;
  const filesDone = progress.files_done || 0;
  const filesPart = filesTotal ? `, ${filesDone}/${filesTotal} files` : (filesDone ? `, ${filesDone} files` : '');
  text.textContent = `Scanning${setPart}${setsPart}${filesPart}`;
  indicator.classList.remove('hidden');
}

function renderAdminScanStatus(p) {
  if (!p) return;
  const status = document.getElementById('admin-scan-status');
  const cancel = document.getElementById('admin-scan-cancel');
  const settings = document.getElementById('admin-scan-settings');
  const busy = p.running || p.state === 'cancelling';
  if (cancel) {
    const wasFocused = document.activeElement === cancel;
    cancel.disabled = !busy || p.state === 'cancelling';
    if (cancel.disabled && wasFocused) document.getElementById('admin-rescan')?.focus();
  }
  if (settings) settings.textContent = `Watch: ${p.watch_seconds ? `polling every ${p.watch_seconds} seconds` : 'Off'}. Schedule: ${p.interval_minutes ? `every ${p.interval_minutes} minutes` : 'Off'}.`;
  if (!status) return;
  let message = p.state === 'cancelling' ? 'Cancelling scan…' : busy
    ? `Scanning${p.current_set ? ` ${p.current_set}` : ''}: ${p.files_done || 0}/${p.files_total || 0} files · ${p.sets_done || 0}/${p.sets_total || 0} sets · ${p.trigger || 'manual'}`
    : p.state === 'cancelled' ? 'Scan cancelled. Rescan when ready.'
      : p.last_error || p.state === 'failed' ? 'Scan failed. Check library root access and server logs, then trigger rescan.'
        : p.state === 'complete' ? `Scan complete · ${p.files_done || 0} files checked.` : 'Idle — no scan running.';
  if (p.pending) message += ' One follow-up scan queued.';
  if (p.watch_error) message += ` ${p.watch_error}`;
  status.textContent = message;
}
