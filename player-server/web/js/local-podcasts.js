import { bindPodcastMultiselect } from './podcast-multiselect.js';
import { localPodcastSnapshot, editLocalPodcast } from './local-podcast-state.js';
import { requestedLocalPodcastResume } from './local-podcast-resume.js';
import { publicationDate, showNotesHTML, episodeArtworkHTML, bindPodcastMetadata, confirmLocalEpisodeDelete } from './podcast-metadata.js';
import { bindSwipeDelete } from './swipe-delete.js';
import { initPodcastPlayerDelete } from './podcast-player-delete.js';
import { openPodcastDiscovery } from './podcast-discovery.js';
import { t, html } from './i18n.js';
import { initPlayer } from './player.js';
import { initQueue, insertQueue, advanceQueue } from './queue.js';
import { initQueuePanel } from './queue-panel.js';
import {
  fetchPodcastFeed,
  feedKeyFromURL,
  listLocalPodcastEpisodes,
  listLocalPodcastFeeds,
  removeLocalPodcastFeed,
  saveLocalPodcastFeed,
} from './local-podcasts-store.js';

export function initLocalPodcastsApp() {
  const header = document.querySelector('.header-main');
  const host = document.querySelector('.main-content');
  const shortcuts = document.getElementById('keyboard-access-toggle');
  header.innerHTML = html`<a href="/index.html" class="logo">Player</a><div class="spacer"></div><button class="btn" data-queue-toggle>Queue</button><button id="theme-toggle" class="icon-btn" aria-label="Toggle theme"></button><a class="btn" href="/login.html">Sign In</a>`;
  if (shortcuts) { shortcuts.className = 'icon-btn'; shortcuts.textContent = '⌨'; header.append(shortcuts); }
  document.querySelector('#sidebar')?.classList.remove('open');
  document.querySelector('.page')?.classList.remove('has-sidebar');
  host.innerHTML = html`<section class="local-podcasts" aria-label="Podcasts on this device">
    <h1>Podcasts on this device</h1>
    <p>Subscribe without signing in. Subscriptions and episode lists stay in this browser. Feeds must allow CORS; blocked feeds need the signed-in server workflow.</p>
    <div class="playlist-actions">
      <button class="btn btn-primary" type="button" data-explore>Explore podcasts</button>
      <button class="btn" type="button" data-subscribe>Add feed URL</button>
      <button class="btn" type="button" data-refresh-all>Refresh feeds</button>
      <a class="btn" href="/index.html?local=files">On this device files</a>
    </div>
    <details><summary>Episode retention</summary><p>Completed episodes leave this browser’s lists after seven days. Playing again or marking uncompleted keeps an episode. Server feeds and other listeners stay unchanged.</p></details>
    <p role="status" aria-live="polite" data-status></p>
    <div data-results></div>
  </section>`;
  initPlayer({ personalActions: false, cast: false, onNext: options => advanceQueue(options?.delta ?? 1, options), onPrevious: options => advanceQueue(options?.delta ?? -1, options) });
  initQueue(); initQueuePanel();
  initPodcastPlayerDelete({ next: () => advanceQueue(1) });
  for (const selector of ['#btn-detach', '#btn-cast', '#btn-download', '#btn-player-notes', '#btn-player-favorite', '#btn-bookmarks', '#btn-save-position']) {
    const control = document.querySelector(selector);
    if (control) { control.disabled = true; control.hidden = true; }
  }

  const status = host.querySelector('[data-status]');
  const results = host.querySelector('[data-results]');
  let busy = false;
  let detailKey = null;
  let episodeLayout = 'list';
  let completionFilter = 'uncompleted';
  let episodeSelection;
  const refreshEpisodes = () => { if (detailKey && !episodeSelection?.active) void render(); };
  const retentionTimer = setInterval(refreshEpisodes, 60 * 1000);
  retentionTimer.unref?.();
  document.addEventListener('local-podcast:state-changed', refreshEpisodes);
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') refreshEpisodes(); });

  function message(text) { status.textContent = t(text); }

  async function playEpisode(episode) {
    if (busy) return;
    busy = true; message('Starting playback…');
    try {
      const media = episode => ({
        id: episode.episode_key,
        type: 'audio',
        file_name: episode.title,
        duration: 0,
        localPodcastURL: episode.enclosure_url,
        localPodcast: true,
        localPodcastEpisode: { feed_key: episode.feed_key, episode_key: episode.episode_key },
        podcast: { title: episode.title, description: episode.description,
          image_url: episode.image_url, feed_image_url: episode.feed_image_url },
      });
      const episodes = await listLocalPodcastEpisodes(episode.feed_key);
      const index = episodes.findIndex(row => row.episode_key === episode.episode_key);
      const remaining = (index < 0 ? [episode] : episodes.slice(index)).filter(row => !localPodcastSnapshot(row).deletion?.deleted && (row.episode_key === episode.episode_key || !localPodcastSnapshot(row).completion?.completed));
      const added = await insertQueue(remaining.map(media), 'now', { replace: true, source: t('Podcasts on this device') });
      message(added ? 'Playing.' : 'Playback is busy. Retry.');
    } catch (error) {
      message(error.message || 'Could not play this episode (enclosure may block CORS).');
    } finally {
      busy = false;
    }
  }

  // State changes (position, completion, deletion) can render again while an
  // earlier render still waits for storage; only the latest one may draw.
  let renderSerial = 0;
  async function render() {
    const renderID = ++renderSerial;
    episodeSelection?.dispose();
    episodeSelection = undefined;
    const hadFocus = results.contains(document.activeElement);
    results.replaceChildren();
    try {
      if (detailKey) {
        const back = document.createElement('button');
        back.className = 'btn btn-ghost';
        back.type = 'button';
        back.textContent = '← Back';
        back.onclick = () => { detailKey = null; void render(); };
        results.append(back);
        const allEpisodes = await listLocalPodcastEpisodes(detailKey);
        if (renderID !== renderSerial) return;
        const filters = document.createElement('div'); filters.className = 'playlist-actions';
        filters.setAttribute('role', 'group'); filters.setAttribute('aria-label', 'Completion filter');
        for (const [value, label] of [['uncompleted', 'Uncompleted'], ['completed', 'Completed'], ['all', 'All']]) {
          const button = document.createElement('button'); button.type = 'button'; button.className = 'btn';
          button.textContent = label; button.setAttribute('aria-pressed', String(completionFilter === value));
          button.onclick = () => { completionFilter = value; void render(); }; filters.append(button);
        }
        results.append(filters);
        const episodes = allEpisodes.filter(ep => {
          const saved = localPodcastSnapshot(ep);
          return !saved.deletion?.deleted && (completionFilter === 'all' || Boolean(saved.completion?.completed) === (completionFilter === 'completed'));
        });
        if (!episodes.length) {
          message(allEpisodes.length ? 'No episodes match this filter.' : 'No episodes cached for this feed yet. Refresh the feed.');
          if (hadFocus) filters.querySelector('button[aria-pressed="true"]')?.focus();
          return;
        }
        message(`${episodes.length} episode${episodes.length === 1 ? '' : 's'}`);
        const list = document.createElement('ul');
        list.className = `local-files-list local-episode-list${episodeLayout === 'grid' ? ' episode-grid' : ''}`;
        const layouts = document.createElement('div');
        layouts.className = 'playlist-actions episode-layout-controls';
        layouts.setAttribute('role', 'group'); layouts.setAttribute('aria-label', 'Episode layout');
        for (const value of ['list', 'grid']) {
          const button = document.createElement('button');
          button.type = 'button'; button.className = 'btn';
          button.textContent = value === 'grid' ? 'Grid' : 'List';
          button.setAttribute('aria-pressed', String(episodeLayout === value));
          button.onclick = () => {
            episodeLayout = value;
            list.classList.toggle('episode-grid', value === 'grid');
            layouts.querySelectorAll('button').forEach(control => control.setAttribute('aria-pressed', String(control === button)));
          };
          layouts.append(button);
        }
        results.append(layouts);
        const selectionEntries = [];
        for (const episode of episodes) {
          const li = document.createElement('li');
          const completed = Boolean(localPodcastSnapshot(episode).completion?.completed);
          li.className = `local-files-row${completed ? ' listened' : ''}`;
          const play = document.createElement('button');
          play.className = 'btn btn-ghost';
          play.type = 'button';
          play.textContent = episode.title;
          play.onclick = () => playEpisode(episode);
          const copy = document.createElement('div');
          copy.className = 'local-podcast-copy';
          const meta = document.createElement('p');
          meta.className = 'subtitle'; meta.textContent = publicationDate(episode.published_at);
          copy.append(play, meta);
          if (completed) {
            const listened = document.createElement('p');
            listened.className = 'episode-listened'; listened.textContent = '✓ Listened';
            copy.append(listened);
          }
          const more = document.createElement('details');
          const summary = document.createElement('summary'); summary.className = 'btn'; summary.textContent = 'More';
          summary.setAttribute('aria-label', `More for ${episode.title}`); more.append(summary);
          for (const [name, label, value] of [
            ['completion', localPodcastSnapshot(episode).completion?.completed ? 'Mark uncompleted' : 'Mark completed', !localPodcastSnapshot(episode).completion?.completed],
            ['deletion', 'Delete episode', true],
          ]) {
            const button = document.createElement('button'); button.type = 'button'; button.className = 'btn'; button.textContent = label;
            button.onclick = () => {
              if (name === 'deletion' && !confirmLocalEpisodeDelete(episode.title)) return;
              try { editLocalPodcast(episode, name, value); } catch (error) { message(error.message); }
            }; more.append(button);
            if (name === 'deletion') {
              bindSwipeDelete(li, () => button.click(), { enabled: () => !list.classList.contains('episode-grid') && !episodeSelection?.active });
            }
          }
          copy.append(more);
          const notes = document.createElement('div'); notes.innerHTML = showNotesHTML(episode.description); copy.append(notes);
          const artwork = document.createElement('span'); artwork.innerHTML = episodeArtworkHTML(episode);
          li.append(artwork, copy);
          bindPodcastMetadata(li);
          list.append(li);
          selectionEntries.push({card: li, ep: episode});
        }
        results.append(list);
        const selectionFeed = detailKey, selectionFilter = completionFilter;
        episodeSelection = bindPodcastMultiselect(results, selectionEntries, {
          current: () => detailKey === selectionFeed && completionFilter === selectionFilter,
          apply: async (ep, action) => { editLocalPodcast(ep, action === 'delete' ? 'deletion' : 'completion', action !== 'uncompleted'); },
          refresh: () => render(), notify: message,
        });
        if (hadFocus) list.querySelector('button')?.focus();
        return;
      }
      const feeds = await listLocalPodcastFeeds();
      if (renderID !== renderSerial) return;
      if (!feeds.length) {
        message('No device subscriptions yet. Subscribe with a feed URL.');
        return;
      }
      message(`${feeds.length} subscription${feeds.length === 1 ? '' : 's'}`);
      const list = document.createElement('ul');
      list.className = 'local-files-list';
      for (const feed of feeds) {
        const li = document.createElement('li');
        li.className = 'local-files-row';
        const open = document.createElement('button');
        open.className = 'btn btn-ghost';
        open.type = 'button';
        open.textContent = feed.title || feed.feed_url;
        open.onclick = () => { detailKey = feed.feed_key; void render(); };
        const more = document.createElement('button');
        more.className = 'icon-btn';
        more.type = 'button';
        more.textContent = '☰';
        more.setAttribute('aria-label', `More for ${feed.title || feed.feed_url}`);
        more.onclick = () => openMore(more, feed);
        const artwork = document.createElement('span');
        artwork.innerHTML = episodeArtworkHTML({ feed_image_url: feed.image_url || feed.episodes?.find(episode => episode.image_url)?.image_url || '' });
        li.append(artwork, open, more);
        bindPodcastMetadata(li);
        list.append(li);
      }
      results.append(list);
    } catch (error) {
      message(error.message || 'Could not open local podcasts.');
    }
  }

  function openMore(button, feed) {
    const popover = document.createElement('div');
    popover.className = 'shortcut-picker';
    popover.setAttribute('popover', 'auto');
    popover.setAttribute('role', 'menu');
    popover.innerHTML = html`<button class="btn btn-ghost" data-action="refresh">Refresh feed</button><button class="btn btn-ghost" data-action="remove">Unsubscribe</button>`;
    popover.querySelector('[data-action="refresh"]').onclick = async () => {
      popover.hidePopover();
      await refreshOne(feed.feed_url);
      button.focus();
    };
    popover.querySelector('[data-action="remove"]').onclick = async () => {
      popover.hidePopover();
      await removeLocalPodcastFeed(feed.feed_key);
      if (detailKey === feed.feed_key) detailKey = null;
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

  async function refreshOne(url) {
    busy = true; message('Refreshing feed…');
    try {
      const feed = await fetchPodcastFeed(url);
      await saveLocalPodcastFeed(feed);
      message(`Updated ${feed.title} (${feed.episodes.length} episodes).`);
      await render();
    } catch (error) {
      message(error.message || 'Feed refresh failed.');
    } finally {
      busy = false;
    }
  }

  host.querySelector('[data-explore]').onclick = () => openPodcastDiscovery({
    subscribe: async url => { const feed = await fetchPodcastFeed(url); await saveLocalPodcastFeed(feed); },
    onSubscribed: render,
  });

  host.querySelector('[data-subscribe]').onclick = async () => {
    const url = prompt('Podcast feed URL');
    if (!url) return;
    try {
      feedKeyFromURL(url);
    } catch {
      message('Enter a valid podcast feed URL, such as https://example.com/feed.xml.');
      return;
    }
    await refreshOne(url);
  };

  host.querySelector('[data-refresh-all]').onclick = async () => {
    const feeds = await listLocalPodcastFeeds();
    let failed = 0;
    for (const feed of feeds) {
      try {
        await refreshOne(feed.feed_url);
      } catch {
        failed += 1;
      }
    }
    if (failed) message(`${failed} feed${failed === 1 ? '' : 's'} could not refresh (often CORS).`);
  };

  void render();
  void resumeRequested();

  // A login page "Continue listening" link names the episode to resume.
  async function resumeRequested() {
    const wanted = requestedLocalPodcastResume();
    if (!wanted) return;
    history.replaceState(history.state, '', '/index.html?local=podcasts');
    const episode = (await listLocalPodcastEpisodes(wanted.feed_key)).find(ep => ep.episode_key === wanted.episode_key);
    if (!episode) { message('This episode is no longer saved in this browser.'); return; }
    detailKey = wanted.feed_key;
    await render();
    await playEpisode(episode);
  }
}
