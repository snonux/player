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
  const label = document.createElement('label');
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
  label.append(caption, select);
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
  let analyser = null;
  let frequency = null;
  let waveform = null;
  let frame = 0;
  let unavailable = false;
  const AudioContext = globalThis.AudioContext || globalThis.webkitAudioContext;
  if (!AudioContext) unavailable = true;

  function wanted() {
    return media?.type === 'audio' && !book && mode !== 'off' && !unavailable;
  }
  function visible() {
    return wanted() && analyser && context?.state === 'running';
  }
  function stop() {
    cancelAnimationFrame(frame);
    frame = 0;
  }
  function sync() {
    select.value = book || unavailable ? 'off' : mode;
    label.classList.toggle('hidden', media?.type !== 'audio');
    select.disabled = book || unavailable;
    caption.textContent = book ? 'Audiobook · visuals off' : unavailable ? 'Visuals unavailable' : !analyser && !audio.paused && wanted() ? 'Visuals (v to start)' : 'Visuals (v)';
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
    if (!context && wanted()) {
      // Creating a graph during autoplay could route sound into a browser-
      // suspended context. Keep native playback until an actual gesture.
      if (navigator.userActivation && !navigator.userActivation.isActive) return;
      try {
        if (new URL(audio.currentSrc || audio.src, location.href).origin !== location.origin) {
          unavailable = true;
          sync();
          return;
        }
        context = new AudioContext();
        if (context.state !== 'running') {
          context.close().catch(() => {});
          context = null;
          unavailable = true;
          sync();
          return;
        }
        const source = context.createMediaElementSource(audio);
        source.connect(context.destination);
        analyser = context.createAnalyser();
        analyser.fftSize = 1024;
        source.connect(analyser);
        frequency = new Uint8Array(analyser.frequencyBinCount);
        waveform = new Uint8Array(analyser.fftSize);
        context.addEventListener('statechange', sync);
      } catch {
        unavailable = true;
        sync();
      }
    }
    // Once routed, off/audiobooks still need a running context for sound.
    if (context?.state === 'suspended') context.resume().then(sync).catch(() => {
      unavailable = true;
      sync();
    });
  }
  function change() {
    mode = select.value;
    try { localStorage.setItem(preferenceKey, mode); } catch {}
    if (!audio.paused) prepare();
    sync();
  }
  select.addEventListener('change', change);
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
      sync();
    },
    prepare,
  };
}
