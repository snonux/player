import { publicationDate, showNotesHTML, episodeArtworkHTML, bindPodcastMetadata } from './podcast-metadata.js';
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
    <p role="status" aria-live="polite" data-status></p>
    <div data-results></div>
  </section>`;
  initPlayer({ personalActions: false, cast: false, onNext: options => advanceQueue(options?.delta ?? 1, options), onPrevious: options => advanceQueue(options?.delta ?? -1, options) });
  initQueue(); initQueuePanel();
  for (const selector of ['#btn-detach', '#btn-cast', '#btn-download', '#btn-player-notes', '#btn-player-favorite', '#btn-bookmarks', '#btn-save-position']) {
    const control = document.querySelector(selector);
    if (control) { control.disabled = true; control.hidden = true; }
  }

  const status = host.querySelector('[data-status]');
  const results = host.querySelector('[data-results]');
  let busy = false;
  let detailKey = null;

  function message(text) { status.textContent = t(text); }

  async function playEpisode(episode) {
    if (busy) return;
    busy = true; message('Starting playback…');
    try {
      const media = {
        id: episode.episode_key,
        type: 'audio',
        file_name: episode.title,
        duration: 0,
        offlinePinURL: episode.enclosure_url,
        localPodcast: true,
      };
      const added = await insertQueue([media], 'now', { replace: true, source: t('Podcasts on this device') });
      message(added ? 'Playing.' : 'Playback is busy. Retry.');
    } catch (error) {
      message(error.message || 'Could not play this episode (enclosure may block CORS).');
    } finally {
      busy = false;
    }
  }

  async function render() {
    results.replaceChildren();
    try {
      if (detailKey) {
        const back = document.createElement('button');
        back.className = 'btn btn-ghost';
        back.type = 'button';
        back.textContent = '← Back';
        back.onclick = () => { detailKey = null; void render(); };
        results.append(back);
        const episodes = await listLocalPodcastEpisodes(detailKey);
        if (!episodes.length) {
          message('No episodes cached for this feed yet. Refresh the feed.');
          return;
        }
        message(`${episodes.length} episode${episodes.length === 1 ? '' : 's'}`);
        const list = document.createElement('ul');
        list.className = 'local-files-list';
        for (const episode of episodes) {
          const li = document.createElement('li');
          li.className = 'local-files-row';
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
          const notes = document.createElement('div'); notes.innerHTML = showNotesHTML(episode.description); copy.append(notes);
          const artwork = document.createElement('span'); artwork.innerHTML = episodeArtworkHTML(episode);
          li.append(artwork, copy);
          bindPodcastMetadata(li);
          list.append(li);
        }
        results.append(list);
        return;
      }
      const feeds = await listLocalPodcastFeeds();
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
        li.append(open, more);
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
}
