import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/widgets/music_favorite_button.dart';

class _Api extends PlayerApiClient {
  _Api() : super(dio: Dio());
  final calls = <String>[];
  bool fail = false;
  @override
  Future<void> setMusicFavorite(String kind, String key, bool value) async {
    calls.add('$kind/$key/$value');
    if (fail) throw Exception('offline');
  }
}

void main() {
  testWidgets('entity heart saves explicit scope and retains state on failure',
      (tester) async {
    final api = _Api(),
        entity = <String, dynamic>{'album_key': 'al_one', 'is_favorite': false};
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: MaterialApp(
            home: Scaffold(
                body: StatefulBuilder(
                    builder: (context, setState) => MusicFavoriteButton(
                        kind: 'album',
                        entity: entity,
                        onChanged: () => setState(() {})))))));
    await tester.tap(find.byTooltip('Favorite album'));
    await tester.pumpAndSettle();
    expect(api.calls, ['album/al_one/true']);
    expect(entity['is_favorite'], true);
    api.fail = true;
    await tester.tap(find.byTooltip('Unfavorite album'));
    await tester.pumpAndSettle();
    expect(api.calls.last, 'album/al_one/false');
    expect(entity['is_favorite'], true);
    expect(
        find.text('Could not save favorite. Check your connection and retry.'),
        findsOneWidget);
  });
}
