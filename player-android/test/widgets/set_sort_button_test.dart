import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/set_sort_button.dart';

void main() {
  test('sort preference validates supported directions', () {
    for (final field in setSortFields.keys.where((key) => key != 'random')) {
      expect(normalizeSetSort('${field}_asc'), '${field}_asc');
      expect(normalizeSetSort('${field}_desc'), '${field}_desc');
    }
    expect(normalizeSetSort('random'), 'random');
    expect(normalizeSetSort('bad_desc'), 'name_asc');
  });
  testWidgets('sort sheet offers fields and direction without closing',
      (tester) async {
    var value = 'name_asc';
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: SetSortButton(
                value: value, onChanged: (next) => value = next))));
    await tester.tap(find.byTooltip('Sort'));
    await tester.pumpAndSettle();
    expect(find.text('Sort this set'), findsOneWidget);
    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();
    expect(value, 'name_desc');
    expect(find.text('Done'), findsOneWidget);
  });
}
