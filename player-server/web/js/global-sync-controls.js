import { GlobalSyncCoordinator } from './global-sync.js';
import { mountGlobalSyncView } from './global-sync-view.js';
import { syncAccount } from './sync-account.js';
import { flushListeningProgress, listeningProgressStatus } from './listening-progress.js';
import { savedSessionSyncStatus, syncSavedSession } from './queue.js';
import { queueStorageKey } from './queue-storage.js';
import {syncAutomaticMusic, mountAutomaticMusicSettings} from './automatic-offline.js';

function pending() {
  const account = syncAccount(), progress = listeningProgressStatus(), queue = savedSessionSyncStatus();
  return { ids: [
    ...(progress.account === account ? progress.pending.map(id => `progress:${id}`) : []),
    ...(queue.key === `${queueStorageKey(location.origin, account)}.server` ? queue.pending.map(id => `queue:${id}`) : [])] };
}
let coordinator;
export function initGlobalSyncControls() {
  if (coordinator) return coordinator;
  const host = document.getElementById('global-sync');
  if (!host) return null;
  const globalButton = document.getElementById('global-sync-now');
  const render = mountGlobalSyncView(host, globalButton, () => void coordinator.syncNow(), () => coordinator.state,
    document.getElementById('menu-sync-now'));
  coordinator = new GlobalSyncCoordinator({ storage: localStorage, domains: {
    pending, progress: () => {
      if (listeningProgressStatus().account !== syncAccount()) throw new Error('Progress account is not ready');
      return flushListeningProgress();
    },
    music: explicit => syncAutomaticMusic({force:explicit}),
    queue: () => {
      const user = syncAccount();
      if (savedSessionSyncStatus().key !== `${queueStorageKey(location.origin, user)}.server`) throw new Error('Session account is not ready');
      return syncSavedSession();
    },
  }, changed: render });
  mountAutomaticMusicSettings(document.getElementById('settings-modal')?.querySelector('.modal'),()=>void coordinator.syncNow());
  const bind = () => {
    const user = syncAccount();
    coordinator.bind(user ? `${location.origin}#${user}` : null);
  };
  bind();
  document.addEventListener('sync:account-changed', bind);
  document.addEventListener('listening-progress:changed', () => coordinator.refresh(
    listeningProgressStatus().account === syncAccount() ? listeningProgressStatus().lastError : null));
  document.addEventListener('queue:changed', () => coordinator.refresh(
    savedSessionSyncStatus().key === `${queueStorageKey(location.origin, syncAccount())}.server` ? savedSessionSyncStatus().lastError : null));
  let lastAttempt = Date.now();
  const trigger = () => {
    if (document.visibilityState === 'hidden' || navigator.onLine === false || coordinator.state.errorKind === 'auth') return;
    lastAttempt = Date.now();
    void coordinator.syncNow({explicit:false});
  };
  window.addEventListener('online', trigger);
  document.addEventListener('visibilitychange', () => { if (Date.now() - lastAttempt >= 60000) trigger(); });
  setInterval(() => { coordinator.refresh(); if (Date.now() - lastAttempt >= 60000) trigger(); }, 60000);
  return coordinator;
}
