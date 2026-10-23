import 'playback_request.dart';

/// Policy for a running queue; its actor retains source and transport ownership.
class MusicAutoplay {
  bool enabled = false, loading = false;
  String status = '';
  int _revision = 0;

  void setEnabled(bool value) {
    cancel();
    enabled = value;
    status = '';
  }

  void reset() => setEnabled(false);

  void cancel() {
    _revision++;
    loading = false;
    if (status == 'Finding related songs…') status = '';
  }

  Future<List<PlaybackRequest>?> resolve({
    required ServerPlaybackRequest seed,
    required Set<String> excluded,
    required Future<List<PlaybackRequest>> Function(ServerPlaybackRequest)
        fetch,
    required bool Function() current,
    bool online = true,
  }) async {
    if (!enabled || loading || !current()) return null;
    if (!online) {
      status = 'Autoplay needs a connection.';
      return [];
    }
    final operation = ++_revision;
    bool wanted() => enabled && operation == _revision && current();
    loading = true;
    status = 'Finding related songs…';
    try {
      final items = await fetch(seed);
      if (!wanted()) return null;
      final seen = Set<String>.of(excluded), songs = <PlaybackRequest>[];
      for (final song in items) {
        if (song is! ServerPlaybackRequest ||
            song.mediaId <= 0 ||
            song.serverOrigin != seed.serverOrigin ||
            song.userId != seed.userId ||
            !seen.add(song.identity)) {
          continue;
        }
        songs.add(song);
        if (songs.length == 12) break;
      }
      status = songs.isEmpty ? 'No additional related suggestions.' : '';
      return songs;
    } catch (_) {
      if (!wanted()) return null;
      status = 'Could not load related songs. Try again when connected.';
      return [];
    } finally {
      if (operation == _revision) {
        loading = false;
        if (status == 'Finding related songs…') status = '';
      }
    }
  }
}
