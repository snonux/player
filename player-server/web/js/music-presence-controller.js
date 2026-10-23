// Per-client transient sharing; no playback or browser dependencies.
export function createMusicPresence({ request, snapshot, render, clientID }) {
  let account = null, sharing = false, open = false, epoch = 0, sequence = 0, busy = false, lastPublished = false, refreshPending = false;
  let rows = [], message = '';
  const publishView = () => render({ sharing, open, rows, message });
  async function remove(owner) {
    if (!owner) return;
    const body = { client_id: clientID, sequence: ++sequence };
    try { await request('DELETE', owner, body); } catch { /* Expiry bounds disconnected cleanup. */ }
  }
  async function refresh() {
    if (!account) return;
    if (busy) { refreshPending = true; return; }
    const owner = account, token = epoch;
    busy = true;
    try {
      const current = snapshot();
      if (sharing && current) {
        await request('PUT', owner, { ...current, client_id: clientID, client: 'web', sequence: ++sequence });
        if (token !== epoch) return;
        lastPublished = true;
      } else if (lastPublished) {
        lastPublished = false;
        await remove(owner);
      }
      if (token !== epoch) return;
      if (open) {
        const result = await request('GET', owner);
        if (token !== epoch) return;
        rows = Array.isArray(result) ? result : [];
      }
      message = '';
    } catch {
      if (token === epoch) { rows = []; message = 'Listeners unavailable. Retry.'; if (lastPublished) { lastPublished = false; void remove(owner); } }
    } finally {
      busy = false;
      if (token === epoch) publishView();
      if (refreshPending) { refreshPending = false; void refresh(); }
    }
  }
  return {
    refresh,
    setAccount(value) {
      if (value === account) return;
      const previous = account;
      epoch++; account = value; sharing = false; lastPublished = false; rows = []; message = ''; publishView();
      void remove(previous);
    },
    setSharing(value) {
      epoch++; sharing = Boolean(value) && Boolean(account); rows = []; message = ''; publishView();
      if (!sharing) { lastPublished = false; void remove(account); }
      void refresh();
    },
    setOpen(value) { open = Boolean(value); rows = []; message = open ? 'Loading listeners…' : ''; publishView(); if (open) void refresh(); },
    clear() { const previous = account; epoch++; account = null; sharing = false; lastPublished = false; rows = []; message = ''; publishView(); void remove(previous); },
  };
}

