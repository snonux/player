// The signed-in account that sync, saved sessions and automatic music
// belong to. Other tabs of this origin follow an account switch, so a tab
// never keeps syncing for an account that was signed out elsewhere.
let account = null;
const channel = globalThis.window?.BroadcastChannel ? new window.BroadcastChannel('player-sync-account') : null;
if (channel) channel.onmessage = event => setSyncAccount(event.data?.account, { broadcast: false });

export function setSyncAccount(id, { broadcast = true } = {}) {
  const next = Number.isSafeInteger(id) && id > 0 ? id : null;
  if (next === account) return;
  account = next;
  if (broadcast) channel?.postMessage({ account });
  globalThis.document?.dispatchEvent(new Event('sync:account-changed'));
}

export function syncAccount() {
  return account;
}
