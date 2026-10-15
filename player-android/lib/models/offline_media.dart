import 'dart:convert';

import 'package:crypto/crypto.dart';

/// Download identity includes both the origin (including port) and account.
String offlineMediaKey(String origin, int userId, int mediaId) => sha256
    .convert(utf8.encode(jsonEncode([origin, userId, mediaId])))
    .toString();

class OfflineMedia {
  const OfflineMedia({
    required this.key,
    required this.origin,
    required this.userId,
    required this.mediaId,
    required this.title,
    required this.type,
    required this.sizeBytes,
    required this.addedAt,
    this.positionSeconds = 0,
    this.finished = false,
  });

  final String key;
  final String origin;
  final int userId;
  final int mediaId;
  final String title;
  final String type;
  final int sizeBytes;
  final DateTime addedAt;
  final double positionSeconds;
  final bool finished;
}
