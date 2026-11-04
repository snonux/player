import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/podcast_show_notes.dart';
import 'package:player_android/services/podcast_metadata.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('zone.foo.player_android/metadata_links');
  test(
      'web URLs retain queries and balanced parentheses but drop sentence punctuation',
      () {
    final parts = podcastShowNotesSegments(
        'Go https://example.com/a_(b)?x=1&y=2. Or (http://example.org/notes). https://user:password@example.com/private');
    expect(parts.where((p) => p.url != null).map((p) => p.url),
        ['https://example.com/a_(b)?x=1&y=2', 'http://example.org/notes']);
  });
  testWidgets('HTTP and HTTPS notes links open the browser and retain prose',
      (tester) async {
    final opened = <String>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(channel,
        (call) async {
      expect(call.method, 'open');
      opened.add(call.arguments as String);
      return null;
    });
    addTearDown(() => tester.binding.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    await tester.pumpWidget(const MaterialApp(
        home: Scaffold(
            body: SingleChildScrollView(
      child: PodcastShowNotes(
          initiallyExpanded: true,
          description:
              '<p>Publisher notes: <a href="https://example.com/notes">Read notes</a> and http://example.org/info.</p><a href="javascript:bad()">Unsafe</a>'),
    ))));
    for (final url in [
      'https://example.com/notes',
      'http://example.org/info'
    ]) {
      final link = find.widgetWithText(TextButton, url);
      expect(link, findsOneWidget);
      expect(tester.getSize(link).height, greaterThanOrEqualTo(48));
      await tester.tap(link);
      await tester.pump();
    }
    expect(opened, ['https://example.com/notes', 'http://example.org/info']);
    expect(find.widgetWithText(TextButton, 'javascript:bad()'), findsNothing);
    expect(tester.takeException(), isNull);
  });
  testWidgets('browser failure leaves the notes and a readable retry message',
      (tester) async {
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        channel, (_) async => throw PlatformException(code: 'no_browser'));
    addTearDown(() => tester.binding.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    await tester.pumpWidget(const MaterialApp(
        home: Scaffold(
            body: PodcastShowNotes(
                initiallyExpanded: true,
                description: 'https://example.com/notes'))));
    await tester
        .tap(find.widgetWithText(TextButton, 'https://example.com/notes'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Could not open link.'), findsOneWidget);
    expect(find.text('Show notes'), findsOneWidget);
  });
}
