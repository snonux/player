import { GlobalSyncCoordinator } from './global-sync.js';
import { mountGlobalSyncView } from './global-sync-view.js';
import { podcastSyncStatus } from './podcast-episode-state.js';
import { pendingPodcastSubscriptions, podcastSubscriptionStatus } from './podcast-subscriptions.js';
import { syncPodcastsForGlobal } from './podcast-global-sync.js';
import { flushListeningProgress, listeningProgressStatus } from './listening-progress.js';
import { savedSessionSyncStatus, syncSavedSession } from './queue.js';
import { queueStorageKey } from './queue-storage.js';

function pending() {
  const episodes = podcastSyncStatus(), subscriptions = pendingPodcastSubscriptions();
  const progress = listeningProgressStatus(), queue = savedSessionSyncStatus();
  return { ids: [...episodes.pendingIds, ...subscriptions.map(edit => edit.edit_id),
    ...(progress.account === episodes.account ? progress.pending.map(id => `progress:${id}`) : []),
    ...(queue.key === `${queueStorageKey(location.origin, episodes.account)}.server` ? queue.pending.map(id => `queue:${id}`) : [])],
    podcasts: episodes.pending + subscriptions.length };
}
let coordinator;
export function initGlobalSyncControls() {
  if (coordinator) return coordinator;
  const host = document.getElementById('global-sync');
  if (!host) return null;
  const globalButton = document.getElementById('global-sync-now');
  const render = mountGlobalSyncView(host, globalButton, () => void coordinator.syncNow(), () => coordinator.state);
  coordinator = new GlobalSyncCoordinator({ storage: localStorage, domains: {
    pending, podcasts: syncPodcastsForGlobal, progress: () => {
      if (listeningProgressStatus().account !== podcastSyncStatus().account) throw new Error('Progress account is not ready');
      return flushListeningProgress();
    },
    queue: () => {
      const user = podcastSyncStatus().account;
      if (savedSessionSyncStatus().key !== `${queueStorageKey(location.origin, user)}.server`) throw new Error('Session account is not ready');
      return syncSavedSession();
    },
  }, changed: render });
  const bind = () => {
    const user = podcastSyncStatus().account;
    coordinator.bind(user ? `${location.origin}#${user}` : null);
  };
  bind();
  document.addEventListener('podcast:account-changed', bind);
  document.addEventListener('podcast:state-changed', () => coordinator.refresh(podcastSyncStatus().lastFailure || podcastSubscriptionStatus().lastError));
  document.addEventListener('listening-progress:changed', () => coordinator.refresh(
    listeningProgressStatus().account === podcastSyncStatus().account ? listeningProgressStatus().lastError : null));
  document.addEventListener('queue:changed', () => coordinator.refresh(
    savedSessionSyncStatus().key === `${queueStorageKey(location.origin, podcastSyncStatus().account)}.server` ? savedSessionSyncStatus().lastError : null));
  let lastAttempt = Date.now();
  const trigger = () => {
    if (document.visibilityState === 'hidden' || navigator.onLine === false || coordinator.state.errorKind === 'auth') return;
    lastAttempt = Date.now();
    void coordinator.syncNow();
  };
  window.addEventListener('online', trigger);
  document.addEventListener('visibilitychange', () => { if (Date.now() - lastAttempt >= 60000) trigger(); });
  setInterval(() => { coordinator.refresh(); if (Date.now() - lastAttempt >= 60000) trigger(); }, 60000);
  return coordinator;
}
