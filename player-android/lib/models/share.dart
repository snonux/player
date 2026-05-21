import 'json_helpers.dart';

/// Represents a share link created by the authenticated user.
///
/// The [fileName] and [mediaType] fields are only populated by the
/// `GET /api/v1/shares` (listMyShares) endpoint; per-media share endpoints
/// omit them.  They are nullable so the model can be used for both shapes.
class Share {
  final String token;
  final int mediaId;
  final int createdBy;
  final DateTime? createdAt;
  final DateTime? expiresAt;
  final int? maxUses;
  final int usedCount;

  /// Human-readable filename returned by [listMyShares]; null when not present.
  final String? fileName;

  /// Media type (e.g. "video", "audio") returned by [listMyShares]; null when
  /// not present.
  final String? mediaType;

  const Share({
    required this.token,
    required this.mediaId,
    required this.createdBy,
    this.createdAt,
    this.expiresAt,
    this.maxUses,
    required this.usedCount,
    this.fileName,
    this.mediaType,
  });

  factory Share.fromJson(Map<String, dynamic> json) => Share(
        token: json['token'] as String? ?? '',
        mediaId: json['media_id'] as int? ?? 0,
        createdBy: json['created_by'] as int? ?? 0,
        createdAt: dateTimeFromJson(json['created_at']),
        expiresAt: dateTimeFromJson(json['expires_at']),
        maxUses: json['max_uses'] as int?,
        usedCount: json['used_count'] as int? ?? 0,
        fileName: json['file_name'] as String?,
        mediaType: json['media_type'] as String?,
      );

  Map<String, dynamic> toJson() => {
        'token': token,
        'media_id': mediaId,
        'created_by': createdBy,
        'created_at': dateTimeToJson(createdAt),
        'expires_at': dateTimeToJson(expiresAt),
        'max_uses': maxUses,
        'used_count': usedCount,
        if (fileName != null) 'file_name': fileName,
        if (mediaType != null) 'media_type': mediaType,
      };
}
