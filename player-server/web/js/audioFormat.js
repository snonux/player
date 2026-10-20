import { mediaResourceUrl } from './lyrics.js';

// formatAudioQuality turns the server's format answer into one line, e.g.
// "FLAC · 24-bit · 96 kHz · 2304 kbps · Stereo". Unknown parts are left out;
// an answer without a codec gives ''.
export function formatAudioQuality(format) {
  if (!format || typeof format.codec_label !== 'string' || !format.codec_label) return '';
  const parts = [format.codec_label];
  const number = (value) => Number.isFinite(value) && value > 0;
  if (number(format.bit_depth)) parts.push(`${format.bit_depth}-bit`);
  if (number(format.sample_rate)) parts.push(`${Number((format.sample_rate / 1000).toFixed(1))} kHz`);
  if (number(format.bitrate)) parts.push(`${Math.round(format.bitrate / 1000)} kbps`);
  const channels = channelName(format.channels, format.channel_layout);
  if (channels) parts.push(channels);
  let text = parts.join(' · ');
  if (format.transcoded === true) text += ' (plays as AAC)';
  return text;
}

function channelName(channels, layout) {
  if (channels === 1) return 'Mono';
  if (channels === 2) return 'Stereo';
  if (typeof layout === 'string' && /^\d+\.\d+/.test(layout)) return layout.match(/^\d+\.\d+/)[0];
  return Number.isInteger(channels) && channels > 2 ? `${channels} ch` : '';
}

// initAudioFormat adds a line under the seek bar that shows the codec and
// quality of the playing audio. It never blocks or interrupts playback.
export function initAudioFormat({ player }) {
  const controls = player?.querySelector('.controls');
  if (!controls) return null;
  const line = document.createElement('div');
  line.className = 'audio-format hidden';
  line.setAttribute('role', 'note');
  line.setAttribute('aria-label', 'Audio format');
  controls.appendChild(line);
  let request = null;
  return {
    setMedia(media, streamUrl) {
      request?.abort();
      request = null;
      line.textContent = '';
      line.removeAttribute('title');
      line.classList.add('hidden');
      const url = media?.type === 'audio' ? mediaResourceUrl(streamUrl, 'format') : null;
      if (!url) return;
      const controller = new AbortController();
      request = controller;
      fetch(url, { signal: controller.signal, credentials: 'same-origin', cache: 'no-store' })
        .then(response => response.ok ? response.json() : null)
        .then(result => {
          if (request !== controller) return;
          const text = formatAudioQuality(result);
          line.textContent = text;
          if (text) line.title = text;
          line.classList.toggle('hidden', !text);
        }).catch(() => {});
    },
  };
}
