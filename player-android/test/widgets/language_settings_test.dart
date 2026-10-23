import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/l10n/music_localizations.dart';
import 'package:player_android/providers/language_provider.dart';
import 'package:player_android/widgets/language_settings.dart';
import 'package:shared_preferences/shared_preferences.dart';

class LanguageHarness extends ConsumerWidget {
  const LanguageHarness({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final choice = ref.watch(languageProvider).valueOrNull?.choice ?? 'system';
    return MaterialApp(
        locale: choice == 'system' ? null : Locale(choice),
        supportedLocales: MusicLocalizations.supportedLocales,
        localizationsDelegates: const [
          MusicLocalizations.delegate,
          GlobalMaterialLocalizations.delegate,
          GlobalWidgetsLocalizations.delegate,
          GlobalCupertinoLocalizations.delegate,
        ],
        home: Scaffold(
            body: Builder(
                builder: (context) => Column(children: [
                      const LanguageSettings(),
                      Text(context.tr('Play {name}', {'name': 'Play'})),
                      Text(context.musicL10n
                          .count(1, '{count} album', '{count} albums')),
                      Text(context.musicL10n
                          .count(2, '{count} album', '{count} albums')),
                      const TextField(key: ValueKey('playlist-name')),
                    ]))));
  }
}

void main() {
  test('locale resolution and English fallback preserve interpolation', () {
    expect(normalizeLanguage('invalid'), 'system');
    expect(resolveMusicLocale('system', const Locale('de', 'DE')),
        const Locale('de'));
    expect(
        resolveMusicLocale('system', const Locale('fr')), const Locale('en'));
    const messages = MusicLocalizations(Locale('de'));
    expect(messages.text('Play {name}', {'name': 'Album <name>'}),
        'Album <name> abspielen');
    expect(messages.text('Future English label'), 'Future English label');
  });

  test('malformed saved preference uses device language', () async {
    SharedPreferences.setMockInitialValues({languageStorageKey: 42});
    final container = ProviderContainer();
    addTearDown(container.dispose);
    expect((await container.read(languageProvider.future)).choice, 'system');
  });

  test('saved language survives a new provider lifecycle', () async {
    SharedPreferences.setMockInitialValues({});
    final first = ProviderContainer();
    await first.read(languageProvider.future);
    await first.read(languageProvider.notifier).choose('de');
    first.dispose();
    final second = ProviderContainer();
    addTearDown(second.dispose);
    expect((await second.read(languageProvider.future)).choice, 'de');
  });

  testWidgets(
      'language switches mounted prose and plurals without losing edits',
      (tester) async {
    SharedPreferences.setMockInitialValues({languageStorageKey: 'en'});
    await tester.pumpWidget(const ProviderScope(child: LanguageHarness()));
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const ValueKey('playlist-name')), 'My unsaved playlist');
    await tester.tap(find.byKey(const ValueKey('language-select')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Deutsch').last);
    await tester.pumpAndSettle();
    expect(find.text('Play abspielen'), findsOneWidget,
        reason: 'metadata is not translated');
    expect(find.text('1 Album'), findsOneWidget);
    expect(find.text('2 Alben'), findsOneWidget);
    expect(find.text('Sprache'), findsOneWidget);
    expect(find.text('My unsaved playlist'), findsOneWidget);
    expect(
        (await SharedPreferences.getInstance()).getString(languageStorageKey),
        'de');
  });
}
