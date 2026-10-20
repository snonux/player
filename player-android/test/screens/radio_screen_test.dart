// Widget tests for RadioScreen: explorer filters, playing a station,
// bookmarks (in the list, the now-playing bar and the Bookmarks tab) and the
// auto-explore controls, with a fake API client, engine and ticker.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/radio_station.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/radio_provider.dart';
import 'package:player_android/screens/radio_screen.dart';
import 'package:player_android/services/radio_controller.dart';

import '../support/radio_fakes.dart';

RadioStation _withCountry(String uuid, String code, List<String> tags) =>
    RadioStation(
      uuid: uuid,
      name: 'Station $uuid',
      streamUrl: '/api/radio/stations/$uuid/stream',
      country: code == 'DE' ? 'Germany' : 'France',
      countryCode: code,
      tags: tags,
      codec: 'AAC',
      bitrate: 64,
    );

void main() {
  late FakeRadioApiClient api;
  late FakeRadioEngine engine;
  late ManualTicker ticker;
  late RadioController radio;

  setUp(() {
    api = FakeRadioApiClient(stations: [
      _withCountry('a', 'DE', ['jazz']),
      _withCountry('b', 'FR', ['rock']),
      _withCountry('c', 'DE', ['jazz', 'rock']),
    ]);
    engine = FakeRadioEngine();
    ticker = ManualTicker();
    radio = RadioController(
      engine: engine,
      ticker: ticker.call,
      exploreSource: (query, limit) => api.searchRadioStations(
          tag: query.tag,
          country: query.country,
          order: query.order,
          limit: limit),
      addBookmark: api.addRadioBookmark,
      removeBookmark: api.removeRadioBookmark,
    );
  });

  Future<void> pumpScreen(WidgetTester tester) async {
    await tester.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(api),
        radioControllerProvider.overrideWith((ref) => radio),
      ],
      child: const MaterialApp(home: RadioScreen()),
    ));
    await tester.pumpAndSettle();
  }

  testWidgets('explore lists popular stations and filters by genre and country',
      (tester) async {
    await pumpScreen(tester);
    expect(find.text('Station a'), findsOneWidget);
    expect(find.text('Station b'), findsOneWidget);
    expect(api.searches.first['order'], RadioOrder.popular);

    await tester.tap(find.byKey(const Key('radio_filter_genre')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('radio_picker_jazz')));
    await tester.pumpAndSettle();
    expect(find.text('Genre: jazz'), findsOneWidget);
    expect(find.text('Station b'), findsNothing);
    expect(find.text('Station a'), findsOneWidget);

    await tester.tap(find.byKey(const Key('radio_filter_country')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('radio_picker_filter')), 'fra');
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('radio_picker_DE')), findsNothing);
    await tester.tap(find.byKey(const Key('radio_picker_FR')));
    await tester.pumpAndSettle();
    expect(find.text('Country: France'), findsOneWidget);
    expect(api.searches.last['country'], 'FR');
    expect(api.searches.last['tag'], 'jazz');
    expect(find.byKey(const Key('radio_explore_empty')), findsOneWidget);

    await tester.tap(find.byTooltip('Clear filter').first);
    await tester.pumpAndSettle();
    expect(api.searches.last['tag'], isNull);
    expect(find.text('Station b'), findsOneWidget);
  });

  testWidgets('search and sort order are sent to the server', (tester) async {
    await pumpScreen(tester);
    await tester.enterText(find.byKey(const Key('radio_search_field')), 'c');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pumpAndSettle();
    expect(api.searches.last['q'], 'c');
    expect(find.text('Station c'), findsOneWidget);
    expect(find.text('Station a'), findsNothing);

    await tester.tap(find.byKey(const Key('radio_order_menu')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('radio_order_votes')));
    await tester.pumpAndSettle();
    expect(api.searches.last['order'], RadioOrder.votes);
    expect(find.text('Sort: Most votes'), findsOneWidget);
  });

  testWidgets('tapping a station plays it and shows the now-playing bar',
      (tester) async {
    await pumpScreen(tester);
    expect(find.byKey(const Key('radio_now_playing')), findsNothing);
    await tester.tap(find.text('Station a'));
    await tester.pump();
    expect(engine.started, ['a']);
    expect(find.text('Connecting…'), findsOneWidget);
    engine.emitPlaying(true);
    await tester.pump();
    expect(find.text('Live'), findsOneWidget);
    expect(find.byKey(const Key('radio_now_playing_name')), findsOneWidget);

    await tester.tap(find.byKey(const Key('radio_play_pause')));
    await tester.pump();
    expect(engine.pauses, 1);

    await tester.tap(find.byKey(const Key('radio_stop')));
    await tester.pump();
    expect(engine.stops, 1);
    expect(find.byKey(const Key('radio_now_playing')), findsNothing);
  });

  testWidgets('a bookmarked station appears on the Bookmarks tab',
      (tester) async {
    await pumpScreen(tester);
    await tester.tap(find.byKey(const Key('radio_tab_bookmarks')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('radio_bookmarks_empty')), findsOneWidget);

    await tester.tap(find.byKey(const Key('radio_tab_explore')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('radio_bookmark_c')));
    await tester.pumpAndSettle();
    expect(api.bookmarks.keys, ['c']);
    expect(find.byTooltip('Remove bookmark'), findsOneWidget);

    await tester.tap(find.byKey(const Key('radio_tab_bookmarks')));
    await tester.pumpAndSettle();
    expect(find.text('Station c'), findsOneWidget);
    expect(find.text('Station a'), findsNothing);

    // Removing it there empties the tab.
    await tester.tap(find.byKey(const Key('radio_bookmark_c')));
    await tester.pumpAndSettle();
    expect(api.bookmarks, isEmpty);
    expect(find.byKey(const Key('radio_bookmarks_empty')), findsOneWidget);
  });

  testWidgets('auto explore counts down, skips, keeps and stops',
      (tester) async {
    await pumpScreen(tester);
    await tester.tap(find.byKey(const Key('radio_auto_explore_button')));
    await tester.pump();
    await tester.pump();
    expect(engine.started, hasLength(1));
    expect(api.searches.last['order'], RadioOrder.random);
    expect(find.textContaining('Tuning in'), findsOneWidget);

    engine.emitPlaying(true);
    await tester.pump();
    expect(find.text('Auto explore · Next station in 30 s'), findsOneWidget);

    await tester.tap(find.byKey(const Key('radio_dwell_15')));
    await tester.pump();
    expect(find.text('Auto explore · Next station in 15 s'), findsOneWidget);
    ticker.tick(5);
    await tester.pump();
    expect(find.text('Auto explore · Next station in 10 s'), findsOneWidget);

    await tester.tap(find.byKey(const Key('radio_explore_next')));
    await tester.pump();
    await tester.pump();
    expect(engine.started, hasLength(2));

    engine.emitPlaying(true);
    await tester.pump();
    final kept = engine.started.last;
    await tester.tap(find.byKey(const Key('radio_explore_keep')));
    await tester.pump();
    expect(find.byKey(const Key('radio_explore_keep')), findsNothing);
    expect(radio.station?.uuid, kept);
    expect(find.text('Live'), findsOneWidget);

    await tester.tap(find.byKey(const Key('radio_auto_explore_button')));
    await tester.pump();
    await tester.pump();
    await tester.tap(find.byKey(const Key('radio_explore_stop')));
    await tester.pump();
    expect(radio.exploring, isFalse);
    expect(find.byKey(const Key('radio_now_playing')), findsNothing);
  });

  testWidgets('a directory failure shows a retry', (tester) async {
    api.searchError = StateError('down');
    await pumpScreen(tester);
    expect(find.byKey(const Key('radio_explore_error')), findsOneWidget);
    api.searchError = null;
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    expect(find.text('Station a'), findsOneWidget);
  });
}
