import 'json_helpers.dart';

class Share {
  final String token;
  final int mediaId;
  final int createdBy;
  final DateTime? createdAt;
  final DateTime? expiresAt;
  final int? maxUses;
  final int usedCount;

  const Share({required this.token, required this.mediaId, required this.createdBy, this.createdAt, this.expiresAt, this.maxUses, required this.usedCount});

  factory Share.fromJson(Map<String, dynamic> json) => Share(token: json['token'] as String? ?? '', mediaId: json['media_id'] as int? ?? 0, createdBy: json['created_by'] as int? ?? 0, createdAt: dateTimeFromJson(json['created_at']), expiresAt: dateTimeFromJson(json['expires_at']), maxUses: json['max_uses'] as int?, usedCount: json['used_count'] as int? ?? 0);

  Map<String, dynamic> toJson() => {'token': token, 'media_id': mediaId, 'created_by': createdBy, 'created_at': dateTimeToJson(createdAt), 'expires_at': dateTimeToJson(expiresAt), 'max_uses': maxUses, 'used_count': usedCount};
}
