import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/podcast_list_screen.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'podcast_browsing_fakes.dart';

void main() => episodeLayoutTests();

void episodeLayoutTests() {
  testWidgets('episode grid keeps metadata, actions and list navigation',
      (tester) async {
    final repository = BrowsingRepository()..count = 4;
    await tester.pumpWidget(browsingApp(const PodcastListScreen(), repository));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Grid'));
    await tester.pumpAndSettle();
    expect(find.byType(Card), findsNWidgets(4));
    expect(find.text('Episode 0'), findsOneWidget);
    expect(find.textContaining('In progress'), findsWidgets);
    await Scrollable.ensureVisible(
        tester.element(find.text('Show notes').first),
        alignment: 0.5);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Show notes').first);
    await tester.pumpAndSettle();
    expect(find.text('Publisher description'), findsOneWidget);
    await Scrollable.ensureVisible(tester.element(find.text('Download').first),
        alignment: 0.5);
    await tester.pumpAndSettle();
    expect(find.text('Download').first.hitTestable(), findsOneWidget);
    await tester.tap(find.text('Show notes').first);
    await tester.pumpAndSettle();
    await Scrollable.ensureVisible(
        tester.element(find.byTooltip('More episode actions').first),
        alignment: 0.5);
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('More episode actions').first);
    await tester.pumpAndSettle();
    expect(find.text('Mark completed'), findsOneWidget);
    expect(find.text('Delete episode'), findsOneWidget);
    Navigator.of(tester.element(find.text('Mark completed')),
            rootNavigator: true)
        .pop();
    await tester.pumpAndSettle();
    await tester.tap(find.text('List'));
    await tester.pumpAndSettle();
    expect(find.byType(Card), findsNothing);
    expect(find.text('Episode 0'), findsOneWidget);
    expect(repository.queries.every((query) => query.$2 == 'uncompleted'), isTrue);
    await tester.tap(find.text('Grid'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Podcasts'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('My podcast'));
    await tester.pumpAndSettle();
    expect(find.byType(Card), findsNWidgets(4));
    expect(tester.takeException(), isNull);
  });

  for (final grid in [false, true]) {
    testWidgets('completion filters retain episodes and undo in ${grid ? 'grid' : 'list'}',
        (tester) async {
      final repository = BrowsingRepository();
      final store = BrowsingStore(repository);
      await tester.pumpWidget(browsingApp(
          const LocalPodcastEpisodesScreen(), repository, store: store));
      await tester.pumpAndSettle();
      expect(repository.queries.last.$2, 'uncompleted');
      if (grid) {
        await tester.tap(find.text('Grid'));
        await tester.pumpAndSettle();
      }
      await tester.tap(find.byTooltip('More episode actions'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Mark completed'));
      await tester.pumpAndSettle();
      expect(find.text('Episode 0'), findsNothing);
      expect(store.saves.single['completed'], isTrue);
      await tester.tap(find.text('Completed'));
      await tester.pumpAndSettle();
      expect(find.text('Episode 0'), findsOneWidget);
      await tester.tap(find.text('All'));
      await tester.pumpAndSettle();
      expect(find.text('Episode 0'), findsOneWidget);
      await tester.tap(find.byTooltip('More episode actions'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Mark uncompleted'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Completed'));
      await tester.pumpAndSettle();
      expect(find.text('Episode 0'), findsNothing);
      await tester.tap(find.text('Uncompleted'));
      await tester.pumpAndSettle();
      expect(find.text('Episode 0'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('grid grows for long titles and enlarged text', (tester) async {
    final repository = BrowsingRepository()..count = 3;
    repository.metadata = {
      'title': 'A very long episode title ${'with readable words ' * 12}',
      'description': 'Detailed show notes ${'for this episode. ' * 20}',
    };
    for (final size in [const Size(390, 844), const Size(844, 390)]) {
      await tester.binding.setSurfaceSize(size);
      await tester.pumpWidget(browsingApp(
          MediaQuery(
              data: MediaQueryData(
                  size: size, textScaler: const TextScaler.linear(1.6)),
              child: const LocalPodcastEpisodesScreen()),
          repository));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Grid'));
      await tester.pumpAndSettle();
      expect(find.byType(Card), findsNWidgets(3));
      if (size.width > 600) {
        expect(tester.getTopLeft(find.byType(Card).at(0)).dx,
            lessThan(tester.getTopLeft(find.byType(Card).at(1)).dx));
      }
      await Scrollable.ensureVisible(
          tester.element(find.text('Show notes').first),
          alignment: 0.5);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Show notes').first);
      await tester.pumpAndSettle();
      expect(find.textContaining('Detailed show notes'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    }
    await tester.binding.setSurfaceSize(null);
  });
}
