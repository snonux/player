// Coordinates existing storage domains. It never owns a player or a queue UI.
export const staleSyncMS = 48 * 60 * 60 * 1000;
export function syncErrorKind(error) {
  if (error?.status === 401) return 'auth';
  if (error?.kind) return error.kind;
  if ([404, 405, 501].includes(error?.status)) return 'unsupported';
  return error instanceof TypeError || globalThis.navigator?.onLine === false ? 'offline' : 'server';
}
export function globalSyncPresentation(state, now = Date.now()) {
  if (!state.account) return { text: '', action: '', hidden: true };
  const stale = Number.isFinite(state.lastFullSuccessMs) && now - state.lastFullSuccessMs >= staleSyncMS;
  const parts = [];
  if (state.active) parts.push('Syncing…');
  else if (state.errorKind) parts.push({ auth: 'Sign in again to sync', rejected: 'Some changes could not sync',
    unsupported: 'Some sync features need a newer server', policy: 'Sync waits for Wi-Fi',
    conflict: 'Choose a saved session in Now Playing' }[state.errorKind] || 'Sync unavailable');
  if (state.lastFullSuccessMs == null) parts.push('Not synced yet');
  else if (stale) parts.push(`Last synced ${Math.floor((now - state.lastFullSuccessMs) / 86400000)} days ago`);
  else if (!state.active && !state.errorKind) parts.push('Synced');
  if (state.pending) parts.push(`${state.pending} pending`);
  if (state.podcastPending) parts.push('Podcast changes stay in this tab until synced; keep it open');
  return { hidden: false, text: parts.join(' · '),
    action: state.active ? 'Syncing…' : state.errorKind === 'auth' ? 'Sign in'
      : state.errorKind || stale ? 'Retry' : 'Sync now' };
}

export class GlobalSyncCoordinator {
  constructor({ storage, domains, now = Date.now, changed = () => {} }) {
    this.storage = storage; this.domains = domains; this.now = now; this.changed = changed;
    this.generation = 0; this.state = { account: null }; this.running = null;
  }
  bind(account) {
    if (account === this.state.account) return;
    this.generation++; this.running = null;
    this.key = account ? `player-global-sync-v1:${account}` : null;
    let stored = {};
    try { stored = JSON.parse(this.storage.getItem(this.key) || '{}'); } catch {}
    this.state = { account, active: false, cursor: stored.cursor || 0, epoch: stored.epoch || '',
      lastFullSuccessMs: Number.isFinite(stored.lastFullSuccessMs) ? stored.lastFullSuccessMs : null,
      errorKind: stored.errorKind || null };
    this.refresh();
  }
  refresh(error) {
    if (!this.state.account) { this.changed(this.state); return; }
    const pending = this.domains.pending();
    this.state.pending = pending.ids.length; this.state.podcastPending = pending.podcasts;
    if (error && !this.state.active) {
      this.state.errorKind = syncErrorKind(error);
      try { this.persist(); } catch {}
    }
    this.changed(this.state);
  }
  persist() {
    const { cursor, epoch, lastFullSuccessMs, errorKind } = this.state;
    this.storage.setItem(this.key, JSON.stringify({ cursor, epoch, lastFullSuccessMs, errorKind }));
  }
  syncNow({explicit=true}={}) {
    if (!this.state.account) return Promise.resolve();
    if (this.running) return this.running;
    const generation = this.generation, current = () => generation === this.generation;
    const starting = new Set(this.domains.pending().ids);
    this.state.active = true; this.refresh();
    const run = this.run(current, starting, explicit).finally(() => {
      if (current()) { this.running = null; this.state.active = false; this.refresh(); }
    });
    this.running = run;
    return run;
  }
  async run(current, starting, explicit) {
    let failure = null;
    const failed = error => { const kind = syncErrorKind(error); if (!failure || kind === 'auth') failure = kind; };
    for (const step of [
      () => this.domains.podcasts(current, async page => {
        if (!current()) return;
        this.state.cursor = page.next_since; this.state.epoch = page.epoch;
        this.persist();
      }, { epoch: this.state.epoch, since: this.state.cursor }),
      () => this.domains.progress(),
      () => this.domains.queue(),
      ...(this.domains.music ? [() => this.domains.music(explicit)] : []),
    ]) {
      if (!current()) return;
      try {
        const result = await step();
        for (const id of result?.excluded || []) starting.delete(id);
      } catch (error) { failed(error); }
    }
    if (!current()) return;
    if (this.domains.pending().ids.some(id => starting.has(id))) failure ||= 'server';
    const previous = this.state.lastFullSuccessMs;
    if (!failure) this.state.lastFullSuccessMs = this.now();
    this.state.errorKind = failure;
    try { this.persist(); } catch {
      this.state.lastFullSuccessMs = previous; this.state.errorKind = 'server';
    }
  }
}
