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

/// True when the footer has something to report beyond "Synced"; a healthy
/// sync stays out of the way and Sync now lives in the library menu.
bool globalSyncNeedsAttention(GlobalSyncState state, int nowMs) =>
    state.configured &&
    (state.active ||
        state.errorKind != null ||
        state.neverSynced ||
        state.staleAt(nowMs) ||
        state.pending > 0);

/// Label for the manual sync action in the footer and the library menu.
String globalSyncActionLabel(GlobalSyncState state, int nowMs) => state.active
    ? 'Syncing…'
    : state.errorKind == 'auth'
        ? 'Sign in required'
        : state.errorKind != null || state.staleAt(nowMs)
            ? 'Retry'
            : 'Sync now';

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
    if (!globalSyncNeedsAttention(state, nowMs)) {
      return const SizedBox.shrink();
    }
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
                  child: Text(globalSyncActionLabel(state, nowMs))),
            ]),
          )),
    );
  }
}

/// Library menu row: shows the sync status and runs a manual sync.
class GlobalSyncMenuTile extends ConsumerWidget {
  const GlobalSyncMenuTile({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sync = ref.watch(globalSyncCoordinatorProvider);
    final state = sync.state;
    if (!state.configured) return const SizedBox.shrink();
    final now = DateTime.now().millisecondsSinceEpoch;
    return ListTile(
      key: const Key('global_sync_menu_tile'),
      leading: const Icon(Icons.sync),
      title: Text(globalSyncActionLabel(state, now)),
      subtitle: Text(globalSyncMessage(state, now)),
      enabled: !state.active && state.errorKind != 'auth',
      onTap: () => unawaited(sync.syncNow()),
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
    // Keep footer semantics off the library subtree — a merged Column node
    // otherwise reports "Not synced yet"/"Synced" over the full screen.
    return Semantics(
      explicitChildNodes: true,
      child: Column(children: [
        Expanded(child: PodcastAssociationHost(child: widget.child)),
        Semantics(
          container: true,
          explicitChildNodes: true,
          child: GlobalSyncStatus(
              state: sync.state,
              nowMs: DateTime.now().millisecondsSinceEpoch,
              onSync: () => unawaited(sync.syncNow())),
        ),
      ]),
    );
  }
}
