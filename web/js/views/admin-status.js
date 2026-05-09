import { API } from '../api.js';
import { toast } from '../utils.js';

let scanProgressTimer = null;

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
  try {
    renderScanProgress(await API.scanProgress());
  } catch {}
}

export function renderScanProgress(progress) {
  const indicator = document.getElementById('scan-indicator');
  const text = document.getElementById('scan-indicator-text');
  if (!indicator || !text || !progress) return;

  if (!progress.running) {
    indicator.classList.add('hidden');
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
