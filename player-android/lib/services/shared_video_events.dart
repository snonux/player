import 'package:flutter/widgets.dart';
import 'package:video_player_platform_interface/video_player_platform_interface.dart';

/// Shares each native event stream with the controller and progress reporter.
/// Some Android plugin versions expose a single-subscription stream. Listening
/// through this adapter preserves one native subscription and all plugin calls.
class SharedVideoEvents extends VideoPlayerPlatform {
  SharedVideoEvents(this.delegate);

  final VideoPlayerPlatform delegate;
  final _events = <int, Stream<VideoEvent>>{};
  final _positions = <int, Duration>{};

  Duration? latestPosition(int? playerId) => _positions[playerId];

  static void install() {
    final current = VideoPlayerPlatform.instance;
    if (current is! SharedVideoEvents) {
      VideoPlayerPlatform.instance = SharedVideoEvents(current);
    }
  }

  @override
  Future<void> init() {
    _events.clear();
    _positions.clear();
    return delegate.init();
  }

  @override
  Future<void> dispose(int playerId) async {
    await delegate.dispose(playerId);
    _events.remove(playerId);
    _positions.remove(playerId);
  }

  @override
  Stream<VideoEvent> videoEventsFor(int playerId) =>
      _events.putIfAbsent(playerId, () {
        final stream = delegate.videoEventsFor(playerId);
        return stream.isBroadcast
            ? stream
            : stream.asBroadcastStream(onCancel: (subscription) {
                subscription.cancel();
              });
      });

  @override
  Future<int?> create(DataSource dataSource) =>
      delegate.createWithOptions(VideoCreationOptions(
          dataSource: dataSource, viewType: VideoViewType.textureView));
  @override
  Future<int?> createWithOptions(VideoCreationOptions options) =>
      delegate.createWithOptions(options);
  @override
  Future<void> setLooping(int playerId, bool looping) =>
      delegate.setLooping(playerId, looping);
  @override
  Future<void> play(int playerId) => delegate.play(playerId);
  @override
  Future<void> pause(int playerId) => delegate.pause(playerId);
  @override
  Future<void> setVolume(int playerId, double volume) =>
      delegate.setVolume(playerId, volume);
  @override
  Future<void> seekTo(int playerId, Duration position) async {
    final events = _events[playerId];
    await delegate.seekTo(playerId, position);
    if (events != null && identical(_events[playerId], events)) {
      _positions[playerId] = position;
    }
  }

  @override
  Future<void> setPlaybackSpeed(int playerId, double speed) =>
      delegate.setPlaybackSpeed(playerId, speed);
  @override
  Future<Duration> getPosition(int playerId) async {
    final events = _events[playerId];
    final position = await delegate.getPosition(playerId);
    if (events != null && identical(_events[playerId], events)) {
      _positions[playerId] = position;
    }
    return position;
  }

  @override
  Widget buildView(int playerId) =>
      delegate.buildViewWithOptions(VideoViewOptions(playerId: playerId));
  @override
  Widget buildViewWithOptions(VideoViewOptions options) =>
      delegate.buildViewWithOptions(options);
  @override
  Future<void> setMixWithOthers(bool mixWithOthers) =>
      delegate.setMixWithOthers(mixWithOthers);
  @override
  Future<void> setAllowBackgroundPlayback(bool allowBackgroundPlayback) =>
      delegate.setAllowBackgroundPlayback(allowBackgroundPlayback);
  @override
  Future<void> setWebOptions(int playerId, VideoPlayerWebOptions options) =>
      delegate.setWebOptions(playerId, options);
  @override
  Future<List<VideoAudioTrack>> getAudioTracks(int playerId) =>
      delegate.getAudioTracks(playerId);
  @override
  Future<void> selectAudioTrack(int playerId, String trackId) =>
      delegate.selectAudioTrack(playerId, trackId);
  @override
  bool isAudioTrackSupportAvailable() =>
      delegate.isAudioTrackSupportAvailable();
  @override
  Future<List<VideoTrack>> getVideoTracks(int playerId) =>
      delegate.getVideoTracks(playerId);
  @override
  Future<void> selectVideoTrack(int playerId, VideoTrack? track) =>
      delegate.selectVideoTrack(playerId, track);
  @override
  bool isVideoTrackSupportAvailable() =>
      delegate.isVideoTrackSupportAvailable();
}
