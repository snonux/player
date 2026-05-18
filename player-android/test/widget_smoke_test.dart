// Widget smoke tests for PlayerAndroidApp.
//
// These tests verify that the two named routes defined in main.dart render
// without errors and contain the expected key widgets. Navigation between
// HomeScreen and NowPlayingScreen is exercised end-to-end.
//
// Run with: flutter test test/widget_smoke_test.dart

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/main.dart';

void main() {
  group('PlayerAndroidApp', () {
    testWidgets('starts on HomeScreen showing Library title', (tester) async {
      await tester.pumpWidget(const PlayerAndroidApp());

      expect(find.text('Library'), findsOneWidget);
      expect(find.text('Now Playing'), findsOneWidget);
    });

    testWidgets('navigates to NowPlayingScreen on button tap', (tester) async {
      await tester.pumpWidget(const PlayerAndroidApp());

      await tester.tap(find.widgetWithText(ElevatedButton, 'Now Playing'));
      await tester.pumpAndSettle();

      expect(find.text('Now Playing'), findsWidgets);
      expect(find.text('No media selected'), findsOneWidget);
    });

    testWidgets('NowPlayingScreen back-navigates to HomeScreen', (tester) async {
      await tester.pumpWidget(const PlayerAndroidApp());

      // Navigate to NowPlayingScreen.
      await tester.tap(find.widgetWithText(ElevatedButton, 'Now Playing'));
      await tester.pumpAndSettle();
      expect(find.text('No media selected'), findsOneWidget);

      // Press the back button provided by Scaffold/AppBar.
      final NavigatorState nav = tester.state(find.byType(Navigator));
      nav.pop();
      await tester.pumpAndSettle();

      expect(find.text('Library'), findsOneWidget);
    });
  });
}
