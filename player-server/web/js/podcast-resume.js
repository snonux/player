// podcast-resume.js — "Continue listening" shortcut on app start.
// When nothing is playing, offer the podcast episode the listener was on last
// so one click resumes it where it left off (Android shows the same shortcut
// in its empty mini-player slot). It stays above the library while browsing
// and goes away as soon as anything plays or the listener hides it.
import { API } from './api.js';
import { episodeSnapshot } from './podcast-episode-state.js';
import { openPodcastsInProgress, resumePodcastEpisode } from './podcast-views.js';
import { escapeHtml, fmtDur } from './utils.js';

// The server lists started episodes most recently listened first; a newer
// local completion or deletion in this browser still wins.
export function pickResumableEpisode(episodes) {
  for (const ep of episodes || []) {
    const snapshot = episodeSnapshot(ep);
    if (snapshot.deletion?.deleted) continue;
    if (snapshot.completion?.completed ?? ep.completed) continue;
    const position = snapshot.position?.seconds ?? ep.position_seconds ?? 0;
    if (position > 0) return { ...ep, position_seconds: position };
  }
  return null;
}

export function resumeSubtitle(ep) {
  const podcast = ep.feed_title || ep.podcast_title || '';
  return [podcast, `Resume at ${fmtDur(ep.position_seconds)}`].filter(Boolean).join(' · ');
}

// Mounts the shortcut at the top of host. Returns an unmount function.
export function mountPodcastResume(host, { isPlaying = () => false } = {}) {
  if (!host) return () => {};
  const el = document.createElement('section');
  el.id = 'podcast-resume';
  el.className = 'podcast-resume';
  el.hidden = true;
  el.setAttribute('aria-label', 'Continue listening');
  host.prepend(el);
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    document.removeEventListener('play', close, true);
    el.remove();
  };
  // Media play events do not bubble; capture sees every player element.
  document.addEventListener('play', close, true);
  (async () => {
    let ep;
    try {
      ep = pickResumableEpisode(await API.subscribedEpisodes({ filter: 'started', limit: 5 }));
    } catch {
      ep = null; // Optional chrome: offline or no podcasts simply hides it.
    }
    if (closed || !ep || isPlaying()) { close(); return; }
    const title = ep.title || 'Podcast episode';
    const subtitle = resumeSubtitle(ep);
    el.innerHTML = `<span class="podcast-resume-icon" aria-hidden="true">🎙︎</span>
      <div class="podcast-resume-text">
        <div class="podcast-resume-label text-muted text-xs">Continue listening</div>
        <div class="podcast-resume-title">${escapeHtml(title)}</div>
        <div class="podcast-resume-subtitle text-muted text-sm">${escapeHtml(subtitle)}</div>
      </div>
      <button type="button" class="btn podcast-resume-play" aria-label="Resume podcast: ${escapeHtml(title)}">▶ Resume</button>
      <button type="button" class="btn podcast-resume-all">All in progress</button>
      <button type="button" class="icon-btn podcast-resume-close" aria-label="Hide continue listening">✕</button>`;
    el.querySelector('.podcast-resume-play').addEventListener('click', () => {
      close();
      resumePodcastEpisode(ep);
    });
    el.querySelector('.podcast-resume-all').addEventListener('click', () => {
      close();
      openPodcastsInProgress();
    });
    el.querySelector('.podcast-resume-close').addEventListener('click', close);
    el.hidden = false;
  })();
  return close;
}
