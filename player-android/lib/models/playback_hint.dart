import 'json_helpers.dart';

class PlaybackHint {
  final int mediaId;
  final double positionSeconds;
  final bool finished;
  final DateTime? updatedAt;

  const PlaybackHint({required this.mediaId, required this.positionSeconds, required this.finished, this.updatedAt});

  factory PlaybackHint.fromJson(Map<String, dynamic> json) => PlaybackHint(mediaId: json['media_id'] as int? ?? 0, positionSeconds: (json['position_seconds'] as num?)?.toDouble() ?? 0, finished: json['finished'] as bool? ?? false, updatedAt: dateTimeFromJson(json['updated_at']));

  Map<String, dynamic> toJson() => {'media_id': mediaId, 'position_seconds': positionSeconds, 'finished': finished, 'updated_at': dateTimeToJson(updatedAt)};
}
