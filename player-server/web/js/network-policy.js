// Browser bearer detection is optional. effectiveType describes speed, not cost.
const key = 'player-music-network';
export const networkPolicyMessage = 'Library audio streaming waits for Wi-Fi. Change Network settings to allow mobile streaming.';
export class MusicNetworkRestricted extends Error {}
export function networkPreferences() {
  try {
    const value = JSON.parse(localStorage.getItem(key));
    return { mode: ['wifi','mobile'].includes(value?.mode) ? value.mode : 'auto',
      wifiQuality: typeof value?.wifiQuality === 'string' ? value.wifiQuality : null,
      mobileQuality: typeof value?.mobileQuality === 'string' ? value.mobileQuality : null,
      wifiPrefetch: value?.wifiPrefetch !== false, mobilePrefetch: value?.mobilePrefetch === true,
      streamWifiOnly: value?.streamWifiOnly === true, syncWifiOnly: value?.syncWifiOnly === true };
  } catch { return {mode:'auto',wifiQuality:null,mobileQuality:null,wifiPrefetch:true,mobilePrefetch:false,streamWifiOnly:false,syncWifiOnly:false}; }
}
export function detectedNetwork(navigatorValue = globalThis.navigator) {
  const type = navigatorValue?.connection?.type;
  if (type === 'wifi' || type === 'ethernet') return 'wifi';
  if (type === 'cellular') return 'mobile';
  return 'unknown';
}
export function musicNetwork() {
  const selected = networkPreferences().mode;
  return selected === 'auto' ? detectedNetwork() : selected;
}
export function networkQuality(fallback) {
  const p = networkPreferences();
  return (musicNetwork() === 'wifi' ? p.wifiQuality : p.mobileQuality) || fallback;
}
export function musicPrefetchAllowed() {
  const p = networkPreferences();
  return !musicStreamingBlocked() && (musicNetwork() === 'wifi' ? p.wifiPrefetch : p.mobilePrefetch);
}
export function musicStreamingBlocked() { return networkPreferences().streamWifiOnly && musicNetwork() !== 'wifi'; }
export function assertMusicStreamingAllowed(media) {
  if (media?.type === 'audio' && musicStreamingBlocked()) throw new MusicNetworkRestricted(networkPolicyMessage);
}
export function musicSessionSyncAllowed() { return !networkPreferences().syncWifiOnly || musicNetwork() === 'wifi'; }
export function saveNetworkPreferences(value) {
  localStorage.setItem(key, JSON.stringify({...networkPreferences(),...value}));
  globalThis.document?.dispatchEvent?.(new Event('music-network:changed'));
}
export function initNetworkSettings(host) {
  if (!host || host.querySelector('[data-music-network]')) return;
  const panel = document.createElement('section'); panel.dataset.musicNetwork='';
  panel.innerHTML = '<h4>Music network</h4><p class="text-muted text-sm">Auto uses the network type only when your browser supplies it. Otherwise choose Wi-Fi or Mobile manually (including in Firefox). Unknown networks use Mobile settings. Wi-Fi includes Ethernet; a mobile hotspot may still cost data.</p><div class="appearance-controls"><label>Network <select data-mode aria-label="Music network"><option value="auto">Auto</option><option value="wifi">Wi-Fi (manual)</option><option value="mobile">Mobile (manual)</option></select></label><label><input type="checkbox" data-wifi-prefetch> Allow prefetch on Wi-Fi</label><label><input type="checkbox" data-mobile-prefetch> Allow prefetch on Mobile / unknown</label><label><input type="checkbox" data-stream> Stream library audio on Wi-Fi only</label><label><input type="checkbox" data-sync> Sync saved music session on Wi-Fi only</label></div><p data-status role="status"></p><p class="text-muted text-sm">Quality for each network is in the player’s Stream quality controls. Prefetch also needs Playback cache prefetch enabled. Changes apply to new tracks and transfers; current playback keeps its source. Downloads, live radio, Cast, public shares, progress and podcast sync keep their own policy.</p>';
  host.appendChild(panel);
  function render(message='') {
    const p=networkPreferences(); panel.querySelector('[data-mode]').value=p.mode;
    for (const [selector,value] of [['wifi-prefetch',p.wifiPrefetch],['mobile-prefetch',p.mobilePrefetch],['stream',p.streamWifiOnly],['sync',p.syncWifiOnly]]) panel.querySelector(`[data-${selector}]`).checked=value;
    const network=musicNetwork();
    panel.querySelector('[data-status]').textContent=message || `Using ${network === 'wifi' ? 'Wi-Fi' : network === 'mobile' ? 'Mobile' : 'Mobile settings (network type unavailable)'}.${p.streamWifiOnly && network !== 'wifi' ? ' New library audio streams paused until Wi-Fi.' : ''}${p.syncWifiOnly && network !== 'wifi' ? ' Session sync waits for Wi-Fi; edits stay here.' : ''}`;
  }
  panel.addEventListener('change',()=>{try {saveNetworkPreferences({mode:panel.querySelector('[data-mode]').value,wifiPrefetch:panel.querySelector('[data-wifi-prefetch]').checked,mobilePrefetch:panel.querySelector('[data-mobile-prefetch]').checked,streamWifiOnly:panel.querySelector('[data-stream]').checked,syncWifiOnly:panel.querySelector('[data-sync]').checked});render();}catch {render('Could not save network settings. Check browser storage.');}});
  globalThis.navigator?.connection?.addEventListener?.('change',()=>{document.dispatchEvent(new Event('music-network:changed'));render();});
  window.addEventListener('storage',event=>{if(event.key===key){document.dispatchEvent(new Event('music-network:changed'));render();}});
  render();
}
