import 'json_helpers.dart';
import 'music_metadata.dart';

class MediaPodcastMetadata {
  const MediaPodcastMetadata(
      {this.title = '',
      this.feedTitle = '',
      this.description = '',
      this.imageUrl = '',
      this.feedImageUrl = '',
      this.publishedAt});
  final String title, feedTitle, description, imageUrl, feedImageUrl;
  final DateTime? publishedAt;
  factory MediaPodcastMetadata.fromJson(Map<String, dynamic> json) =>
      MediaPodcastMetadata(
          title: json['title'] as String? ?? '',
          feedTitle: json['feed_title'] as String? ?? '',
          description: json['description'] as String? ?? '',
          imageUrl: json['image_url'] as String? ?? '',
          feedImageUrl: json['feed_image_url'] as String? ?? '',
          publishedAt: dateTimeFromJson(json['published_at']));
  Map<String, dynamic> toJson() => {
        'title': title,
        'feed_title': feedTitle,
        'description': description,
        'image_url': imageUrl,
        'feed_image_url': feedImageUrl,
        'published_at': dateTimeToJson(publishedAt)
      };
}

class Media {
  final int rating;
  final int personalPlayCount;
  final DateTime? lastPlayedAt;
  final int id, setId, bitrate, fileSizeBytes, width, height, playCount;
  final String relPath,
      fileName,
      absPath,
      type,
      codec,
      resolution,
      thumbnailPath;
  final double duration;
  final MusicMetadata? music;
  final MediaPodcastMetadata? podcast;
  String get displayTitle {
    if (type != 'audio') return fileName;
    final episodeTitle = podcast?.title.trim() ?? '';
    if (episodeTitle.isNotEmpty) return episodeTitle;
    final title = music?.title.trim() ?? '';
    if (title.isNotEmpty) return title;
    final stem = fileName.replaceFirst(RegExp(r'\.[^.]+$'), '');
    return stem.isEmpty ? fileName : stem;
  }

  String get musicSubtitle => type == 'audio' && podcast != null
      ? podcast!.feedTitle
      : type == 'audio'
          ? [music?.artist ?? '', music?.album ?? '']
              .where((s) => s.isNotEmpty)
              .join(' · ')
          : '';
  final bool favorite;

  /// True when this device should play the server's compatibility stream
  /// (`/compat`) instead of the original file. The server decides from
  /// container and codec; false for servers that predate the field.
  final bool transcoded;
  final List<String> tags;
  final DateTime? deletedAt, createdAt;

  /// Saved playback position in seconds. Only GET /api/v1/in-progress sends
  /// it; null elsewhere.
  final double? positionSeconds;

  const Media(
      {required this.id,
      required this.setId,
      required this.relPath,
      required this.fileName,
      required this.absPath,
      required this.type,
      required this.duration,
      required this.codec,
      required this.resolution,
      required this.bitrate,
      required this.fileSizeBytes,
      required this.width,
      required this.height,
      required this.thumbnailPath,
      required this.playCount,
      this.music,
      this.podcast,
      this.rating = 0,
      this.personalPlayCount = 0,
      this.lastPlayedAt,
      this.favorite = false,
      this.transcoded = false,
      this.tags = const [],
      this.deletedAt,
      this.createdAt,
      this.positionSeconds});

  factory Media.fromJson(Map<String, dynamic> json) => Media(
      podcast: json['podcast'] is Map<String, dynamic>
          ? MediaPodcastMetadata.fromJson(
              json['podcast'] as Map<String, dynamic>)
          : null,
      music: json['music'] is Map<String, dynamic>
          ? MusicMetadata.fromJson(json['music'] as Map<String, dynamic>)
          : null,
      id: json['id'] as int? ?? 0,
      setId: json['set_id'] as int? ?? 0,
      relPath: json['rel_path'] as String? ?? '',
      fileName: json['file_name'] as String? ?? '',
      absPath: json['abs_path'] as String? ?? '',
      type: json['type'] as String? ?? '',
      duration: (json['duration'] as num?)?.toDouble() ?? 0,
      codec: json['codec'] as String? ?? '',
      resolution: json['resolution'] as String? ?? '',
      bitrate: json['bitrate'] as int? ?? 0,
      fileSizeBytes: json['file_size_bytes'] as int? ?? 0,
      width: json['width'] as int? ?? 0,
      height: json['height'] as int? ?? 0,
      thumbnailPath: json['thumbnail_path'] as String? ?? '',
      playCount: json['play_count'] as int? ?? 0,
      rating: json['rating'] as int? ?? 0,
      personalPlayCount: json['personal_play_count'] as int? ?? 0,
      lastPlayedAt: dateTimeFromJson(json['last_played_at']),
      favorite: json['favorite'] as bool? ?? false,
      transcoded: json['transcoded'] as bool? ??
          false, // Safe tag deserialization: keep only elements that are already Strings,
// silently dropping ints, nulls, or other unexpected types. This tolerates
// malformed server responses without throwing a TypeError at runtime.
      tags: (json['tags'] as List<dynamic>? ?? []).whereType<String>().toList(),
      deletedAt: dateTimeFromJson(json['deleted_at']),
      createdAt: dateTimeFromJson(json['created_at']),
      positionSeconds: (json['position_seconds'] as num?)?.toDouble());

  Map<String, dynamic> toJson() => {
        if (music != null) 'music': music!.toJson(),
        if (podcast != null) 'podcast': podcast!.toJson(),
        'id': id,
        'set_id': setId,
        'rel_path': relPath,
        'file_name': fileName,
        'abs_path': absPath,
        'type': type,
        'duration': duration,
        'codec': codec,
        'resolution': resolution,
        'bitrate': bitrate,
        'file_size_bytes': fileSizeBytes,
        'width': width,
        'height': height,
        'thumbnail_path': thumbnailPath,
        'play_count': playCount,
        'rating': rating,
        'personal_play_count': personalPlayCount,
        if (lastPlayedAt != null)
          'last_played_at': dateTimeToJson(lastPlayedAt),
        'favorite': favorite,
        'transcoded': transcoded,
        'tags': tags,
        'deleted_at': dateTimeToJson(deletedAt),
        'created_at': dateTimeToJson(createdAt),
        if (positionSeconds != null) 'position_seconds': positionSeconds
      };
}
