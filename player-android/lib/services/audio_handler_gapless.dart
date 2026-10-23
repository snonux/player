part of 'audio_handler.dart';

class _PreparedNativeSong {
  _PreparedNativeSong(this.entry, this.outgoingDuration);
  final MusicQueueEntry entry;
  final Duration outgoingDuration;
}

class _NativeGaplessState {
  bool enabled = false, adjusting = false;
  int generation = 0,
      currentIndex = 0,
      transportIntent = 0,
      preferenceRevision = 0;
  String status = 'Gapless off.', token = '';
  ConcatenatingAudioSource? playlist;
  List<_PreparedNativeSong?> children = [];
  void invalidate() {
    generation++;
    token = '';
  }
}

/// Decoder preparation stays inside the audio owner; routes never own a
/// second player or make per-track source replacements at a natural boundary.
extension GaplessAudioHandler on PlayerAudioHandler {
  Future<void> setGaplessEnabled(bool value) async {
    _gapless.preferenceRevision++;
    _gapless.enabled = value;
    _gapless.status = value
        ? 'Gapless applies when you next open a Music queue song.'
        : 'Gapless off.';
    _refreshGapless();
    _queueChanges.add(null);
    try {
      final preferences = await SharedPreferences.getInstance();
      await preferences.setBool('gapless-music', value);
    } catch (_) {/* A session preference still works without persistence. */}
  }

  void _restoreGapless() {
    final revision = _gapless.preferenceRevision;
    unawaited(() async {
      try {
        final preferences = await SharedPreferences.getInstance();
        if (revision != _gapless.preferenceRevision) return;
        _gapless.enabled = preferences.getBool('gapless-music') ?? false;
        _gapless.status = _gapless.enabled
            ? 'Gapless applies when you open a Music queue song.'
            : 'Gapless off.';
        _queueChanges.add(null);
      } catch (_) {/* Playback does not depend on optional preferences. */}
    }());
  }

  MusicQueueEntry? _gaplessTarget() {
    if (!_gapless.enabled ||
        !_queueActive ||
        _musicWrap ||
        !_canControlSource ||
        _movingTrack ||
        _queueBusy ||
        _queueIndex < 0 ||
        _albumQueue.isEmpty) {
      return null;
    }
    var target =
        _queueRepeat == MusicRepeat.one ? _queueIndex : _queueIndex + 1;
    if (target >= _albumQueue.length && _queueRepeat == MusicRepeat.all) {
      target = 0;
    }
    if (target >= _albumQueue.length) return null;
    final entry = _albumQueue[target];
    // Resume-capable spoken/long items retain their ordinary loading policy.
    if (entry.request.startPosition != 0 ||
        entry.request.savePosition == null ||
        entry.request.markFinished == null ||
        RegExp(r'\.m4b$|(^|[^a-z])audio[\s_-]*books?([^a-z]|$)',
                caseSensitive: false)
            .hasMatch(entry.request.visualContext)) {
      return null;
    }
    return entry;
  }

  void _gaplessMessage(String value) {
    if (_gapless.status == value) return;
    _gapless.status = value;
    _queueChanges.add(null);
  }

  void _refreshGapless() {
    if (_gapless.adjusting) return;
    final playlist = _gapless.playlist;
    if (playlist == null) return;
    final entry = _gaplessTarget();
    final token = '$_sourceGeneration:$_queueScope:'
        '${_queueIndex >= 0 && _queueIndex < _albumQueue.length ? _albumQueue[_queueIndex].id : -1}:'
        '${entry?.id}:$_queueRepeat:${_gapless.enabled}';
    if (_gapless.token == token) return;
    _gapless.token = token;
    final preparation = ++_gapless.generation, source = _sourceGeneration;
    final intent = _gapless.transportIntent;
    bool current() =>
        preparation == _gapless.generation &&
        source == _sourceGeneration &&
        identical(playlist, _gapless.playlist) &&
        _ownsSource?.call() != false;
    unawaited(() async {
      try {
        // Queue edits remove the decoder's old successor before preparing a
        // new one. Pause/resume only if an already-buffered child must change.
        await _serializeSourceOperation(() async {
          if (!current()) return;
          final tail = _gapless.currentIndex + 1;
          if (playlist.children.length > tail) {
            final playing = _player.playing;
            await _player.pause();
            if (!current()) return;
            _gapless.adjusting = true;
            try {
              _gapless.children.removeRange(tail, _gapless.children.length);
              await playlist.removeRange(tail, playlist.children.length);
            } finally {
              _gapless.adjusting = false;
            }
            if (playing &&
                current() &&
                _canControlSource &&
                intent == _gapless.transportIntent &&
                sleepTimer.allowsPlayback(_sourceSleepIntent)) {
              unawaited(_player.play().catchError(_onPlaybackError));
            }
          }
        });
        if (!current()) return;
        if (entry == null) {
          _gaplessMessage(_gapless.enabled
              ? 'Normal advance · final song or item requires resume.'
              : 'Gapless off.');
          return;
        }
        final prepare = prepareGaplessSource;
        if (prepare == null) {
          _gaplessMessage('Normal advance · decoder preparation unavailable.');
          return;
        }
        _gaplessMessage('Gapless · preparing next song…');
        final audio = await prepare(entry.request, current);
        if (!current()) return;
        if (audio == null) {
          _gaplessMessage(
              'Next song uses normal advance · preparation unavailable.');
          return;
        }
        await _serializeSourceOperation(() async {
          if (!current() || _gaplessTarget()?.id != entry.id) return;
          _gapless.children.add(
              _PreparedNativeSong(entry, _player.duration ?? Duration.zero));
          try {
            await playlist.add(audio);
          } catch (_) {
            if (_gapless.children.length > playlist.children.length) {
              _gapless.children.removeLast();
            }
            rethrow;
          }
          if (current()) {
            _gaplessMessage('Gapless ready · native decoder playlist.');
          }
        });
      } catch (_) {
        if (current()) {
          _gaplessMessage(
              'Next song uses normal advance · preparation failed.');
        }
      }
    }());
  }

  void _onGaplessIndex(int? index) {
    if (index == null ||
        _gapless.adjusting ||
        index == _gapless.currentIndex ||
        index < 0 ||
        index >= _gapless.children.length ||
        _gapless.playlist == null ||
        !_canControlSource ||
        _movingTrack) {
      return;
    }
    final prepared = _gapless.children[index];
    if (prepared == null) return;
    final target = _gaplessTarget();
    if (target?.id != prepared.entry.id ||
        !sleepTimer.allowsPlayback(_sourceSleepIntent)) {
      unawaited(_player.pause());
      _gapless.invalidate();
      _gaplessMessage('Queue changed · choose a track to continue.');
      return;
    }
    final previous = _progress;
    _progress = null;
    // Snapshot outgoing duration before installing the next reporting session;
    // the native player already reports the successor's position here.
    unawaited(previous?.close(
        completed: true, finalPosition: prepared.outgoingDuration));
    _gapless.currentIndex = index;
    _sourceGeneration++;
    _musicEntries = _albumQueue.map((entry) => entry.request).toList();
    _musicIndex = _albumQueue.indexWhere((entry) => entry.id == target!.id);
    _queueIndex = _musicIndex;
    _queuePosition = 0;
    _queuePositionEntry = target!.id;
    _musicPosition = _player.position;
    _collectionEnded = false;
    setMediaRequest(target.request);
    _musicChanged?.call(target.request);
    startProgress(
        savePosition: target.request.savePosition!,
        saveListeningPosition: target.request.saveListeningPosition,
        markFinished: target.request.markFinished!);
    _gapless.invalidate();
    // Discard completed native children so a long queue does not retain an
    // unbounded graph. Suppress the index renumbering event during removal.
    final playlist = _gapless.playlist!, generation = _sourceGeneration;
    _gapless.adjusting = true;
    _publishMusic();
    _broadcastState();
    unawaited(_serializeSourceOperation(() async {
      try {
        if (generation != _sourceGeneration ||
            !identical(playlist, _gapless.playlist)) {
          return;
        }
        _gapless.children.removeRange(0, index);
        await playlist.removeRange(0, index);
        _gapless.currentIndex = 0;
      } catch (_) {
        _gaplessMessage('Normal advance · decoder queue update failed.');
      } finally {
        _gapless.adjusting = false;
        if (generation == _sourceGeneration) _refreshGapless();
      }
    }));
  }
}
