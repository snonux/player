import 'json_helpers.dart';

class PodcastFeed {
  final int id, setId, checkIntervalMinutes, consecutiveFailures;
  final String feedUrl, title, description, imageUrl, lastETag;
  final bool autoDownload;
  final DateTime? lastCheckedAt, nextCheckAt, createdAt;

  const PodcastFeed({required this.id, required this.setId, required this.feedUrl, required this.title, required this.description, required this.imageUrl, this.lastCheckedAt, required this.lastETag, required this.checkIntervalMinutes, required this.autoDownload, required this.consecutiveFailures, this.nextCheckAt, this.createdAt});

  factory PodcastFeed.fromJson(Map<String, dynamic> json) => PodcastFeed(id: json['id'] as int? ?? 0, setId: json['set_id'] as int? ?? 0, feedUrl: json['feed_url'] as String? ?? '', title: json['title'] as String? ?? '', description: json['description'] as String? ?? '', imageUrl: json['image_url'] as String? ?? '', lastCheckedAt: dateTimeFromJson(json['last_checked_at']), lastETag: json['last_etag'] as String? ?? '', checkIntervalMinutes: json['check_interval_minutes'] as int? ?? 0, autoDownload: json['auto_download'] as bool? ?? false, consecutiveFailures: json['consecutive_failures'] as int? ?? 0, nextCheckAt: dateTimeFromJson(json['next_check_at']), createdAt: dateTimeFromJson(json['created_at']));

  Map<String, dynamic> toJson() => {'id': id, 'set_id': setId, 'feed_url': feedUrl, 'title': title, 'description': description, 'image_url': imageUrl, 'last_checked_at': dateTimeToJson(lastCheckedAt), 'last_etag': lastETag, 'check_interval_minutes': checkIntervalMinutes, 'auto_download': autoDownload, 'consecutive_failures': consecutiveFailures, 'next_check_at': dateTimeToJson(nextCheckAt), 'created_at': dateTimeToJson(createdAt)};
}
