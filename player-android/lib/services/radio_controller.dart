import 'dart:async';

import 'package:flutter/foundation.dart';

import '../models/radio_station.dart';

/// Plays one radio station at a time on the device's audio stack.
///
/// The production implementation ([AudioHandlerRadioEngine]) uses the shared
/// background audio handler; tests supply a fake.
abstract class RadioPlaybackEngine {
  /// Loads [station] and starts playing it, replacing whatever played before
  /// (including library audio). Completes when the stream is loaded and
  /// throws when it cannot be. [onStoppedElsewhere] is called when something
  /// else takes over the player later (library playback, logout, the
  /// notification's stop button).
  Future<void> start(
    RadioStation station, {
    required VoidCallback onStoppedElsewhere,
  });

  Future<void> pause();

  /// Resumes the paused station. Returns false when the station no longer
  /// owns the player (stopped from the notification); start it again then.
  Future<bool> resume();
  Future<void> stop();

  /// True while audio is actually coming out (loaded and not paused).
  Stream<bool> get playing;

  /// Playback failures after a successful [start] (lost connection, decoder).
  Stream<Object> get errors;
}

/// Creates the countdown ticker; injected so tests can drive time by hand.
typedef RadioTicker = Timer Function(
    Duration period, void Function(Timer) onTick);

/// Fetches up to [limit] stations of [query] in random order for auto explore.
typedef RadioStationSource = Future<List<RadioStation>> Function(
    RadioQuery query, int limit);

enum RadioStatus { idle, loading, playing, paused, error }

/// Dwell times the user can choose for auto explore.
const radioExploreDwellChoices = [
  Duration(seconds: 15),
  Duration(seconds: 30),
  Duration(seconds: 60),
];

/// A station that has not started within this time counts as failed while
/// exploring and is skipped.
const radioExploreStartTimeout = Duration(seconds: 10);

/// Auto explore gives up after this many stations in a row failed to start,
/// instead of hammering a server that cannot relay any of them.
const radioExploreMaxFailures = 5;

/// Stations fetched per auto-explore batch.
const radioExploreBatchSize = 20;

/// App-wide web radio state: the station on air, its playback status, the
/// user's bookmarked station ids and auto explore.
///
/// Lives as long as the account's API client, so radio keeps playing (and
/// exploring) while the user leaves the radio screen.
class RadioController extends ChangeNotifier {
  RadioController({
    required RadioPlaybackEngine engine,
    required RadioStationSource exploreSource,
    required Future<RadioStation> Function(String uuid) addBookmark,
    required Future<void> Function(String uuid) removeBookmark,
    RadioTicker? ticker,
  })  : _engine = engine,
        _exploreSource = exploreSource,
        _addBookmark = addBookmark,
        _removeBookmark = removeBookmark,
        _ticker = ticker ?? Timer.periodic {
    _playingSubscription = _engine.playing.listen(_onPlaying);
    _errorSubscription = _engine.errors.listen(_onEngineError);
  }

  final RadioPlaybackEngine _engine;
  final RadioStationSource _exploreSource;
  final Future<RadioStation> Function(String) _addBookmark;
  final Future<void> Function(String) _removeBookmark;
  final RadioTicker _ticker;
  late final StreamSubscription<bool> _playingSubscription;
  late final StreamSubscription<Object> _errorSubscription;

  RadioStation? _station;
  RadioStatus _status = RadioStatus.idle;
  String? _error;
  int _attempt = 0;
  bool _disposed = false;

  final Set<String> _bookmarks = {};
  final Set<String> _pendingBookmarks = {};
  int _bookmarkRevision = 0;

  bool _exploring = false;
  Duration _dwell = const Duration(seconds: 30);
  RadioQuery _exploreQuery = const RadioQuery();
  final List<RadioStation> _explorePool = [];
  final Set<String> _explored = {};
  Timer? _tick;
  int _secondsLeft = 0;
  int _loadingSeconds = 0;
  int _failures = 0;
  String? _exploreMessage;

  RadioStation? get station => _station;
  RadioStatus get status => _status;

  /// Readable reason of the last failure, for the now-playing bar.
  String? get error => _error;

  bool get exploring => _exploring;
  Duration get dwell => _dwell;

  /// Seconds until auto explore moves on; counts only while audio plays.
  int get secondsLeft => _secondsLeft;

  /// Why auto explore stopped by itself (no more stations, fetch failed).
  String? get exploreMessage => _exploreMessage;

  /// Changes whenever bookmarks are added or removed, so lists can reload.
  int get bookmarkRevision => _bookmarkRevision;

  bool isBookmarked(RadioStation station) => _bookmarks.contains(station.uuid);

  bool isBookmarkPending(RadioStation station) =>
      _pendingBookmarks.contains(station.uuid);

  /// Records the server's bookmark state of [stations] (search results and
  /// the bookmark list both carry it).
  void rememberBookmarks(Iterable<RadioStation> stations,
      {bool complete = false}) {
    if (complete) {
      _bookmarks.removeWhere((uuid) => !_pendingBookmarks.contains(uuid));
    }
    for (final station in stations) {
      if (_pendingBookmarks.contains(station.uuid)) continue;
      if (station.bookmarked) {
        _bookmarks.add(station.uuid);
      } else {
        _bookmarks.remove(station.uuid);
      }
    }
    _notify();
  }

  /// Bookmarks [station], or removes its bookmark. Throws when the server
  /// refuses; the previous state is kept then.
  Future<void> toggleBookmark(RadioStation station) async {
    final uuid = station.uuid;
    if (!_pendingBookmarks.add(uuid)) return;
    final add = !_bookmarks.contains(uuid);
    _notify();
    try {
      if (add) {
        await _addBookmark(uuid);
        _bookmarks.add(uuid);
      } else {
        await _removeBookmark(uuid);
        _bookmarks.remove(uuid);
      }
      _bookmarkRevision++;
    } finally {
      _pendingBookmarks.remove(uuid);
      _notify();
    }
  }

  /// Plays [station] chosen by the user. Ends auto explore.
  Future<void> play(RadioStation station) {
    _endExplore();
    return _start(station);
  }

  Future<void> togglePause() async {
    switch (_status) {
      case RadioStatus.playing:
        await _engine.pause();
      case RadioStatus.paused:
        final station = _station;
        if (!await _engine.resume() && station != null) await _start(station);
      case RadioStatus.error:
        final station = _station;
        if (station != null) await _start(station);
      case RadioStatus.idle:
      case RadioStatus.loading:
        break;
    }
  }

  /// Stops radio and auto explore.
  Future<void> stop() async {
    _endExplore();
    _attempt++;
    _station = null;
    _status = RadioStatus.idle;
    _error = null;
    _notify();
    await _engine.stop();
  }

  // ---------------------------------------------------------------------------
  // Auto explore
  // ---------------------------------------------------------------------------

  /// Starts playing random stations of [query], one after the other, each
  /// for [dwell] (default: the last choice).
  Future<void> startExplore(RadioQuery query, {Duration? dwell}) async {
    _endExplore();
    _exploring = true;
    _exploreMessage = null;
    _exploreQuery = query;
    _failures = 0;
    if (dwell != null) _dwell = dwell;
    _tick = _ticker(const Duration(seconds: 1), (_) => _onTick());
    _notify();
    await _exploreNext();
  }

  void setDwell(Duration dwell) {
    _dwell = dwell;
    if (_exploring && _secondsLeft > dwell.inSeconds) {
      _secondsLeft = dwell.inSeconds;
    }
    _notify();
  }

  /// Skips to the next station now.
  Future<void> exploreNext() => _exploring ? _exploreNext() : Future.value();

  /// Stops exploring and keeps the current station playing.
  void keep() {
    _endExplore();
    _notify();
  }

  /// Stops exploring and the station.
  Future<void> stopExplore() => stop();

  Future<void> _exploreNext({bool failed = false}) async {
    final attempt = ++_attempt;
    if (failed) {
      if (++_failures >= radioExploreMaxFailures) {
        await stop();
        _exploreMessage = 'Stopped exploring: stations did not start.';
        _notify();
        return;
      }
    }
    _secondsLeft = _dwell.inSeconds;
    _loadingSeconds = 0;
    // Tuning: the ticker must not count the previous station's failure again
    // while the next one is fetched.
    _status = RadioStatus.loading;
    _error = null;
    _notify();
    final station = await _nextExploreStation();
    if (!_exploring || attempt != _attempt) return;
    if (station == null) {
      final message = _exploreMessage;
      await stop();
      _exploreMessage = message;
      _notify();
      return;
    }
    _explored.add(station.uuid);
    await _start(station, attempt: attempt);
  }

  Future<RadioStation?> _nextExploreStation() async {
    for (var fetches = 0; fetches < 2; fetches++) {
      while (_explorePool.isNotEmpty) {
        final candidate = _explorePool.removeAt(0);
        if (!_explored.contains(candidate.uuid)) return candidate;
      }
      final List<RadioStation> batch;
      try {
        batch = await _exploreSource(
            _exploreQuery.copyWith(order: RadioOrder.random),
            radioExploreBatchSize);
      } catch (_) {
        _exploreMessage = 'Could not load stations to explore.';
        return null;
      }
      if (!_exploring) return null;
      rememberBookmarks(batch);
      _explorePool.addAll(batch.where((s) => !_explored.contains(s.uuid)));
      if (_explorePool.isEmpty && batch.isNotEmpty && fetches == 0) {
        // Everything in this filter was heard once: start over.
        _explored.clear();
        _explorePool.addAll(batch);
      }
    }
    _exploreMessage = 'No stations to explore for this filter.';
    return null;
  }

  void _onTick() {
    if (!_exploring) return;
    switch (_status) {
      case RadioStatus.playing:
        _secondsLeft--;
        if (_secondsLeft <= 0) {
          unawaited(_exploreNext());
        } else {
          _notify();
        }
      case RadioStatus.loading:
        _loadingSeconds++;
        if (_loadingSeconds >= radioExploreStartTimeout.inSeconds) {
          unawaited(_exploreNext(failed: true));
        }
      case RadioStatus.error:
        unawaited(_exploreNext(failed: true));
      case RadioStatus.paused:
      case RadioStatus.idle:
        break;
    }
  }

  void _endExplore() {
    _exploring = false;
    _tick?.cancel();
    _tick = null;
    _explorePool.clear();
    _explored.clear();
    _secondsLeft = 0;
  }

  // ---------------------------------------------------------------------------
  // Playback
  // ---------------------------------------------------------------------------

  Future<void> _start(RadioStation station, {int? attempt}) async {
    final current = attempt ?? ++_attempt;
    _station = station;
    _status = RadioStatus.loading;
    _error = null;
    _notify();
    try {
      await _engine.start(station, onStoppedElsewhere: () {
        if (current != _attempt) return;
        _endExplore();
        _attempt++;
        _station = null;
        _status = RadioStatus.idle;
        _notify();
      });
    } catch (error) {
      if (current != _attempt) return;
      _status = RadioStatus.error;
      _error = radioPlaybackErrorMessage(error);
      _notify();
      if (_exploring) unawaited(_exploreNext(failed: true));
    }
  }

  void _onPlaying(bool playing) {
    if (_station == null || _status == RadioStatus.error) return;
    if (playing) _failures = 0;
    final next = playing
        ? RadioStatus.playing
        : (_status == RadioStatus.loading
            ? RadioStatus.loading
            : RadioStatus.paused);
    if (next == _status) return;
    _status = next;
    _notify();
  }

  void _onEngineError(Object error) {
    if (_station == null) return;
    _status = RadioStatus.error;
    _error = radioPlaybackErrorMessage(error);
    _notify();
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _endExplore();
    _attempt++;
    unawaited(_playingSubscription.cancel());
    unawaited(_errorSubscription.cancel());
    if (_station != null) unawaited(_engine.stop());
    super.dispose();
  }
}

/// Readable text for a station that cannot be played.
String radioPlaybackErrorMessage(Object error) {
  if (error is TimeoutException) {
    return 'The station did not answer in time.';
  }
  return 'This station cannot be played right now.';
}
