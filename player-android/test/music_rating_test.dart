import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/screens/music_search_screen.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/widgets/music_rating_button.dart';
import 'package:player_android/widgets/music_rating_filter.dart';

class _RatingsClient extends PlayerApiClient {
  _RatingsClient() : super(dio: Dio());
  bool fail = false;
  final calls = <List<Object>>[];
  final searches = <Map<String, String>>[];
  @override
  Future<dynamic> filteredMusic(String endpoint, Map<String, String> facets,
      {Map<String, String> query = const {}}) async {
    searches.add({...query});
    return {
      'songs': <Map<String, dynamic>>[],
      'albums': [
        {
          'album_key': 'album',
          'title': 'Highest album',
          'artist': 'Artist',
          'rating': 5
        }
      ],
      'artists': <Map<String, dynamic>>[]
    };
  }

  @override
  Future<void> setMusicRating(String kind, String key, int rating) async {
    calls.add([kind, key, rating]);
    if (fail) throw StateError('offline');
  }
}

void main() {
  test('rated filter and order preserve caller maps and omit unrated', () {
    final items = <Map<String, dynamic>>[
      {'id': 1, 'rating': 2},
      {'id': 2, 'rating': 0},
      {'id': 3, 'rating': 5}
    ];
    expect(ratingFiltered(items, true, true).map((e) => e['id']), [3, 1]);
    expect(items.map((e) => e['id']), [1, 2, 3]);
  });
  testWidgets(
      'native rating menu sets changes clears and preserves value on failure',
      (tester) async {
    final client = _RatingsClient();
    final entity = <String, dynamic>{'id': 7, 'rating': 0};
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp(
            home: Scaffold(
                body: StatefulBuilder(
                    builder: (context, setState) => MusicRatingButton(
                        kind: 'song',
                        entity: entity,
                        onChanged: () => setState(() {})))))));
    Future<void> choose(String label) async {
      await tester.tap(find.byType(PopupMenuButton<int>));
      await tester.pumpAndSettle();
      await tester.tap(find.text(label));
      await tester.pumpAndSettle();
    }

    await choose('5 stars');
    expect(entity['rating'], 5);
    expect(client.calls.last, ['song', '7', 5]);
    await choose('2 stars');
    expect(entity['rating'], 2);
    client.fail = true;
    await choose('4 stars');
    expect(entity['rating'], 2);
    expect(find.textContaining('Could not save rating.'), findsOneWidget);
    client.fail = false;
    await choose('Unrated · Clear rating');
    expect(entity['rating'], 0);
    expect(client.calls.last, ['song', '7', 0]);
  });
  testWidgets(
      'highest rated empty search loads production results and survives Rated only off',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final client = _RatingsClient();
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: const MaterialApp(home: MusicSearchScreen())));
    await tester.pumpAndSettle();
    expect(find.text('Search songs, albums and artists.'), findsOneWidget);
    Future<void> filter(String label) async {
      await tester.tap(find.byTooltip('Rating filters and order'));
      await tester.pumpAndSettle();
      await tester.tap(find.ancestor(
          of: find.text(label),
          matching: find.byType(CheckedPopupMenuItem<String>)));
      await tester.pumpAndSettle();
    }

    await filter('Highest rated first');
    expect(client.searches.last['sort'], 'rating');
    expect(find.text('Highest album'), findsOneWidget);
    await filter('Rated only');
    expect(client.searches.last['rated'], 'true');
    await filter('Rated only');
    expect(client.searches.last.containsKey('rated'), isFalse);
    expect(client.searches.last['sort'], 'rating');
    expect(find.text('Highest album'), findsOneWidget);
    expect(find.text('Search songs, albums and artists.'), findsNothing);
  });
}
