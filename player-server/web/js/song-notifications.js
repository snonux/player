import { API } from './api.js';
import { mediaTitle, musicSubtitle } from './music.js';

// Optional announcements belong to the window that actually owns playback.
// Recheck authentication before exposing private track text outside the page.
export function createSongNotifications({ Notification: Notice, storage, hidden, authorize, render = () => {} }) {
  let account = null, enabled = false, busy = false, epoch = 0, preferenceEpoch = 0, disabledLocally = false, last = null, notice = null;
  const key = () => `player-song-notifications:${account}`;
  const supported = () => typeof Notice === 'function';
  const close = () => { try { notice?.close(); } catch {} notice = null; };
  function refresh(message = '') {
    if (account !== null) {
      try { enabled = !disabledLocally && storage.getItem(key()) === 'on'; } catch {}
    }
    if (!enabled || Notice?.permission !== 'granted') close();
    render({ enabled, busy, disabled: !account || !supported() || (!enabled && Notice.permission === 'denied'),
      message: message || (!supported() ? 'Song notifications need a supported browser on HTTPS or localhost.' :
        Notice.permission === 'denied' ? 'Notifications are blocked. Allow them in browser site settings to enable song notifications.' :
        enabled && Notice.permission === 'granted' ? 'On for this account on this browser. Track names may appear on your desktop or lock screen.' :
        'Off. Enable to announce songs only while the player window is in the background.') });
  }
  function setAccount(id) {
    epoch++; preferenceEpoch++; close(); account = Number.isInteger(id) && id > 0 ? id : null;
    enabled = false; disabledLocally = false; last = null; refresh();
  }
  async function toggle() {
    if (!account || !supported() || busy) return;
    if (enabled) {
      epoch++; preferenceEpoch++; close(); enabled = false; disabledLocally = true;
      try { storage.removeItem(key()); refresh(); } catch { refresh('Off for this window. Could not save; disable again after reopening Player when browser storage is available.'); }
      return;
    }
    busy = true; const token = preferenceEpoch;
    // requestPermission must run directly in the user's click handler.
    let permission;
    try { permission = await Notice.requestPermission(); }
    catch { permission = 'default'; }
    if (token !== preferenceEpoch) { busy = false; refresh(); return; }
    busy = false;
    if (permission !== 'granted') { refresh(permission === 'denied' ? '' : 'Permission was not granted. Enable again when ready.'); return; }
    try { storage.setItem(key(), 'on'); disabledLocally = false; enabled = true; refresh(); }
    catch { enabled = false; refresh('Could not enable: browser storage is unavailable.'); }
  }
  async function playing(media, stillPlaying = () => true) {
    if (!account || media?.type !== 'audio' || !Number.isInteger(media.id)) return;
    const id = String(media.id);
    if (last === id) return;
    last = id;
    const token = ++epoch;
    refresh();
    if (!enabled || !supported() || Notice.permission !== 'granted') return;
    // Share the consumed track across detach/reattach without storing its text.
    try {
      const previous = storage.getItem(`${key()}:last`);
      storage.setItem(`${key()}:last`, id);
      if (previous === id) return;
    } catch { return; }
    if (!hidden()) return;
    const owner = account;
    try {
      if (await authorize() !== owner || token !== epoch || !hidden() || !stillPlaying()) return;
      refresh();
      if (!enabled || Notice.permission !== 'granted') return;
      close();
      notice = new Notice(mediaTitle(media).slice(0, 160), {
        body: musicSubtitle(media).slice(0, 240), tag: 'player-song', silent: true,
      });
    } catch { /* Notification failure must never affect playback. */ }
  }
  return { setAccount, toggle, playing, refresh, suspend: () => { epoch++; close(); }, clear: () => setAccount(null), close };
}

// BFCache retains this authenticated document. Keep its account preference,
// but cancel pending announcements; every later announcement rechecks auth.
export function bindSongNotificationLifecycle(controller, windowTarget, documentTarget) {
  windowTarget.addEventListener('pagehide', event => {
    if (event.persisted) controller.suspend();
    else controller.clear();
  });
  windowTarget.addEventListener('pageshow', event => { if (event.persisted) controller.refresh(); });
  documentTarget.addEventListener('visibilitychange', () => {
    if (documentTarget.visibilityState === 'visible') controller.close();
    controller.refresh();
  });
}

let controller;
export function initSongNotifications(account) {
  if (!controller) {
    const button = document.getElementById('song-notifications-toggle');
    const status = document.getElementById('song-notifications-status');
    let storage;
    try { storage = globalThis.localStorage; } catch { storage = { getItem() { throw Error(); } }; }
    controller = createSongNotifications({
      Notification: globalThis.isSecureContext ? globalThis.Notification : undefined, storage,
      hidden: () => document.visibilityState === 'hidden', authorize: async () => (await API.config())?.user_id,
      render: value => {
        if (button) { button.textContent = value.busy ? 'Requesting permission…' : value.enabled ? 'Disable song notifications' : 'Enable song notifications'; button.disabled = value.disabled || value.busy; }
        if (status) status.textContent = value.message;
      },
    });
    button?.addEventListener('click', () => void controller.toggle());
    window.addEventListener('storage', event => {
      if (event.key === 'player-song-notifications-logout') controller.clear();
      else controller.refresh();
    });
    bindSongNotificationLifecycle(controller, window, document);
  }
  controller.setAccount(account);
}
export function announceSong(media, stillPlaying) { void controller?.playing(media, stillPlaying); }
export function suspendSongAnnouncement() { controller?.suspend(); }
export function clearSongNotifications() {
  controller?.clear();
  try { localStorage.setItem('player-song-notifications-logout', String(Date.now())); } catch {}
}
