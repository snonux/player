import 'dart:async';
import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../api/music_presence_api.dart';
import '../providers/api_client_provider.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/cast_provider.dart';
import '../services/music_presence.dart';
import '../services/playback_request.dart';

/// Optional listeners view; observing never enables sharing.
class MusicPresencePanel extends ConsumerStatefulWidget {
  const MusicPresencePanel({super.key});
  @override
  ConsumerState<MusicPresencePanel> createState() => _MusicPresencePanelState();
}

class _MusicPresencePanelState extends ConsumerState<MusicPresencePanel> {
  late final MusicPresence _presence;
  Timer? _timer;
  StreamSubscription<PlaybackState>? _states;
  StreamSubscription<MediaItem?>? _items;
  String? _lastPlayback;

  @override
  void initState() {
    super.initState();
    // Capture the transport for an account; no request may silently move to a
    // newly configured server while an old client is being removed.
    final client = ref.read(apiClientProvider);
    _presence = MusicPresence(
        request: (method, user, epoch, body) async {
          if (method == 'GET') return client.musicListeners(user, epoch);
          if (method == 'PUT') {
            await client.publishMusicPresence(user, epoch, body!);
          } else {
            await client.removeMusicPresence(user, epoch,
                body!['client_id'] as String, body['sequence'] as int);
          }
          return null;
        },
        snapshot: _snapshot)
      ..addListener(_changed);
    final handler = ref.read(audioHandlerProvider);
    void changed() {
      final snapshot = _snapshot();
      final identity = '${snapshot?['media_id']}:${snapshot?['state']}';
      if (_lastPlayback != identity) {
        _lastPlayback = identity;
        unawaited(_presence.refresh());
      }
    }

    _states = handler.playbackState.listen((_) => changed());
    _items = handler.mediaItem.listen((_) => changed());
    _timer = Timer.periodic(
        const Duration(seconds: 10), (_) => unawaited(_presence.refresh()));
  }

  Map<String, dynamic>? _snapshot() {
    final handler = ref.read(audioHandlerProvider),
        request = ref.read(audioHandlerProvider).currentActivatedMediaRequest;
    final user = ref.read(authStateProvider).valueOrNull?.user?.id;
    final origin = ref.read(playerBaseUrlProvider).origin;
    final state = handler.playbackState.value;
    if (!ref.read(credentialMutationQueueProvider).credentialsEnabled ||
        request is! ServerPlaybackRequest ||
        request.userId != user ||
        request.serverOrigin != origin ||
        (ref.read(castControllerProvider).active != null ||
            ref.read(castControllerProvider).isStarting) ||
        state.processingState != AudioProcessingState.ready) {
      return null;
    }
    return {
      'media_id': request.mediaId,
      'state': state.playing ? 'playing' : 'paused',
      'position_seconds': state.position.inMilliseconds / 1000
    };
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final user = ref.watch(authStateProvider).valueOrNull?.user?.id;
    final origin = ref.watch(playerBaseUrlProvider).origin;
    final epoch = ref.watch(credentialMutationQueueProvider).generation;
    // The host must key this panel by account/origin/client. Rebuild clears
    // visible previous-account rows before any new asynchronous response.
    if (_account != '$origin:$user:$epoch') {
      _account = '$origin:$user:$epoch';
      _presence.removeListener(_changed);
      _presence.setAccount(origin, user, epoch);
      _presence.addListener(_changed);
    }
    return ExpansionTile(
        leading: const Icon(Icons.headphones_outlined),
        title: const Text('Listeners'),
        subtitle: Text(_presence.sharing
            ? 'Sharing my listening'
            : 'See who is listening · sharing off'),
        onExpansionChanged: _presence.setOpen,
        children: [
          SwitchListTile(
              title: const Text('Share my listening'),
              value: _presence.sharing,
              onChanged: user == null ? null : _presence.setSharing),
          const Padding(
              padding: EdgeInsets.all(16),
              child: Text(
                  'Off by default. Sharing shows your username, client and current song to signed-in people who can access that song. Local server-library music only; downloads, public shares, radio and remote output are excluded.')),
          if (_presence.message.isNotEmpty)
            ListTile(
                title: Text(_presence.message),
                trailing: _presence.message.contains('unavailable')
                    ? TextButton(
                        onPressed: _presence.refresh,
                        child: const Text('Retry'))
                    : null),
          if (_presence.open &&
              _presence.message.isEmpty &&
              _presence.rows.isEmpty)
            const ListTile(title: Text('No listeners sharing right now.')),
          for (final row in _presence.rows)
            ListTile(
                title: Text(
                    '${row['username']} · ${row['client']} · ${row['state'] == 'playing' ? 'Playing' : 'Paused'}'),
                subtitle: Text(
                    '${row['title']}\n${row['artist'] ?? ''}\n${_time(row['position_seconds'])} / ${_time(row['duration'])}')),
        ]);
  }

  String? _account;

  @override
  void dispose() {
    _timer?.cancel();
    unawaited(_states?.cancel());
    unawaited(_items?.cancel());
    _presence.removeListener(_changed);
    _presence.dispose();
    super.dispose();
  }
}

String _time(dynamic seconds) {
  final value = seconds is num && seconds.isFinite
      ? seconds.floor().clamp(0, 2147483647)
      : 0;
  return '${value ~/ 60}:${(value % 60).toString().padLeft(2, '0')}';
}
