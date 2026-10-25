import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/automatic_music_provider.dart';
import '../providers/global_sync_provider.dart';
import '../services/automatic_music.dart';

class AutomaticMusicSettings extends ConsumerStatefulWidget {
  const AutomaticMusicSettings({super.key});
  @override
  ConsumerState<AutomaticMusicSettings> createState() =>
      _AutomaticMusicSettingsState();
}

class _AutomaticMusicSettingsState
    extends ConsumerState<AutomaticMusicSettings> {
  String? error;
  bool saving = false;
  @override
  Widget build(BuildContext context) {
    final music = ref.watch(automaticMusicProvider),
        scheduler = ref.watch(automaticMusicSchedulerProvider);
    final p = music.policy, s = music.status;
    final enabled = music.key != null && !music.loading && !saving;
    Future<void> change(String field, dynamic value) async {
      setState(() {
        saving = true;
        error = null;
      });
      try {
        await music.savePolicy(
            AutomaticMusicPolicy.fromJson({...p.toJson(), field: value}));
      } catch (_) {
        if (mounted) {
          setState(() => error =
              'Could not save automatic music settings. Check storage and retry.');
        }
      } finally {
        if (mounted) setState(() => saving = false);
      }
    }

    String time(String field) => s[field] is int
        ? DateTime.fromMillisecondsSinceEpoch(s[field] as int)
            .toLocal()
            .toString()
        : 'never';
    return ExpansionTile(
        title: const Text('Automatic offline music'),
        subtitle: Text(music.active
            ? 'Syncing…'
            : p.enabled
                ? 'Enabled'
                : 'Paused'),
        children: [
          const Padding(
              padding: EdgeInsets.all(16),
              child: Text(
                  'Keep selected music current for this account. Existing downloads retain their quality; new copies use Download quality.')),
          SwitchListTile(
              title: const Text('Keep music current'),
              value: p.enabled,
              onChanged: enabled ? (v) => change('enabled', v) : null),
          CheckboxListTile(
              title: const Text('Pinned playlists'),
              value: p.playlists,
              onChanged: enabled ? (v) => change('playlists', v) : null),
          CheckboxListTile(
              title: const Text('Starred songs'),
              value: p.starred,
              onChanged: enabled ? (v) => change('starred', v) : null),
          CheckboxListTile(
              title: const Text('Newest 50 songs (recently added)'),
              value: p.recent,
              onChanged: enabled ? (v) => change('recent', v) : null),
          Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: DropdownButtonFormField<int>(
                  key: ValueKey('${music.key}:${p.hours}'),
                  initialValue: p.hours,
                  decoration: const InputDecoration(labelText: 'Check every'),
                  items: [
                    for (final hours in [1, 6, 24])
                      DropdownMenuItem(
                          value: hours,
                          child:
                              Text('$hours ${hours == 1 ? 'hour' : 'hours'}'))
                  ],
                  onChanged: enabled ? (v) => change('hours', v) : null)),
          SwitchListTile(
              title: const Text('Download on Wi-Fi only'),
              value: p.wifiOnly,
              onChanged: enabled ? (v) => change('wifiOnly', v) : null),
          Padding(
              padding: const EdgeInsets.all(16),
              child: Semantics(
                  liveRegion: true,
                  child: Text(error ??
                      scheduler.error ??
                      (music.loading
                          ? 'Loading settings…'
                          : music.key == null
                              ? 'Sign in and unlock this account to configure automatic music.'
                              : 'What will be kept: ${[
                                  if (p.playlists) "pinned playlists",
                                  if (p.starred) "starred songs",
                                  if (p.recent) "newest 50 songs"
                                ].join(", ")}${p.selected ? "" : "nothing selected"}. ${s['members'] ?? 0} current songs · ${s['retained'] ?? 0} automatic copies · ${s['retired'] ?? 0} retired. ${s['message'] ?? ""}\nLast attempt: ${time('lastAttempt')}\nLast completed: ${time('lastSuccess')}\nNext check: ${p.enabled && p.selected ? time('nextDue') : "paused"}')))),
          Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Wrap(spacing: 12, children: [
                FilledButton(
                    onPressed: enabled &&
                            p.enabled &&
                            p.selected &&
                            !music.active
                        ? () =>
                            ref.read(globalSyncCoordinatorProvider).syncNow()
                        : null,
                    child: const Text('Sync now')),
                TextButton(
                    onPressed: music.active ? music.cancel : null,
                    child: const Text('Cancel current run')),
                if (scheduler.error != null)
                  TextButton(
                      onPressed: scheduler.retry,
                      child: const Text('Retry Android scheduling'))
              ])),
          const Padding(
              padding: EdgeInsets.all(16),
              child: Text(
                  'Android checks on an inexact schedule while the Player engine and unlocked account are available, including during background audio. A closed engine or locked account waits until you open and unlock Player. Wi-Fi policy also applies to Sync now; unknown networks wait (see Music network settings). Removed songs leave automatic collections after a complete sync. Retired copies stay in Downloads to protect playback and history. At most 200 automatic copies are retained; remove retired downloads explicitly to free space.')),
        ]);
  }
}
