import 'package:flutter/foundation.dart';

enum PlaybackSourceKind { server, local, publicShare }

typedef ReadPlaybackPosition = Future<double?> Function();
typedef SavePlaybackPosition = Future<void> Function(double seconds);
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
    required this.sourceUri,
    this.startPosition,
    this.readPosition,
    this.savePosition,
    this.markFinished,
  });

  final String identity;
  final String title;
  final Uri sourceUri;
  final double? startPosition;
  final ReadPlaybackPosition? readPosition;
  final SavePlaybackPosition? savePosition;
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
    super.startPosition,
    super.readPosition,
    super.savePosition,
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
        startPosition: startPosition,
        readPosition: readPosition,
        savePosition: savePosition,
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
    super.startPosition,
    super.readPosition,
    super.savePosition,
    super.markFinished,
  }) : super(identity: 'local:$localMediaId');

  final int localMediaId;

  @override
  PlaybackSourceKind get kind => PlaybackSourceKind.local;
}

@immutable
final class PublicSharePlaybackRequest extends PlaybackRequest {
  const PublicSharePlaybackRequest({
    required this.shareToken,
    required super.sourceUri,
    required super.title,
  }) : super(identity: 'public-share:$shareToken');

  final String shareToken;

  @override
  PlaybackSourceKind get kind => PlaybackSourceKind.publicShare;
}
