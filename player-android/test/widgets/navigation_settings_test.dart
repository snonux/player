import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/providers/navigation_preferences_provider.dart';
import 'package:player_android/widgets/navigation_settings.dart';

class _FailingNavigation extends NavigationPreferencesNotifier {
  @override
  Future<NavigationPreferences> build() async => const NavigationPreferences();
  @override
  Future<void> save(NavigationPreferences value) async =>
      throw StateError('disk full');
}

void main() {
  test('hidden or invalid landing safely falls back and persists', () async {
    SharedPreferences.setMockInitialValues({});
    final container = ProviderContainer();
    addTearDown(container.dispose);
    expect(
        (await container.read(navigationPreferencesProvider.future))
            .initialCollection,
        MusicCollection.library);
    await container.read(navigationPreferencesProvider.notifier).save(
        const NavigationPreferences(
            landing: MusicCollection.albums, hidden: {MusicCollection.albums}));
    expect(
        container
            .read(navigationPreferencesProvider)
            .requireValue
            .initialCollection,
        MusicCollection.library);
    container.invalidate(navigationPreferencesProvider);
    expect(
        (await container.read(navigationPreferencesProvider.future))
            .initialCollection,
        MusicCollection.library);
    expect(
        NavigationPreferences.fromJson({'landing': 'bogus', 'hidden': 'bad'})
            .initialCollection,
        MusicCollection.library);
  });

  testWidgets('failed save keeps controlled selector at saved value',
      (tester) async {
    await tester.pumpWidget(ProviderScope(
        overrides: [
          navigationPreferencesProvider.overrideWith(_FailingNavigation.new)
        ],
        child: const MaterialApp(
            home: Scaffold(
                body: SingleChildScrollView(child: NavigationSettings())))));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(DropdownButton<MusicCollection>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Albums').last);
    await tester.pumpAndSettle();
    expect(
        tester
            .widget<DropdownButton<MusicCollection>>(
                find.byType(DropdownButton<MusicCollection>))
            .value,
        MusicCollection.library);
    expect(
        find.text('Navigation could not be saved. Try again.'), findsOneWidget);
  });

  testWidgets('hiding landing resets default and reset restores shortcuts',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    await tester.pumpWidget(const ProviderScope(
        child: MaterialApp(
            home: Scaffold(
                body: SingleChildScrollView(child: NavigationSettings())))));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(DropdownButton<MusicCollection>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Albums').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Show Albums shortcut'));
    await tester.pumpAndSettle();
    final dropdown = tester.widget<DropdownButton<MusicCollection>>(
        find.byType(DropdownButton<MusicCollection>));
    expect(dropdown.value, MusicCollection.library);
    expect(find.text('All music views'), findsOneWidget);
    await tester.ensureVisible(find.text('Reset navigation'));
    await tester.tap(find.text('Reset navigation'));
    await tester.pumpAndSettle();
    expect(
        tester
            .widgetList<SwitchListTile>(find.byType(SwitchListTile))
            .every((v) => v.value),
        isTrue);
  });
}
