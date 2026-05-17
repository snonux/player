import 'json_helpers.dart';

class User {
  final int id;
  final String username;
  final bool isAdmin;
  final DateTime? createdAt;

  const User({required this.id, required this.username, required this.isAdmin, this.createdAt});

  factory User.fromJson(Map<String, dynamic> json) => User(id: json['id'] as int? ?? 0, username: json['username'] as String? ?? '', isAdmin: json['is_admin'] as bool? ?? false, createdAt: dateTimeFromJson(json['created_at']));

  Map<String, dynamic> toJson() => {'id': id, 'username': username, 'is_admin': isAdmin, 'created_at': dateTimeToJson(createdAt)};
}
