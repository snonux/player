import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/playback_cache_provider.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/playback_cache.dart';

/// Playback dependencies for screens whose tests do not start media.
///
/// Keep the real handler and settings widgets mounted, while excluding native
/// playback/storage plugins. Playback and cache tests use their own fixtures.
List<Override> idlePlaybackOverrides() => [
      audioHandlerProvider.overrideWith((ref) {
        final player = _IdlePlayer();
        ref.onDispose(() => unawaited(player.dispose()));
        return PlayerAudioHandler(player);
      }),
      playbackCacheProvider.overrideWith((ref) => _EmptyCache()),
    ];

class _IdlePlayer extends AudioPlayer {
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<int?> get currentIndexStream => const Stream.empty();
  @override
  Stream<SequenceState?> get sequenceStateStream => const Stream.empty();
  @override
  Future<void> stop() async {}
}

class _EmptyCache extends PlaybackCache {
  @override
  Future<void> initialize() async {}
}
