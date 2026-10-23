import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/music_autoplay.dart';
import 'package:player_android/services/playback_request.dart';

ServerPlaybackRequest song(int id, {int user = 1}) => ServerPlaybackRequest(
    mediaId: id,
    serverOrigin: 'http://player.test',
    userId: user,
    sourceUri: Uri.parse('http://player.test/$id'),
    title: '$id');

void main() {
  test('explicit enable, distinct account-scoped bounded candidates', () async {
    final policy = MusicAutoplay();
    var calls = 0;
    Future<List<PlaybackRequest>> fetch(ServerPlaybackRequest _) async {
      calls++;
      return [
        song(1),
        song(2),
        song(3),
        song(3),
        song(4, user: 2),
        for (var id = 5; id < 30; id++) song(id)
      ];
    }

    Future<List<PlaybackRequest>?> resolve() => policy.resolve(
        seed: song(1),
        excluded: {song(1).identity, song(2).identity},
        fetch: fetch,
        current: () => true);
    expect(await resolve(), isNull);
    expect(calls, 0);
    policy.setEnabled(true);
    final songs = await resolve();
    expect(songs!.length, 12);
    expect(songs.first.identity, song(3).identity);
    expect(songs.map((s) => s.identity).toSet().length, 12);
    expect(songs.any((s) => s.identity == song(4, user: 2).identity), false);
    policy.reset();
    expect(policy.enabled, false);
  });
  test('offline, exhausted suggestions and errors preserve finite queue',
      () async {
    final policy = MusicAutoplay()..setEnabled(true);
    var calls = 0;
    Future<List<PlaybackRequest>> fetch(ServerPlaybackRequest _) async {
      calls++;
      return [];
    }

    expect(
        await policy.resolve(
            seed: song(1),
            excluded: {},
            fetch: fetch,
            current: () => true,
            online: false),
        isEmpty);
    expect(calls, 0);
    expect(policy.status, contains('connection'));
    expect(
        await policy.resolve(
            seed: song(1), excluded: {}, fetch: fetch, current: () => true),
        isEmpty);
    expect(policy.status, 'No additional related suggestions.');
    expect(
        await policy.resolve(
            seed: song(1),
            excluded: {},
            fetch: (_) async => throw StateError('offline'),
            current: () => true),
        isEmpty);
    expect(policy.status, contains('Could not load'));
  });
  test('disable and actor ownership changes cancel pending lookup', () async {
    final policy = MusicAutoplay()..setEnabled(true);
    final completion = Completer<List<PlaybackRequest>>();
    final pending = policy.resolve(
        seed: song(1),
        excluded: {},
        fetch: (_) => completion.future,
        current: () => true);
    expect(policy.loading, true);
    expect(
        await policy.resolve(
            seed: song(1),
            excluded: {},
            fetch: (_) async => [],
            current: () => true),
        isNull);
    policy.setEnabled(false);
    completion.complete([song(2)]);
    expect(await pending, isNull);
    expect(policy.loading, false);
    policy.setEnabled(true);
    var current = true;
    final moved = Completer<List<PlaybackRequest>>();
    final stale = policy.resolve(
        seed: song(1),
        excluded: {},
        fetch: (_) => moved.future,
        current: () => current);
    current = false;
    moved.complete([song(2)]);
    expect(await stale, isNull);
  });
}
