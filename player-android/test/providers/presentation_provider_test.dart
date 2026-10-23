import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/presentation_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('appearance survives a fresh provider without changing theme mode',
      () async {
    SharedPreferences.setMockInitialValues({'theme_mode': 'system'});
    final first = ProviderContainer();
    await first.read(presentationProvider.future);
    await first.read(presentationProvider.notifier).save(
        const PresentationPreferences(
            accent: PlayerAccent.teal,
            shape: ArtworkShape.circle,
            display: ArtworkDisplay.contain,
            keepScreenOn: true));
    first.dispose();
    final second = ProviderContainer();
    addTearDown(second.dispose);
    final value = await second.read(presentationProvider.future);
    expect(value.accent, PlayerAccent.teal);
    expect(value.shape, ArtworkShape.circle);
    expect(value.display, ArtworkDisplay.contain);
    expect(value.keepScreenOn, isTrue);
    expect((await SharedPreferences.getInstance()).getString('theme_mode'),
        'system');
  });
  test('malformed preferences recover to safe defaults', () async {
    for (final raw in [
      '[]',
      'broken',
      '{"accent":"future","shape":"future","keepScreenOn":"true"}'
    ]) {
      SharedPreferences.setMockInitialValues({'player_presentation': raw});
      final container = ProviderContainer();
      final value = await container.read(presentationProvider.future);
      expect(value.accent, PlayerAccent.blue);
      expect(value.shape, ArtworkShape.rounded);
      expect(value.display, ArtworkDisplay.cover);
      expect(value.keepScreenOn, isFalse);
      container.dispose();
    }
  });
}
