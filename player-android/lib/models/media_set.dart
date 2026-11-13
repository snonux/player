import 'json_helpers.dart';

class MediaSet {
  final int id;
  final String name;
  final String rootPath;
  final String coverThumbnailPath;
  final DateTime? createdAt;

  const MediaSet(
      {required this.id,
      required this.name,
      required this.rootPath,
      required this.coverThumbnailPath,
      this.createdAt});

  factory MediaSet.fromJson(Map<String, dynamic> json) => MediaSet(
      id: json['id'] as int? ?? 0,
      name: json['name'] as String? ?? '',
      rootPath: json['root_path'] as String? ?? '',
      coverThumbnailPath: json['cover_thumbnail_path'] as String? ?? '',
      createdAt: dateTimeFromJson(json['created_at']));

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'root_path': rootPath,
        'cover_thumbnail_path': coverThumbnailPath,
        'created_at': dateTimeToJson(createdAt)
      };
}
