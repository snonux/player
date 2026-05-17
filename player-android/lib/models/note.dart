import 'json_helpers.dart';

class Note {
  final int id;
  final int mediaId;
  final int userId;
  final String content;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  const Note({required this.id, required this.mediaId, required this.userId, required this.content, this.createdAt, this.updatedAt});

  factory Note.fromJson(Map<String, dynamic> json) => Note(id: json['id'] as int? ?? 0, mediaId: json['media_id'] as int? ?? 0, userId: json['user_id'] as int? ?? 0, content: json['content'] as String? ?? '', createdAt: dateTimeFromJson(json['created_at']), updatedAt: dateTimeFromJson(json['updated_at']));

  Map<String, dynamic> toJson() => {'id': id, 'media_id': mediaId, 'user_id': userId, 'content': content, 'created_at': dateTimeToJson(createdAt), 'updated_at': dateTimeToJson(updatedAt)};
}
