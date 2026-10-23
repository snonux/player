export const equalizerKey = 'player-equalizer-v1';
export const frequencies = [60, 230, 910, 3600, 14000];
export const presets = { Flat: [0, 0, 0, 0, 0], 'Bass boost': [6, 3, 0, -2, -4], Voice: [-4, -2, 4, 3, -2] };
export function equalizerSettings(value) {
  return { enabled: value?.enabled === true, bands: frequencies.map((_, i) => {
    const n = value?.bands?.[i];
    return Number.isFinite(n) ? Math.max(-12, Math.min(12, n)) : 0;
  }) };
}
export function effectiveBands(settings) {
  const { enabled, bands } = equalizerSettings(settings);
  const headroom = Math.max(0, ...bands);
  return bands.map(n => enabled ? n - headroom : 0);
}
export function createEqualizerControls(controls, changed) {
  let settings;
  try { settings = equalizerSettings(JSON.parse(localStorage.getItem(equalizerKey))); } catch { settings = equalizerSettings(); }
  const panel = document.createElement('div');
  panel.className = 'equalizer-control loudness-control hidden';
  panel.innerHTML = `<details><summary>Equalizer</summary><label><input type="checkbox" aria-label="Enable equalizer"> Enable equalizer</label><label>Preset <select aria-label="Equalizer preset">${Object.keys(presets).map(name => `<option>${name}</option>`).join('')}<option>Custom</option></select></label>${frequencies.map(f => `<label>${f < 1000 ? `${f} Hz` : `${f / 1000} kHz`} <input type="range" min="-12" max="12" step="1" aria-label="Equalizer ${f} Hz"><output></output></label>`).join('')}<p>Local audio only; remote receivers use their own effects. Boosts reserve headroom and may reduce overall volume. Saved on this browser.</p><button type="button" class="btn btn-sm">Reset flat</button><button type="button" class="btn btn-sm">Retry equalizer</button><span role="status"></span></details>`;
  const enabled = panel.querySelector('input[type="checkbox"]'), select = panel.querySelector('select');
  const sliders = [...panel.querySelectorAll('input[type="range"]')], outputs = [...panel.querySelectorAll('output')];
  function set(value, { persist = true, announce = true } = {}) {
    settings = equalizerSettings(value);
    enabled.checked = settings.enabled;
    select.value = Object.keys(presets).find(name => presets[name].every((n, i) => n === settings.bands[i])) || 'Custom';
    sliders.forEach((slider, i) => { slider.value = settings.bands[i]; outputs[i].textContent = `${settings.bands[i]} dB`; });
    if (persist) { try { localStorage.setItem(equalizerKey, JSON.stringify(settings)); } catch {} }
    if (announce) changed();
  }
  enabled.addEventListener('change', () => set({ ...settings, enabled: enabled.checked }));
  select.addEventListener('change', () => { if (presets[select.value]) set({ enabled: true, bands: presets[select.value] }); });
  sliders.forEach((slider, i) => slider.addEventListener('input', () => { const bands = [...settings.bands]; bands[i] = Number(slider.value); set({ enabled: true, bands }); }));
  const buttons = panel.querySelectorAll('button');
  buttons[0].addEventListener('click', () => set({ enabled: false, bands: presets.Flat }));
  buttons[1].addEventListener('click', changed);
  const details = panel.querySelector('details');
  details.addEventListener('keydown', event => {
    if (event.target.tagName === 'SUMMARY' && ['Enter', ' '].includes(event.key)) event.stopPropagation();
    if (event.key === 'Escape' && details.open) { event.preventDefault(); event.stopPropagation(); details.open = false; details.querySelector('summary').focus(); }
  });
  globalThis.addEventListener?.('storage', event => {
    if (event.key !== equalizerKey && event.key !== null) return;
    try { set(JSON.parse(event.newValue), { persist: false }); } catch { set({}, { persist: false }); }
  });
  set(settings, { persist: false, announce: false });
  controls.appendChild(panel);
  return { get settings() { return settings; }, set, update(media, message) {
    panel.classList.toggle('hidden', media?.type !== 'audio');
    panel.querySelector('[role="status"]').textContent = message;
    buttons[1].hidden = !settings.enabled;
  } };
}
