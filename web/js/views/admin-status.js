import { API } from '../api.js';

let scanProgressTimer = null;

export function showAdmin() {
  const btn = document.getElementById('admin-toggle');
  if (btn) btn.classList.remove('hidden');
  startScanProgressPolling();
}

function startScanProgressPolling() {
  if (scanProgressTimer) return;
  pollScanProgress();
  scanProgressTimer = setInterval(pollScanProgress, 2000);
}

async function pollScanProgress() {
  try {
    renderScanProgress(await API.scanProgress());
  } catch {}
}

function renderScanProgress(progress) {
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
  const filesPart = progress.files_done ? `, ${progress.files_done} files` : '';
  text.textContent = `Scanning${setPart}${setsPart}${filesPart}`;
  indicator.classList.remove('hidden');
}
