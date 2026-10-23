import { musicSessionSyncAllowed } from './network-policy.js';
// Portable sessions contain identifiers only. All responses are storage-only;
// applying a session is a separate, explicit action owned by the queue UI.
export class SavedQueueSync {
  constructor({ storage, request, changed = () => {} }) {
    this.storage = storage; this.request = request; this.changed = changed;
    this.epoch = 0; this.key = null; this.state = {}; this.running = null;
  }
  bind(key) {
    if (this.key === key) return;
    this.epoch++; this.key = key; this.running = null;
    try { this.state = JSON.parse(this.storage.getItem(key) || '{}'); }
    catch { this.state = {}; }
    this.message = ''; this.changed();
  }
  persist() {
    try { this.storage.setItem(this.key, JSON.stringify(this.state)); }
    catch { this.message = 'Session could not be saved on this device.'; }
    this.changed();
  }
  capture(snapshot) {
    if (!this.key) return;
    const encoded = JSON.stringify(snapshot);
    if (encoded === this.state.local) return;
    this.state.local = encoded;
    const mutation = { ...snapshot, mutation_id: crypto.randomUUID() };
    this.state.pending = mutation;
    if (this.state.hold) this.state.conflict = true;
    this.persist();
  }
  async sync() {
    if (!this.key || this.running) return this.running;
    if (!musicSessionSyncAllowed()) { this.message = 'Saved music session sync waits for Wi-Fi. Changes stay on this device.'; this.changed(); return; }
    const epoch = this.epoch, key = this.key;
    const current = () => this.epoch === epoch && this.key === key;
    this.running = (async () => {
      await Promise.resolve();
      try {
        if (this.state.revision === undefined) {
          const remote = await this.request('GET');
          if (!current()) return;
          this.state.remote = remote; this.state.revision = remote.revision;
          // A new device never silently replaces a populated remote session.
          if (remote.revision > 0) {
            if (this.state.pending && remote.mutation_id === this.state.pending.mutation_id) { this.state.pending = null; this.state.hold = false; }
            else { this.state.hold = true; this.state.conflict = !!this.state.pending; }
          }
        }
        const pending = this.state.pending;
        if (pending && !this.state.conflict) {
          const result = await this.request('PUT', pending, this.state.revision);
          if (!current()) return;
          this.state.remote = result.queue; this.state.revision = result.queue.revision;
          if (result.conflict) this.state.conflict = true;
          else if (this.state.pending?.mutation_id === pending.mutation_id) this.state.pending = null;
        }
        if (!current()) return;
        this.message = this.state.conflict ? 'Another client saved a session. Choose which session to keep.' : this.state.pending ? 'Session changes pending.' : 'Session saved.';
        this.persist();
      } catch (error) {
        if (!current()) return;
        this.message = error.status === 401 ? 'Sign in to sync your session.' : 'Session sync unavailable. Changes stay on this device.';
        this.changed();
      } finally { if (current()) this.running = null; }
    })();
    return this.running;
  }
  async refresh() {
    if (!musicSessionSyncAllowed()) { this.message = 'Continue session waits for Wi-Fi. Change Music network settings or retry on Wi-Fi.'; this.changed(); return null; }
    const epoch = this.epoch;
    try {
      const remote = await this.request('GET');
      if (epoch !== this.epoch) return null;
      if (this.state.pending && this.state.revision !== undefined && remote.revision !== this.state.revision) this.state.conflict = true;
      this.state.remote = remote;
      if (!this.state.pending) this.state.revision = remote.revision;
      this.persist(); return remote;
    } catch (error) {
      if (epoch === this.epoch) { this.message = 'Can’t load saved session. Check your connection and retry.'; this.changed(); }
      return null;
    }
  }
  replace() {
    if (!this.state.remote || !this.state.local) return;
    this.state.revision = this.state.remote.revision; this.state.conflict = false; this.state.hold = false;
    this.state.pending = { ...JSON.parse(this.state.local), mutation_id: crypto.randomUUID() };
    this.persist(); return this.sync();
  }
  keepOther() {
    this.state.pending = null; this.state.conflict = false; this.state.hold = true;
    this.state.revision = this.state.remote?.revision;
    this.message = 'Other session kept. This queue stays on this device.';
    // Local playback is deliberately untouched. Future edits require an explicit
    // replace instead of quietly taking ownership again.
    this.state.revision = undefined;
    this.persist();
  }
  adopt(remote, snapshot) {
    this.state.remote = remote; this.state.revision = remote.revision;
    this.state.local = JSON.stringify(snapshot); this.state.pending = null; this.state.conflict = false; this.state.hold = false;
    this.message = 'Session continued · paused. Resume when you’re ready.';
    this.persist();
  }
}

export async function savedQueueRequest(method, body, revision) {
  if (!musicSessionSyncAllowed()) throw new Error('Saved music session sync waits for Wi-Fi.');
  const response = await fetch('/api/v1/queue', { method, credentials: 'include',
    headers: { Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json', 'If-Match': `"${revision}"` } : {}) },
    ...(body ? { body: JSON.stringify(body) } : {}) });
  if (response.status === 409) return { conflict: true, queue: await response.json() };
  if (!response.ok) { const error = new Error('Session sync unavailable'); error.status = response.status; throw error; }
  const queue = await response.json();
  return method === 'PUT' ? { conflict: false, queue } : queue;
}
