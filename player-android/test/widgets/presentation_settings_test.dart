import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/presentation_provider.dart';
import 'package:player_android/widgets/presentation_settings.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('failed saves restore all visible choices and the next save base',
      (tester) async {
    SharedPreferences.resetStatic();
    final stored = <String, Object>{};
    var rejectWrites = true;
    const channel = MethodChannel('plugins.flutter.io/shared_preferences');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      if (call.method == 'getAll') return stored;
      if (call.method == 'setString') {
        if (rejectWrites) return false;
        final args = call.arguments as Map;
        stored[args['key'] as String] = args['value'] as String;
        return true;
      }
      throw StateError('Unexpected preferences method ${call.method}');
    });
    addTearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, null);
      SharedPreferences.resetStatic();
    });
    final container = ProviderContainer();
    addTearDown(container.dispose);
    await container.read(presentationProvider.future);
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(
            home: Scaffold(
                body: SingleChildScrollView(child: PresentationSettings())))));
    await tester.pumpAndSettle();

    Future<void> choose<T>(String label) async {
      await tester.tap(find.byType(DropdownButton<T>));
      await tester.pumpAndSettle();
      await tester.tap(find.text(label).last);
      await tester.pumpAndSettle();
    }

    void expectDefaults() {
      expect(
          tester
              .widget<DropdownButton<PlayerAccent>>(
                  find.byType(DropdownButton<PlayerAccent>))
              .value,
          PlayerAccent.blue);
      expect(
          tester
              .widget<DropdownButton<ArtworkShape>>(
                  find.byType(DropdownButton<ArtworkShape>))
              .value,
          ArtworkShape.rounded);
      expect(
          tester
              .widget<DropdownButton<ArtworkDisplay>>(
                  find.byType(DropdownButton<ArtworkDisplay>))
              .value,
          ArtworkDisplay.cover);
      expect(find.text('Blue').hitTestable(), findsOneWidget);
      expect(find.text('Rounded').hitTestable(), findsOneWidget);
      expect(find.text('Fill frame').hitTestable(), findsOneWidget);
    }

    for (final attempt in [
      () => choose<PlayerAccent>('Teal'),
      () => choose<ArtworkShape>('Circle'),
      () => choose<ArtworkDisplay>('Fit whole image'),
    ]) {
      await attempt();
      expectDefaults();
      expect(stored, isEmpty);
      expect(container.read(presentationProvider).requireValue.accent,
          PlayerAccent.blue);
      expect(
          find.text('Appearance could not be saved. Try again.'), findsWidgets);
    }
    rejectWrites = false;
    await choose<ArtworkShape>('Square');
    final saved =
        jsonDecode(stored['flutter.player_presentation']! as String) as Map;
    expect(saved['accent'], 'blue');
    expect(saved['shape'], 'square');
    expect(saved['display'], 'cover');
    expect(saved['keepScreenOn'], false);
    expect(container.read(presentationProvider).requireValue.shape,
        ArtworkShape.square);
  });
}
