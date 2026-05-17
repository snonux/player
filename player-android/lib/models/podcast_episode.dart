import 'json_helpers.dart';

class PodcastEpisode {
  final int id, feedId;
  final int? mediaId, fileSize;
  final String guid, title, description, episodeUrl, fileName;
  final double? durationSeconds;
  final bool isDownloaded, isCompleted;
  final double positionSeconds;
  final DateTime? publishedAt, createdAt;

  const PodcastEpisode({required this.id, required this.feedId, this.mediaId, required this.guid, required this.title, required this.description, this.publishedAt, required this.episodeUrl, this.durationSeconds, this.fileSize, required this.fileName, required this.isDownloaded, this.isCompleted = false, this.positionSeconds = 0, this.createdAt});

  factory PodcastEpisode.fromJson(Map<String, dynamic> json) => PodcastEpisode(id: json['id'] as int? ?? 0, feedId: json['feed_id'] as int? ?? 0, mediaId: json['media_id'] as int?, guid: json['guid'] as String? ?? '', title: json['title'] as String? ?? '', description: json['description'] as String? ?? '', publishedAt: dateTimeFromJson(json['published_at']), episodeUrl: json['episode_url'] as String? ?? '', durationSeconds: (json['duration_seconds'] as num?)?.toDouble(), fileSize: json['file_size'] as int?, fileName: json['file_name'] as String? ?? '', isDownloaded: json['is_downloaded'] as bool? ?? false, isCompleted: json['is_completed'] as bool? ?? false, positionSeconds: (json['position_seconds'] as num?)?.toDouble() ?? 0, createdAt: dateTimeFromJson(json['created_at']));

  Map<String, dynamic> toJson() => {'id': id, 'feed_id': feedId, 'media_id': mediaId, 'guid': guid, 'title': title, 'description': description, 'published_at': dateTimeToJson(publishedAt), 'episode_url': episodeUrl, 'duration_seconds': durationSeconds, 'file_size': fileSize, 'file_name': fileName, 'is_downloaded': isDownloaded, 'is_completed': isCompleted, 'position_seconds': positionSeconds, 'created_at': dateTimeToJson(createdAt)};
}
