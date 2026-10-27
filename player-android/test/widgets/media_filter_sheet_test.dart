import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/widgets/media_filter_sheet.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  bool fail = false;
  @override
  Future<List<MediaSet>> listSets() async {
    if (fail) throw Exception('offline');
    return [
      for (final id in [1, 2])
        MediaSet(
            id: id,
            name: 'Set $id',
            rootPath: '',
            coverThumbnailPath: '',
            isPodcast: false)
    ];
  }

  @override
  Future<List<Tag>> listTags() async =>
      const [Tag(id: 1, name: 'keep'), Tag(id: 2, name: 'calm')];
}

Future<void> _open(
    WidgetTester tester, _Client client, ValueChanged<MediaFilter?> result,
    {MediaFilter? filter}) async {
  await tester.pumpWidget(MaterialApp(
      home: Scaffold(
          body: Builder(
    builder: (context) => TextButton(
        onPressed: () async {
          result(await showModalBottomSheet<MediaFilter>(
              context: context,
              isScrollControlled: true,
              builder: (_) => MediaFilterSheet(
                    initialFilter: filter ?? const MediaFilter.empty(),
                    currentSetId: 1,
                    currentSetName: 'Set 1',
                    client: client,
                    onSetsLoaded: (_) {},
                  )));
        },
        child: const Text('Open')),
  ))));
  await tester.tap(find.text('Open'));
  await tester.pumpAndSettle();
}

/// Scrolls the lazily built sheet list until [finder] is on screen.
Future<void> _reveal(WidgetTester tester, Finder finder) async {
  final list = find
      .descendant(
          of: find.byType(MediaFilterSheet), matching: find.byType(Scrollable))
      .first;
  tester.state<ScrollableState>(list).position.jumpTo(0);
  await tester.pump();
  await tester.scrollUntilVisible(finder, 100, scrollable: list);
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
}

Future<void> _tapOption(WidgetTester tester, String text) async {
  await _reveal(tester, find.text(text));
  await tester.tap(find.text(text));
}

Future<void> _field(WidgetTester tester, int index, String text) async {
  final field = find.byKey(Key('media_filter_bound_$index'));
  await _reveal(tester, field);
  await tester.enterText(field, text);
}

void main() {
  testWidgets('cancel draft; AND tags, union and exact zero conversion',
      (tester) async {
    MediaFilter? result;
    await _open(tester, _Client(), (value) => result = value);
    await _tapOption(tester, 'Set 2');
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(result, isNull);
    await tester.tap(find.text('Open'));
    await tester.pumpAndSettle();
    await _tapOption(tester, 'Set 2');
    await _tapOption(tester, 'keep');
    await _tapOption(tester, 'calm');
    for (final entry in [(0, '0'), (1, '1.5'), (2, '0'), (3, '2')]) {
      await _field(tester, entry.$1, entry.$2);
    }
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(result!.setIds, [1, 2]);
    expect(result!.tags, ['calm', 'keep']);
    expect(result!.minDuration, 0);
    expect(result!.maxDuration, 90);
    expect(result!.fileSizeMin, 0);
    expect(result!.fileSizeMax, 2097152);
  });
  testWidgets('invalid, nonfinite, reversed and overflowing bounds never apply',
      (tester) async {
    MediaFilter? result;
    await _open(tester, _Client(), (value) => result = value);
    for (final entry in [
      (0, 'NaN'),
      (0, '-1'),
      (0, '1e309'),
      (2, '1.5'),
      (2, '8796093022208'),
      (2, '-1')
    ]) {
      await _field(tester, entry.$1, entry.$2);
      await tester.tap(find.text('Apply'));
      await tester.pumpAndSettle();
      expect(result, isNull);
      expect(find.text('Apply'), findsOneWidget);
      await _field(tester, entry.$1, '');
    }
    await _field(tester, 0, '2');
    await _field(tester, 1, '1');
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(find.text('Maximum must be at least minimum'), findsOneWidget);
    await tester.tap(find.text('Clear filters'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(result, const MediaFilter.empty());
  });
  testWidgets('all scope and failed options preserve draft until Retry',
      (tester) async {
    final client = _Client()..fail = true;
    MediaFilter? result;
    await _open(tester, client, (value) => result = value,
        filter: MediaFilter(tags: ['missing'], minDuration: 0));
    await _reveal(tester, find.text('Retry filter options'));
    await _reveal(tester, find.text('missing'));
    expect(find.text('missing'), findsOneWidget);
    client.fail = false;
    await _reveal(tester, find.text('Retry filter options'));
    await tester.tap(find.text('Retry filter options'));
    await tester.pumpAndSettle();
    await _tapOption(tester, 'Set 1');
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(result!.setIds, isEmpty);
    expect(result!.tags, ['missing']);
    expect(result!.minDuration, 0);
  });
  testWidgets('Clear an applied draft removes existing ranges and keeps sort',
      (tester) async {
    MediaFilter? result;
    await _open(tester, _Client(), (value) => result = value,
        filter: MediaFilter(
            query: 'old',
            tags: ['keep'],
            setIds: [],
            minDuration: 60,
            maxDuration: 120,
            fileSizeMin: 1048576,
            fileSizeMax: 2097152,
            sortBy: 'duration'));
    await tester.tap(find.text('Clear filters'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(result, MediaFilter(sortBy: 'duration'));
  });
  testWidgets('type and sort are chosen in the sheet and apply together',
      (tester) async {
    MediaFilter? result;
    await _open(tester, _Client(), (value) => result = value,
        filter: MediaFilter(sortBy: 'name', favoritesOnly: true));
    expect(
        tester
            .widget<ChoiceChip>(find.byKey(const Key('sort_option_name')))
            .selected,
        isTrue);
    await tester.tap(find.byKey(const Key('type_chip_audio')));
    await tester.tap(find.byKey(const Key('sort_option_random')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(result!.type, 'audio');
    expect(result!.sortBy, 'random');
    expect(result!.favoritesOnly, isTrue);
  });
}
