import 'dart:convert';
import 'music_metadata.dart';

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
    this.music,
    this.automationMusicEligible = false,
    this.sourceName = '',
    this.downloadProfile = '',
    this.contentType = '',
    this.positionSeconds = 0,
    this.finished = false,
  });

  final String key;
  final String origin;
  final int userId;
  final int mediaId;
  final String title;
  final String sourceName;
  final String downloadProfile;
  final String contentType;
  String get downloadQualityLabel =>
      offlineDownloadQualityLabel(downloadProfile);
  final MusicMetadata? music;

  /// Membership in the authenticated music catalog when this copy was saved.
  /// Legacy copies remain playable manually but fail closed for automation.
  final bool automationMusicEligible;
  final String type;
  final int sizeBytes;
  final DateTime addedAt;
  final double positionSeconds;
  final bool finished;
}

String offlineDownloadQualityLabel(String profile) => switch (profile) {
      'original' => 'Original',
      'aac128' => 'AAC 128 kbps',
      'aac256' => 'AAC 256 kbps',
      'mp3192' => 'MP3 192 kbps',
      'opus96' => 'Opus 96 kbps',
      _ => 'Existing copy (quality not recorded)',
    };
