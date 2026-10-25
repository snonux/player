import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/global_sync_provider.dart';
import '../services/global_sync_coordinator.dart';
import 'podcast_association.dart';

String globalSyncMessage(GlobalSyncState state, int nowMs) {
  if (!state.configured) return '';
  final parts = <String>[];
  if (state.active) parts.add('Syncing…');
  if (!state.active && state.errorKind != null) {
    parts.add(switch (state.errorKind) {
      'auth' => 'Sign in again to sync',
      'rejected' => 'Some changes could not sync',
      'unsupported' => 'Some sync features need a newer server',
      'policy' => 'Sync waits for Wi-Fi',
      'conflict' => 'Choose a saved session in Now Playing',
      _ => 'Sync unavailable',
    });
  }
  if (state.neverSynced) {
    parts.add('Not synced yet');
  } else if (state.staleAt(nowMs)) {
    final days =
        (nowMs - state.lastFullSuccessMs!) ~/ Duration.millisecondsPerDay;
    parts.add('Last synced $days days ago');
  } else if (!state.active && state.errorKind == null) {
    parts.add('Synced');
  }
  if (state.pending > 0) {
    parts.add('${state.pending} pending · changes saved on this device');
  }
  return parts.join(' · ');
}

/// Pure presentation, also used by focused widget checks.
class GlobalSyncStatus extends StatelessWidget {
  const GlobalSyncStatus(
      {super.key,
      required this.state,
      required this.nowMs,
      required this.onSync});
  final GlobalSyncState state;
  final int nowMs;
  final VoidCallback onSync;
  @override
  Widget build(BuildContext context) {
    if (!state.configured) return const SizedBox.shrink();
    return Material(
      color: Theme.of(context).colorScheme.surface,
      child: SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
            child: Row(children: [
              Expanded(
                  child: Text(globalSyncMessage(state, nowMs),
                      style: Theme.of(context).textTheme.bodySmall)),
              TextButton(
                  onPressed:
                      state.active || state.errorKind == 'auth' ? null : onSync,
                  child: Text(state.active
                      ? 'Syncing…'
                      : state.errorKind == 'auth'
                          ? 'Sign in required'
                          : state.errorKind != null || state.staleAt(nowMs)
                              ? 'Retry'
                              : 'Sync now')),
            ]),
          )),
    );
  }
}

/// Global chrome remains available while playback and navigation continue.
class GlobalSyncChrome extends ConsumerStatefulWidget {
  const GlobalSyncChrome({super.key, required this.child});
  final Widget child;
  @override
  ConsumerState<GlobalSyncChrome> createState() => _GlobalSyncChromeState();
}

class _GlobalSyncChromeState extends ConsumerState<GlobalSyncChrome> {
  Timer? _clock;
  @override
  void initState() {
    super.initState();
    _clock = Timer.periodic(const Duration(minutes: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _clock?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final sync = ref.watch(globalSyncCoordinatorProvider);
    return Column(children: [
      Expanded(child: PodcastAssociationHost(child: widget.child)),
      GlobalSyncStatus(
          state: sync.state,
          nowMs: DateTime.now().millisecondsSinceEpoch,
          onSync: () => unawaited(sync.syncNow())),
    ]);
  }
}
