import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/keyboard_access.dart';

Future<void> pressHint(WidgetTester tester, String label) async {
  final hint = find.byWidgetPredicate((widget) =>
      widget is Text &&
      widget.key is ValueKey<String> &&
      (widget.key! as ValueKey<String>).value.startsWith('shortcut-') &&
      (widget.key! as ValueKey<String>).value.contains(label));
  expect(hint, findsOneWidget);
  final key = tester.widget<Text>(hint).data!;
  for (final letter in key.split('')) {
    await tester.sendKeyEvent(LogicalKeyboardKey(letter.codeUnitAt(0)),
        character: letter);
    await tester.pump();
  }
  await tester.pumpAndSettle();
}

Widget app(Widget child) => MaterialApp(
      builder: (context, child) => KeyboardAccess(child: child!),
      home: Scaffold(body: child),
    );

void main() {
  test('codes remain unique and prefix-free for large or repeated menus', () {
    for (final count in [1, 22, 100, 1000]) {
      final keys = controlShortcutKeys(List.filled(count, 'Repeated title'));
      expect(keys.toSet().length, count);
      expect(
          keys.every((key) =>
              keys.every((other) => key == other || !other.startsWith(key))),
          isTrue);
    }
  });

  testWidgets(
      'physical keys open real menus, select entries and preserve text entry',
      (tester) async {
    var picked = '';
    final text = TextEditingController();
    addTearDown(text.dispose);
    await tester.pumpWidget(app(Column(children: [
      PopupMenuButton<String>(
          tooltip: 'Open actions',
          onSelected: (value) => picked = value,
          itemBuilder: (_) => const [
                PopupMenuItem(value: 'alpha', child: Text('Alpha')),
                PopupMenuItem(value: 'beta', child: Text('Beta'))
              ]),
      TextField(
          controller: text,
          decoration: const InputDecoration(labelText: 'Notes')),
      TextButton(onPressed: () => picked = 'saved', child: const Text('Save')),
    ])));
    await tester.sendKeyEvent(LogicalKeyboardKey.f2);
    await tester.pumpAndSettle();
    await pressHint(tester, 'Open actions');
    expect(find.text('Alpha'), findsOneWidget);
    await pressHint(tester, 'Beta');
    expect(picked, 'beta');
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(TextField));
    await tester.enterText(find.byType(TextField), 'p;n is literal');
    await tester.sendKeyEvent(LogicalKeyboardKey.semicolon, character: ';');
    await tester.pump();
    expect(find.textContaining('Control shortcuts'), findsNothing);
    await tester.sendKeyEvent(LogicalKeyboardKey.f2);
    await tester.pumpAndSettle();
    await pressHint(tester, 'Save');
    expect(picked, 'saved');
    expect(text.text, 'p;n is literal');
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('slider arrows adjust only the selected control and Escape exits',
      (tester) async {
    var volume = 0.5;
    await tester.pumpWidget(app(StatefulBuilder(
        builder: (context, setState) => Semantics(
            label: 'Volume',
            child: Slider(
                value: volume,
                divisions: 10,
                onChanged: (value) => setState(() => volume = value))))));
    await tester.sendKeyEvent(LogicalKeyboardKey.f2);
    await tester.pumpAndSettle();
    await pressHint(tester, 'Volume');
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pumpAndSettle();
    expect(volume, greaterThan(0.5));
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();
    expect(find.textContaining('Control shortcuts'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('scroll areas expose shortcuts and reveal offscreen controls',
      (tester) async {
    final scroll = ScrollController();
    addTearDown(scroll.dispose);
    await tester.pumpWidget(app(ListView(controller: scroll, children: [
      for (var index = 0; index < 40; index++)
        SizedBox(
            height: 80,
            child: TextButton(onPressed: () {}, child: Text('Item $index'))),
    ])));
    await tester.sendKeyEvent(LogicalKeyboardKey.f2);
    await tester.pumpAndSettle();
    await pressHint(tester, 'Scroll or adjust');
    await tester.sendKeyEvent(LogicalKeyboardKey.pageDown);
    await tester.pumpAndSettle();
    expect(scroll.offset, greaterThan(0));
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
