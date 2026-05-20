import 'json_helpers.dart';

class Media {
  final int id, setId, bitrate, fileSizeBytes, width, height, playCount;
  final String relPath, fileName, absPath, type, codec, resolution, thumbnailPath;
  final double duration;
  final bool favorite;
  final List<String> tags;
  final DateTime? deletedAt, createdAt;

  const Media({required this.id, required this.setId, required this.relPath, required this.fileName, required this.absPath, required this.type, required this.duration, required this.codec, required this.resolution, required this.bitrate, required this.fileSizeBytes, required this.width, required this.height, required this.thumbnailPath, required this.playCount, this.favorite = false, this.tags = const [], this.deletedAt, this.createdAt});

  factory Media.fromJson(Map<String, dynamic> json) => Media(id: json['id'] as int? ?? 0, setId: json['set_id'] as int? ?? 0, relPath: json['rel_path'] as String? ?? '', fileName: json['file_name'] as String? ?? '', absPath: json['abs_path'] as String? ?? '', type: json['type'] as String? ?? '', duration: (json['duration'] as num?)?.toDouble() ?? 0, codec: json['codec'] as String? ?? '', resolution: json['resolution'] as String? ?? '', bitrate: json['bitrate'] as int? ?? 0, fileSizeBytes: json['file_size_bytes'] as int? ?? 0, width: json['width'] as int? ?? 0, height: json['height'] as int? ?? 0, thumbnailPath: json['thumbnail_path'] as String? ?? '', playCount: json['play_count'] as int? ?? 0, favorite: json['favorite'] as bool? ?? false, // Safe tag deserialization: keep only elements that are already Strings,
// silently dropping ints, nulls, or other unexpected types. This tolerates
// malformed server responses without throwing a TypeError at runtime.
tags: (json['tags'] as List<dynamic>? ?? []).whereType<String>().toList(), deletedAt: dateTimeFromJson(json['deleted_at']), createdAt: dateTimeFromJson(json['created_at']));

  Map<String, dynamic> toJson() => {'id': id, 'set_id': setId, 'rel_path': relPath, 'file_name': fileName, 'abs_path': absPath, 'type': type, 'duration': duration, 'codec': codec, 'resolution': resolution, 'bitrate': bitrate, 'file_size_bytes': fileSizeBytes, 'width': width, 'height': height, 'thumbnail_path': thumbnailPath, 'play_count': playCount, 'favorite': favorite, 'tags': tags, 'deleted_at': dateTimeToJson(deletedAt), 'created_at': dateTimeToJson(createdAt)};
}
