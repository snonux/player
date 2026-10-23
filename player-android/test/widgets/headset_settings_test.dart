import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/providers/headset_preferences_provider.dart';
import 'package:player_android/services/headset_preferences.dart';
import 'package:player_android/widgets/headset_settings.dart';

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));
  for (final brightness in Brightness.values) {
    testWidgets('headphone preferences wrap and save in $brightness at 320px',
        (tester) async {
      tester.view.resetPhysicalSize();
      tester.view.physicalSize = const Size(320, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final prefs = HeadsetPreferences();
      await prefs.restore();
      await tester.pumpWidget(ProviderScope(
          overrides: [
            headsetPreferencesProvider.overrideWithValue(prefs),
          ],
          child: MaterialApp(
              theme: ThemeData(brightness: brightness),
              home: const Scaffold(
                  body: SingleChildScrollView(
                      child: Padding(
                          padding: EdgeInsets.all(24),
                          child: HeadsetSettings()))))));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.text('Pause when headphones disconnect'), findsOneWidget);
      expect(find.text('Resume when headphones reconnect'), findsOneWidget);
      await tester.tap(find.text('Resume when headphones reconnect'));
      await tester.pumpAndSettle();
      expect(prefs.resumeOnReconnect, isTrue);
      await tester.tap(find.text('Pause when headphones disconnect'));
      await tester.pumpAndSettle();
      expect(prefs.pauseOnDisconnect, isFalse);
      final switches = tester
          .widgetList<SwitchListTile>(find.byType(SwitchListTile))
          .toList();
      expect(switches[1].onChanged, isNull);
      final restored = HeadsetPreferences();
      await restored.restore();
      expect(restored.pauseOnDisconnect, isFalse);
      expect(restored.resumeOnReconnect, isTrue);
      expect(tester.takeException(), isNull);
    });
  }
}
