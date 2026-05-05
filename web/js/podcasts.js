// podcastUI.js — Podcast feed manager + episode rendering.

export function createPodcastManager(container, api, renderEpisodeList) {
  const el = document.createElement('div');
  el.id = 'podcast-manager';
  el.className = 'modal';
  el.innerHTML = `
    <div class="modal-content">
      <h2>Podcast Feeds</h2>
      <div class="podcast-add-form">
        <input id="pm-url" type="url" placeholder="https://example.com/feed.xml" />
        <input id="pm-name" type="text" placeholder="Folder name (optional)" />
        <button id="pm-add">Subscribe</button>
      </div>
      <div class="podcast-list" id="pm-list"></div>
      <button class="btn-close">Close</button>
    </div>
  `;
  container.appendChild(el);

  const urlInput = el.querySelector('#pm-url');
  const nameInput = el.querySelector('#pm-name');
  const addBtn = el.querySelector('#pm-add');
  const listEl = el.querySelector('#pm-list');
  const closeBtn = el.querySelector('.btn-close');

  async function refresh() {
    try {
      const podcasts = await api.podcasts();
      listEl.innerHTML = podcasts.length === 0
        ? '<p>No podcasts subscribed yet.</p>'
        : podcasts.map(p => `
          <div class="podcast-item" data-id="${p.id}">
            <strong>${p.name}</strong>
            <span class="badge">${p.is_podcast ? 'Podcast' : 'Set'}</span>
          </div>
        `).join('');
    } catch (err) {
      listEl.innerHTML = `<p class="error">Error: ${err.message}</p>`;
    }
  }

  addBtn.addEventListener('click', async () => {
    const url = urlInput.value.trim();
    if (!url) return;
    addBtn.disabled = true;
    try {
      await api.subscribePodcast(url, nameInput.value.trim());
      urlInput.value = '';
      nameInput.value = '';
      await refresh();
    } catch (err) {
      alert('Failed to subscribe: ' + err.message);
    }
    addBtn.disabled = false;
  });

  closeBtn.addEventListener('click', () => el.classList.remove('open'));

  return {
    open() {
      el.classList.add('open');
      refresh();
    },
    close() {
      el.classList.remove('open');
    }
  };
}

export function renderEpisodeCard(ep, onDownload, onToggleComplete) {
  const isDownloaded = ep.is_downloaded;
  const completed = ep.is_completed;
  const dateStr = ep.published_at ? new Date(ep.published_at).toLocaleDateString() : '';
  const duration = ep.duration_seconds ? formatDuration(ep.duration_seconds) : '';
  const size = ep.file_size ? formatBytes(ep.file_size) : '';

  return `
    <div class="card episode-card ${completed ? 'completed' : ''}" data-episode-id="${ep.id}">
      <div class="card-info">
        <div class="title">${escapeHtml(ep.title)}</div>
        <div class="meta">
          ${dateStr ? `<span class="date">${dateStr}</span>` : ''}
          ${duration ? `<span class="duration">${duration}</span>` : ''}
          ${size ? `<span class="size">${size}</span>` : ''}
        </div>
      </div>
      <div class="card-actions">
        ${!isDownloaded
          ? `<button class="btn-download-episode" title="Download to server">Download</button>`
          : `<button class="btn-play" title="Play">Play</button>`
        }
        <button class="btn-complete ${completed ? 'active' : ''}" title="Mark as listened">
          ${completed ? '&#10003; Listened' : 'Mark listened'}
        </button>
      </div>
    </div>
  `;
}

function formatDuration(s) {
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  if (h > 0) return `${h}h ${m % 60}m`;
  return `${m}m`;
}

function formatBytes(b) {
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
  return `${(b / (1024 * 1024)).toFixed(1)} MB`;
}

function escapeHtml(str) {
  if (!str) return '';
  return str.replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
}
