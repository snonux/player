import { t, html } from './i18n.js';
import { initPlayer } from './player.js';
import { initQueue, insertQueue, advanceQueue } from './queue.js';
import { initQueuePanel } from './queue-panel.js';
import { addLocalFiles, listLocalFiles, removeLocalFile, localFileToMedia } from './local-files-store.js';

// Browser "On this device" library: pick files, persist blobs best-effort, play via object URLs.
export function initLocalFilesApp() {
  const header = document.querySelector('.header-main');
  const host = document.querySelector('.main-content');
  const shortcuts = document.getElementById('keyboard-access-toggle');
  header.innerHTML = html`<a href="/index.html" class="logo">Player</a><div class="spacer"></div><button class="btn" data-queue-toggle>Queue</button><button id="theme-toggle" class="icon-btn" aria-label="Toggle theme"></button><a class="btn" href="/login.html">Sign In</a>`;
  if (shortcuts) { shortcuts.className = 'icon-btn'; shortcuts.textContent = '⌨'; header.append(shortcuts); }
  document.querySelector('#sidebar')?.classList.remove('open');
  document.querySelector('.page')?.classList.remove('has-sidebar');
  host.innerHTML = html`<section class="local-files" aria-label="On this device">
    <h1>On this device</h1>
    <p>Play audio or video files from this computer. Copies are stored in this browser only; clearing site data removes them. Supported formats depend on Chrome or Firefox.</p>
    <div class="playlist-actions">
      <button class="btn btn-primary" type="button" data-add-files>Add files</button>
      <input hidden type="file" data-file-input multiple accept="audio/*,video/*,.mp3,.m4a,.flac,.ogg,.opus,.wav,.aac,.mp4,.webm,.mkv,.mov">
      <button class="btn" type="button" data-refresh>Refresh</button>
    </div>
    <p role="status" aria-live="polite" data-status></p>
    <ul data-results class="local-files-list"></ul>
  </section>`;
  initPlayer({ personalActions: false, cast: false, onNext: options => advanceQueue(options?.delta ?? 1, options), onPrevious: options => advanceQueue(options?.delta ?? -1, options) });
  initQueue(); initQueuePanel();
  for (const selector of ['#btn-detach', '#btn-cast', '#btn-download', '#btn-player-notes', '#btn-player-favorite', '#btn-bookmarks', '#btn-save-position']) {
    const control = document.querySelector(selector);
    if (control) { control.disabled = true; control.hidden = true; }
  }
  const status = host.querySelector('[data-status]');
  const results = host.querySelector('[data-results]');
  const input = host.querySelector('[data-file-input]');
  let busy = false;
  const objectUrls = new Set();

  function message(text) { status.textContent = t(text); }
  function revokeAll() { for (const url of objectUrls) URL.revokeObjectURL(url); objectUrls.clear(); }

  async function playRow(row) {
    if (busy) return;
    busy = true; message('Starting playback…');
    try {
      const media = localFileToMedia(row);
      objectUrls.add(media.offlinePinURL);
      const added = await insertQueue([media], 'now', { replace: true, source: t('On this device') });
      message(added ? 'Playing.' : 'Playback is busy. Retry.');
    } catch (error) {
      message(error.message || 'Could not play this file.');
    } finally {
      busy = false;
    }
  }

  async function render() {
    revokeAll();
    results.replaceChildren();
    message('Loading local files…');
    try {
      const rows = await listLocalFiles();
      if (!rows.length) {
        message('Your device library is empty. Add audio or video files stored on this computer.');
        return;
      }
      message(`${rows.length} local file${rows.length === 1 ? '' : 's'}`);
      for (const row of rows) {
        const li = document.createElement('li');
        li.className = 'local-files-row';
        const play = document.createElement('button');
        play.className = 'btn btn-ghost';
        play.type = 'button';
        play.textContent = row.name;
        play.setAttribute('aria-label', `Play ${row.name}`);
        play.onclick = () => playRow(row);
        const more = document.createElement('button');
        more.className = 'icon-btn';
        more.type = 'button';
        more.textContent = '☰';
        more.setAttribute('aria-label', `More for ${row.name}`);
        more.onclick = () => openMore(more, row);
        li.append(play, more);
        results.append(li);
      }
    } catch (error) {
      message(error.message || 'Could not open the local library.');
    }
  }

  function openMore(button, row) {
    const popover = document.createElement('div');
    popover.className = 'shortcut-picker';
    popover.setAttribute('popover', 'auto');
    popover.setAttribute('role', 'menu');
    popover.innerHTML = html`<button class="btn btn-ghost" data-action="remove">Remove</button>`;
    popover.querySelector('[data-action="remove"]').onclick = async () => {
      popover.hidePopover();
      await removeLocalFile(row.id);
      await render();
      button.focus();
    };
    popover.addEventListener('toggle', event => {
      if (event.newState === 'closed') { popover.remove(); button.focus(); }
    });
    document.body.append(popover);
    popover.showPopover();
    popover.querySelector('button')?.focus();
  }

  host.querySelector('[data-add-files]').onclick = () => input.click();
  host.querySelector('[data-refresh]').onclick = () => { void render(); };
  input.onchange = async () => {
    if (!input.files?.length) return;
    busy = true; message('Adding files…');
    try {
      const saved = await addLocalFiles(input.files);
      message(saved.length ? `Added ${saved.length} file${saved.length === 1 ? '' : 's'}.` : 'No supported audio or video files in that selection.');
      await render();
    } catch (error) {
      message(error.message || 'Could not save local files.');
    } finally {
      busy = false;
      input.value = '';
    }
  };
  void render();
}
