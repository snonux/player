import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:just_audio/just_audio.dart';

import 'audio_progress_session.dart';
import 'music_sequence.dart';
import 'music_queue.dart';
import 'music_queue_snapshot.dart';
import 'playback_session_coordinator.dart';
import 'playback_request.dart';

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
  PlayerAudioHandler(AudioPlayer player) : _player = player {
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
  List<PlaybackRequest> _musicEntries = const [];
  int _musicIndex = -1;
  bool _movingTrack = false;
  bool _musicWrap = true;
  bool _collectionEnded = false;
  AudioProgressSession? _completingProgress;
  int _completionCycle = 0;
  MusicRepeat _queueRepeat = MusicRepeat.off;
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
  MusicQueueState get musicQueue => MusicQueueState(
      entries: List.unmodifiable(_albumQueue),
      index: _queueIndex,
      label: _queueLabel,
      active: _queueActive && _canControlSource,
      busy: _queueBusy || (_queueActive && _movingTrack),
      canUndo: _queueUndo != null,
      status: _queueStatus,
      repeat: _queueRepeat,
      upNextIds: Set.unmodifiable(_queueNextIds),
      restored: _queueRestored,
      restoreFailed: _queueRestoreFailed,
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
      repeat: _queueRepeat);
  void _notifyQueue() {
    persistMusicQueue?.call();
    _queueChanges.add(null);
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
      MusicRepeat repeat = MusicRepeat.off}) {
    _albumQueue = entries;
    _queueRepeat = repeat;
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
    _queueRepeat = MusicRepeat
        .values[(_queueRepeat.index + 1) % MusicRepeat.values.length];
    _notifyQueue();
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
    ++_queueScope;
    _queueRepeat = MusicRepeat.off;
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
        beginSourceSession(ownsSource: () => lease.isCurrent);
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
      await _moveTrack(index - _musicIndex, resumePosition: resumePosition);
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
  }

  void _syncQueueSequence() {
    if (_queueActive) {
      _musicEntries = _albumQueue.map((e) => e.request).toList();
      _musicIndex = _queueIndex;
      _publishMusic();
    }
    _notifyQueue();
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
    _rememberQueue();
    _invalidateQueueNext();
    final current = _albumQueue[_queueIndex];
    final entry = _albumQueue.removeAt(index);
    _albumQueue.insert(target, entry);
    _queueIndex = _albumQueue.indexOf(current);
    _queueStatus = 'Track moved. Undo available.';
    _syncQueueSequence();
  }

  Future<void> removeQueue(int index) async {
    if (musicQueue.busy || index < 0 || index >= _albumQueue.length) return;
    final old = List<MusicQueueEntry>.of(_albumQueue), oldIndex = _queueIndex;
    final oldNext = Set<int>.of(_queueNextIds), oldLabel = _queueLabel;
    final oldEpoch = _queueNextEpoch;
    _invalidateQueueNext();
    final current = _albumQueue[_queueIndex];
    final operation = ++_queueOperation;
    _queueBusy = true;
    try {
      if (index == _queueIndex && _queueActive) await stop();
      if (operation != _queueOperation) return;
      _albumQueue.removeAt(index);
      _queueIndex = index == oldIndex
          ? (index < _albumQueue.length ? index : _albumQueue.length - 1)
          : _albumQueue.indexOf(current);
      _queueUndo = old;
      _queueUndoIndex = oldIndex;
      _queueUndoNextIds = oldNext;
      _queueUndoLabel = oldLabel;
      _queueUndoNextEpoch = oldEpoch;
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
    final old = List<MusicQueueEntry>.of(_albumQueue), oldIndex = _queueIndex;
    final oldNext = Set<int>.of(_queueNextIds), oldLabel = _queueLabel;
    final oldEpoch = _queueNextEpoch;
    _invalidateQueueNext();
    final operation = ++_queueOperation;
    _queueBusy = true;
    try {
      if (_queueActive) await stop();
      if (operation != _queueOperation) return;
      _albumQueue = [];
      _queueIndex = -1;
      _queueRestored = false;
      _queueUndo = old;
      _queueUndoIndex = oldIndex;
      _queueUndoNextIds = oldNext;
      _queueUndoLabel = oldLabel;
      _queueUndoNextEpoch = oldEpoch;
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
    final added = entries
        .map((request) => MusicQueueEntry(++_queueSerial, request))
        .toList();
    if (replace) {
      _albumQueue.clear();
      _queueIndex = -1;
      _queueLabel = sourceLabel;
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
    try {
      await progress.record(force: true, completed: true);
      if (cycle != _completionCycle ||
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
      {Duration? resumePosition, bool completed = false}) async {
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
    if (delta < 0) _invalidateQueueNext();
    _collectionEnded = false;
    _failedMusicDelta = delta;
    if (delta != 0) _retryMusicPosition = null;
    _movingTrack = true;
    final generation = ++_sourceGeneration;
    final owns = _ownsSource;
    bool current() => generation == _sourceGeneration && owns?.call() != false;
    _controlsEnabled = false;
    try {
      await endProgress();
      if (!current()) return;
      await _player.pause();
      _publishMusic(loading: true);
      if (!_musicWrap &&
          !queueWrap &&
          (target < 0 || target >= _musicEntries.length)) {
        _controlsEnabled = true;
        _collectionEnded = completed;
        _failedMusicDelta = null;
        _publishMusic();
        _broadcastState();
        return;
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
      setMediaItem(id: request.identity, title: request.title);
      startProgress(
          savePosition: request.savePosition!,
          markFinished: request.markFinished!);
      _musicPosition = Duration(milliseconds: (position * 1000).round());
      _retryMusicPosition = null;
      _failedMusicDelta = null;
      _musicChanged?.call(request);
      activateSourceSession(generation);
      _publishMusic();
      unawaited(play());
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
  int beginSourceSession({bool Function()? ownsSource}) {
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
    return true;
  }

  bool isSourceSessionCurrent(int generation) =>
      generation == _sourceGeneration && _canControlSource;

  bool get _canControlSource =>
      _controlsEnabled && _ownsSource?.call() != false;

  /// Loads only while [generation] still owns the shared player.
  Future<void> loadSourceForSession(
    AudioSource source,
    int generation,
  ) =>
      _serializeSourceOperation(() async {
        if (generation != _sourceGeneration) return;
        await _player.setAudioSource(source);
      });

  /// A delayed seek from an old screen cannot seek a replacement source.
  Future<bool> seekForSession(Duration position, int generation) {
    // The player slider and +/- controls use this entry point directly.
    // A current seek intent supersedes an end callback waiting on progress I/O.
    if (generation == _sourceGeneration) ++_completionCycle;
    return _serializeSourceOperation(() async {
      if (generation != _sourceGeneration) return false;
      await _player.seek(position);
      return generation == _sourceGeneration;
    });
  }

  /// Starts tracking the loaded item. Call after the source and resume seek
  /// complete. The callbacks capture account-scoped API/queue dependencies
  /// without making the media handler depend on HTTP or local persistence.
  void startProgress({
    required Future<void> Function(double) savePosition,
    required Future<void> Function() markFinished,
  }) {
    if (_progress != null) {
      throw StateError('End the current audio progress session first');
    }
    _progress = AudioProgressSession(
      position: () => _player.position,
      duration: () => _player.duration,
      playing: () => _player.playing,
      savePosition: savePosition,
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
  Future<void> play() async {
    if (!_canControlSource) return;
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
            markFinished: request.markFinished!);
      }
      await _player.play();
    } catch (error) {
      _onPlaybackError(error);
    }
  }

  @override
  Future<void> pause() async {
    await _player.pause();
    await _progress?.record(force: true);
    if (_queueActive && _queueIndex >= 0) {
      _queuePosition = _player.position.inMilliseconds / 1000;
      _queuePositionEntry = _albumQueue[_queueIndex].id;
      persistMusicQueue?.call();
    }
  }

  @override
  Future<void> stop() async {
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
    final processingState = _mapProcessingState(_player.processingState);

    playbackState.add(
      PlaybackState(
        // Advertise all supported actions so the notification, lock screen,
        // and Bluetooth headset buttons all get the correct controls.
        controls: [
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
        systemActions: const {
          MediaAction.seek,
          MediaAction.seekForward,
          MediaAction.seekBackward,
        },
        androidCompactActionIndices: const [0, 1, 3], // prev, play/pause, next
        processingState: processingState,
        playing: _player.playing,
        updatePosition: _player.position,
        bufferedPosition: _player.bufferedPosition,
        speed: _player.speed,
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
