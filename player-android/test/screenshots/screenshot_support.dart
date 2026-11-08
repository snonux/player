// Renders screens with real fonts into PNG files for design review and docs.
// Run: SCREENSHOT_ROBOTO=/path/Roboto.ttf flutter test --update-goldens test/screenshots/podcasts_shot.dart
// The *_shot.dart names keep these out of the normal test run; images land in out/.
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

Future<void> loadScreenshotFonts() async {
  SharedPreferences.setMockInitialValues({});
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  for (final name in ['dev.fluttercommunity.plus/connectivity_status']) {
    messenger.setMockStreamHandler(
        EventChannel(name), MockStreamHandler.inline(onListen: (_, __) {}));
  }
  messenger.setMockMethodCallHandler(
      const MethodChannel('dev.fluttercommunity.plus/connectivity'),
      (_) async => ['wifi']);
  final roboto = File(Platform.environment['SCREENSHOT_ROBOTO'] ??
      '/tmp/claude-0/fonts/Roboto.ttf');
  if (roboto.existsSync()) {
    final loader = FontLoader('Roboto')
      ..addFont(Future.value(ByteData.sublistView(roboto.readAsBytesSync())));
    await loader.load();
  }
  final flutterRoot =
      Platform.environment['FLUTTER_ROOT'] ?? '/tmp/claude-0/sdk/flutter';
  final icons = File(
      '$flutterRoot/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf');
  if (icons.existsSync()) {
    final loader = FontLoader('MaterialIcons')
      ..addFont(Future.value(ByteData.sublistView(icons.readAsBytesSync())));
    await loader.load();
  }
}

void usePhone(WidgetTester tester, {double height = 915}) {
  tester.view.physicalSize = Size(412 * 2, height * 2);
  tester.view.devicePixelRatio = 2;
  addTearDown(tester.view.reset);
}
