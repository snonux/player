import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../l10n/music_localizations.dart';
import '../providers/language_provider.dart';

class LanguageSettings extends ConsumerStatefulWidget {
  const LanguageSettings({super.key});
  @override
  ConsumerState<LanguageSettings> createState() => _LanguageSettingsState();
}

class _LanguageSettingsState extends ConsumerState<LanguageSettings> {
  bool _saving = false;
  @override
  Widget build(BuildContext context) {
    final state = ref.watch(languageProvider);
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      InputDecorator(
          decoration: InputDecoration(labelText: context.tr('Language')),
          child: DropdownButtonHideUnderline(
              child: DropdownButton<String>(
                  key: const ValueKey('language-select'),
                  value: state.valueOrNull?.choice ?? 'system',
                  isExpanded: true,
                  items: [
                    DropdownMenuItem(
                        value: 'system',
                        child: Text(context.tr('Device language'))),
                    const DropdownMenuItem(value: 'en', child: Text('English')),
                    const DropdownMenuItem(value: 'de', child: Text('Deutsch')),
                  ],
                  onChanged: state.isLoading || _saving
                      ? null
                      : (value) async {
                          if (value == null) return;
                          setState(() => _saving = true);
                          await ref
                              .read(languageProvider.notifier)
                              .choose(value);
                          if (mounted) setState(() => _saving = false);
                        }))),
      Text(context.tr('Saved on this device for all accounts.')),
      if (state.isLoading || _saving) const LinearProgressIndicator(),
      if (state.hasError)
        Text(context.tr(
            'Language could not be loaded. Choose a language to try saving again.')),
      if (state.valueOrNull?.sessionOnly == true)
        Semantics(
            liveRegion: true,
            child: Text(context.tr(
                'Language changed for this session. Device storage is unavailable.'))),
      const SizedBox(height: 16),
    ]);
  }
}
