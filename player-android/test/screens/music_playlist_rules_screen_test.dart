import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/music_playlist_rules_screen.dart';

void main() {
  testWidgets('smart form saves readable rules and preserves draft on failure',
      (tester) async {
    Map<String, dynamic>? saved;
    var fail = true;
    await tester.pumpWidget(MaterialApp(
        home: MusicPlaylistRulesScreen(
      rules: const {
        'match': 'all',
        'conditions': [
          {'field': 'genre', 'operator': 'contains', 'value': 'Synth'}
        ],
        'sort': 'title',
        'descending': false,
        'limit': 100
      },
      save: (rules) async {
        saved = rules;
        if (fail) throw Exception('offline');
      },
    )));
    expect(find.textContaining('Shared smart playlists'), findsOneWidget);
    await tester.enterText(find.widgetWithText(TextFormField, 'Synth'), 'Jazz');
    await tester.scrollUntilVisible(find.text('Save rules'), 250,
        scrollable: find.byType(Scrollable).first);
    await tester.ensureVisible(find.text('Save rules'));
    await tester.tap(find.text('Save rules'));
    await tester.pumpAndSettle();
    expect(find.text('Could not save rules. Check your connection and retry.'),
        findsOneWidget);
    expect(saved!['conditions'][0]['value'], 'Jazz');
    expect(saved!['limit'], 100);
    fail = false;
    await tester.tap(find.text('Save rules'));
    await tester.pumpAndSettle();
  });
}
