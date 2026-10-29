import 'podcast_repository.dart';

/// Optional authenticated catalogue fallback. Personal registers in these
/// responses are deliberately ignored; synchronization owns their clocks.
class PodcastServerMetadata {
  PodcastServerMetadata(
      {required this.repository,
      required this.scope,
      required this.current,
      required this.fetchPage});
  final PodcastRepository repository;
  final String scope;
  final bool Function() current;
  final Future<List<Map<String, dynamic>>> Function(
      String feed, int offset, int limit) fetchPage;

  Future<void> refresh(String feed, {void Function()? onPage}) async {
    const limit = 200;
    var offset = 0;
    while (current()) {
      final rows = await fetchPage(feed, offset, limit);
      if (!current()) throw StateError('Account changed');
      await repository.importServerEpisodes(scope, feed, rows,
          current: current);
      if (!current()) throw StateError('Account changed');
      onPage?.call();
      if (rows.length < limit) return;
      offset += rows.length;
    }
    throw StateError('Account changed');
  }
}
