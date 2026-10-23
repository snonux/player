import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../providers/navigation_preferences_provider.dart';

class NavigationSettings extends ConsumerWidget {
  const NavigationSettings({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(navigationPreferencesProvider);
    final value = state.valueOrNull ?? const NavigationPreferences();
    Future<void> save(NavigationPreferences next) async {
      try {
        await ref.read(navigationPreferencesProvider.notifier).save(next);
      } catch (_) {
        if (context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
              content: Text('Navigation could not be saved. Try again.')));
        }
      }
    }

    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      Text('Navigation', style: Theme.of(context).textTheme.titleMedium),
      const Text(
          'Saved on this device for all accounts. Hidden shortcuts remain available in All music views. Explicit links keep their destination.'),
      if (state.isLoading) const LinearProgressIndicator(),
      if (state.hasError)
        const Text(
            'Navigation could not be loaded. Choose settings to try saving again.'),
      InputDecorator(
          decoration: const InputDecoration(labelText: 'Initial collection'),
          child: DropdownButtonHideUnderline(
              child: DropdownButton<MusicCollection>(
                  value: value.initialCollection,
                  isExpanded: true,
                  items: [
                    for (final v in MusicCollection.values)
                      DropdownMenuItem(value: v, child: Text(v.label))
                  ],
                  onChanged: state.isLoading
                      ? null
                      : (v) {
                          if (v != null) {
                            save(value.copyWith(
                                landing: v,
                                hidden: {...value.hidden}..remove(v)));
                          }
                        }))),
      for (final section in MusicCollection.values.skip(1))
        SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: Text('Show ${section.label} shortcut'),
            value: !value.hidden.contains(section),
            onChanged: state.isLoading
                ? null
                : (show) => save(value.copyWith(
                    hidden: show
                        ? ({...value.hidden}..remove(section))
                        : {...value.hidden, section}))),
      TextButton(
          onPressed: state.isLoading
              ? null
              : () => save(const NavigationPreferences()),
          child: const Text('Reset navigation')),
      const AllMusicViews(),
    ]);
  }
}

class AllMusicViews extends StatelessWidget {
  const AllMusicViews({super.key, this.closeDrawer = false});
  final bool closeDrawer;
  @override
  Widget build(BuildContext context) => ExpansionTile(
          tilePadding: EdgeInsets.zero,
          title: const Text('All music views'),
          children: [
            for (final section in MusicCollection.values.skip(1))
              ListTile(
                  title: Text('Open ${section.label}'),
                  onTap: () {
                    if (closeDrawer) Navigator.of(context).pop();
                    context.push(section.route);
                  })
          ]);
}
