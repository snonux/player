import 'package:flutter/material.dart';
import '../services/equalizer.dart';

Future<void> showEqualizerSheet(
        BuildContext context, EqualizerController controller) =>
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) => AnimatedBuilder(
          animation: controller,
          builder: (context, _) {
            final settings = controller.settings;
            final preset = equalizerPresets.entries
                    .where((entry) => List.generate(
                            5, (i) => entry.value[i] == settings.bands[i])
                        .every((same) => same))
                    .firstOrNull
                    ?.key ??
                'Custom';
            return SingleChildScrollView(
                child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('Equalizer',
                            style: Theme.of(context).textTheme.titleLarge),
                        SwitchListTile(
                            contentPadding: EdgeInsets.zero,
                            title: const Text('Enable equalizer'),
                            subtitle: const Text(
                                'Turn off to bypass and keep your curve.'),
                            value: settings.enabled,
                            onChanged: (enabled) => controller.update(
                                EqualizerSettings(
                                    enabled: enabled, bands: settings.bands))),
                        DropdownButtonFormField<String>(
                            key: ValueKey(preset),
                            initialValue: preset,
                            decoration: const InputDecoration(
                                labelText: 'Equalizer preset'),
                            items: [...equalizerPresets.keys, 'Custom']
                                .map((name) => DropdownMenuItem(
                                    value: name, child: Text(name)))
                                .toList(),
                            onChanged: (name) {
                              final bands = equalizerPresets[name];
                              if (bands != null) {
                                controller.update(EqualizerSettings(
                                    enabled: true, bands: bands));
                              }
                            }),
                        const SizedBox(height: 12),
                        for (var i = 0; i < 5; i++) ...[
                          Text(
                              '${equalizerFrequencies[i]} Hz: ${settings.bands[i].toStringAsFixed(0)} dB'),
                          Slider(
                              value: settings.bands[i],
                              min: -12,
                              max: 12,
                              divisions: 24,
                              label:
                                  '${equalizerFrequencies[i]} hertz ${settings.bands[i].toStringAsFixed(0)} decibels',
                              semanticFormatterCallback: (value) =>
                                  '${equalizerFrequencies[i]} hertz ${value.round()} decibels',
                              onChanged: (value) {
                                final bands = [...settings.bands];
                                bands[i] = value;
                                controller.update(EqualizerSettings(
                                    enabled: true, bands: bands));
                              }),
                        ],
                        const Text(
                            'Local audio only; remote receivers use their own effects. Boosts reserve headroom and may reduce overall volume. Saved on this device. Frequency response depends on the device audio effects.'),
                        const SizedBox(height: 12),
                        Semantics(
                            liveRegion: true, child: Text(controller.status)),
                        Wrap(spacing: 12, children: [
                          TextButton(
                              onPressed: () =>
                                  controller.update(const EqualizerSettings()),
                              child: const Text('Reset flat')),
                          TextButton(
                              onPressed: controller.apply,
                              child: const Text('Retry equalizer')),
                          TextButton(
                              onPressed: () => Navigator.pop(context),
                              child: const Text('Done')),
                        ]),
                      ],
                    )));
          }),
    );
