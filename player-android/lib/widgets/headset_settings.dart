import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/headset_preferences_provider.dart';

class HeadsetSettings extends ConsumerWidget {
  const HeadsetSettings({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = ref.watch(headsetPreferencesProvider);
    return ListenableBuilder(
        listenable: policy,
        builder: (context, _) =>
            Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              Text('Headphones',
                  style: Theme.of(context).textTheme.titleMedium),
              const Text(
                  'On this device · wired headphones and Bluetooth audio. '
                  'Phone calls and audio focus still take priority.'),
              if (policy.loading || policy.saving)
                const LinearProgressIndicator(),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Pause when headphones disconnect'),
                subtitle: const Text(
                    'Turn off to allow playback through the phone speaker.'),
                value: policy.pauseOnDisconnect,
                onChanged: policy.loading || policy.saving
                    ? null
                    : (value) => policy.save(pauseOnDisconnect: value),
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Resume when headphones reconnect'),
                subtitle: const Text(
                    'Only resumes playback paused by disconnecting. '
                    'Pausing yourself, switching libraries or a sleep timer cancels resume.'),
                value: policy.resumeOnReconnect,
                onChanged:
                    policy.loading || policy.saving || !policy.pauseOnDisconnect
                        ? null
                        : (value) => policy.save(resumeOnReconnect: value),
              ),
              if (policy.error != null)
                Text(policy.error!,
                    semanticsLabel: policy.error,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
            ]));
  }
}
