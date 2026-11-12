// local-podcast-resume.js — started podcasts of this browser, without sign-in.
// "Podcasts on this device" keep their progress in this browser, so the login
// page can list the episodes in progress and resume one in one click, like
// Android's sign-in screen does.
import { listLocalPodcastFeeds, listLocalPodcastEpisodes } from './local-podcasts-store.js';
import { localPodcastSnapshot } from './local-podcast-state.js';
import { escapeHtml, fmtDur } from './utils.js';

export const RESUME_PARAM = 'resume';
const LIMIT = 10;

// Seconds to resume a browser-device episode from; 0 starts it fresh.
export function localPodcastResumeSeconds(ep) {
  const saved = localPodcastSnapshot(ep);
  if (saved.deletion?.deleted || saved.completion?.completed) return 0;
  const seconds = Number(saved.position?.seconds);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
}

// Started, unfinished episodes, most recently played first.
export async function listLocalPodcastsInProgress(limit = LIMIT) {
  const started = [];
  for (const feed of await listLocalPodcastFeeds()) {
    for (const ep of await listLocalPodcastEpisodes(feed.feed_key)) {
      const position = localPodcastResumeSeconds(ep);
      if (position <= 0) continue;
      started.push({ ...ep, feed_title: feed.title || '', position_seconds: position,
        played_ms: localPodcastSnapshot(ep).position?.observed_ms || 0 });
    }
  }
  return started.sort((a, b) => b.played_ms - a.played_ms).slice(0, limit);
}

// The episode rides in the fragment: the server serves this public device
// shell only for the exact query, and the fragment never reaches it.
export function localPodcastResumeHref(ep) {
  const params = new URLSearchParams({ [RESUME_PARAM]: JSON.stringify([ep.feed_key, ep.episode_key]) });
  return `/index.html?local=podcasts#${params}`;
}

// The episode a login page link asked to resume, or null.
export function requestedLocalPodcastResume(hash = globalThis.location?.hash || '') {
  try {
    const [feed_key, episode_key] = JSON.parse(new URLSearchParams(hash.replace(/^#/, '')).get(RESUME_PARAM) || 'null') || [];
    return typeof feed_key === 'string' && typeof episode_key === 'string' ? { feed_key, episode_key } : null;
  } catch {
    return null;
  }
}

// Fills host with resume links; an empty or unreadable library hides it.
export async function mountLocalPodcastResume(host) {
  if (!host) return;
  let episodes = [];
  try { episodes = await listLocalPodcastsInProgress(); } catch { /* Optional shortcut. */ }
  if (!episodes.length) { host.hidden = true; return; }
  host.innerHTML = `<h2 class="local-resume-heading">Continue listening</h2>
    <ul class="local-resume-list">${episodes.map(ep => {
      const title = ep.title || 'Podcast episode';
      const subtitle = [ep.feed_title, `Resume at ${fmtDur(ep.position_seconds)}`].filter(Boolean).join(' · ');
      return `<li><a class="local-resume-item" href="${escapeHtml(localPodcastResumeHref(ep))}" aria-label="Resume podcast: ${escapeHtml(title)}, ${escapeHtml(subtitle)}">
        <span class="local-resume-title">${escapeHtml(title)}</span>
        <span class="local-resume-subtitle text-muted text-sm">${escapeHtml(subtitle)}</span></a></li>`;
    }).join('')}</ul>`;
  host.hidden = false;
}
