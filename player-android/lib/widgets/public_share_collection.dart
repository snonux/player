import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import '../models/media.dart';
import 'share_quality_picker.dart';
import '../providers/headset_preferences_provider.dart';
import '../services/headset_policy.dart';
import '../providers/playback_session_provider.dart';
import '../providers/public_api_client_provider.dart';
import '../services/playback_preparer.dart';
import '../services/playback_request.dart';
import '../services/playback_session_coordinator.dart';

/// Guest occurrences carry only public metadata and token-scoped URLs.
class PublicShareItem {
  const PublicShareItem(this.title, this.path, this.unavailable);
  final String title, path;
  final bool unavailable;
  factory PublicShareItem.fromJson(Map<String, dynamic> map) => PublicShareItem(
      Media.fromJson(map['media'] as Map<String, dynamic>? ?? {}).displayTitle,
      (map['playback_url'] ?? map['stream_url'] ?? '') as String,
      map['unavailable'] == true);
}

/// A page-local anonymous queue; never reads account credentials or progress.
class PublicShareCollection extends ConsumerStatefulWidget {
  const PublicShareCollection(
      {super.key,
      required this.token,
      required this.title,
      required this.items,
      this.description = '',
      this.audioQuality = '',
      this.player});
  final String token, title;
  final String description;
  final String audioQuality;
  final AudioPlayer? player;
  final List<PublicShareItem> items;
  @override
  ConsumerState<PublicShareCollection> createState() => _CollectionState();
}

class _CollectionState extends ConsumerState<PublicShareCollection> {
  late final _player = widget.player ?? AudioPlayer(handleInterruptions: false);
  late final HeadsetPolicy _headset;
  late final Future<void> _headsetReady;
  int _transport = 0;
  StreamSubscription<PlayerState>? _states;
  StreamSubscription<PlaybackEvent>? _events;
  PlaybackSessionLease? _lease;
  PlaybackPreparation? _preparation;
  int _index = -1, _generation = 0;
  bool _busy = false;
  String? _message;

  @override
  void initState() {
    super.initState();
    _headset = HeadsetPolicy(
      preferences: ref.read(headsetPreferencesProvider),
      isPlaying: () => _player.playing,
      canResume: () =>
          mounted &&
          _lease?.isCurrent == true &&
          !_busy &&
          _message == null &&
          _player.processingState == ProcessingState.ready,
      pausePlayback: () => _pause(automatic: true),
      resumePlayback: () => _play(automatic: true),
      volume: () => _player.volume,
      setVolume: _player.setVolume,
    );
    _headsetReady = ref.read(headsetAudioSessionProvider).then((session) {
      if (mounted) _headset.attach(session);
    });
    _events = _player.playbackEventStream.listen((_) {},
        onError: (Object _, StackTrace __) {
      if (mounted) {
        setState(() {
          _busy = false;
          _message =
              'Playback interrupted. Check your connection or link expiry, then select a song to retry.';
        });
      }
    });
    _states = _player.playerStateStream.listen((state) {
      if (mounted) setState(() {});
      if (state.processingState == ProcessingState.completed &&
          !_busy &&
          _lease?.isCurrent == true) {
        final next = _adjacent(1);
        if (next >= 0) {
          unawaited(_select(next, automatic: true));
        }
      }
    });
  }

  int _adjacent(int direction) {
    for (var i = _index + direction;
        i >= 0 && i < widget.items.length;
        i += direction) {
      if (!widget.items[i].unavailable) return i;
    }
    return -1;
  }

  Future<void> _select(int index, {bool automatic = false}) async {
    if (automatic && !_headset.allowsAutomaticPlayback) return;
    if (_busy || index < 0 || widget.items[index].unavailable) return;
    _headset.cancelResume();
    if (!automatic) _headset.userPlaybackIntent();
    ++_transport;
    final operation = ++_generation;
    _preparation?.cancel();
    setState(() {
      _index = index;
      _busy = true;
      _message = 'Loading song…';
    });
    try {
      await _headsetReady;
      if (!mounted || operation != _generation) return;
      final client = ref.read(publicApiClientProvider);
      final path = Uri.parse(widget.items[index].path);
      // Never pass an arbitrary server-supplied URL to the native player.
      final prefix = '/s/${widget.token}/items/$index/';
      if (path.hasAuthority ||
          path.hasScheme ||
          !path.path.startsWith(prefix) ||
          !['stream', 'compat'].contains(path.path.substring(prefix.length))) {
        throw StateError('Invalid public stream');
      }
      final uri = Uri.parse('${client.baseUrl}$path');
      final coordinator = ref.read(playbackSessionCoordinatorProvider);
      final prior = _lease;
      _lease = null;
      if (prior != null) await prior.stop();
      if (!mounted || operation != _generation) return;
      final lease = await coordinator.claim(
          kind: PlaybackSourceKind.publicShare,
          identity: 'share:${widget.token}:$index',
          sourceUri: uri.toString(),
          stop: () async {
            final externallyStopped = _lease != null;
            _headset.cancelResume();
            if (externallyStopped) {
              _headset.suspend();
              ++_transport;
              ++_generation;
            }
            _lease = null;
            _preparation?.cancel();
            await _player.stop();
            if (mounted && externallyStopped) {
              setState(() {
                _busy = false;
              });
            }
          });
      if (lease == null || !mounted || operation != _generation) {
        if (lease != null) await lease.release();
        return;
      }
      _lease = lease;
      bool current() => mounted && operation == _generation && lease.isCurrent;
      _preparation = const PlaybackPreparer()
          .prepare(uri, probe: client.probePlayback, stillWanted: current);
      if (!await _preparation!.ready || !current()) return;
      await _player.setUrl(uri.toString());
      if (!current()) return;
      setState(() {
        _busy = false;
        _message = null;
      });
      unawaited(_play(automatic: true));
    } catch (_) {
      if (mounted && operation == _generation) {
        setState(() {
          _busy = false;
          _message =
              'Song unavailable. Check your connection or whether the link expired, then select a song to retry.';
        });
      }
    }
  }

  Future<void> _pause({bool automatic = false}) async {
    if (!automatic) _headset.suspend();
    ++_transport;
    await _player.pause();
  }

  Future<void> _play({bool automatic = false}) async {
    if (!automatic) _headset.userPlaybackIntent();
    final transport = ++_transport;
    final generation = _generation;
    bool current() =>
        mounted &&
        _lease?.isCurrent == true &&
        generation == _generation &&
        transport == _transport &&
        _headset.allowsAutomaticPlayback;
    if (!current()) return;
    try {
      if (_player.processingState == ProcessingState.completed) {
        await _player.seek(Duration.zero);
      }
      if (!current()) return;
      await _player.play();
    } catch (_) {
      if (current()) {
        setState(() => _message = 'Could not play song. Select it to retry.');
      }
    }
  }

  @override
  void dispose() {
    ++_generation;
    ++_transport;
    _headset.suspend();
    _headset.dispose();
    _preparation?.cancel();
    unawaited(_states?.cancel());
    unawaited(_events?.cancel());
    final lease = _lease;
    if (lease != null) unawaited(lease.release());
    unawaited(_player.dispose());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ListView(padding: const EdgeInsets.all(16), children: [
      Text(widget.title, style: Theme.of(context).textTheme.headlineSmall),
      if (widget.description.isNotEmpty) Text(widget.description),
      Text('Shared audio quality: ${shareQualityLabel(widget.audioQuality)}'),
      Text('${widget.items.length} songs · Shared collection'),
      if (_message != null)
        Padding(
            padding: const EdgeInsets.symmetric(vertical: 12),
            child: Semantics(liveRegion: true, child: Text(_message!))),
      if (widget.items.every((item) => item.unavailable))
        const Text('No songs are currently available.'),
      Wrap(spacing: 8, children: [
        IconButton(
            tooltip: 'Previous song',
            onPressed: _busy || _adjacent(-1) < 0
                ? null
                : () => _select(_adjacent(-1)),
            icon: const Icon(Icons.skip_previous)),
        FilledButton.icon(
            onPressed: _busy
                ? null
                : () async {
                    if (_index < 0 || _lease?.isCurrent != true) {
                      final first =
                          widget.items.indexWhere((item) => !item.unavailable);
                      if (first >= 0) await _select(first);
                    } else if (_player.playing) {
                      await _pause();
                    } else {
                      unawaited(_play());
                    }
                  },
            icon: Icon(_player.playing ? Icons.pause : Icons.play_arrow),
            label: Text(_player.playing ? 'Pause' : 'Play')),
        IconButton(
            tooltip: 'Next song',
            onPressed:
                _busy || _adjacent(1) < 0 ? null : () => _select(_adjacent(1)),
            icon: const Icon(Icons.skip_next)),
      ]),
      StreamBuilder<Duration>(
          stream: _player.positionStream,
          builder: (context, snapshot) {
            final max = (_player.duration?.inMilliseconds ?? 0).toDouble();
            return Slider(
                label: 'Playback position',
                min: 0,
                max: max > 0 ? max : 1,
                value: (snapshot.data?.inMilliseconds ?? 0)
                    .toDouble()
                    .clamp(0, max > 0 ? max : 1),
                onChanged: max > 0 && !_busy
                    ? (value) =>
                        _player.seek(Duration(milliseconds: value.round()))
                    : null);
          }),
      for (var i = 0; i < widget.items.length; i++)
        ListTile(
            leading: Text('${i + 1}'),
            selected: i == _index,
            title: Text(widget.items[i].title),
            subtitle:
                widget.items[i].unavailable ? const Text('Unavailable') : null,
            trailing: const Icon(Icons.play_arrow),
            onTap:
                widget.items[i].unavailable || _busy ? null : () => _select(i)),
    ]);
  }
}
