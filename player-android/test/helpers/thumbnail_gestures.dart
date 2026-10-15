import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Future<void> pinchPreviews(WidgetTester tester, double factor) async {
  final area =
      tester.getRect(find.byKey(const Key('library_preview_gestures')));
  final center = area.center;
  final first = await tester.createGesture(pointer: 41);
  final second = await tester.createGesture(pointer: 42);
  await first.down(center - const Offset(60, 0));
  await second.down(center + const Offset(60, 0));
  await tester.pump();
  await first.moveTo(center - Offset(60 * factor, 0));
  await second.moveTo(center + Offset(60 * factor, 0));
  await tester.pump();
  await first.up();
  await second.up();
  await tester.pumpAndSettle();
}
