import 'json_helpers.dart';

/// Independent filesystem root that owns sets (admin libraries API).
class MusicLibrary {
  final int id;
  final String name;
  final String rootPath;
  final DateTime? createdAt;
  final MusicLibraryStats? stats;

  const MusicLibrary({
    required this.id,
    required this.name,
    required this.rootPath,
    this.createdAt,
    this.stats,
  });

  factory MusicLibrary.fromJson(Map<String, dynamic> json) => MusicLibrary(
        id: json['id'] as int? ?? 0,
        name: json['name'] as String? ?? '',
        rootPath: json['root_path'] as String? ?? '',
        createdAt: dateTimeFromJson(json['created_at']),
        stats: json['stats'] is Map<String, dynamic>
            ? MusicLibraryStats.fromJson(json['stats'] as Map<String, dynamic>)
            : null,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'root_path': rootPath,
        'created_at': dateTimeToJson(createdAt),
        if (stats != null) 'stats': stats!.toJson(),
      };
}

/// Aggregates media under one library for admin display.
class MusicLibraryStats {
  final int setCount;
  final int songCount;
  final int totalBytes;
  final double totalDuration;

  const MusicLibraryStats({
    this.setCount = 0,
    this.songCount = 0,
    this.totalBytes = 0,
    this.totalDuration = 0,
  });

  factory MusicLibraryStats.fromJson(Map<String, dynamic> json) =>
      MusicLibraryStats(
        setCount: json['set_count'] as int? ?? 0,
        songCount: json['song_count'] as int? ?? 0,
        totalBytes: (json['total_bytes'] as num?)?.toInt() ?? 0,
        totalDuration: (json['total_duration'] as num?)?.toDouble() ?? 0,
      );

  Map<String, dynamic> toJson() => {
        'set_count': setCount,
        'song_count': songCount,
        'total_bytes': totalBytes,
        'total_duration': totalDuration,
      };
}
