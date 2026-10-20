// Unit tests for RadioController: playback state, bookmarks and auto explore
// with a fake engine and a hand-driven ticker.

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/radio_station.dart';
import 'package:player_android/services/radio_controller.dart';

import '../support/radio_fakes.dart';

Future<void> _settle() => Future<void>.delayed(Duration.zero);

void main() {
  late FakeRadioEngine engine;
  late ManualTicker ticker;
  late FakeRadioApiClient api;
  late List<RadioQuery> exploreQueries;
  late RadioController radio;

  setUp(() {
    engine = FakeRadioEngine();
    ticker = ManualTicker();
    api = FakeRadioApiClient(
        stations: [for (var i = 1; i <= 4; i++) radioStation('s$i')]);
    exploreQueries = [];
    radio = RadioController(
      engine: engine,
      ticker: ticker.call,
      exploreSource: (query, limit) {
        exploreQueries.add(query);
        return api.searchRadioStations(
            tag: query.tag, order: query.order, limit: limit);
      },
      addBookmark: api.addRadioBookmark,
      removeBookmark: api.removeRadioBookmark,
    );
  });

  tearDown(() => radio.dispose());

  test('play reports loading, then live, pause and stop', () async {
    await radio.play(radioStation('s1'));
    expect(engine.started, ['s1']);
    expect(radio.status, RadioStatus.loading);
    engine.emitPlaying(true);
    await _settle();
    expect(radio.status, RadioStatus.playing);
    await radio.togglePause();
    expect(engine.pauses, 1);
    engine.emitPlaying(false);
    await _settle();
    expect(radio.status, RadioStatus.paused);
    await radio.togglePause();
    expect(engine.resumes, 1);
    await radio.stop();
    expect(radio.station, isNull);
    expect(radio.status, RadioStatus.idle);
    expect(engine.stops, 1);
  });

  test('a station that cannot start shows an error and can be retried',
      () async {
    engine.failing.add('s1');
    await radio.play(radioStation('s1'));
    expect(radio.status, RadioStatus.error);
    expect(radio.error, isNotEmpty);
    engine.failing.clear();
    await radio.togglePause();
    expect(engine.started, ['s1', 's1']);
    expect(radio.status, RadioStatus.loading);
  });

  test('Play after a stop from the notification restarts the station',
      () async {
    await radio.play(radioStation('s1'));
    engine.emitPlaying(true);
    await _settle();
    engine.emitPlaying(false);
    await _settle();
    expect(radio.status, RadioStatus.paused);
    engine.resumable = false;
    await radio.togglePause();
    expect(engine.started, ['s1', 's1']);
    expect(radio.status, RadioStatus.loading);
  });

  test('library playback taking over clears the radio state', () async {
    await radio.play(radioStation('s1'));
    engine.emitPlaying(true);
    await _settle();
    engine.stoppedElsewhere!();
    expect(radio.station, isNull);
    expect(radio.status, RadioStatus.idle);
  });

  test('bookmarks toggle through the API and bump the revision', () async {
    final station = radioStation('s2');
    expect(radio.isBookmarked(station), isFalse);
    await radio.toggleBookmark(station);
    expect(radio.isBookmarked(station), isTrue);
    expect(api.bookmarks.keys, ['s2']);
    expect(radio.bookmarkRevision, 1);
    await radio.toggleBookmark(station);
    expect(radio.isBookmarked(station), isFalse);
    expect(api.bookmarks, isEmpty);
    expect(radio.bookmarkRevision, 2);
  });

  test('a refused bookmark keeps the previous state', () async {
    final failing = RadioController(
      engine: engine,
      ticker: ticker.call,
      exploreSource: (_, __) async => [],
      addBookmark: (_) async => throw StateError('offline'),
      removeBookmark: (_) async {},
    );
    addTearDown(failing.dispose);
    final station = radioStation('s1');
    await expectLater(failing.toggleBookmark(station), throwsStateError);
    expect(failing.isBookmarked(station), isFalse);
    expect(failing.isBookmarkPending(station), isFalse);
  });

  test('rememberBookmarks takes the server state of listed stations', () {
    radio.rememberBookmarks(
        [radioStation('s1', bookmarked: true), radioStation('s2')]);
    expect(radio.isBookmarked(radioStation('s1')), isTrue);
    expect(radio.isBookmarked(radioStation('s2')), isFalse);
    radio.rememberBookmarks([], complete: true);
    expect(radio.isBookmarked(radioStation('s1')), isFalse);
  });

  group('auto explore', () {
    test('moves to the next random station after the dwell time', () async {
      await radio.startExplore(const RadioQuery(tag: 'jazz'),
          dwell: const Duration(seconds: 15));
      expect(exploreQueries.single.order, RadioOrder.random);
      expect(exploreQueries.single.tag, 'jazz');
      expect(engine.started, ['s1']);
      expect(radio.exploring, isTrue);

      // The countdown waits while the station is still connecting.
      ticker.tick(3);
      expect(radio.secondsLeft, 15);

      engine.emitPlaying(true);
      await _settle();
      ticker.tick(14);
      expect(radio.secondsLeft, 1);
      expect(engine.started, ['s1']);
      ticker.tick();
      await _settle();
      expect(engine.started, ['s1', 's2']);
      expect(radio.secondsLeft, 15);
    });

    test('skips a station that fails or does not start in time', () async {
      engine.failing.add('s1');
      engine.hanging.add('s2');
      await radio.startExplore(const RadioQuery());
      await _settle();
      expect(engine.started, ['s1', 's2']);
      ticker.tick(radioExploreStartTimeout.inSeconds);
      await _settle();
      expect(engine.started, ['s1', 's2', 's3']);
    });

    test('a playback error while exploring moves on', () async {
      await radio.startExplore(const RadioQuery());
      engine.emitPlaying(true);
      await _settle();
      engine.emitError(StateError('connection lost'));
      await _settle();
      ticker.tick();
      await _settle();
      expect(engine.started, ['s1', 's2']);
    });

    test('Next skips now, Keep stays on the station', () async {
      await radio.startExplore(const RadioQuery());
      engine.emitPlaying(true);
      await _settle();
      await radio.exploreNext();
      expect(engine.started, ['s1', 's2']);
      engine.emitPlaying(true);
      await _settle();
      radio.keep();
      expect(radio.exploring, isFalse);
      expect(ticker.active, 0);
      expect(radio.station?.uuid, 's2');
      ticker.tick(100);
      expect(engine.started, ['s1', 's2']);
    });

    test('choosing a station by hand ends exploring', () async {
      await radio.startExplore(const RadioQuery());
      await radio.play(radioStation('s4'));
      expect(radio.exploring, isFalse);
      expect(radio.station?.uuid, 's4');
    });

    test('stops with a message when the filter has no stations', () async {
      api.stations.clear();
      await radio.startExplore(const RadioQuery(tag: 'none'));
      expect(radio.exploring, isFalse);
      expect(radio.station, isNull);
      expect(radio.exploreMessage, contains('No stations'));
    });

    test('gives up after several stations in a row fail', () async {
      api.stations
        ..clear()
        ..addAll([for (var i = 0; i < 10; i++) radioStation('bad$i')]);
      engine.failing.addAll(api.stations.map((s) => s.uuid));
      await radio.startExplore(const RadioQuery());
      for (var i = 0; i < 10; i++) {
        await _settle();
      }
      expect(engine.started, hasLength(radioExploreMaxFailures));
      expect(radio.exploring, isFalse);
      expect(radio.exploreMessage, contains('did not start'));
    });

    test('Stop ends exploring and playback', () async {
      await radio.startExplore(const RadioQuery());
      await radio.stopExplore();
      expect(radio.exploring, isFalse);
      expect(radio.station, isNull);
      expect(engine.stops, 1);
    });
  });
}
