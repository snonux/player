import 'json_helpers.dart';

class MediaBookmark {
  const MediaBookmark(
      {required this.id,
      required this.positionSeconds,
      required this.label,
      this.createdAt});
  final int id;
  final double positionSeconds;
  final String label;
  final DateTime? createdAt;

  factory MediaBookmark.fromJson(Map<String, dynamic> json) {
    final id = json['id'] as int;
    final seconds = (json['position_seconds'] as num).toDouble();
    if (id <= 0 || !validBookmarkPosition(seconds)) {
      throw const FormatException('Invalid bookmark position.');
    }
    return MediaBookmark(
        id: id,
        positionSeconds: seconds,
        label: json['label'] as String? ?? '',
        createdAt: dateTimeFromJson(json['created_at']));
  }
}

/// Leaves headroom below Duration's signed 64-bit microsecond limit.
bool validBookmarkPosition(double seconds) =>
    seconds.isFinite && seconds >= 0 && seconds <= 9000000000000;
