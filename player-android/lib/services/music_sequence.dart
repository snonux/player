import '../models/media.dart';
import '../api/player_api_client.dart';
import '../models/media_set.dart';
import 'playback_request.dart';

/// Use source context, never embedded titles, to distinguish spoken audio.
bool isMusicTrack(Media media, Iterable<MediaSet> sets) {
  final matching = sets.where((set) => set.id == media.setId);
  final set = matching.isEmpty ? null : matching.first;
  return media.type == 'audio' &&
      set?.isPodcast != true &&
      !RegExp(r'\.m4b$', caseSensitive: false).hasMatch(media.fileName) &&
      !RegExp(r'(^|[^a-z])audio[\s_-]*books?([^a-z]|$)', caseSensitive: false)
          .hasMatch('${media.fileName} ${media.relPath} ${set?.name ?? ''}');
}

/// Current handler-owned sequence state, observed by the visible player only.
class MusicSequenceState {
  const MusicSequenceState(
      {required this.request,
      required this.index,
      required this.length,
      required this.sourceGeneration,
      this.loading = false,
      this.error});
  final PlaybackRequest request;
  final int index, length, sourceGeneration;
  final bool loading;
  final Object? error;
  bool get hasNext => index + 1 < length;
}

/// Traverses the same folders-first order as web Sets playback. Reads only
/// the source set through the account API; errors keep their permission meaning.
Future<List<Media>> browseMusicSet(PlayerApiClient client, int setId) async {
  final visited = <String>{};
  Future<List<Media>> visit(String path) async {
    if (!visited.add(path)) return [];
    final data = await client.browseSet(setId, parent: path);
    final canonical = data['current_path'] as String? ?? path;
    final result = <Media>[];
    for (final folder
        in (data['folders'] as List? ?? []).whereType<Map<String, dynamic>>()) {
      final name = folder['name'] as String? ?? '';
      if (name.isEmpty) continue;
      result.addAll(await visit(canonical.isEmpty ? name : '$canonical/$name'));
    }
    result.addAll((data['media'] as List? ?? [])
        .whereType<Map<String, dynamic>>()
        .map(Media.fromJson));
    return result;
  }

  return visit('');
}
