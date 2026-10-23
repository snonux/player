import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/cast_provider.dart';
import '../services/cast/cast_controller.dart';
import '../services/playback_request.dart';
import '../widgets/cast_controls.dart';

/// Shared by the audio and video player screens: the cast button, the
/// remote controls while the item plays on a cast device, and the hand-back
/// to the phone when casting ends.
///
/// The cast itself belongs to [CastController], not to the screen: closing
/// the screen leaves the TV playing and progress being saved. Starting the
/// cast takes the shared player from the screen, like another item would;
/// the screen shows [CastPlaybackView] instead of its stopped state while
/// [isCastingRequest] is true.
mixin CastScreenMixin<T extends ConsumerStatefulWidget> on ConsumerState<T> {
  StreamSubscription<CastEnded>? _castEnded;
  late final CastController _observedCast;
  List<PlaybackRequest> get castQueue => const [];
  int get castQueueIndex => 0;
  String? get castOccurrenceID => null;
  bool get castRepeatAll => false;
  bool get castRepeatOne => false;
  void onRemoteRequestChanged(PlaybackRequest request) {}

  void _remoteChanged() {
    final active = _observedCast.active;
    final request = active?.request;
    final identity = castableRequest?.identity;
    if (mounted &&
        active?.device.isRenderer == true &&
        request != null &&
        (identity == request.identity ||
            _observedCast.rendererQueue
                .any((entry) => entry.identity == identity))) {
      onRemoteRequestChanged(request);
    }
  }

  /// The request the screen shows, or null before it is known.
  PlaybackRequest? get castableRequest;

  /// Whether the item is a video, which changes what the receiver shows.
  bool get castIsVideo;

  /// Where local playback is now, to continue from on the cast device.
  Duration currentLocalPosition();

  /// Pauses local playback before the cast takes over.
  Future<void> pauseLocalPlayback();

  /// Casting of this screen's item ended. Continue on the phone from
  /// [position] without playing (null: the item finished on the TV).
  void onCastEnded(Duration? position);

  @override
  void initState() {
    super.initState();
    _observedCast = ref.read(castControllerProvider);
    _observedCast.addListener(_remoteChanged);
    _castEnded = ref.read(castControllerProvider).ended.listen((ended) {
      if (!mounted || ended.identity != castableRequest?.identity) return;
      final failure = ended.failure;
      if (failure != null) _showCastMessage(castFailureMessage(failure));
      onCastEnded(ended.position);
    });
  }

  @override
  void dispose() {
    _observedCast.removeListener(_remoteChanged);
    unawaited(_castEnded?.cancel());
    super.dispose();
  }

  /// True while this screen's item plays on a cast device.
  bool get isCastingRequest {
    final request = castableRequest;
    return request != null &&
        ref.watch(castControllerProvider).isCasting(request.identity);
  }

  /// The app bar's cast button; empty for items a cast device cannot reach.
  /// [ready] is false while local playback is not set up yet.
  Widget buildCastButton({required bool ready}) {
    final request = castableRequest;
    if (request == null || !isCastable(request)) return const SizedBox.shrink();
    final controller = ref.watch(castControllerProvider);
    final casting = controller.isCasting(request.identity);
    if (!casting && !ready && !controller.isStarting) {
      return const SizedBox.shrink();
    }
    return CastButton(
      casting: casting,
      busy: controller.isStarting,
      onCast: () => unawaited(_pickAndCast(request)),
      onStop: () => unawaited(stopCasting()),
    );
  }

  Future<void> _pickAndCast(PlaybackRequest request) async {
    final device = await showCastDevicePicker(
        context, ref.read(castDiscoveryProvider),
        includeRenderers: !castIsVideo && request is ServerPlaybackRequest);
    if (device == null || !mounted) return;
    final position = currentLocalPosition();
    try {
      await pauseLocalPlayback();
    } catch (_) {}
    try {
      await ref.read(castControllerProvider).start(
            device: device,
            request: request,
            position: position,
            isVideo: castIsVideo,
            rendererQueue: castQueue,
            rendererIndex: castQueueIndex,
            existingOccurrence: castOccurrenceID,
            rendererRepeatAll: castRepeatAll,
            rendererRepeatOne: castRepeatOne,
          );
    } on CastStartException catch (error) {
      _showCastMessage(error.message);
    }
  }

  /// Ends casting; [onCastEnded] then continues on the phone.
  Future<void> stopCasting() async {
    await ref.read(castControllerProvider).stop();
  }

  void _showCastMessage(String message) {
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(context)
        ?.showSnackBar(SnackBar(content: Text(message)));
  }
}
