import { savedLyrics } from './saved-lyrics.js';
import { offlineMusicMode } from './offline-context.js';

// Derive access from the explicit playback URL, including detached/share URLs.
// Unknown or cross-origin streams never trigger a library request by media ID.
export function lyricsUrl(streamUrl, base = location.href) {
  return mediaResourceUrl(streamUrl, 'lyrics', base);
}

// mediaResourceUrl maps a stream or compat URL to a sibling endpoint
// (lyrics, format) under the same media or share path.
export function mediaResourceUrl(streamUrl, resource, base = location.href) {
  try {
    const url = new URL(streamUrl, base);
    if (url.origin !== new URL(base).origin ||
        !/^\/(?:api\/(?:v1\/)?media\/\d+|s\/[^/]+)\/(?:stream|compat)$/.test(url.pathname)) return null;
    url.pathname = url.pathname.replace(/\/(?:stream|compat)$/, `/${resource}`);
    const view = url.searchParams.get('view');
    url.search = '';
    if (view) url.searchParams.set('view', view);
    return url.pathname + url.search;
  } catch { return null; }
}

const validTime = value => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 1e9;
const validLanguage = value => typeof value === 'string' && value.length <= 64 && /^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$/.test(value);

export function normalizeLyrics(data) {
  if (!Array.isArray(data?.lines)) return { synced: false, lines: [] };
  const lines = data.lines.filter(line => typeof line?.text === 'string').map(raw => {
    const line = { text: raw.text, start: raw.start };
    if (validTime(raw.end) && validTime(raw.start) && raw.end > raw.start) line.end = raw.end;
    if (typeof raw.voice === 'string' && new TextEncoder().encode(raw.voice).length <= 128 && raw.voice) line.voice = raw.voice;
    if (raw.translations && typeof raw.translations === 'object' && !Array.isArray(raw.translations)) {
      const entries = Object.entries(raw.translations);
      if (entries.length <= 32) line.translations = Object.fromEntries(entries.filter(([language, text]) => validLanguage(language) && typeof text === 'string' && text.trim()));
    }
    if (Array.isArray(raw.words) && raw.words.length && raw.words.length <= 32768 && validTime(raw.start) &&
        raw.words.every((word, i) => typeof word?.text === 'string' && validTime(word.start) && word.start >= raw.start &&
          (!i || word.start >= raw.words[i-1].start) && (line.end === undefined || word.start < line.end) &&
          (word.end === undefined || validTime(word.end) && word.end > word.start && (line.end === undefined || word.end <= line.end))) &&
        raw.words.map(word => word.text).join('') === raw.text) {
      line.words = raw.words.map(word => ({ text: word.text, start: word.start, end: word.end }));
    }
    return line;
  });
  const synced = data.synced === true && lines.length > 0 && lines.every((line, i) =>
    validTime(line.start) && (i === 0 || line.start >= lines[i - 1].start));
  return { synced, language: validLanguage(data.language) ? data.language : '', lines: lines.some(line => line.text.trim()) ? lines : [] };
}

function translationText(line, language) {
  return language && Object.hasOwn(line.translations || {}, language) && typeof line.translations[language] === 'string' ? line.translations[language] : '';
}

export function lyricText(line, language = '') {
  return translationText(line, language) || line.text;
}

// Explicit ends allow overlapping voices; implicit ends use the next distinct cue.
export function activeLyricIndices(lines, seconds) {
  if (!Number.isFinite(seconds)) return [];
  const active = [];
  let end = Infinity;
  for (let i = lines.length - 1; i >= 0; i--) {
    if (i + 1 < lines.length && lines[i+1].start > lines[i].start) end = lines[i+1].start;
    if (seconds >= lines[i].start && seconds < (lines[i].end ?? end)) active.push(i);
  }
  return active.reverse();
}

export function currentWordIndex(words, seconds, lineEnd = Infinity) {
  if (!words || !Number.isFinite(seconds) || seconds >= lineEnd) return -1;
  // Equal timestamps choose the last word, as with repeated line timestamps.
  const index = currentLyricIndex(words, seconds);
  return index >= 0 && seconds < (words[index].end ?? words[index+1]?.start ?? lineEnd) ? index : -1;
}

export function currentLyricIndex(lines, seconds) {
  if (!Number.isFinite(seconds)) return -1;
  // Last timestamp at or before playback, including repeated timestamps.
  let low = 0, high = lines.length;
  while (low < high) {
    const mid = (low + high) >>> 1;
    if (lines[mid].start <= seconds) low = mid + 1;
    else high = mid;
  }
  return low - 1;
}

// Metadata must belong to a usable loaded source before a local seek. Lyrics
// may arrive before audio metadata or contain timestamps beyond the recording.
export function seekLyric(audio, seconds) {
  if (!audio?.getAttribute('src') || audio.error || audio.readyState < 1 ||
      !Number.isFinite(seconds) || seconds < 0 ||
      !Number.isFinite(audio.duration) || audio.duration <= 0 || seconds > audio.duration) return false;
  try {
    audio.currentTime = seconds;
    return true;
  } catch { return false; }
}

export function initLyrics({ audio, player, getPosition = () => audio.currentTime,
  onSeek = seconds => seekLyric(audio, seconds) }) {
  const stage = player?.querySelector('.stage');
  const controls = player?.querySelector('.controls');
  const transport = player?.querySelector('.transport-controls');
  if (!audio || !stage || !controls) return null;
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'btn btn-sm lyrics-toggle hidden';
  button.textContent = 'Lyrics';
  button.setAttribute('aria-controls', 'audio-lyrics');
  const panel = document.createElement('div');
  panel.id = 'audio-lyrics';
  panel.className = 'audio-lyrics hidden';
  panel.setAttribute('role', 'region');
  panel.setAttribute('aria-label', 'Lyrics');
  panel.tabIndex = 0;
  // Primary strip control when lyrics exist; compactControls leaves it there.
  (transport || controls).appendChild(button);
  stage.appendChild(panel);
  const languageControl = document.createElement('label');
  languageControl.className = 'lyrics-language hidden';
  languageControl.textContent = 'Language ';
  const languageSelect = document.createElement('select');
  languageSelect.setAttribute('aria-label', 'Lyrics language');
  languageControl.appendChild(languageSelect);
  controls.appendChild(languageControl);
  const providerControls=document.createElement('div');providerControls.className='lyrics-provider-controls hidden';controls.appendChild(providerControls);
  const availability=document.createElement('span'); availability.className='text-muted text-sm'; availability.setAttribute('role','status'); providerControls.appendChild(availability);
  const lookup=document.createElement('button');lookup.type='button';lookup.className='btn btn-sm hidden';lookup.textContent='Find lyrics online';lookup.title='Send track tags to the configured lyric provider';lookup.setAttribute('aria-label','Find lyrics online (send track tags)');providerControls.appendChild(lookup);
  const remove=document.createElement('button');remove.type='button';remove.className='btn btn-sm hidden';remove.textContent='Remove provider lyrics';providerControls.appendChild(remove);
  let reload=null;
  lookup.addEventListener('click',()=>reload?.('POST'));
  remove.addEventListener('click',()=>reload?.('DELETE'));
  let language = '';
  let wordNodes = [];
  let rows = [];
  let wanted = true;
  let data = { synced: false, lines: [] };
  let request = null;
  let index = -1;
  let reading = false;
  function follow() {
    if (!data.synced) return;
    const active = panel.children[index];
    if (active) panel.scrollTop = active.offsetTop - panel.clientHeight / 2 + active.clientHeight / 2;
    else panel.scrollTop = 0;
  }
  function visibility() {
    button.classList.toggle('hidden', !data.lines.length);
    languageControl.classList.toggle('hidden', !wanted || !data.lines.some(line => Object.keys(line.translations || {}).length));
    panel.classList.toggle('hidden', !wanted || !data.lines.length);
    player.classList.toggle('has-lyrics', wanted && data.lines.length > 0);
    button.setAttribute('aria-pressed', String(wanted));
  }
  function update() {
    if (!data.synced) return;
    const seconds = getPosition();
    const activeIndices = activeLyricIndices(data.lines, seconds);
    const activeSet = new Set(activeIndices);
    const next = activeIndices.at(-1) ?? -1;
    const moved = index !== next;
    index = next;
    for (let i = 0; i < rows.length; i++) {
      const active = activeSet.has(i);
      if (active) rows[i].setAttribute('aria-current', 'true');
      else rows[i].removeAttribute('aria-current');
      const translated = translationText(data.lines[i], language);
      const word = active && !translated ? currentWordIndex(data.lines[i].words, seconds, data.lines[i].end) : -1;
      wordNodes[i]?.forEach((node, j) => node.classList.toggle('lyric-word-current', j === word));
    }
    if (moved && wanted && !reading && !panel.contains(document.activeElement)) follow();
  }
  function renderRows() {
    wordNodes = [];
    for (let i = 0; i < rows.length; i++) {
      const row = rows[i], line = data.lines[i];
      const text = lyricText(line, language);
      row.replaceChildren();
      const prefix = line.voice ? `Voice: ${line.voice} · ` : '';
      if (line.voice) {
        const voice = document.createElement('span');
        voice.className = 'lyric-voice'; voice.textContent = `Voice: ${line.voice}`;
        row.appendChild(voice);
      }
      const translated = translationText(line, language);
      const fallback = language && !translated;
      const content = document.createElement('span');
      if (fallback) content.setAttribute('lang', data.language || 'und');
      else if (language || data.language) content.setAttribute('lang', language || data.language);
      if (!translated && line.words?.length) {
        wordNodes[i] = line.words.map(word => {
          const span = document.createElement('span'); span.textContent = word.text; content.appendChild(span); return span;
        });
      } else content.textContent = text;
      row.appendChild(content);
      if (fallback) {
        const note = document.createElement('span'); note.className = 'lyric-language-fallback';
        note.textContent = ` (Original${data.language ? ': ' + data.language : ''})`; row.appendChild(note);
      }
      if (data.synced) {
        const minutes = Math.floor(line.start / 60);
        const seconds = String(Math.floor(line.start % 60)).padStart(2, '0');
        const label = `Seek to ${minutes}:${seconds}: ${prefix}${text || 'Instrumental'}`;
        row.setAttribute('aria-label', label); row.title = label;
      }
    }
    update();
  }
  languageSelect.addEventListener('change', () => { language = languageSelect.value; renderRows(); });

  button.addEventListener('click', () => {
    wanted = !wanted;
    reading = false;
    visibility();
    index = -1;
    // Remove a previous highlight before recomputing after a hidden seek.
    for (const line of panel.children) line.removeAttribute('aria-current');
    update();
  });
  // Auto-follow never moves a reader's focus or fights wheel/touch scrolling.
  // Leave the region or activate a line/toggle to return to following playback.
  for (const event of ['wheel', 'touchstart']) panel.addEventListener(event, () => { reading = true; }, { passive: true });
  panel.addEventListener('focusout', event => {
    if (panel.contains(event.relatedTarget)) return;
    reading = false;
    if (wanted) follow();
  });
  panel.addEventListener('pointerleave', () => {
    if (panel.contains(document.activeElement)) return;
    reading = false;
    if (wanted) follow();
  });
  // Reading keys belong to this scroll region. Stop the library's document
  // shortcuts while preserving native arrow/page/home/end scrolling; playback
  // shortcuts (Space/p, horizontal seek, Escape) still reach the player.
  panel.addEventListener('keydown', (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey || event.target?.tagName === 'SELECT') return;
    if (['ArrowUp', 'ArrowDown', 'PageUp', 'PageDown', 'Home', 'End'].includes(event.key)) {
      reading = true;
      event.stopPropagation();
    } else if (event.key === 'j' || event.key === 'k') {
      reading = true;
      event.preventDefault();
      event.stopPropagation();
      const step = panel.firstElementChild?.clientHeight || 24;
      panel.scrollTop += event.key === 'j' ? step : -step;
    }
  });
  for (const event of ['timeupdate', 'seeking', 'seeked', 'loadedmetadata']) audio.addEventListener(event, update);
  return {
    update,
    setMedia(media, streamUrl, {pending=false,backing=null,saved=false} = {}) {
      const hadFocus = panel.contains(document.activeElement) || languageControl.contains(document.activeElement);
      request?.abort();
      request = null;
      reload=null;availability.textContent='';providerControls.classList.toggle('hidden',true);lookup.classList.toggle('hidden',true);remove.classList.toggle('hidden',true);
      data = { synced: false, lines: [] };
      index = -1;
      reading = false;
      panel.removeAttribute('aria-description');
      // A removed lyric button must not strand focus on the page body.
      if (hadFocus) controls.querySelector('button:not(.lyrics-toggle):not(:disabled)')?.focus();
      language = '';
      rows = []; wordNodes = [];
      languageSelect.replaceChildren();
      panel.replaceChildren();
      panel.scrollTop = 0;
      visibility();
      if(pending)return;
      const url = media?.type === 'audio' ? lyricsUrl(streamUrl) : null;
      const savedSource=saved || !!backing || !!media?.offlinePinURL || offlineMusicMode();
      if (!url && !savedSource) return;
      const controller = new AbortController();
      request = controller;
      const load=(method='GET')=>{
      lookup.disabled=true;remove.disabled=true;
      const loading=savedSource ? savedLyrics(media,streamUrl,controller.signal,backing) : fetch(url, {method, signal: controller.signal, credentials: 'same-origin', redirect:'manual', cache: 'no-store' })
        .then(response => response.ok ? response.json() : null)
        .catch(error=>{if(method==='GET' && error instanceof TypeError)return savedLyrics(media,streamUrl,controller.signal,backing);throw error;});
      return loading
        .then(result => {
          if (request !== controller) return;
          rows=[];wordNodes=[];languageSelect.replaceChildren();language='';
          lookup.disabled=false;remove.disabled=false;
          const enabled=result?.provider_enabled===true && !savedSource;
          providerControls.classList.toggle('hidden',!enabled && !result?.source && !savedSource);
          lookup.classList.toggle('hidden',!enabled || ['sidecar','embedded'].includes(result?.source));
          remove.classList.toggle('hidden',!enabled || result?.source!=='provider');
          lookup.textContent=['error','busy'].includes(result?.provider_state)?'Retry lyric lookup':'Find lyrics online';
          availability.textContent=result?.provider_state==='error'?'Lyric lookup failed. Audio remains available.':result?.provider_state==='busy'?'Lyric lookup busy. Try again.':result?.provider_state==='empty'?'No provider match.':result?.saved?'Saved lyrics':result?.source?`Lyrics: ${result.source}`:savedSource?'No saved lyrics':'';
          data = normalizeLyrics(result);
          panel.setAttribute('aria-description', data.synced
            ? 'Select a line to seek. Tab to a line and press Enter.'
            : 'Read lyrics with arrows, Page Up or Down, Home or End, or j and k.');
          const languages = [...new Set(data.lines.flatMap(line => Object.keys(line.translations || {})))].sort();
          for (const code of ['', ...languages]) {
            const option = document.createElement('option'); option.value = code;
            option.textContent = code || `Original${data.language ? ' (' + data.language + ')' : ''}`;
            languageSelect.appendChild(option);
          }
          languageSelect.value = '';
          const fragment = document.createDocumentFragment();
          for (const line of data.lines) {
            const row = document.createElement(data.synced ? 'button' : 'p');
            row.textContent = line.text;
            if (data.synced) {
              row.type = 'button';
              row.className = 'lyric-seek';
              row.addEventListener('click', () => {
                // A retained/detached old node cannot seek a replacement track.
                if (request !== controller || !panel.contains(row)) return;
                if (onSeek(line.start) !== true) return;
                reading = false;
                update();
                follow();
              });
            }
            rows.push(row);
            fragment.appendChild(row);
          }
          panel.replaceChildren(fragment);
          renderRows();
          visibility();
          update();
          if(method!=='GET') (enabled && !['sidecar','embedded'].includes(result?.source) ? lookup : button).focus();
        }).catch(() => {if(request===controller){lookup.disabled=false;remove.disabled=false;availability.textContent='Could not load lyrics. Try again.';}}); // Lyrics never interrupt playback.
      };
      reload=load;void load();
    },
  };
}
