import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/radio_station.dart';
import 'package:player_android/services/radio_controller.dart';

RadioStation radioStation(String uuid,
        {String? name, bool bookmarked = false, String country = 'Germany'}) =>
    RadioStation(
      uuid: uuid,
      name: name ?? 'Station $uuid',
      streamUrl: '/api/radio/stations/$uuid/stream',
      country: country,
      codec: 'MP3',
      bitrate: 128,
      tags: const ['jazz'],
      bookmarked: bookmarked,
    );

/// Records what the controller asks for; [start] succeeds unless the station
/// is listed in [failing], and never reports audio until [emitPlaying].
class FakeRadioEngine implements RadioPlaybackEngine {
  final started = <String>[];
  final failing = <String>{};
  final hanging = <String>{};
  int stops = 0;
  int pauses = 0;
  int resumes = 0;
  VoidCallback? stoppedElsewhere;
  final _playing = StreamController<bool>.broadcast();
  final _errors = StreamController<Object>.broadcast();

  void emitPlaying(bool playing) => _playing.add(playing);
  void emitError(Object error) => _errors.add(error);

  @override
  Future<void> start(RadioStation station,
      {required VoidCallback onStoppedElsewhere}) async {
    started.add(station.uuid);
    stoppedElsewhere = onStoppedElsewhere;
    if (hanging.contains(station.uuid)) return Completer<void>().future;
    if (failing.contains(station.uuid)) throw StateError('dead stream');
  }

  @override
  Future<void> pause() async => pauses++;

  @override
  Future<bool> resume() async {
    resumes++;
    return resumable;
  }

  bool resumable = true;

  @override
  Future<void> stop() async => stops++;

  @override
  Stream<bool> get playing => _playing.stream;

  @override
  Stream<Object> get errors => _errors.stream;
}

/// A periodic timer advanced by hand with [tick].
class ManualTicker {
  final _timers = <_ManualTimer>[];

  Timer call(Duration period, void Function(Timer) onTick) {
    final timer = _ManualTimer(onTick);
    _timers.add(timer);
    return timer;
  }

  void tick([int times = 1]) {
    for (var i = 0; i < times; i++) {
      for (final timer in List.of(_timers)) {
        if (timer.isActive) timer.onTick(timer);
      }
    }
  }

  int get active => _timers.where((t) => t.isActive).length;
}

class _ManualTimer implements Timer {
  _ManualTimer(this.onTick);

  final void Function(Timer) onTick;
  bool _active = true;

  @override
  void cancel() => _active = false;

  @override
  bool get isActive => _active;

  @override
  int get tick => 0;
}

/// In-memory radio API: [stations] is the directory, [bookmarks] the
/// signed-in user's bookmarks.
class FakeRadioApiClient extends PlayerApiClient {
  FakeRadioApiClient({List<RadioStation>? stations})
      : stations = stations ?? [],
        super(dio: Dio(BaseOptions(baseUrl: 'https://player.test')));

  final List<RadioStation> stations;
  final Map<String, RadioStation> bookmarks = {};
  final searches = <Map<String, Object?>>[];
  Object? searchError;

  @override
  Future<List<RadioStation>> searchRadioStations({
    String? search,
    String? tag,
    String? country,
    RadioOrder order = RadioOrder.popular,
    int? limit,
    int? offset,
  }) async {
    searches.add({
      'q': search,
      'tag': tag,
      'country': country,
      'order': order,
      'limit': limit,
      'offset': offset,
    });
    if (searchError != null) throw searchError!;
    return stations
        .where((s) =>
            (search == null ||
                search.isEmpty ||
                s.name.toLowerCase().contains(search.toLowerCase())) &&
            (tag == null || s.tags.contains(tag)) &&
            (country == null || s.countryCode == country))
        .skip(offset ?? 0)
        .take(limit ?? 50)
        .map((s) => s.copyWith(bookmarked: bookmarks.containsKey(s.uuid)))
        .toList();
  }

  @override
  Future<List<RadioTag>> listRadioTags({int? limit}) async =>
      const [RadioTag(name: 'jazz', stationCount: 3), RadioTag(name: 'rock')];

  @override
  Future<List<RadioCountry>> listRadioCountries() async => const [
        RadioCountry(code: 'DE', name: 'Germany', stationCount: 2),
        RadioCountry(code: 'FR', name: 'France', stationCount: 1),
      ];

  @override
  Future<List<RadioStation>> listRadioBookmarks() async =>
      bookmarks.values.toList().reversed.toList();

  @override
  Future<RadioStation> addRadioBookmark(String uuid) async {
    final station =
        stations.firstWhere((s) => s.uuid == uuid).copyWith(bookmarked: true);
    bookmarks[uuid] = station;
    return station;
  }

  @override
  Future<void> removeRadioBookmark(String uuid) async {
    bookmarks.remove(uuid);
  }
}
