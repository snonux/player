import '../providers/auth_state_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../api/music_history_api.dart';
import '../providers/api_client_provider.dart';

/// A server-account history surface; device files and public shares are separate.
class ListeningHistorySettings extends ConsumerStatefulWidget {
  const ListeningHistorySettings({super.key});
  @override
  ConsumerState<ListeningHistorySettings> createState() =>
      _ListeningHistorySettingsState();
}

class _ListeningHistorySettingsState
    extends ConsumerState<ListeningHistorySettings> {
  bool busy = false;
  bool loaded = false;
  bool recording = true;
  int days = 365;
  int retentionRevision = 0;
  int? displayEpoch;
  int? beforeId;
  int? beforeMs;
  String? error;
  final events = <Map<String, dynamic>>[];

  int get epoch => ref.read(credentialMutationQueueProvider).generation;

  Future<void> load({bool more = false}) async {
    if (busy) return;
    setState(() {
      busy = true;
      error = null;
    });
    final expectedEpoch = epoch;
    final client = ref.read(apiClientProvider);
    try {
      final result = await client.listeningHistory(
          credentialEpoch: expectedEpoch,
          beforeId: more ? beforeId : null,
          beforeMs: more ? beforeMs : null);
      if (!mounted || epoch != expectedEpoch) return;
      final settings = result['settings'] as Map;
      setState(() {
        if (!more) events.clear();
        events.addAll((result['events'] as List)
            .map((e) => Map<String, dynamic>.from(e as Map)));
        recording = settings['recording'] as bool;
        days = settings['retention_days'] as int;
        beforeId = result['next_before_id'] as int?;
        beforeMs = result['next_before_ms'] as int?;
        loaded = true;
      });
    } catch (_) {
      if (mounted && epoch == expectedEpoch) {
        setState(() => error =
            'Could not load listening history. Connect to the server and retry.');
      }
    } finally {
      if (mounted && epoch == expectedEpoch) setState(() => busy = false);
    }
  }

  Future<void> configure(bool nextRecording, int nextDays,
      {int? confirmedEpoch}) async {
    final expectedEpoch = confirmedEpoch ?? epoch;
    if (busy || epoch != expectedEpoch) return;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await ref.read(apiClientProvider).configureListeningHistory(
          nextRecording, nextDays,
          credentialEpoch: expectedEpoch);
    } catch (_) {
      if (mounted && epoch == expectedEpoch) {
        setState(() {
          retentionRevision++;
          error =
              'Could not save history settings. Connect to the server and retry.';
        });
      }
      return;
    } finally {
      if (mounted && epoch == expectedEpoch) setState(() => busy = false);
    }
    if (mounted && epoch == expectedEpoch) await load();
  }

  Future<void> clear() async {
    final expectedEpoch = epoch;
    final accepted = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
                title: const Text('Clear listening history?'),
                content: const Text(
                    'This erases recorded history for this account on every device. Personal play counters and resume positions remain. This cannot be undone.'),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('Cancel')),
                  TextButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('Clear history'))
                ]));
    if (accepted != true || !mounted || epoch != expectedEpoch) return;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await ref
          .read(apiClientProvider)
          .clearListeningHistory(credentialEpoch: expectedEpoch);
    } catch (_) {
      if (mounted && epoch == expectedEpoch) {
        setState(() => error =
            'Could not clear history. Connect to the server and retry.');
      }
      return;
    } finally {
      if (mounted && epoch == expectedEpoch) setState(() => busy = false);
    }
    if (mounted && epoch == expectedEpoch) await load();
  }

  @override
  Widget build(BuildContext context) {
    ref.watch(authStateProvider);
    if (displayEpoch != epoch) {
      displayEpoch = epoch;
      events.clear();
      loaded = false;
      busy = false;
      error = null;
      beforeId = null;
      beforeMs = null;
      retentionRevision++;
    }
    return ExpansionTile(
      tilePadding: EdgeInsets.zero,
      leading: const Icon(Icons.history),
      title: const Text('Listening history'),
      subtitle: const Text('Personal qualified plays and retention'),
      onExpansionChanged: (open) {
        if (open && !loaded) load();
      },
      children: [
        const Text(
            'New music plays appear after 60 counted listening seconds. Offline plays appear after syncing. History is separate from personal counters and resume. Earlier counters are not backfilled.'),
        if (busy) const LinearProgressIndicator(),
        if (error != null)
          Text(error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
              semanticsLabel: error),
        if (loaded) ...[
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Record listening history'),
              value: recording,
              onChanged: busy ? null : (value) => configure(value, days)),
          DropdownButtonFormField<int>(
              key: ValueKey((days, retentionRevision)),
              initialValue: days,
              decoration: const InputDecoration(labelText: 'Keep history'),
              items: const [
                DropdownMenuItem(value: 30, child: Text('30 days')),
                DropdownMenuItem(value: 90, child: Text('90 days')),
                DropdownMenuItem(value: 365, child: Text('1 year')),
                DropdownMenuItem(value: 0, child: Text('Forever'))
              ],
              onChanged: busy
                  ? null
                  : (value) {
                      if (value != null && value != days) {
                        final expectedEpoch = epoch;
                        showDialog<bool>(
                            context: context,
                            builder: (context) => AlertDialog(
                                    title:
                                        const Text('Change history retention?'),
                                    content: const Text(
                                        'Shortening retention permanently removes older history. Increasing it cannot restore removed plays.'),
                                    actions: [
                                      TextButton(
                                          onPressed: () =>
                                              Navigator.pop(context, false),
                                          child: const Text('Cancel')),
                                      TextButton(
                                          onPressed: () =>
                                              Navigator.pop(context, true),
                                          child: const Text('Save'))
                                    ])).then((ok) {
                          if (ok == true && mounted && epoch == expectedEpoch) {
                            configure(recording, value,
                                confirmedEpoch: expectedEpoch);
                          } else if (mounted && epoch == expectedEpoch) {
                            setState(() {
                              retentionRevision++;
                            });
                          }
                        });
                      }
                    }),
          if (!recording)
            const Text(
                'Recording paused. Plays from this paused period will not be added later.'),
        ],
        Wrap(spacing: 8, children: [
          TextButton(
              onPressed: busy ? null : () => load(),
              child: const Text('Refresh history')),
          if (loaded)
            TextButton(
                onPressed: busy ? null : clear,
                child: const Text('Clear history'))
        ]),
        if (loaded && events.isEmpty)
          const ListTile(title: Text('No recorded plays.')),
        for (final event in events)
          ListTile(
              title: Text(event['title'] as String,
                  maxLines: 2, overflow: TextOverflow.ellipsis),
              subtitle: Text(
                  '${event['artist'] ?? ''}\n${DateTime.parse(event['played_at'] as String).toLocal()}'),
              isThreeLine: true),
        if (beforeId != null)
          TextButton(
              onPressed: busy ? null : () => load(more: true),
              child: const Text('Show older plays')),
      ],
    );
  }
}
