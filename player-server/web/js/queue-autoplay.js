// Policy for one running queue. Queue/source ownership remains with its actor.
export class QueueAutoplay {
  enabled = false;
  loading = false;
  status = '';
  revision = 0;

  setEnabled(enabled) {
    this.cancel();
    this.enabled = !!enabled;
    this.status = '';
  }

  reset() { this.setEnabled(false); }

  cancel() { this.revision++; this.loading = false; if (this.status === 'Finding related songs…') this.status = ''; }

  async resolve({ seedId, excluded, fetch, current, online = true }) {
    if (!this.enabled || this.loading || !current()) return null;
    if (!online) { this.status = 'Autoplay needs a connection.'; return []; }
    const operation = ++this.revision;
    const wanted = () => this.enabled && operation === this.revision && current();
    this.loading = true;
    this.status = 'Finding related songs…';
    try {
      const items = await fetch(seedId);
      if (!wanted()) return null;
      const seen = new Set(excluded.map(String));
      const songs = [];
      for (const item of items) {
        const song = item.song;
        if (song?.type !== 'audio' || !Number.isSafeInteger(song.id) || song.id <= 0 || seen.has(String(song.id))) continue;
        seen.add(String(song.id));
        songs.push(song);
        if (songs.length === 12) break;
      }
      this.status = songs.length ? '' : 'No additional related suggestions.';
      return songs;
    } catch {
      if (!wanted()) return null;
      this.status = 'Could not load related songs. Try again when connected.';
      return [];
    } finally {
      if (operation === this.revision) {
        this.loading = false;
        if (this.status === 'Finding related songs…') this.status = '';
      }
    }
  }
}
