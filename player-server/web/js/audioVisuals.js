import { createEqualizerControls, effectiveBands, frequencies } from './equalizer.js';
import { createLoudnessControls, replayGain, sameAlbum } from './replay-gain.js';
import { queueState } from './queue.js';

// One graph per player/audio element. Keep the direct destination connected
// even when visuals are off: disconnecting it would mute routed playback.
export const VISUAL_MODES = ['bars', 'wave', 'radial', 'mirror', 'orbit', 'off'];
const labels = ['Spectrum', 'Waveform', 'Radial', 'Mirror', 'Orbit', 'Off'];
const preferenceKey = 'player-audio-visual';

export function isAudiobook(media, setName = '') {
  return /\.m4b$/i.test(media?.file_name || '') ||
    /(^|[^a-z])audio[\s_-]*books?([^a-z]|$)/i.test(`${media?.file_name || ''} ${media?.rel_path || ''} ${setName}`);
}

export function initAudioVisuals({ audio, player, coverArt }) {
  const stage = player?.querySelector('.stage');
  const controls = player?.querySelector('.controls');
  if (!audio || !stage || !controls || !globalThis.MutationObserver) return null;
  const canvas = document.createElement('canvas');
  canvas.className = 'audio-visual hidden';
  canvas.setAttribute('aria-hidden', 'true');
  const ctx = canvas.getContext('2d');
  if (!ctx) return null;
  stage.appendChild(canvas);
  const label = document.createElement('div');
  label.className = 'audio-visual-control hidden';
  const caption = document.createElement('span');
  caption.textContent = 'Visuals (v)';
  const select = document.createElement('select');
  select.setAttribute('aria-label', 'Audio visualization');
  select.title = 'Press v to cycle five visualizations and Off';
  VISUAL_MODES.forEach((mode, i) => {
    const option = document.createElement('option');
    option.value = mode;
    option.textContent = labels[i];
    select.appendChild(option);
  });
  const retry = document.createElement('button');
  retry.type = 'button';
  retry.className = 'btn btn-sm';
  retry.textContent = 'Retry visuals';
  retry.hidden = true;
  const status = document.createElement('span');
  status.className = 'audio-visual-status';
  status.setAttribute('role', 'status');
  label.append(caption, select, retry, status);
  controls.appendChild(label);
  let mode = globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'off' : 'bars';
  try {
    const saved = localStorage.getItem(preferenceKey);
    if (VISUAL_MODES.includes(saved)) mode = saved;
  } catch {}
  let media = null;
  let book = false;
  let art = false;
  let context = null;
  let source = null;
  let routed = false;
  let gainNode = null;
  let remoteAlbumContext = null;
  let filters = null;
  let casting = false;
  const equalizer = createEqualizerControls(controls, () => {
    updateGain(); prepare(); sync(); document.dispatchEvent(new Event('loudness:changed'));
  });
  const loudness = createLoudnessControls(controls, () => {
    updateGain(); prepare(); sync(); document.dispatchEvent(new Event('loudness:changed'));
  });
  let analyser = null;
  let frequency = null;
  let waveform = null;
  let frame = 0;
  const AudioContext = globalThis.AudioContext || globalThis.webkitAudioContext;
  const unavailable = !AudioContext;
  let failure = '';

  function albumContext() {
    if (remoteAlbumContext !== null) return remoteAlbumContext;
    const queue = queueState();
    return queue.active && !queue.shuffle && sameAlbum(queue.entries.map(entry => entry.media));
  }
  function updateGain() {
    const result = replayGain(media?.music, loudness.settings, albumContext());
    if (filters) effectiveBands(equalizer.settings).forEach((gain, i) => filters[i].gain.setValueAtTime(gain, context.currentTime));
    equalizer.update(media, casting ? 'Equalizer applies to local playback only. Cast uses receiver effects.' : !equalizer.settings.enabled ? 'Equalizer bypassed.' : unavailable ? 'Equalizer unavailable in this browser.' : failure ||
      (context?.state === 'running' && routed ? 'Equalizer active. Boost headroom reserved.' : 'Start playback to allow equalizer processing.'));
    if (gainNode) gainNode.gain.setValueAtTime(result.multiplier, context.currentTime);
    loudness.update(media, loudness.settings.mode === 'off' ? result.message :
      unavailable ? 'Loudness processing unavailable in this browser.' : failure ||
      (context?.state === 'running' && routed ? result.message : 'Start playback to allow loudness processing.'));
  }
  function processingWanted() { return wanted() || (media?.type === 'audio' && (loudness.settings.mode !== 'off' || equalizer.settings.enabled) && !unavailable && !failure); }
  function wanted() {
    return media?.type === 'audio' && !book && mode !== 'off' && !unavailable && !failure;
  }
  function visible() {
    return wanted() && analyser && context?.state === 'running';
  }
  function stop() {
    cancelAnimationFrame(frame);
    frame = 0;
  }
  function sync() {
    updateGain();
    select.value = book || unavailable ? 'off' : mode;
    label.classList.toggle('hidden', media?.type !== 'audio');
    select.disabled = book || unavailable;
    caption.textContent = book ? 'Audiobook · visuals off' : unavailable ? 'Visuals unavailable' : 'Visuals (v)';
    retry.hidden = book || unavailable || mode === 'off' || (!failure && (audio.paused || visible()));
    status.textContent = book || mode === 'off' ? '' : unavailable ? 'Web Audio is disabled or unsupported in this browser.' : failure ||
      (!visible() && !audio.paused ? 'Start visuals to allow audio processing in this browser.' : '');
    if (!failure && !visible()) retry.textContent = 'Start visuals';
    else retry.textContent = 'Retry visuals';
    canvas.classList.toggle('hidden', !visible());
    if (media?.type === 'audio') coverArt?.classList.toggle('hidden', visible() || !art);
    stop();
    if (visible() && analyser && context.state === 'running' && !audio.paused && !audio.ended && !document.hidden &&
        player.classList.contains('open') && !player.classList.contains('minimized') && !player.classList.contains('hidden')) {
      frame = requestAnimationFrame(draw);
    }
  }
  function prepare() {
    // Only server same-origin streams are supported. Creating a source for
    // cross-origin media without CORS makes Web Audio output silence.
    if (media?.type !== 'audio') return;
    failure = '';
    if (!context && processingWanted()) {
      // Track details or conversion can outlast transient click activation.
      // Web Audio accepts the page's prior gesture (sticky activation); wait
      // for a running context below before routing native audio through it.
      if (navigator.userActivation && !navigator.userActivation.hasBeenActive) return;
      try {
        if (new URL(audio.currentSrc || audio.src, location.href).origin !== location.origin) {
          failure = 'Audio processing requires a stream from this server.';
          sync();
          return;
        }
        context = new AudioContext();
        context.addEventListener('statechange', () => {
          connectGraph();
          sync();
        });
      } catch (error) {
        failed(error);
      }
    }
    connectGraph();
    // Firefox starts suspended even inside a click handler. Resume before
    // capturing the element so a blocked context cannot silence native audio.
    // A blocked resume is recoverable on the next gesture, not unsupported.
    if (context?.state === 'suspended') context.resume().then(() => {
      connectGraph();
      sync();
    }).catch(failed);
  }
  function failed(error) {
    // Keep runtime failures recoverable: Firefox can reject audio processing
    // because of site permissions or a temporarily unavailable output device.
    // Never discard a captured source: an element can only be captured once.
    failure = `Audio processing could not start (${error?.name || 'Error'}). Retry, or check this browser's audio settings.`;
    console.warn('Player audio visualization:', error?.name || 'Error');
    sync();
  }
  function connectGraph() {
    if (!processingWanted() || context?.state !== 'running') return;
    try {
      // The source may change while resume is pending (including next track).
      if (new URL(audio.currentSrc || audio.src, location.href).origin !== location.origin) {
        failure = 'Audio processing requires a stream from this server.';
      } else {
        gainNode ||= context.createGain();
        updateGain();
        if (!routed) {
          gainNode.connect(context.destination);
          source ||= context.createMediaElementSource(audio);
          filters ||= frequencies.map(frequency => {
            const filter = context.createBiquadFilter();
            filter.type = 'peaking'; filter.frequency.value = frequency; filter.Q.value = 0.7;
            return filter;
          });
          filters.forEach((filter, i) => filter.connect(filters[i + 1] || gainNode));
          source.connect(filters[0]);
          updateGain();
          routed = true;
        }
        if (wanted() && !analyser) {
          const nextAnalyser = context.createAnalyser();
          nextAnalyser.fftSize = 1024;
          gainNode.connect(nextAnalyser);
          frequency = new Uint8Array(nextAnalyser.frequencyBinCount);
          waveform = new Uint8Array(nextAnalyser.fftSize);
          analyser = nextAnalyser;
        }
      }
    } catch (error) {
      failed(error);
    }
    sync();
  }
  function change() {
    mode = select.value;
    try { localStorage.setItem(preferenceKey, mode); } catch {}
    if (!audio.paused) prepare();
    sync();
  }
  document.addEventListener('queue:changed', () => { updateGain(); });
  select.addEventListener('change', change);
  retry.addEventListener('click', () => { prepare(); sync(); });
  document.addEventListener('keydown', (event) => {
    const tag = event.target?.tagName;
    if (event.key !== 'v' || event.repeat || event.ctrlKey || event.metaKey || event.altKey ||
        ['INPUT', 'TEXTAREA', 'SELECT'].includes(tag) || event.target?.isContentEditable ||
        document.querySelector('.modal-overlay.open, #image-lightbox.open') || media?.type !== 'audio' || book || unavailable ||
        player.classList.contains('hidden') || !player.classList.contains('open')) return;
    event.preventDefault();
    select.value = VISUAL_MODES[(VISUAL_MODES.indexOf(mode) + 1) % VISUAL_MODES.length];
    change();
  });
  for (const event of ['playing', 'pause', 'ended', 'emptied', 'error']) audio.addEventListener(event, sync);
  document.addEventListener('visibilitychange', sync);
  new MutationObserver(sync).observe(player, { attributes: true, attributeFilter: ['class'] });

  function draw() {
    frame = 0;
    if (audio.paused || audio.ended || document.hidden) return;
    const rect = stage.getBoundingClientRect();
    const scale = Math.min(globalThis.devicePixelRatio || 1, 2);
    const w = Math.max(1, Math.round(rect.width * scale));
    const h = Math.max(1, Math.round(rect.height * scale));
    if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; }
    const theme = getComputedStyle(player);
    ctx.fillStyle = theme.getPropertyValue('--player-stage-bg').trim();
    ctx.fillRect(0, 0, w, h);
    ctx.fillStyle = theme.getPropertyValue('--accent').trim();
    ctx.strokeStyle = ctx.fillStyle;
    ctx.lineWidth = 2 * scale;
    analyser.getByteFrequencyData(frequency);
    analyser.getByteTimeDomainData(waveform);
    const count = 48;
    if (mode === 'wave') {
      ctx.beginPath();
      for (let i = 0; i < waveform.length; i++) {
        const x = i / (waveform.length - 1) * w;
        const y = waveform[i] / 255 * h;
        if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
      }
      ctx.stroke();
    } else if (mode === 'bars' || mode === 'mirror') {
      for (let i = 0; i < count; i++) {
        const value = frequency[Math.floor(i * frequency.length / count)] / 255;
        const height = Math.max(scale, value * h * (mode === 'mirror' ? 0.45 : 0.9));
        ctx.fillRect(i * w / count, mode === 'mirror' ? h / 2 - height : h - height, Math.max(1, w / count - 2 * scale), height * (mode === 'mirror' ? 2 : 1));
      }
    } else {
      const radius = Math.min(w, h) * 0.2;
      for (let i = 0; i < count; i++) {
        const angle = i / count * Math.PI * 2;
        const value = frequency[Math.floor(i * frequency.length / count)] / 255;
        const outer = radius + value * Math.min(w, h) * 0.25;
        ctx.beginPath();
        if (mode === 'radial') {
          ctx.moveTo(w / 2 + Math.cos(angle) * radius, h / 2 + Math.sin(angle) * radius);
          ctx.lineTo(w / 2 + Math.cos(angle) * outer, h / 2 + Math.sin(angle) * outer);
          ctx.stroke();
        } else {
          ctx.arc(w / 2 + Math.cos(angle) * outer, h / 2 + Math.sin(angle) * outer, (2 + value * 6) * scale, 0, Math.PI * 2);
          ctx.fill();
        }
      }
    }
    frame = requestAnimationFrame(draw);
  }
  return {
    setMedia(next, thumbnailUrl = '', setName = '') {
      stop();
      media = next;
      book = isAudiobook(next, setName);
      art = !!thumbnailUrl;
      updateGain();
      sync();
    },
    prepare,
    loudnessState: () => ({ settings: { ...loudness.settings }, equalizer: equalizer.settings, albumContext: albumContext() }),
    setLoudnessState(value, { remoteContext = false } = {}) {
      if (!value) return;
      remoteAlbumContext = remoteContext ? value.albumContext === true : null;
      if (value.equalizer) equalizer.set(value.equalizer, { announce: false });
      if (value.settings) loudness.set(value.settings, { announce: false });
      updateGain(); prepare(); sync();
    },
    setCasting(value) { casting = value === true; updateGain(); },
    setLoudnessContext(value) { remoteAlbumContext = value === true; updateGain(); sync(); },
  };
}
