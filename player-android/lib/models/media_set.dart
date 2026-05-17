import 'json_helpers.dart';

class MediaSet {
  final int id;
  final String name;
  final String rootPath;
  final String coverThumbnailPath;
  final bool isPodcast;
  final DateTime? createdAt;

  const MediaSet({required this.id, required this.name, required this.rootPath, required this.coverThumbnailPath, required this.isPodcast, this.createdAt});

  factory MediaSet.fromJson(Map<String, dynamic> json) => MediaSet(id: json['id'] as int? ?? 0, name: json['name'] as String? ?? '', rootPath: json['root_path'] as String? ?? '', coverThumbnailPath: json['cover_thumbnail_path'] as String? ?? '', isPodcast: json['is_podcast'] as bool? ?? false, createdAt: dateTimeFromJson(json['created_at']));

  Map<String, dynamic> toJson() => {'id': id, 'name': name, 'root_path': rootPath, 'cover_thumbnail_path': coverThumbnailPath, 'is_podcast': isPodcast, 'created_at': dateTimeToJson(createdAt)};
}
