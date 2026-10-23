import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/services.dart';

import 'playback_request.dart';

/// Publishes the existing handler's identity and controls to the launcher.
/// No source, credentials or recovery state is stored by the widget.
class PlaybackWidgetObserver {
  PlaybackWidgetObserver(this.handler,
      {this.allowMetadata,
      this.channel = const MethodChannel('zone.foo.player_android/widgets')}) {
    _items = handler.mediaItem.listen((_) => _publish());
    _states = handler.playbackState.listen((_) => _publish());
  }

  final AudioHandler handler;
  final bool Function()? allowMetadata;
  final MethodChannel channel;
  late final StreamSubscription<MediaItem?> _items;
  late final StreamSubscription<PlaybackState> _states;
  Map<String, Object?>? _last;

  void refresh() => _publish();

  void _publish() {
    final item = handler.mediaItem.value;
    final state = handler.playbackState.value;
    final failed = state.processingState == AudioProcessingState.error;
    final loading = state.processingState == AudioProcessingState.loading ||
        state.processingState == AudioProcessingState.buffering;
    final active = item != null &&
        (allowMetadata?.call() ?? true) &&
        state.processingState != AudioProcessingState.idle;
    final snapshot = <String, Object?>{
      'title': active ? item.title : '',
      'artist': active ? item.artist ?? '' : '',
      'album': active ? item.album ?? '' : '',
      // Artwork loader produces private file URIs. Never fetch authenticated
      // URLs from a launcher or publish those URLs to another process.
      'artPath': active && item.artUri?.scheme == 'file'
          ? item.artUri!.toFilePath()
          : null,
      'playing': active && state.playing && !failed,
      'status': !active
          ? 'empty'
          : failed
              ? 'error'
              : loading
                  ? 'loading'
                  : 'ready',
      'previous': active &&
          !failed &&
          state.controls.contains(MediaControl.skipToPrevious),
      'next':
          active && !failed && state.controls.contains(MediaControl.skipToNext),
      'toggle': active &&
          !failed &&
          (state.controls.contains(MediaControl.play) ||
              state.controls.contains(MediaControl.pause)),
    };
    if (_last != null &&
        snapshot.entries.every((e) => _last![e.key] == e.value)) {
      return;
    }
    _last = snapshot;
    // Launcher support is optional on other platforms and in widget tests.
    unawaited(channel
        .invokeMethod<void>('update', snapshot)
        .catchError((Object _) {}));
  }

  Future<void> dispose() async {
    await _items.cancel();
    await _states.cancel();
    await channel.invokeMethod<void>('update', <String, Object?>{});
  }
}

/// Private downloaded media keeps its original account boundary even though
/// playback uses local bytes. Device documents and public shares are independent.
bool playbackWidgetMetadataAllowed(PlaybackRequest? request,
    {required String origin,
    required int? userId,
    required bool credentialsEnabled}) {
  if (request is LocalPlaybackRequest ||
      request is PublicSharePlaybackRequest) {
    return true;
  }
  final (String?, int?) owner = switch (request) {
    ServerPlaybackRequest() => (request.serverOrigin, request.userId),
    OfflinePlaybackRequest() => (request.serverOrigin, request.userId),
    _ => (null, null),
  };
  return credentialsEnabled &&
      owner.$1 != null &&
      owner.$1!.isNotEmpty &&
      owner.$2 != null &&
      owner.$2! > 0 &&
      owner.$1 == origin &&
      owner.$2 == userId;
}
