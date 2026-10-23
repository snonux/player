import 'package:flutter/material.dart';
import '../services/replay_gain.dart';

Future<void> showLoudnessSheet(
        BuildContext context, ReplayGainController controller) =>
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) => AnimatedBuilder(
          animation: controller,
          builder: (context, _) {
            final settings = controller.settings;
            void change(
                {LoudnessMode? mode, double? preamp, double? untagged}) {
              controller.update(LoudnessSettings(
                  mode: mode ?? settings.mode,
                  preamp: preamp ?? settings.preamp,
                  untagged: untagged ?? settings.untagged));
            }

            Widget slider(String label, double value,
                    ValueChanged<double> onChanged) =>
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text('$label: ${value.toStringAsFixed(0)} dB'),
                  Slider(
                      label: '$label ${value.toStringAsFixed(0)} decibels',
                      value: value,
                      min: -12,
                      max: 12,
                      divisions: 24,
                      onChanged: onChanged),
                ]);
            return SingleChildScrollView(
                child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text('Loudness',
                              style: Theme.of(context).textTheme.titleLarge),
                          const SizedBox(height: 12),
                          const Text(
                              'Balance song volume using embedded ReplayGain tags. Automatic keeps album differences for an ordered album queue; otherwise it balances each song. Local playback only; Cast uses receiver volume.'),
                          const SizedBox(height: 16),
                          DropdownButtonFormField<LoudnessMode>(
                              initialValue: settings.mode,
                              decoration: const InputDecoration(
                                  labelText: 'Loudness mode'),
                              items: LoudnessMode.values
                                  .map((mode) => DropdownMenuItem(
                                      value: mode,
                                      child: Text(mode.name[0].toUpperCase() +
                                          mode.name.substring(1))))
                                  .toList(),
                              onChanged: (mode) {
                                if (mode != null) change(mode: mode);
                              }),
                          const SizedBox(height: 12),
                          Semantics(
                              liveRegion: true, child: Text(controller.status)),
                          ExpansionTile(
                              title: const Text('Advanced loudness'),
                              tilePadding: EdgeInsets.zero,
                              children: [
                                slider('Preamp', settings.preamp,
                                    (value) => change(preamp: value)),
                                slider('Untagged gain', settings.untagged,
                                    (value) => change(untagged: value)),
                                const Text(
                                    'Positive boost needs a valid peak tag. Missing tags use untagged gain; unknown peaks cannot be boosted.'),
                              ]),
                          Align(
                              alignment: Alignment.centerRight,
                              child: TextButton(
                                  onPressed: () => Navigator.pop(context),
                                  child: const Text('Done'))),
                        ])));
          }),
    );
