import 'package:flutter/foundation.dart';
import '../models/music_metadata.dart';

enum PlaybackSourceKind { server, local, publicShare }

typedef ReadPlaybackPosition = Future<double?> Function();
typedef SavePlaybackPosition = Future<void> Function(double seconds);
typedef SaveListeningPosition = Future<void> Function(
    double seconds, String occurrence);
typedef MarkPlaybackFinished = Future<void> Function();

/// A source selected for the shared audio/video controls.
///
/// The variants keep authentication and progress ownership explicit. Local
/// and public-share requests never contain server credentials or API clients.
@immutable
sealed class PlaybackRequest {
  const PlaybackRequest({
    required this.identity,
    required this.title,
    this.sourceName = '',
    this.artist = '',
    this.album = '',
    this.artworkUri,
    this.music,
    required this.sourceUri,
    this.startPosition,
    this.readPosition,
    this.savePosition,
    this.saveListeningPosition,
    this.markFinished,
  });

  final String identity;
  final String title;
  final String sourceName;
  final String artist;
  final String album;
  final Uri? artworkUri;
  final MusicMetadata? music;
  String get subtitle => [artist.isEmpty ? 'Unknown artist' : artist, album]
      .where((s) => s.isNotEmpty)
      .join(' · ');
  String get visualContext => '$sourceName $title ${sourceUri.path}';
  final Uri sourceUri;
  final double? startPosition;
  final ReadPlaybackPosition? readPosition;
  final SavePlaybackPosition? savePosition;
  final SaveListeningPosition? saveListeningPosition;
  final MarkPlaybackFinished? markFinished;

  PlaybackSourceKind get kind;
}

@immutable
final class ServerPlaybackRequest extends PlaybackRequest {
  const ServerPlaybackRequest({
    required this.mediaId,
    required this.serverOrigin,
    required this.userId,
    required super.sourceUri,
    required super.title,
    super.sourceName,
    super.artist,
    super.album,
    super.artworkUri,
    super.music,
    super.startPosition,
    super.readPosition,
    super.savePosition,
    super.saveListeningPosition,
    super.markFinished,
  }) : super(identity: 'server:$serverOrigin:$userId:$mediaId');

  final int mediaId;
  final String serverOrigin;
  final int userId;

  /// The same item and progress callbacks, played from [sourceUri]. Used to
  /// switch between the original stream and the compatibility stream.
  ServerPlaybackRequest withSourceUri(Uri sourceUri) => ServerPlaybackRequest(
        mediaId: mediaId,
        serverOrigin: serverOrigin,
        userId: userId,
        sourceUri: sourceUri,
        title: title,
        sourceName: sourceName,
        artist: artist,
        album: album,
        artworkUri: artworkUri,
        music: music,
        startPosition: startPosition,
        readPosition: readPosition,
        savePosition: savePosition,
        saveListeningPosition: saveListeningPosition,
        markFinished: markFinished,
      );

  @override
  PlaybackSourceKind get kind => PlaybackSourceKind.server;
}

@immutable
final class LocalPlaybackRequest extends PlaybackRequest {
  const LocalPlaybackRequest({
    required this.localMediaId,
    required super.sourceUri,
    required super.title,
    super.sourceName,
    super.artist,
    super.album,
    super.artworkUri,
    super.music,
    super.startPosition,
    super.readPosition,
    super.savePosition,
    super.markFinished,
  }) : super(identity: 'local:$localMediaId');

  final int localMediaId;

  @override
  PlaybackSourceKind get kind => PlaybackSourceKind.local;
}

/// An owned app-private file, with progress kept on the device. Its identity
/// cannot collide with a document-library ID or another server/account.
@immutable
final class OfflinePlaybackRequest extends PlaybackRequest {
  const OfflinePlaybackRequest({
    required this.downloadKey,
    this.serverOrigin,
    this.userId,
    required super.sourceUri,
    required super.title,
    super.sourceName,
    super.artist,
    super.album,
    super.artworkUri,
    super.music,
    super.readPosition,
    super.savePosition,
    super.markFinished,
  }) : super(identity: 'offline:$downloadKey');

  final String downloadKey;

  /// Download ownership for external identity surfaces. Older/test callers
  /// without an owner remain playable locally but external metadata fails shut.
  final String? serverOrigin;
  final int? userId;

  @override
  PlaybackSourceKind get kind => PlaybackSourceKind.local;
}

/// Extracts the share token from a public share media URL
/// (`.../s/{token}/stream` or `.../s/{token}/compat`), also when the server
/// is mounted under a path prefix. Returns 'unknown' for any other URL.
String shareTokenFromUrl(Uri url) {
  final segments = url.pathSegments;
  final marker = segments.length - 3;
  return marker >= 0 && segments[marker] == 's'
      ? segments[marker + 1]
      : 'unknown';
}

@immutable
final class PublicSharePlaybackRequest extends PlaybackRequest {
  const PublicSharePlaybackRequest({
    required this.shareToken,
    required super.sourceUri,
    required super.title,
    super.sourceName,
    super.artist,
    super.album,
    super.artworkUri,
    super.music,
  }) : super(identity: 'public-share:$shareToken');

  final String shareToken;

  @override
  PlaybackSourceKind get kind => PlaybackSourceKind.publicShare;
}

/// An account-owned live station. There is no file progress or finite timeline.
@immutable
final class LiveRadioPlaybackRequest extends PlaybackRequest {
  const LiveRadioPlaybackRequest({
    required this.stationId,
    required this.serverOrigin,
    required this.userId,
    required super.sourceUri,
    required super.title,
    super.artworkUri,
  }) : super(
            identity: 'radio:$serverOrigin:$userId:$stationId',
            sourceName: 'Internet radio',
            artist: 'Live radio');

  final int stationId;
  final String serverOrigin;
  final int userId;
  @override
  PlaybackSourceKind get kind => PlaybackSourceKind.server;
}
