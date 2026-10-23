import 'package:audio_service/audio_service.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/playback_widget_observer.dart';
import 'package:player_android/services/playback_request.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('test/playback_widgets');
  final snapshots = <Map<Object?, Object?>>[];
  late BaseAudioHandler handler;
  late PlaybackWidgetObserver observer;
  var allowed = true;

  Future<void> settle() async => Future<void>.delayed(Duration.zero);

  setUp(() {
    snapshots.clear();
    allowed = true;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      snapshots.add(Map<Object?, Object?>.from(call.arguments as Map));
      return null;
    });
    handler = BaseAudioHandler();
    observer = PlaybackWidgetObserver(handler,
        channel: channel, allowMetadata: () => allowed);
  });
  tearDown(() async {
    await observer.dispose();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
  });

  test('publishes live identity and advertised transport, clears art on change',
      () async {
    handler.mediaItem.add(MediaItem(
        id: 'a',
        title: 'A',
        artist: 'Artist',
        album: 'Album',
        artUri: Uri.file('/cache/cover.art')));
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.ready,
        playing: true,
        controls: [MediaControl.pause, MediaControl.skipToNext]));
    await settle();
    expect(snapshots.last, containsPair('title', 'A'));
    expect(snapshots.last, containsPair('artPath', '/cache/cover.art'));
    expect(snapshots.last, containsPair('next', true));
    expect(snapshots.last, containsPair('previous', false));
    handler.mediaItem.add(const MediaItem(id: 'b', title: 'B'));
    await settle();
    expect(snapshots.last['artPath'], isNull);
    expect(snapshots.last['album'], '');
    expect(snapshots.last['title'], 'B');
  });

  test(
      'auth invalidation hides metadata without requiring another playback event',
      () async {
    handler.mediaItem.add(const MediaItem(id: 'private', title: 'Private'));
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.ready,
        controls: [MediaControl.play, MediaControl.skipToNext]));
    await settle();
    allowed = false;
    observer.refresh();
    await settle();
    expect(snapshots.last['title'], '');
    expect(snapshots.last['status'], 'empty');
    expect(snapshots.last['next'], false);
    expect(snapshots.last['toggle'], false);
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.ready,
        playing: true,
        controls: [MediaControl.pause]));
    await settle();
    expect(snapshots.last['title'], '');
  });

  test(
      'loading/error/clear states and protected artwork never leak network URI',
      () async {
    handler.mediaItem.add(MediaItem(
        id: 'a',
        title: 'A',
        artUri: Uri.parse('https://private.invalid/cover?token=secret')));
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.loading,
        controls: [MediaControl.play]));
    await settle();
    expect(snapshots.last['status'], 'loading');
    expect(snapshots.last['artPath'], isNull);
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.error,
        controls: [MediaControl.play, MediaControl.skipToNext]));
    await settle();
    expect(snapshots.last['status'], 'error');
    expect(snapshots.last['toggle'], false);
    expect(snapshots.last['next'], false);
    handler.mediaItem.add(null);
    await settle();
    expect(snapshots.last['title'], '');
    expect(snapshots.last['status'], 'empty');
  });
  test(
      'offline owner A hides on account B, lock and logout refresh; local stays available',
      () async {
    await observer.dispose();
    PlaybackRequest request = OfflinePlaybackRequest(
        downloadKey: 'A',
        serverOrigin: 'https://a.invalid',
        userId: 1,
        sourceUri: Uri.file('/private/A.mp3'),
        title: 'Private A');
    var origin = 'https://a.invalid';
    int? user = 1;
    var unlocked = true;
    observer = PlaybackWidgetObserver(handler,
        channel: channel,
        allowMetadata: () => playbackWidgetMetadataAllowed(request,
            origin: origin, userId: user, credentialsEnabled: unlocked));
    handler.mediaItem.add(const MediaItem(id: 'offline:A', title: 'Private A'));
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.ready,
        controls: [MediaControl.play, MediaControl.skipToNext]));
    await settle();
    expect(snapshots.last['title'], 'Private A');
    user = 2; // Same server, different account; no audio event follows.
    observer.refresh();
    await settle();
    expect(snapshots.last['title'], '');
    expect(snapshots.last['next'], false);
    user = 1;
    origin = 'https://b.invalid';
    observer.refresh();
    await settle();
    expect(snapshots.last['title'], '');
    origin = 'https://a.invalid';
    unlocked = false; // Local lock, even if auth still retains the old user.
    observer.refresh();
    await settle();
    expect(snapshots.last['title'], '');
    user = null; // Logout.
    observer.refresh();
    await settle();
    expect(snapshots.last['toggle'], false);
    request = LocalPlaybackRequest(
        localMediaId: 7,
        sourceUri: Uri.parse('content://documents/song'),
        title: 'Device song');
    handler.mediaItem
        .add(const MediaItem(id: 'local:document', title: 'Device song'));
    await settle();
    expect(snapshots.last['title'], 'Device song');
    expect(snapshots.last['toggle'], true);
  });

  test(
      'unknown offline owner fails closed and public shares remain independent',
      () {
    final unknown = OfflinePlaybackRequest(
        downloadKey: 'legacy',
        sourceUri: Uri.file('/private/song.mp3'),
        title: 'Legacy');
    expect(
        playbackWidgetMetadataAllowed(unknown,
            origin: 'https://a.invalid', userId: 1, credentialsEnabled: true),
        false);
    final public = PublicSharePlaybackRequest(
        shareToken: 'public',
        sourceUri: Uri.parse('https://a.invalid/s/public/stream'),
        title: 'Public');
    expect(
        playbackWidgetMetadataAllowed(public,
            origin: 'https://b.invalid',
            userId: null,
            credentialsEnabled: false),
        true);
  });
}
