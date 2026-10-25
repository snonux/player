import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/music_automation_provider.dart';
import '../services/music_automation.dart';

class MusicAutomationSettings extends ConsumerStatefulWidget {
  const MusicAutomationSettings({super.key});
  @override
  ConsumerState<MusicAutomationSettings> createState() =>
      _MusicAutomationSettingsState();
}

class _MusicAutomationSettingsState
    extends ConsumerState<MusicAutomationSettings> with WidgetsBindingObserver {
  String status = 'No automation has run yet.';
  String behavior = '';
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    refresh();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) refresh();
  }

  Future<void> refresh() async {
    try {
      final origin = ref.read(playerBaseUrlProvider).origin;
      final user = ref.read(authStateProvider).valueOrNull?.user;
      final value = await musicAutomationChannel.invokeMethod<String>('status');
      final prefs = await SharedPreferences.getInstance();
      if (mounted) {
        setState(() {
          status = value ?? status;
          behavior = user == null
              ? ''
              : 'Automation playback for ${user.username}: ${prefs.getBool('music-automation-offline-${musicAutomationScope(origin, user.id)}') == true ? 'Offline copies only' : 'Online music allowed'}';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() =>
            status = 'Automation is unavailable. Reopen Player and retry.');
      }
    }
  }

  Future<void> revoke() async {
    try {
      await musicAutomationChannel.invokeMethod<void>('revoke');
      await refresh();
    } catch (_) {
      if (mounted) {
        setState(() => status = 'Could not revoke saved actions. Retry.');
      }
    }
  }

  @override
  Widget build(BuildContext context) => ExpansionTile(
          tilePadding: EdgeInsets.zero,
          title: const Text('Music automation'),
          subtitle: const Text('Tasker / Locale · Android only'),
          onExpansionChanged: (expanded) {
            if (expanded) refresh();
          },
          children: [
            const Text(
                'In Tasker, Locale or a compatible automation app, add the '
                'Player music action. Choose Start queued music, Shuffle and start queued '
                'music, Use offline music only or Allow online music, then Save action. '
                'Each saved action belongs to your selected server account.'),
            const SizedBox(height: 12),
            const Text(
                'Open Player, unlock that account and queue music first. '
                'After restarting, open and unlock Player again. Other active '
                'sources are left for you to choose in Player. Offline only affects '
                'future automation starts and requires every queued song to be saved '
                'in Downloads or Playback cache. Allow online follows your normal '
            'network settings. For older saved copies, allow online and run '
            'Start once while connected to validate music without downloading '
            'it again. Policy actions do not start playback.'),
            const SizedBox(height: 12),
            if (behavior.isNotEmpty) Text(behavior),
            Text(status, semanticsLabel: 'Last automation result: $status'),
            Wrap(spacing: 12, children: [
              TextButton(
                  onPressed: refresh, child: const Text('Refresh result')),
              TextButton(
                  onPressed: revoke, child: const Text('Revoke saved actions')),
            ]),
          ]);
}
