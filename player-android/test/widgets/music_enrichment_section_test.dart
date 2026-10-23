import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/widgets/music_enrichment_section.dart';

class _Api extends PlayerApiClient {
  _Api() : super(dio: Dio());
  bool fail = false;
  Map<String, dynamic> response = {
    'enabled': true,
    'can_refresh': true,
    'state': 'unfetched'
  };
  final refreshes = <bool>[];
  @override
  Future<Map<String, dynamic>> musicEnrichment(String kind, String key,
      {bool refresh = false}) async {
    refreshes.add(refresh);
    if (fail) throw Exception('offline');
    return response;
  }
}

void main() {
  testWidgets(
      'About refreshes without replacing primary details and retains biography offline',
      (tester) async {
    final api = _Api();
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: const MaterialApp(
            home: Scaffold(
                body: SingleChildScrollView(
                    child: Column(children: [
          Text('Album playback'),
          MusicEnrichmentSection(kind: 'artist', entityKey: 'artist-key')
        ]))))));
    await tester.pumpAndSettle();
    expect(api.refreshes, [false]);
    await tester.tap(find.text('About'));
    await tester.pumpAndSettle();
    api.response = {
      'enabled': true,
      'can_refresh': true,
      'state': 'ready',
      'source': 'Local fixture',
      'description': 'Fixture biography'
    };
    await tester.tap(find.text('Refresh metadata'));
    await tester.pumpAndSettle();
    expect(api.refreshes, [false, true]);
    expect(find.text('Fixture biography'), findsOneWidget);
    expect(find.text('Album playback'), findsOneWidget);
    api.fail = true;
    await tester.tap(find.text('Refresh metadata'));
    await tester.pumpAndSettle();
    expect(find.text('Fixture biography'), findsOneWidget);
    expect(find.text('Retry'), findsOneWidget);
  });
  testWidgets('Disabled provider hides optional About', (tester) async {
    final api = _Api()..response = {'enabled': false};
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: const MaterialApp(
            home: Scaffold(
                body: MusicEnrichmentSection(
                    kind: 'album', entityKey: 'album-key')))));
    await tester.pumpAndSettle();
    expect(find.text('About'), findsNothing);
  });
}
