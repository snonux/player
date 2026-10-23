// A stable transport seam. DOM audio remains untouched and owns ordinary
// streaming; a decoded transport can temporarily supply the same controls.
const events = ['timeupdate', 'loadedmetadata', 'durationchange', 'progress', 'loadeddata', 'canplay', 'seeked', 'seeking', 'ended', 'play', 'pause', 'playing', 'error', 'ratechange', 'volumechange', 'loadstart'];
export function createAudioTransport(native) {
  const target = new EventTarget();
  let decoded = null;
  const listeners = new Map();
  const emit = name => target.dispatchEvent(new Event(name));
  for (const name of events) native.addEventListener(name, () => { if (!decoded) emit(name); });
  const transport = {
    native,
    get decoded() { return decoded; },
    use(value) {
      if (decoded === value) return;
      for (const [name, listener] of listeners) decoded?.removeEventListener(name, listener);
      listeners.clear();
      decoded?.dispose();
      decoded = value;
      if (decoded) {
        native.pause();
        decoded.volume = native.volume; decoded.muted = native.muted;
        for (const name of events) {
          const listener = () => emit(name); listeners.set(name, listener);
          decoded.addEventListener(name, listener);
        }
      }
    },
    addEventListener: target.addEventListener.bind(target),
    removeEventListener: target.removeEventListener.bind(target),
    dispatchEvent: target.dispatchEvent.bind(target),
    play: () => (decoded || native).play(),
    pause: () => (decoded || native).pause(),
    load() { transport.use(null); native.load(); },
    getAttribute: name => name === 'src' && decoded ? decoded.src : native.getAttribute(name),
    removeAttribute(name) { if (name === 'src') transport.use(null); native.removeAttribute(name); },
  };
  for (const name of ['currentTime', 'duration', 'paused', 'ended', 'readyState', 'error', 'buffered', 'currentSrc', 'src', 'networkState']) {
    Object.defineProperty(transport, name, { get: () => (decoded || native)[name], set: value => { (decoded || native)[name] = value; } });
  }
  for (const name of ['volume', 'muted', 'playbackRate', 'defaultPlaybackRate', 'preservesPitch']) {
    Object.defineProperty(transport, name, { get: () => (decoded || native)[name] ?? native[name], set: value => {
      native[name] = value;
      if (decoded) decoded[name] = value;
    } });
  }
  return transport;
}
