import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/presentation_provider.dart';

class PresentationSettings extends ConsumerWidget {
  const PresentationSettings({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(presentationProvider);
    final value = state.valueOrNull ?? const PresentationPreferences();
    Future<void> save(PresentationPreferences next) async {
      try {
        await ref.read(presentationProvider.notifier).save(next);
      } catch (_) {
        if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
              content: Text('Appearance could not be saved. Try again.')));
        }
      }
    }

    Widget choice<T>(String label, T selected, List<T> options,
            List<String> labels, PresentationPreferences Function(T) update) =>
        Padding(
            padding: const EdgeInsets.only(top: 12),
            child: InputDecorator(
              decoration: InputDecoration(labelText: label),
              child: DropdownButtonHideUnderline(
                  child: DropdownButton<T>(
                      key: ValueKey(label),
                      value: selected,
                      isExpanded: true,
                      items: [
                        for (var i = 0; i < options.length; i++)
                          DropdownMenuItem(
                              value: options[i], child: Text(labels[i]))
                      ],
                      onChanged: state.isLoading
                          ? null
                          : (v) {
                              if (v != null) save(update(v));
                            })),
            ));
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      if (state.isLoading) const LinearProgressIndicator(),
      if (state.hasError)
        const Text(
            'Appearance could not be loaded. Choose settings to try saving again.'),
      choice('Accent', value.accent, PlayerAccent.values,
          ['Blue', 'Teal', 'Plum'], (v) => value.copyWith(accent: v)),
      choice('Player artwork shape', value.shape, ArtworkShape.values,
          ['Rounded', 'Square', 'Circle'], (v) => value.copyWith(shape: v)),
      choice('Player artwork display', value.display, ArtworkDisplay.values,
          ['Fit whole image', 'Fill frame'], (v) => value.copyWith(display: v)),
      SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('Keep screen on while playing'),
          subtitle: const Text(
              'While audio plays with Player in the foreground. Uses more battery.'),
          value: value.keepScreenOn,
          onChanged: state.isLoading
              ? null
              : (v) => save(value.copyWith(keepScreenOn: v))),
    ]);
  }
}
