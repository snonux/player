import 'dart:math';
import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:just_audio/just_audio.dart';
import 'package:rxdart/rxdart.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'equalizer.dart';
import 'audio_progress_session.dart';
import 'sleep_timer.dart';
import 'music_sequence.dart';
import 'music_queue.dart';
import 'music_autoplay.dart';
import 'music_shuffle.dart';
import 'music_queue_snapshot.dart';
import 'playback_session_coordinator.dart';
import 'playback_request.dart';
import 'replay_gain.dart';
import 'loudness_effect.dart';
import 'headset_policy.dart';
import 'headset_preferences.dart';
import 'android_auto_catalog.dart';

part 'audio_handler_gapless.dart';

// ---------------------------------------------------------------------------
// PlayerAudioHandler
// ---------------------------------------------------------------------------

/// [BaseAudioHandler] subclass that wraps a [just_audio] [AudioPlayer] and
/// bridges it to the Android media-session / iOS AVAudioSession so that:
///   - Playback continues in the background as a foreground service.
///   - Lock-screen and notification controls (play/pause/seek/skip) work.
///   - Audio focus is honoured: playback pauses on phone calls or other
///     audio interruptions and resumes when focus is regained.
///   - Bluetooth headset media buttons (play, pause, next, previous) are
///     forwarded by [AudioService] and handled here.
///
/// The active progress session belongs to this handler so reporting continues
/// when the player screen is disposed during background playback.
///
/// Native playback failures are caught here and republished on
/// [playbackErrors]; nothing the player reports may escape as an unhandled
/// asynchronous error, because the handler outlives every screen.
///
/// The handler is registered once via [AudioService.init] in [main].
/// Consumers retrieve the singleton through [audioHandlerProvider].
class PlayerAudioHandler extends BaseAudioHandler with SeekHandler {
  /// Creates the handler with an already-configured [AudioPlayer].
  ///
  /// The player is injected so that tests can supply a mock without any
  /// platform channels (Dependency Inversion Principle).
  PlayerAudioHandler(AudioPlayer player,
      {LoudnessEffect? loudnessEnhancer,
      HeadsetPreferences? headsetPreferences})
      : headsetPreferences = headsetPreferences ?? HeadsetPreferences(),
        _player = player,
        loudness = ReplayGainController(player, loudnessEnhancer),
        equalizer = EqualizerController(player) {
    unawaited(equalizer.restore());
    _restoreGapless();
    _player.currentIndexStream.listen(_onGaplessIndex, onError: (Object _) {});
    // Propagate just_audio's playback state into the audio_service stream so
    // the notification, lock screen, and Wear OS clients see live updates.
    // just_audio adds a failure (for example `PlatformException(0, Source
    // error, {index: 0})` for a file ExoPlayer cannot decode) to this stream
    // as an error, in addition to throwing it from setAudioSource. Without
    // an error handler that second report is an unhandled exception.
    _player.playbackEventStream.listen(
      _onPlaybackEvent,
      onError: (Object error, StackTrace _) => _onPlaybackError(error),
    );

    // Propagate playing/paused transitions, which are not always carried in
    // playback events (just_audio emits them separately).
    _player.playingStream.listen((playing) {
      _broadcastState();
      final progress = _progress;
      if (!playing && progress != null) {
        unawaited(progress.record(force: true));
      }
    });

    // Propagate processing-state changes (e.g. loading → ready → completed).
    _player.processingStateStream.listen((state) {
      _broadcastState();
      final progress = _progress;
      if (state == ProcessingState.completed && progress != null) {
        unawaited(_completeTrack(progress));
      }
    });
  }

  final AudioPlayer _player;
  final ReplayGainController loudness;
  final EqualizerController equalizer;
  final HeadsetPreferences headsetPreferences;
  int _transportRevision = 0;

  /// Captured before external catalog resolution so a later Pause wins.
  int get transportRevision => _transportRevision;

  AndroidAutoCatalog? autoCatalog;
  // Retain two streams rather than one subject per album/page visited.
  final _autoLibraryChanges = BehaviorSubject.seeded(<String, dynamic>{});
  final _autoQueueChanges = BehaviorSubject.seeded(<String, dynamic>{});

  /// Invalidates subscribed host views after account or queue changes.
  void refreshAutoChildren({bool queueOnly = false}) {
    if (!queueOnly) _autoLibraryChanges.add(<String, dynamic>{});
    _autoQueueChanges.add(<String, dynamic>{});
  }

  @override
  ValueStream<Map<String, dynamic>> subscribeToChildren(String parentMediaId) =>
      autoCatalog?.isQueueParent(parentMediaId) == true
          ? _autoQueueChanges
          : _autoLibraryChanges;

  /// Updates the host window and active index even for paused queue edits.
  void publishAutoQueue(List<MediaItem> items, String title) {
    queue.add(items);
    queueTitle.add(title);
    _broadcastState();
  }

  @override
  Future<List<MediaItem>> getChildren(String parentMediaId,
          [Map<String, dynamic>? options]) async =>
      await autoCatalog?.children(parentMediaId) ?? [];

  @override
  Future<List<MediaItem>> search(String query,
          [Map<String, dynamic>? extras]) async =>
      await autoCatalog?.search(query) ?? [];

  Future<void> _runAutoAction(
      Future<void> Function(AndroidAutoCatalog) action) async {
    try {
      final catalog = autoCatalog;
      if (catalog == null) throw StateError('Music is initializing');
      await action(catalog);
    } catch (_) {
      // A failed new selection must not mark an existing audible source as
      // stopped or as a decoder failure. Hosts receive a concise action error.
      playbackState.add(playbackState.value.copyWith(
          errorCode: 1,
          errorMessage:
              'Could not play music. Open Player on your phone, then retry.'));
    }
  }

  @override
  Future<void> playFromMediaId(String mediaId,
          [Map<String, dynamic>? extras]) =>
      _runAutoAction((catalog) => catalog.play(mediaId));

  @override
  Future<void> playFromSearch(String query, [Map<String, dynamic>? extras]) =>
      _runAutoAction((catalog) => catalog.playSearch(query));

  @override
  Future<void> skipToQueueItem(int index) async {
    final published = queue.value;
    if (index < 0 || index >= published.length) return;
    await playFromMediaId(published[index].id);
  }

  int? get _autoQueueIndex {
    if (!_queueActive || _queueIndex < 0 || _queueIndex >= _albumQueue.length) {
      return null;
    }
    final occurrence = _albumQueue[_queueIndex].id;
    final index = queue.value
        .indexWhere((item) => item.extras?['queueOccurrence'] == occurrence);
    return index < 0 ? null : index;
  }

  late final HeadsetPolicy headset = HeadsetPolicy(
    preferences: headsetPreferences,
    isPlaying: () => _player.playing,
    canResume: () {
      sleepTimer.check();
      return _canControlSource &&
          !sleepTimer.expired &&
          (_player.processingState == ProcessingState.ready ||
              isLiveRadio && _liveReconnectNeeded && _liveSource != null) &&
          _musicState?.error == null;
    },
    pausePlayback: () => pause(automatic: true),
    resumePlayback: () => play(automatic: true),
    volume: () => _player.volume,
    setVolume: _player.setVolume,
  );
  final _gapless = _NativeGaplessState();
  Future<AudioSource?> Function(PlaybackRequest, bool Function())?
      prepareGaplessSource;
  bool get gaplessEnabled => _gapless.enabled;
  String get gaplessStatus => _gapless.status;

  Future<void> _refreshLoudness() => loudness.setTrack(_metadataRequest?.music,
      albumContext: _queueActive &&
          !_queueShuffle &&
          sameLoudnessAlbum(_albumQueue.map((entry) => entry.request.music)));
  Future<void> Function()? pauseSleepRemote;
  int _sourceSleepIntent = 0;
  late final SleepTimer sleepTimer = SleepTimer(onExpire: () async {
    headset.suspend();
    _transportRevision++;
    // Set the latch synchronously before any I/O: delayed automatic loads
    // cannot turn playback back on after expiry.
    if (isLiveRadio) _liveReconnectNeeded = true;
    final localPause = isLiveRadio ? _player.stop() : _player.pause();
    final remotePause = pauseSleepRemote?.call();
    await localPause;
    await remotePause;
    await _progress?.record(force: true);
  });
  Future<Uri?> Function(PlaybackRequest request, bool Function() current)?
      artworkLoader;
  int _metadataEpoch = 0;
  PlaybackRequest? _metadataRequest;
  PlaybackRequest? get currentMetadataRequest => _metadataRequest;

  /// The request activated on the decoder, excluding prepared or failed tracks.
  /// Display metadata may lead source activation while the next loader awaits.
  PlaybackRequest? get currentActivatedMediaRequest => _canControlSource &&
          !_movingTrack &&
          _failedMusicDelta == null &&
          _musicState?.loading != true &&
          _musicState?.error == null
      ? _metadataRequest
      : null;

  void clearMediaItem() {
    _metadataEpoch++;
    _metadataRequest = null;
    unawaited(_refreshLoudness());
    mediaItem.add(null);
  }

  void setMediaRequest(PlaybackRequest request) {
    final same = identical(_metadataRequest, request);
    _metadataRequest = request;
    unawaited(_refreshLoudness());
    final epoch = same ? _metadataEpoch : ++_metadataEpoch;
    mediaItem.add(MediaItem(
        id: request.identity,
        title: request.title,
        artist: request.artist.isEmpty ? 'Unknown artist' : request.artist,
        album: request.album.isEmpty ? null : request.album,
        artUri: same ? mediaItem.value?.artUri : null,
        duration:
            request is LiveRadioPlaybackRequest ? null : _player.duration));
    if (same || request.artworkUri == null || artworkLoader == null) return;
    unawaited(() async {
      try {
        bool current() =>
            epoch == _metadataEpoch &&
            _ownsSource?.call() != false &&
            identical(_metadataRequest, request);
        final art = await artworkLoader!(request, current);
        if (art != null &&
            epoch == _metadataEpoch &&
            _ownsSource?.call() != false &&
            identical(_metadataRequest, request)) {
          mediaItem.add(mediaItem.value!.copyWith(artUri: art));
        }
      } catch (_) {/* Optional artwork must never fail playback. */}
    }());
  }

  List<PlaybackRequest> _musicEntries = const [];
  int _musicIndex = -1;
  bool _movingTrack = false;
  bool _musicWrap = true;
  bool _collectionEnded = false;
  AudioProgressSession? _completingProgress;
  int _completionCycle = 0;
  MusicRepeat _queueRepeat = MusicRepeat.off;
  bool _queueShuffle = false, _queueUndoShuffle = false;
  String _queueShuffleMode = 'tracks', _queueUndoShuffleMode = 'tracks';
  List<int> _queueCanonical = [], _queueUndoCanonical = [];
  int? _failedMusicDelta;
  Duration? _retryMusicPosition;
  Duration _musicPosition = Duration.zero;
  StreamSubscription<Duration>? _musicPositions;
  Future<List<PlaybackRequest>> Function()? _expandMusic;
  Future<AudioSource?> Function(PlaybackRequest, bool Function())? _musicLoader;
  void Function(PlaybackRequest)? _musicChanged;
  final _musicChanges = StreamController<MusicSequenceState>.broadcast();
  MusicSequenceState? _musicState;
  MusicSequenceState? get musicState => _musicState;
  PlaybackSessionLease? _sequenceLease;
  PlaybackSessionLease? get sequenceLease => _sequenceLease;
  Stream<MusicSequenceState> get musicChanges => _musicChanges.stream;

  List<MusicQueueEntry> _albumQueue = [];
  List<MusicQueueEntry>? _queueUndo;
  int _queueSerial = 0, _queueIndex = -1, _queueUndoIndex = -1;
  Set<int> _queueNextIds = {}, _queueUndoNextIds = {};
  String _queueUndoLabel = '';
  int _queueNextEpoch = 0, _queueUndoNextEpoch = 0;
  int _queueOperation = 0;
  String _queueLabel = '', _queueStatus = '';
  bool _queueActive = false, _queueBusy = false;
  Future<PlaybackSessionLease?> Function(PlaybackRequest)? _queueClaim;
  Future<AudioSource?> Function(PlaybackRequest, bool Function())? _queueLoader;
  final _queueChanges = StreamController<void>.broadcast();
  Stream<void> get queueChanges => _queueChanges.stream;
  final musicAutoplay = MusicAutoplay();
  Future<List<PlaybackRequest>> Function(ServerPlaybackRequest seed)?
      relatedSongsForAutoplay;
  bool Function()? autoplayOnline;
  bool autoplayAvailable = true;

  void setQueueAutoplay(bool enabled) {
    musicAutoplay.setEnabled(enabled);
    _queueStatus = musicAutoplay.status;
    _notifyQueue();
  }

  MusicQueueState get musicQueue => MusicQueueState(
      entries: List.unmodifiable(_albumQueue),
      index: _queueIndex,
      label: _queueLabel,
      active: _queueActive && _canControlSource,
      busy: _queueBusy ||
          (_queueActive && _movingTrack) ||
          musicAutoplay.loading,
      canUndo: _queueUndo != null,
      status: musicAutoplay.status.isNotEmpty ? musicAutoplay.status : _queueStatus,
      repeat: _queueRepeat,
      shuffle: _queueShuffle,
      shuffleMode: _queueShuffleMode,
      shuffleEligible: _shuffleEligible,
      upNextIds: Set.unmodifiable(_queueNextIds),
      restored: _queueRestored,
      restoreFailed: _queueRestoreFailed,
      autoplayEnabled: musicAutoplay.enabled,
      autoplayAvailable: autoplayAvailable,
      autoplayStatus: musicAutoplay.status,
      position: _queuePositionEntry ==
              (_queueIndex >= 0 && _queueIndex < _albumQueue.length
                  ? _albumQueue[_queueIndex].id
                  : null)
          ? _queuePosition
          : 0);
  void Function()? persistMusicQueue;
  Future<void> Function()? retryQueueRestore;
  int _queueScope = 0;
  int _queueRecoveryGeneration = 0;
  int get queueRecoveryGeneration => _queueRecoveryGeneration;
  int get queueScope => _queueScope;
  bool _queueRestored = false, _queueRestoreFailed = false;
  double _queuePosition = 0;
  int? _queuePositionEntry;
  MusicQueueSnapshot get queueSnapshot => MusicQueueSnapshot(
      entries: _albumQueue
          .where((e) => e.request is ServerPlaybackRequest)
          .map((e) =>
              (id: e.id, mediaId: (e.request as ServerPlaybackRequest).mediaId))
          .toList(),
      index: _queueIndex,
      position: _queuePositionEntry ==
              (_queueIndex >= 0 && _queueIndex < _albumQueue.length
                  ? _albumQueue[_queueIndex].id
                  : null)
          ? _queuePosition
          : 0,
      label: _queueLabel,
      upNext: Set.of(_queueNextIds),
      repeat: _queueRepeat,
      shuffle: _queueShuffle,
      shuffleMode: _queueShuffleMode,
      canonical: List.of(_queueCanonical));
  void _notifyQueue() {
    unawaited(_refreshLoudness());
    persistMusicQueue?.call();
    _queueChanges.add(null);
    _refreshGapless();
  }

  void queueRestoreStatus(String message,
      {bool busy = false, bool failed = false}) {
    _queueBusy = busy;
    _queueRestoreFailed = failed;
    _queueStatus = message;
    _queueChanges.add(null);
  }

  void restoreMusicQueue(
      {required List<MusicQueueEntry> entries,
      required int index,
      required String label,
      required double position,
      required Set<int> upNext,
      required Future<PlaybackSessionLease?> Function(PlaybackRequest) claim,
      required Future<AudioSource?> Function(PlaybackRequest, bool Function())
          load,
      int unavailable = 0,
      MusicRepeat repeat = MusicRepeat.off,
      bool shuffle = false,
      String shuffleMode = 'tracks',
      List<int>? canonical}) {
    musicAutoplay.reset();
    _albumQueue = entries;
    _queueRepeat = repeat;
    _queueShuffle = shuffle;
    _queueShuffleMode = shuffleMode;
    _queueCanonical = canonical ?? entries.map((e) => e.id).toList();
    _queueIndex = index;
    _queueLabel = label;
    for (final entry in entries) {
      if (entry.id > _queueSerial) _queueSerial = entry.id;
    }
    _queuePositionEntry = index >= 0 ? entries[index].id : null;
    _queuePosition = position;
    _queueRestored = entries.isNotEmpty;
    _queueClaim = claim;
    _queueLoader = load;
    _queueActive = false;
    _queueUndo = null;
    _queueNextIds = upNext;
    _queueBusy = false;
    _queueRestoreFailed = false;
    _queueStatus = entries.isNotEmpty
        ? 'Queue restored · paused. Resume when you’re ready.'
        : 'Queue is empty.';
    if (unavailable > 0) {
      _queueStatus +=
          ' $unavailable ${unavailable == 1 ? 'song is' : 'songs are'} no longer available.';
    }
    _notifyQueue();
  }

  void cycleQueueRepeat() {
    if (musicQueue.busy || _queueRestoreFailed) return;
    musicAutoplay.cancel();
    _queueRepeat = MusicRepeat
        .values[(_queueRepeat.index + 1) % MusicRepeat.values.length];
    _notifyQueue();
  }

  int get _shuffleEligible => _albumQueue
      .skip(_queueIndex + 1)
      .where((e) => !_queueNextIds.contains(e.id))
      .length;

  Future<Map<String, dynamic>> Function(List<int>)? loadQueueAlbums;
  Future<void> shuffleQueue({bool reshuffle = false, String? mode}) async {
    if (musicQueue.busy ||
        _queueRestoreFailed ||
        ((!_queueShuffle || reshuffle || mode != null) &&
            _shuffleEligible < 2)) {
      return;
    }
    musicAutoplay.cancel();
    final chosen = mode ?? _queueShuffleMode;
    if (!['tracks', 'albums'].contains(chosen)) return;
    final enabled = reshuffle || mode != null || !_queueShuffle;
    Map<String, dynamic> albums = {};
    if (enabled && chosen == 'albums') {
      final operation = ++_queueOperation, scope = _queueScope;
      final selected = _queueIndex;
      final ids = _albumQueue
          .map((e) => e.request)
          .whereType<ServerPlaybackRequest>()
          .map((r) => r.mediaId)
          .toSet()
          .toList();
      _queueBusy = true;
      _queueStatus = 'Loading album order…';
      _queueChanges.add(null);
      try {
        if (loadQueueAlbums == null) {
          throw StateError('Album catalog unavailable');
        }
        albums = await loadQueueAlbums!(ids);
      } catch (_) {
        if (operation == _queueOperation && scope == _queueScope) {
          _queueBusy = false;
          _queueStatus =
              'Could not shuffle albums. Check your connection and retry.';
          _queueChanges.add(null);
        }
        return;
      }
      if (operation != _queueOperation || scope != _queueScope) return;
      _queueBusy = false;
      if (selected != _queueIndex) {
        _queueStatus = 'Track changed; choose Shuffle albums again.';
        _queueChanges.add(null);
        return;
      }
    }
    _rememberQueue();
    _queueShuffle = reshuffle || mode != null || !_queueShuffle;
    _queueShuffleMode = mode ?? _queueShuffleMode;
    _albumQueue = orderMusicRemainder(_albumQueue, _queueIndex, _queueCanonical,
        _queueNextIds, _queueShuffle && _queueShuffleMode != 'albums');
    if (_queueShuffle && _queueShuffleMode == 'albums') {
      final prefix = _albumQueue.take(_queueIndex + 1).toList();
      final remaining = _albumQueue.skip(_queueIndex + 1).toList();
      _albumQueue = [
        ...prefix,
        ...remaining.where((e) => _queueNextIds.contains(e.id)),
        ...shuffledMusicAlbums(
            remaining.where((e) => !_queueNextIds.contains(e.id)).toList(),
            albums,
            null,
            _queueIndex >= 0 ? _albumQueue[_queueIndex] : null)
      ];
    }
    _queueStatus =
        '${_queueShuffle ? (_queueShuffleMode == 'albums' ? 'Upcoming albums shuffled; album song order preserved.' : 'Upcoming songs shuffled.') : 'Remaining song order restored.'} Undo available.';
    _syncQueueSequence();
  }

  void queueStorageFailed() {
    _queueStatus = 'Queue could not be saved on this device.';
    _queueChanges.add(null);
  }

  void _invalidateQueueNext() {
    _queueNextIds.clear();
    ++_queueNextEpoch;
  }

  void _deactivateQueue() {
    if (_queueActive &&
        _canControlSource &&
        !_movingTrack &&
        _queueIndex >= 0) {
      _queuePosition = _player.position.inMilliseconds / 1000;
      _queuePositionEntry = _albumQueue[_queueIndex].id;
    }
    _queueActive = false;
    _queueUndo = null;
    _invalidateQueueNext();
    _queueUndoNextIds.clear();
    if (_albumQueue.isNotEmpty) {
      _queueStatus = 'Queue paused. Choose a track to play.';
    }
    _notifyQueue();
  }

  /// Called when account/server/library scope changes; retained callbacks must
  /// never cross an authentication boundary.
  void clearRetainedQueue() {
    persistMusicQueue?.call();
    persistMusicQueue = null;
    retryQueueRestore = null;
    musicAutoplay.reset();
    ++_queueScope;
    _queueRepeat = MusicRepeat.off;
    _queueShuffle = false;
    _queueShuffleMode = 'tracks';
    _queueCanonical = [];
    _queueRestored = false;
    _queueRestoreFailed = false;
    _queuePosition = 0;
    _queuePositionEntry = null;
    ++_queueOperation;
    _queueBusy = false;
    if (_queueActive) {
      ++_sourceGeneration;
      _controlsEnabled = false;
    }
    _albumQueue = [];
    _queueIndex = -1;
    _queueClaim = null;
    _queueLoader = null;
    _queueUndo = null;
    _queueActive = false;
    _queueLabel = '';
    _queueStatus = '';
    _invalidateQueueNext();
    _queueUndoNextIds.clear();
    _queueUndoLabel = '';
    _notifyQueue();
  }

  /// Explicit queue play acquires fresh ownership after another source took over.
  Future<void> resumeQueue() =>
      jumpQueue(_queueIndex, preserveNext: _queueRestored);

  Future<void> jumpQueue(int index, {bool preserveNext = false}) async {
    if (musicQueue.busy || index < 0 || index >= _albumQueue.length) return;
    final sleepIntent = sleepTimer.beginPlaybackIntent();
    final retainedNext = preserveNext ? Set<int>.of(_queueNextIds) : <int>{};
    if (!preserveNext) _invalidateQueueNext();
    final entry = _albumQueue[index], claim = _queueClaim, load = _queueLoader;
    final resumePosition = _queueRestored && _queuePositionEntry == entry.id
        ? Duration(milliseconds: (_queuePosition * 1000).round())
        : null;
    if (!_queueActive && claim == null) return;
    final operation = ++_queueOperation;
    _queueBusy = true;
    _notifyQueue();
    try {
      if (!_queueActive || !_canControlSource) {
        final lease = await claim!(entry.request);
        if (lease == null ||
            operation != _queueOperation ||
            !_albumQueue.contains(entry)) {
          return;
        }
        beginSourceSession(
            ownsSource: () => lease.isCurrent, sleepIntent: sleepIntent);
        _musicEntries = _albumQueue.map((e) => e.request).toList();
        _musicIndex = index;
        _musicWrap = false;
        _musicLoader = load;
        _sequenceLease = lease;
        _musicChanged = lease.updateSource;
        _expandMusic = () async => _musicEntries;
        _controlsEnabled = true;
        _queueActive = true;
        _watchMusicPositions();
      }
      if (preserveNext) _queueNextIds = retainedNext;
      _queueRestored = false;
      await _moveTrack(index - _musicIndex,
          resumePosition: resumePosition, playbackIntent: sleepIntent);
      if (operation != _queueOperation) return;
      _queueStatus = _musicState?.error == null
          ? ''
          : 'Could not play track. Tap the track to retry.';
    } finally {
      if (operation == _queueOperation) {
        _queueBusy = false;
        _notifyQueue();
      }
    }
  }

  void _rememberQueue() {
    _queueUndo = List.of(_albumQueue);
    _queueUndoIndex = _queueIndex;
    _queueUndoNextIds = Set.of(_queueNextIds);
    _queueUndoLabel = _queueLabel;
    _queueUndoNextEpoch = _queueNextEpoch;
    _queueUndoShuffle = _queueShuffle;
    _queueUndoShuffleMode = _queueShuffleMode;
    _queueUndoCanonical = List.of(_queueCanonical);
  }

  void _syncQueueSequence() {
    if (_queueActive) {
      _musicEntries = _albumQueue.map((e) => e.request).toList();
      _musicIndex = _queueIndex;
      _publishMusic();
    }
    _notifyQueue();
  }

  Future<bool> _appendAutoplayRelated(bool Function() current) async {
    if (_queueIndex < 0 || _queueIndex >= _albumQueue.length) return false;
    final seedEntry = _albumQueue[_queueIndex];
    final seed = seedEntry.request;
    if (seed is! ServerPlaybackRequest) return false;
    final fetch = relatedSongsForAutoplay;
    if (fetch == null) {
      musicAutoplay.status =
          'Could not load related songs. Try again when connected.';
      _notifyQueue();
      return false;
    }
    final occurrence = seedEntry.id;
    final pending = musicAutoplay.resolve(
      seed: seed,
      excluded: {for (final entry in _albumQueue) entry.request.identity},
      fetch: fetch,
      current: () =>
          current() &&
          _queueActive &&
          _queueIndex >= 0 &&
          _queueIndex < _albumQueue.length &&
          _albumQueue[_queueIndex].id == occurrence,
      online: autoplayOnline?.call() ?? autoplayAvailable,
    );
    _notifyQueue();
    final songs = await pending;
    _notifyQueue();
    if (songs == null || songs.isEmpty) return false;
    final added = [
      for (final request in songs)
        MusicQueueEntry(++_queueSerial, request, autoplayRelated: true)
    ];
    _albumQueue.addAll(added);
    _queueCanonical.addAll(added.map((entry) => entry.id));
    _notifyQueue();
    return true;
  }

  void moveQueue(int index, int delta) {
    final target = index + delta;
    if (musicQueue.busy ||
        index < 0 ||
        index >= _albumQueue.length ||
        target < 0 ||
        target >= _albumQueue.length) {
      return;
    }
    musicAutoplay.cancel();
    _rememberQueue();
    _invalidateQueueNext();
    final current = _albumQueue[_queueIndex];
    final entry = _albumQueue.removeAt(index);
    _albumQueue.insert(target, entry);
    _queueIndex = _albumQueue.indexOf(current);
    _queueCanonical = _albumQueue.map((e) => e.id).toList();
    _queueStatus = 'Track moved. Undo available.';
    _syncQueueSequence();
  }

  Future<void> removeQueue(int index) async {
    if (musicQueue.busy || index < 0 || index >= _albumQueue.length) return;
    musicAutoplay.cancel();
    final old = List<MusicQueueEntry>.of(_albumQueue), oldIndex = _queueIndex;
    final oldNext = Set<int>.of(_queueNextIds), oldLabel = _queueLabel;
    final oldEpoch = _queueNextEpoch;
    final oldShuffleMode = _queueShuffleMode;
    final oldShuffle = _queueShuffle,
        oldCanonical = List<int>.of(_queueCanonical);
    _invalidateQueueNext();
    final current = _albumQueue[_queueIndex];
    final operation = ++_queueOperation;
    _queueBusy = true;
    try {
      if (index == _queueIndex && _queueActive) await stop();
      if (operation != _queueOperation) return;
      _albumQueue.removeAt(index);
      _queueCanonical.removeWhere((id) => !_albumQueue.any((e) => e.id == id));
      _queueIndex = index == oldIndex
          ? (index < _albumQueue.length ? index : _albumQueue.length - 1)
          : _albumQueue.indexOf(current);
      _queueUndo = old;
      _queueUndoIndex = oldIndex;
      _queueUndoNextIds = oldNext;
      _queueUndoLabel = oldLabel;
      _queueUndoNextEpoch = oldEpoch;
      _queueUndoShuffle = oldShuffle;
      _queueUndoShuffleMode = oldShuffleMode;
      _queueUndoCanonical = oldCanonical;
      _queueStatus =
          'Track removed. Choose a track to play if paused. Undo available.';
      _syncQueueSequence();
    } finally {
      if (operation == _queueOperation) {
        _queueBusy = false;
        _notifyQueue();
      }
    }
  }

  Future<void> clearQueue() async {
    if (musicQueue.busy || _albumQueue.isEmpty) return;
    musicAutoplay.cancel();
    final old = List<MusicQueueEntry>.of(_albumQueue), oldIndex = _queueIndex;
    final oldNext = Set<int>.of(_queueNextIds), oldLabel = _queueLabel;
    final oldEpoch = _queueNextEpoch;
    final oldShuffleMode = _queueShuffleMode;
    final oldShuffle = _queueShuffle,
        oldCanonical = List<int>.of(_queueCanonical);
    _invalidateQueueNext();
    final operation = ++_queueOperation;
    _queueBusy = true;
    try {
      if (_queueActive) await stop();
      if (operation != _queueOperation) return;
      _albumQueue = [];
      _queueCanonical = [];
      _queueIndex = -1;
      _queueRestored = false;
      _queueUndo = old;
      _queueUndoIndex = oldIndex;
      _queueUndoNextIds = oldNext;
      _queueUndoLabel = oldLabel;
      _queueUndoNextEpoch = oldEpoch;
      _queueUndoShuffle = oldShuffle;
      _queueUndoShuffleMode = oldShuffleMode;
      _queueUndoCanonical = oldCanonical;
      _queueStatus = 'Queue cleared. Undo available.';
    } finally {
      if (operation == _queueOperation) {
        _queueBusy = false;
        _notifyQueue();
      }
    }
  }

  /// Adds occurrences without taking ownership until explicit Play now/jump.
  Future<bool> insertQueue({
    required List<PlaybackRequest> entries,
    required QueueInsertion action,
    bool replace = false,
    bool shuffled = false,
    String sourceLabel = 'Collection',
    required Future<PlaybackSessionLease?> Function(PlaybackRequest) claim,
    required Future<AudioSource?> Function(PlaybackRequest, bool Function())
        load,
  }) async {
    if (musicQueue.busy || entries.isEmpty) return false;
    ++_queueRecoveryGeneration;
    _queueRestoreFailed = false;
    _rememberQueue();
    final previous = _queueUndo!;
    final previousIndex = _queueUndoIndex;
    final previousNext = Set<int>.of(_queueUndoNextIds);
    final previousLabel = _queueUndoLabel;
    final previousEpoch = _queueUndoNextEpoch;
    final previousShuffleMode = _queueUndoShuffleMode;
    final previousShuffle = _queueUndoShuffle,
        previousCanonical = List<int>.of(_queueUndoCanonical);
    final added = entries
        .map((request) => MusicQueueEntry(++_queueSerial, request))
        .toList();
    musicAutoplay.cancel();
    if (replace) {
      musicAutoplay.reset();
      _albumQueue.clear();
      _queueIndex = -1;
      _queueLabel = sourceLabel;
      _queueShuffle = shuffled;
      _queueShuffleMode = 'tracks';
      _queueCanonical = added.map((e) => e.id).toList();
      if (shuffled) added.shuffle();
      _invalidateQueueNext();
    }
    var at = action == QueueInsertion.append
        ? _albumQueue.length
        : (_queueIndex + 1).clamp(0, _albumQueue.length);
    if (action == QueueInsertion.next) {
      while (at < _albumQueue.length &&
          _queueNextIds.contains(_albumQueue[at].id)) {
        at++;
      }
    }
    if (!replace) {
      final anchor = at > 0 ? _albumQueue[at - 1].id : null;
      _queueCanonical.insertAll(
          action == QueueInsertion.append
              ? _queueCanonical.length
              : anchor == null
                  ? 0
                  : _queueCanonical.indexOf(anchor) + 1,
          added.map((e) => e.id));
    }
    _albumQueue.insertAll(at, added);
    if (_queueIndex < 0) _queueIndex = 0;
    if (_queueLabel.isEmpty) _queueLabel = 'Songs you added';
    _queueClaim = claim;
    _queueLoader = load;
    if (action == QueueInsertion.next) {
      _queueNextIds.addAll(added.map((entry) => entry.id));
    }
    _syncQueueSequence();
    if (action == QueueInsertion.now) {
      await jumpQueue(at);
      if (!_queueActive || _albumQueue[_queueIndex].id != added.first.id) {
        return false;
      }
      _queueUndo = previous;
      _queueUndoIndex = previousIndex;
      _queueUndoNextIds = previousNext;
      _queueUndoLabel = previousLabel;
      _queueUndoNextEpoch = previousEpoch;
      _queueUndoShuffle = previousShuffle;
      _queueUndoShuffleMode = previousShuffleMode;
      _queueUndoCanonical = previousCanonical;
    }
    if (action != QueueInsertion.now || _musicState?.error == null) {
      _queueStatus =
          'Added ${added.length} ${added.length == 1 ? 'song' : 'songs'}. Undo available.';
    }
    _notifyQueue();
    return true;
  }

  Object? get queueUndoToken => _queueUndo;

  Future<void> undoQueue({Object? expected}) async {
    if (expected != null && !identical(expected, _queueUndo)) return;
    if (musicQueue.busy || _queueUndo == null) return;
    final previous = _queueUndo!, previousIndex = _queueUndoIndex;
    final previousNext = Set<int>.of(_queueUndoNextIds),
        previousLabel = _queueUndoLabel;
    final previousEpoch = _queueUndoNextEpoch;
    final previousShuffleMode = _queueUndoShuffleMode;
    final previousShuffle = _queueUndoShuffle,
        previousCanonical = List<int>.of(_queueUndoCanonical);
    final current = _queueIndex >= 0 && _queueIndex < _albumQueue.length
        ? _albumQueue[_queueIndex]
        : null;
    final operation = ++_queueOperation;
    _queueBusy = true;
    try {
      if (_queueActive && !previous.contains(current)) await stop();
      if (operation != _queueOperation) return;
      _albumQueue = previous;
      _queueIndex =
          _queueActive && current != null && previous.contains(current)
              ? previous.indexOf(current)
              : previousIndex;
      _queueLabel = previousLabel;
      _queueShuffle = previousShuffle;
      _queueShuffleMode = previousShuffleMode;
      _queueCanonical = previousCanonical;
      _queueNextIds = previousEpoch == _queueNextEpoch ? previousNext : {};
      _queueUndo = null;
      _queueStatus = 'Queue restored. Choose a track to play if paused.';
      _syncQueueSequence();
    } finally {
      if (operation == _queueOperation) {
        _queueBusy = false;
        _notifyQueue();
      }
    }
  }

  /// Transfers the displayed source list and account-scoped loader to the background
  /// handler. Neither callback may capture a screen or a WidgetRef.
  void startMusicSequence(
      {bool wrap = true,
      String queueLabel = 'Album',
      PlaybackSessionLease? sequenceLease,
      Future<PlaybackSessionLease?> Function(PlaybackRequest)? claimQueue,
      required List<PlaybackRequest> entries,
      required int index,
      required Future<AudioSource?> Function(PlaybackRequest, bool Function())
          load,
      required void Function(PlaybackRequest) changed,
      required Future<List<PlaybackRequest>> Function() expand}) {
    unawaited(_musicPositions?.cancel());
    _musicPosition = _player.position;
    _failedMusicDelta = null;
    _retryMusicPosition = null;
    _watchMusicPositions();
    _collectionEnded = false;
    _musicWrap = wrap;
    _musicEntries = List.unmodifiable(entries);
    _musicIndex = index;
    _musicLoader = load;
    _sequenceLease = sequenceLease;
    _musicChanged = changed;
    _expandMusic = expand;
    if (!wrap) {
      ++_queueRecoveryGeneration;
      _queueRestored = false;
      _queuePosition = 0;
      _queuePositionEntry = null;
      ++_queueOperation;
      _queueBusy = false;
      _albumQueue = entries
          .map((request) => MusicQueueEntry(++_queueSerial, request))
          .toList();
      _queueIndex = index;
      _queueShuffle = false;
      _queueShuffleMode = 'tracks';
      _queueCanonical = _albumQueue.map((e) => e.id).toList();
      _queueLabel = queueLabel;
      _queueClaim = claimQueue;
      _queueLoader = load;
      _queueUndo = null;
      _queueActive = true;
      _queueStatus = '';
      _invalidateQueueNext();
    }
    _publishMusic();
    _broadcastState();
    _notifyQueue();
  }

  void _watchMusicPositions() {
    unawaited(_musicPositions?.cancel());
    _musicPositions = _player.positionStream.listen((position) {
      if (_musicIndex >= 0 &&
          _canControlSource &&
          _player.processingState == ProcessingState.ready) {
        _musicPosition = position;
        if (_queueActive && !_movingTrack && _queueIndex >= 0) {
          final seconds = position.inMilliseconds / 1000;
          final changed = seconds.floor() != _queuePosition.floor();
          _queuePosition = seconds;
          _queuePositionEntry = _albumQueue[_queueIndex].id;
          if (changed) persistMusicQueue?.call();
        }
      }
    });
  }

  Future<void> retryMusicTrack() {
    final delta = _failedMusicDelta ?? 0;
    return _moveTrack(delta,
        resumePosition: delta == 0 ? _retryMusicPosition : null);
  }

  void _publishMusic({bool loading = false, Object? error}) {
    if (_musicIndex < 0) return;
    final state = MusicSequenceState(
        request: _musicEntries[_musicIndex],
        index: _musicIndex,
        length: _musicEntries.length,
        sourceGeneration: _sourceGeneration,
        loading: loading,
        error: error);
    _musicState = state;
    _musicChanges.add(state);
    if (_queueActive) {
      if (_queueIndex != _musicIndex) {
        _queuePosition = 0;
        _queuePositionEntry = null;
      }
      _queueIndex = _musicIndex;
      _queueNextIds.removeAll(
          _albumQueue.take(_queueIndex + 1).map((entry) => entry.id));
      if (error != null) {
        _queueStatus = 'Could not play track. Tap the track to retry.';
      }
    }
    _notifyQueue();
  }

  Future<void> _completeTrack(AudioProgressSession progress) async {
    if (identical(_completingProgress, progress)) return;
    _completingProgress = progress;
    final generation = _sourceGeneration, cycle = _completionCycle;
    final sleepIntent = _sourceSleepIntent;
    try {
      await progress.record(force: true, completed: true);
      if (!sleepTimer.allowsPlayback(sleepIntent) ||
          cycle != _completionCycle ||
          generation != _sourceGeneration ||
          !identical(progress, _progress) ||
          !_canControlSource) {
        return;
      }
      if (_musicIndex >= 0) await _moveTrack(1, completed: true);
    } finally {
      if (identical(_completingProgress, progress)) _completingProgress = null;
    }
  }

  Future<void> _moveTrack(int delta,
      {Duration? resumePosition,
      bool completed = false,
      int? playbackIntent}) async {
    if (_movingTrack || !_canControlSource || _musicIndex < 0) return;
    final repeatOne =
        _queueActive && completed && _queueRepeat == MusicRepeat.one;
    final queueWrap = _queueActive && _queueRepeat == MusicRepeat.all;
    var target = repeatOne ? _musicIndex : _musicIndex + delta;
    if (queueWrap) target %= _musicEntries.length;
    if (completed && _queueActive && (repeatOne || target == 0)) {
      resumePosition = Duration.zero;
    }
    // Manual boundary controls are no-ops: keep the source, playback and its
    // progress session. Natural completion still flushes and stops below.
    if (!_musicWrap &&
        !queueWrap &&
        !completed &&
        (target < 0 || target >= _musicEntries.length)) {
      return;
    }
    // A deliberate Next/Previous/queue selection is a new playback intent.
    // Completion keeps the current policy, and later pause/noisy/focus events
    // invalidate this transition even while its source loader is pending.
    if (!completed) headset.userPlaybackIntent();
    final transport = ++_transportRevision;
    final sleepIntent = playbackIntent ??
        (completed ? _sourceSleepIntent : sleepTimer.beginPlaybackIntent());
    if (delta < 0) _invalidateQueueNext();
    _collectionEnded = false;
    _failedMusicDelta = delta;
    if (delta != 0) _retryMusicPosition = null;
    _movingTrack = true;
    _gapless.invalidate();
    final pauseForTransition =
        _gapless.playlist != null ? _player.pause() : null;
    final generation = ++_sourceGeneration;
    if (!(!_musicWrap &&
        !queueWrap &&
        completed &&
        (target < 0 || target >= _musicEntries.length))) {
      clearMediaItem();
    }
    final owns = _ownsSource;
    bool current() => generation == _sourceGeneration && owns?.call() != false;
    _controlsEnabled = false;
    try {
      await endProgress();
      if (!current()) return;
      await (pauseForTransition ?? _player.pause());
      _publishMusic(loading: true);
      if (!_musicWrap &&
          !queueWrap &&
          (target < 0 || target >= _musicEntries.length)) {
        if (completed &&
            delta > 0 &&
            _queueActive &&
            _queueRepeat == MusicRepeat.off) {
          final appended = await _appendAutoplayRelated(current);
          if (!current()) return;
          if (appended) {
            target = _queueIndex + 1;
            _musicEntries = _albumQueue.map((e) => e.request).toList();
            _musicIndex = _queueIndex;
          } else {
            _controlsEnabled = true;
            _collectionEnded = completed;
            _failedMusicDelta = null;
            _publishMusic();
            _broadcastState();
            return;
          }
        } else {
          _controlsEnabled = true;
          _collectionEnded = completed;
          _failedMusicDelta = null;
          _publishMusic();
          _broadcastState();
          return;
        }
      }
      if (target < 0 || target >= _musicEntries.length) {
        final old = _musicEntries[_musicIndex].identity;
        final expanded = await _expandMusic!();
        if (!current()) return;
        final base = expanded.indexWhere((request) => request.identity == old);
        if (expanded.isNotEmpty && base >= 0) {
          _musicEntries = List.unmodifiable(expanded);
          target = (base + delta) % expanded.length;
        } else {
          target = (target % _musicEntries.length);
        }
      }
      _musicIndex = target;
      _failedMusicDelta = 0;
      _publishMusic(loading: true);
      final request = _musicEntries[target];
      setMediaRequest(request);
      final source = await _musicLoader!(request, current);
      if (!current()) return;
      if (source == null) throw StateError('Playback preparation stopped');
      await loadSourceForSession(source, generation);
      if (!current()) return;
      double position = resumePosition?.inMilliseconds.toDouble() ?? -1;
      position = position >= 0 ? position / 1000 : (request.startPosition ?? 0);
      if (resumePosition == null &&
          request.startPosition == null &&
          request.readPosition != null) {
        try {
          position = await request.readPosition!() ?? 0;
        } catch (_) {}
      }
      if (!current()) return;
      await seekForSession(
          Duration(milliseconds: (position * 1000).round()), generation);
      if (!current()) return;
      setMediaRequest(request);
      startProgress(
          savePosition: request.savePosition!,
          saveListeningPosition: request.saveListeningPosition,
          markFinished: request.markFinished!);
      _musicPosition = Duration(milliseconds: (position * 1000).round());
      _retryMusicPosition = null;
      _failedMusicDelta = null;
      _musicChanged?.call(request);
      activateSourceSession(generation);
      _publishMusic();
      _sourceSleepIntent = sleepIntent;
      // Loading already applied this track's gain. Start the decoder directly:
      // handler.play performs another asynchronous gain pass, whereas this
      // transition must establish the playing state before returning. The
      // native play Future lasts until pause/end and must remain unawaited.
      sleepTimer.check();
      if (current() &&
          _canControlSource &&
          transport == _transportRevision &&
          headset.allowsAutomaticPlayback &&
          sleepTimer.allowsPlayback(sleepIntent)) {
        ++_completionCycle;
        unawaited(_player.play().catchError(_onPlaybackError));
      }
    } catch (error) {
      if (current()) {
        _controlsEnabled = true;
        _onPlaybackError(error);
      }
    } finally {
      _movingTrack = false;
      _notifyQueue();
    }
  }

  AudioProgressSession? _progress;
  int _stopGeneration = 0;
  int _sourceGeneration = 0;
  bool _controlsEnabled = false;
  bool Function()? _ownsSource;
  Future<void>? _sourceOperationTail;
  final _playbackErrors = StreamController<Object>.broadcast();

  /// Failures reported by the native player: undecodable sources, lost
  /// connections, and errors from starting playback. The player screen
  /// listens once its source is loaded to replace the controls with a
  /// readable message; load failures are thrown to the loader as well.
  Stream<Object> get playbackErrors => _playbackErrors.stream;

  /// Changes immediately when playback is stopped (including logout).
  /// Pending screen setup must not restart playback from an older generation.
  int get stopGeneration => _stopGeneration;

  // ---------------------------------------------------------------------------
  // Public accessors (used by AudioPlayerScreen to avoid a second Player)
  // ---------------------------------------------------------------------------

  /// The underlying [AudioPlayer] so the screen can subscribe to position /
  /// duration streams and still use bearer-token authenticated sources.
  ///
  AudioPlayer get player => _player;

  /// Invalidates pending source work from the previous audio session.
  int beginSourceSession({bool Function()? ownsSource, int? sleepIntent}) {
    _liveSource = null;
    _liveReconnectNeeded = false;
    headset.userPlaybackIntent();
    _transportRevision++;
    _gapless.invalidate();
    _gapless.playlist = null;
    _gapless.transportIntent++;
    _gapless.status = _gapless.enabled
        ? 'Normal advance · open a Music queue for gapless.'
        : 'Gapless off.';
    _sourceSleepIntent = sleepIntent ?? sleepTimer.beginPlaybackIntent();
    clearMediaItem();
    ++_queueRecoveryGeneration;
    if (_queueBusy && _albumQueue.isEmpty) _queueStatus = '';
    _queueBusy = false;
    _queueRestoreFailed = false;
    _deactivateQueue();
    unawaited(_musicPositions?.cancel());
    _musicPositions = null;
    _failedMusicDelta = null;
    _retryMusicPosition = null;
    _musicEntries = const [];
    _musicIndex = -1;
    _collectionEnded = false;
    _musicState = null;
    _sequenceLease = null;
    _musicLoader = null;
    _expandMusic = null;
    _musicChanged = null;
    _controlsEnabled = false;
    _ownsSource = ownsSource;
    return ++_sourceGeneration;
  }

  /// Enables media controls only after loading and restoring this source.
  bool activateSourceSession(int generation) {
    if (generation != _sourceGeneration || _ownsSource?.call() == false) {
      return false;
    }
    _controlsEnabled = true;
    _refreshGapless();
    return true;
  }

  bool isSourceSessionCurrent(int generation) =>
      generation == _sourceGeneration && _canControlSource;

  AudioSource? _liveSource;
  bool _liveReconnectNeeded = false;

  bool get isLiveRadio => _metadataRequest is LiveRadioPlaybackRequest;

  bool get _canControlSource =>
      _controlsEnabled && _ownsSource?.call() != false;

  /// Loads only while [generation] still owns the shared player.
  Future<void> loadSourceForSession(
    AudioSource source,
    int generation,
  ) =>
      _serializeSourceOperation(() async {
        if (generation != _sourceGeneration) return;
        await _refreshLoudness();
        if (generation != _sourceGeneration) return;
        _gapless.invalidate();
        final playlist = _gapless.enabled
            ? ConcatenatingAudioSource(
                children: [source], useLazyPreparation: false)
            : null;
        _gapless.playlist = playlist;
        _gapless.currentIndex = 0;
        _gapless.children = [null];
        if (isLiveRadio) {
          await _player.setSpeed(1);
          if (generation != _sourceGeneration) return;
        }
        await _player.setAudioSource(playlist ?? source);
      });

  /// Loads a live source without replacing the original user playback intent.
  /// Deliberate pause/unplug/sleep during loading leaves usable paused controls.
  Future<void> loadLiveSourceForSession(
      AudioSource source, int generation, int transport) async {
    if (generation != _sourceGeneration || _ownsSource?.call() == false) return;
    _liveSource = source;
    try {
      await loadSourceForSession(source, generation);
    } catch (_) {
      if (generation == _sourceGeneration && transport != _transportRevision) {
        activateSourceSession(generation);
        return;
      }
      rethrow;
    }
    if (!activateSourceSession(generation)) return;
    if (transport != _transportRevision ||
        !headset.allowsAutomaticPlayback ||
        !sleepTimer.allowsPlayback(_sourceSleepIntent)) {
      return;
    }
    _liveReconnectNeeded = false;
    unawaited(play(automatic: true));
  }

  /// A delayed seek from an old screen cannot seek a replacement source.
  Future<bool> seekForSession(Duration position, int generation) {
    // The player slider and +/- controls use this entry point directly.
    // A current seek intent supersedes an end callback waiting on progress I/O.
    if (generation == _sourceGeneration) ++_completionCycle;
    return _serializeSourceOperation(() async {
      if (generation != _sourceGeneration || isLiveRadio) return false;
      await _player.seek(position);
      return generation == _sourceGeneration;
    });
  }

  /// Starts tracking the loaded item. Call after the source and resume seek
  /// complete. The callbacks capture account-scoped API/queue dependencies
  /// without making the media handler depend on HTTP or local persistence.
  String? _listeningOccurrenceID;

  /// Active personal listening identity, reusable across output handoffs.
  String? get listeningOccurrenceID =>
      _progress == null ? null : _listeningOccurrenceID;

  void startProgress({
    required Future<void> Function(double) savePosition,
    required Future<void> Function() markFinished,
    SaveListeningPosition? saveListeningPosition,
    String? listeningOccurrenceID,
  }) {
    if (_progress != null) {
      throw StateError('End the current audio progress session first');
    }
    final random = Random.secure();
    final occurrence = listeningOccurrenceID ??
        List.generate(16,
                (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'))
            .join();
    _listeningOccurrenceID = occurrence;
    _progress = AudioProgressSession(
      position: () => _player.position,
      duration: () => _player.duration,
      playing: () => _player.playing,
      savePosition: saveListeningPosition == null
          ? savePosition
          : (seconds) => saveListeningPosition(seconds, occurrence),
      markFinished: markFinished,
    );
  }

  /// Saves the current session without pausing background playback.
  Future<void> saveProgress() async {
    if (_queueActive &&
        _canControlSource &&
        !_movingTrack &&
        _queueIndex >= 0) {
      _queuePosition = _player.position.inMilliseconds / 1000;
      _queuePositionEntry = _albumQueue[_queueIndex].id;
      persistMusicQueue?.call();
    }
    await _progress?.record(force: true);
  }

  /// Flushes and detaches the current item before its source is replaced.
  Future<void> endProgress() async {
    final session = _progress;
    _progress = null;
    await session?.close();
  }

  // ---------------------------------------------------------------------------
  // BaseAudioHandler — playback controls
  // ---------------------------------------------------------------------------

  /// Starts playback. A failure is reported on [playbackErrors] instead of
  /// being thrown: callers (the screen, the notification, a headset button)
  /// do not await the outcome, so a thrown error would go unhandled.
  @override
  Future<void> play({bool automatic = false}) async {
    if (!automatic) headset.userPlaybackIntent();
    if (automatic && !headset.allowsAutomaticPlayback) return;
    final transport = ++_transportRevision;
    _gapless.transportIntent++;
    sleepTimer.check();
    final sleepIntent =
        automatic ? _sourceSleepIntent : sleepTimer.beginPlaybackIntent();
    if (!sleepTimer.allowsPlayback(sleepIntent)) return;
    if (!automatic) _sourceSleepIntent = sleepIntent;
    if (!_canControlSource) return;
    final generation = _sourceGeneration;
    if (isLiveRadio &&
        (!automatic || _liveReconnectNeeded) &&
        _liveSource != null &&
        !_player.playing) {
      try {
        await loadSourceForSession(_liveSource!, generation);
      } catch (error) {
        if (generation == _sourceGeneration &&
            transport == _transportRevision) {
          _onPlaybackError(error);
        }
        return;
      }
      if (generation == _sourceGeneration && transport == _transportRevision) {
        _liveReconnectNeeded = false;
      }
    }
    await _refreshLoudness();
    if (generation != _sourceGeneration ||
        !_canControlSource ||
        transport != _transportRevision) {
      return;
    }
    ++_completionCycle;
    // The visible Retry owns failed album transitions. Native Play must not
    // resume the previous source while the failed target is shown.
    if (!_musicWrap && _musicState?.error != null) return;
    try {
      // A completed album ends its old terminal progress session. Replaying
      // that source starts a fresh session; a deliberate seek back is kept.
      if (_collectionEnded && _musicIndex >= 0 && _progress == null) {
        _collectionEnded = false;
        final generation = _sourceGeneration;
        final total = _player.duration;
        if (total != null && _player.position >= total) {
          await seekForSession(Duration.zero, generation);
        }
        if (generation != _sourceGeneration || !_canControlSource) return;
        final request = _musicEntries[_musicIndex];
        startProgress(
            savePosition: request.savePosition!,
            saveListeningPosition: request.saveListeningPosition,
            markFinished: request.markFinished!);
      }
      if (!sleepTimer.allowsPlayback(sleepIntent) ||
          transport != _transportRevision) {
        return;
      }
      await _player.play();
    } catch (error) {
      _onPlaybackError(error);
    }
  }

  @override
  Future<void> pause({bool automatic = false}) async {
    if (!automatic) headset.suspend();
    _transportRevision++;
    _gapless.transportIntent++;
    if (isLiveRadio) {
      _liveReconnectNeeded = true;
      await _player.stop(); // Resume reconnects at the live edge.
    } else {
      await _player.pause();
    }
    await _progress?.record(force: true);
    if (_queueActive && _queueIndex >= 0) {
      _queuePosition = _player.position.inMilliseconds / 1000;
      _queuePositionEntry = _albumQueue[_queueIndex].id;
      persistMusicQueue?.call();
    }
  }

  @override
  Future<void> stop() async {
    _liveSource = null;
    _liveReconnectNeeded = false;
    headset.suspend();
    _transportRevision++;
    _gapless.invalidate();
    _gapless.playlist = null;
    _gapless.transportIntent++;
    clearMediaItem();
    _deactivateQueue();
    unawaited(_musicPositions?.cancel());
    _musicPositions = null;
    _failedMusicDelta = null;
    _retryMusicPosition = null;
    _musicEntries = const [];
    _musicIndex = -1;
    _collectionEnded = false;
    _musicState = null;
    _sequenceLease = null;
    _musicLoader = null;
    _expandMusic = null;
    _musicChanged = null;
    _controlsEnabled = false;
    _stopGeneration++;
    final generation = ++_sourceGeneration;
    await endProgress();
    // Stop immediately even when setAudioSource is still pending. The bumped
    // generation prevents that stale screen from seeking or starting playback
    // after the source operation finishes.
    if (generation != _sourceGeneration) return;
    await _player.stop();
    if (generation != _sourceGeneration) return;
    await super.stop();
  }

  /// Seeks to [position] within the current item.
  ///
  /// [SeekHandler] mixin provides the default [fastForward] / [rewind]
  /// implementations in terms of this method.
  @override
  Future<void> seek(Duration position) async {
    ++_completionCycle;
    if (_canControlSource) await seekForSession(position, _sourceGeneration);
  }

  Future<T> _serializeSourceOperation<T>(Future<T> Function() operation) {
    final previous = _sourceOperationTail;
    final release = Completer<void>();
    final tail = release.future;
    _sourceOperationTail = tail;
    return _runSourceOperation(previous, release, tail, operation);
  }

  Future<T> _runSourceOperation<T>(
    Future<void>? previous,
    Completer<void> release,
    Future<void> tail,
    Future<T> Function() operation,
  ) async {
    if (previous != null) await previous;
    try {
      return await operation();
    } finally {
      release.complete();
      if (identical(_sourceOperationTail, tail)) _sourceOperationTail = null;
    }
  }

  /// Skip forward 15 seconds (media-button "next" maps to a short skip for
  /// podcast and audiobook use-cases rather than a full track change).
  @override
  Future<void> skipToNext() async {
    if (isLiveRadio) return;
    if (_musicIndex >= 0) return _moveTrack(1);
    if (!_canControlSource) return;
    final current = _player.position;
    final total = _player.duration ?? Duration.zero;
    final next =
        _clamp(current + const Duration(seconds: 15), Duration.zero, total);
    await seek(next);
  }

  /// Skip back 15 seconds.
  @override
  Future<void> skipToPrevious() async {
    if (isLiveRadio) return;
    if (_musicIndex >= 0) return _moveTrack(-1);
    if (!_canControlSource) return;
    final current = _player.position;
    final total = _player.duration ?? Duration.zero;
    final prev =
        _clamp(current - const Duration(seconds: 15), Duration.zero, total);
    await seek(prev);
  }

  @override
  Future<void> fastForward() => _seekBy(const Duration(seconds: 15));

  @override
  Future<void> rewind() => _seekBy(const Duration(seconds: -15));

  Future<void> _seekBy(Duration delta) => seek(_clamp(_player.position + delta,
      Duration.zero, _player.duration ?? Duration.zero));

  /// Changes playback speed and refreshes the notification state.
  @override
  Future<void> setSpeed(double speed) async {
    if (!_canControlSource) return;
    await _player.setSpeed(speed);
    _broadcastState();
  }

  // ---------------------------------------------------------------------------
  // Media item helpers (called by AudioPlayerScreen after player is ready)
  // ---------------------------------------------------------------------------

  /// Pushes a new [MediaItem] onto the [mediaItem] stream so the Android media
  /// notification and lock-screen controls show the correct title and duration.
  ///
  /// Named [setMediaItem] rather than [updateMediaItem] to avoid clashing with
  /// [BaseAudioHandler.updateMediaItem] (which takes a full [MediaItem] and
  /// notifies children in a queue scenario — not applicable here).
  ///
  /// Called once per session after the player finishes loading the source.
  void setMediaItem({required String id, required String title}) {
    mediaItem.add(
      MediaItem(
        id: id,
        title: title,
        duration: _player.duration,
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Internal — state broadcast
  // ---------------------------------------------------------------------------

  /// Converts [just_audio]'s [PlaybackEvent] into [audio_service]'s
  /// [PlaybackState] and pushes it onto the [playbackState] stream.
  ///
  /// Called on every playback event so the notification and lock-screen
  /// controls always reflect the true player state.
  void _onPlaybackEvent(PlaybackEvent event) => _broadcastState();

  /// Marks the media session as failed, so the notification stops showing a
  /// playing item, and tells the screen (if one is listening).
  void _onPlaybackError(Object error) {
    _collectionEnded = false;
    if (_musicIndex >= 0) {
      // Runtime failures belong to the handler's current track even after
      // Back. A later Retry must not rebuild the original route's source.
      if (!_movingTrack) {
        _retryMusicPosition = _player.processingState == ProcessingState.ready
            ? _player.position
            : _musicPosition;
      }
      _publishMusic(error: error);
    }
    playbackState.add(
      playbackState.value.copyWith(
        processingState: AudioProcessingState.error,
        playing: false,
      ),
    );
    _playbackErrors.add(error);
  }

  /// Emits the current [PlaybackState] derived from the underlying player.
  ///
  /// Maps [just_audio]'s processing state to [audio_service]'s equivalents and
  /// advertises which controls are enabled so Android can render the correct
  /// notification buttons.
  void _broadcastState() {
    final live = isLiveRadio;
    final processingState = live &&
            _canControlSource &&
            _liveReconnectNeeded &&
            _player.processingState == ProcessingState.idle
        ? AudioProcessingState.ready
        : _mapProcessingState(_player.processingState);

    playbackState.add(
      PlaybackState(
        // Advertise all supported actions so the notification, lock screen,
        // and Bluetooth headset buttons all get the correct controls.
        controls: live
            ? [
                if (_player.playing) MediaControl.pause else MediaControl.play,
                MediaControl.stop,
              ]
            : [
                if (_musicIndex >= 0)
                  MediaControl.skipToPrevious
                else
                  MediaControl.rewind,
                if (_player.playing) MediaControl.pause else MediaControl.play,
                MediaControl.stop,
                if (_musicIndex < 0)
                  MediaControl.fastForward
                else
                  MediaControl.skipToNext,
              ],
        systemActions: live
            ? const {
                MediaAction.playFromMediaId,
                MediaAction.playFromSearch,
                MediaAction.skipToQueueItem,
              }
            : const {
                MediaAction.seek,
                MediaAction.seekForward,
                MediaAction.seekBackward,
                MediaAction.playFromMediaId,
                MediaAction.playFromSearch,
                MediaAction.skipToQueueItem,
              },
        androidCompactActionIndices:
            live ? const [0, 1] : const [0, 1, 3], // prev, play/pause, next
        processingState: processingState,
        playing: _player.playing,
        updatePosition: live ? Duration.zero : _player.position,
        bufferedPosition: live ? Duration.zero : _player.bufferedPosition,
        speed: _player.speed,
        queueIndex: _autoQueueIndex,
      ),
    );
  }

  /// Translates [just_audio]'s [ProcessingState] to [audio_service]'s
  /// [AudioProcessingState] so the notification can show loading spinners.
  AudioProcessingState _mapProcessingState(ProcessingState state) {
    switch (state) {
      case ProcessingState.idle:
        return AudioProcessingState.idle;
      case ProcessingState.loading:
        return AudioProcessingState.loading;
      case ProcessingState.buffering:
        return AudioProcessingState.buffering;
      case ProcessingState.ready:
        return AudioProcessingState.ready;
      case ProcessingState.completed:
        return AudioProcessingState.completed;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  /// Clamps [value] to [[min], [max]]; [Duration] doesn't implement [Comparable]
  /// so we do the comparison manually (mirrors VideoPlayerScreen pattern).
  Duration _clamp(Duration value, Duration min, Duration max) {
    if (value < min) return min;
    if (max > Duration.zero && value > max) return max;
    return value;
  }
}
