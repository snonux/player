import 'dart:async';
import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/cast_provider.dart';
import '../services/cast/cast_session.dart';
import '../providers/presentation_provider.dart';

/// Window behavior only: never changes background audio or acquires a CPU lock.
class PlaybackScreenAwake extends ConsumerStatefulWidget {
  const PlaybackScreenAwake({super.key, required this.child});
  final Widget child;
  @override
  ConsumerState<PlaybackScreenAwake> createState() =>
      _PlaybackScreenAwakeState();
}

class _PlaybackScreenAwakeState extends ConsumerState<PlaybackScreenAwake>
    with WidgetsBindingObserver {
  static const _channel = MethodChannel('zone.foo.player_android/screen_awake');
  StreamSubscription<PlaybackState>? _subscription;
  bool _playing = false;
  bool _remotePlaying = false;
  bool _enabled = false;
  bool _foreground = true;
  bool? _sent;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _foreground = WidgetsBinding.instance.lifecycleState == null ||
        WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed;
    final handler = ref.read(audioHandlerProvider);
    void update(PlaybackState state) {
      _playing =
          state.playing && state.processingState == AudioProcessingState.ready;
      _sync();
    }

    update(handler.playbackState.value);
    _subscription = handler.playbackState.listen(update);
  }

  void _sync() {
    final awake = (_playing || _remotePlaying) && _enabled && _foreground;
    if (_sent == awake) return;
    _sent = awake;
    unawaited(_channel
        .invokeMethod<void>('setEnabled', awake)
        .catchError((Object error) {
      // Unsupported hosts (including widget tests) have no Android window.
      _sent = null;
      debugPrint('Screen awake unavailable: $error');
    }));
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    _sync();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _subscription?.cancel();
    unawaited(_channel
        .invokeMethod<void>('setEnabled', false)
        .catchError((Object _) {}));
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    _remotePlaying =
        ref.watch(castControllerProvider).active?.status.playerState ==
            CastPlayerState.playing;
    _enabled =
        ref.watch(presentationProvider).valueOrNull?.keepScreenOn ?? false;
    _sync();
    return widget.child;
  }
}
