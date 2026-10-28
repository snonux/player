import 'dart:convert';

import 'json_helpers.dart';

/// Ambiguous move/retag match left for admin recovery.
class MusicIdentityConflict {
  final int id;
  final int libraryId;
  final int missingMediaId;
  final List<int> candidateMediaIds;
  final String missingPath;
  final List<String> candidatePaths;
  final String reason;
  final DateTime? createdAt;
  final DateTime? resolvedAt;
  final String resolution;

  const MusicIdentityConflict({
    required this.id,
    required this.libraryId,
    required this.missingMediaId,
    this.candidateMediaIds = const [],
    this.missingPath = '',
    this.candidatePaths = const [],
    this.reason = '',
    this.createdAt,
    this.resolvedAt,
    this.resolution = '',
  });

  factory MusicIdentityConflict.fromJson(Map<String, dynamic> json) {
    final rawIds = json['candidate_media_ids'];
    return MusicIdentityConflict(
      id: json['id'] as int? ?? 0,
      libraryId: json['library_id'] as int? ?? 0,
      missingMediaId: json['missing_media_id'] as int? ?? 0,
      candidateMediaIds: _parseCandidateIds(rawIds),
      missingPath: json['missing_path'] as String? ?? '',
      candidatePaths: (json['candidate_paths'] as List?)
              ?.map((e) => e?.toString() ?? '')
              .where((s) => s.isNotEmpty)
              .toList() ??
          const [],
      reason: json['reason'] as String? ?? '',
      createdAt: dateTimeFromJson(json['created_at']),
      resolvedAt: dateTimeFromJson(json['resolved_at']),
      resolution: json['resolution'] as String? ?? '',
    );
  }

  /// Server stores candidate ids as a JSON array string; tolerate a real list.
  static List<int> _parseCandidateIds(Object? raw) {
    if (raw is List) {
      return raw
          .map((e) => e is num ? e.toInt() : int.tryParse('$e'))
          .whereType<int>()
          .toList();
    }
    if (raw is! String || raw.isEmpty) return const [];
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! List) return const [];
      return decoded
          .map((e) => e is num ? e.toInt() : int.tryParse('$e'))
          .whereType<int>()
          .toList();
    } on FormatException {
      return const [];
    }
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'library_id': libraryId,
        'missing_media_id': missingMediaId,
        'candidate_media_ids': jsonEncode(candidateMediaIds),
        'missing_path': missingPath,
        'candidate_paths': candidatePaths,
        'reason': reason,
        'created_at': dateTimeToJson(createdAt),
        'resolved_at': dateTimeToJson(resolvedAt),
        'resolution': resolution,
      };
}
